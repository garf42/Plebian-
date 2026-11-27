package feeds

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/emersion/go-ical"

	"plebian/server/config"
	"plebian/server/store"
	"plebian/server/underlay"
	"plebian/server/watch"
)

const calendarSecret = "calendar" // a secret iCal address is a credential

// Calendar is the calendar feed: an iCalendar URL fetched conditionally (ETag /
// Last-Modified) at half the owner's usual time to answer. An entry lands when written,
// again when edited (the newer row covers it), and again as each instance comes due,
// early by the owner's usual time to answer, so a due entry can't slip past one stop
// window.
type Calendar struct {
	L       *Lander
	Watch   *watch.Watch
	Secrets *config.Secrets
	HTTP    *http.Client
	Usual   func(ctx context.Context) time.Duration
	Loc     func(ctx context.Context) *time.Location // floating times are the owner's
	Log     *log.Logger

	once sync.Once
	kick chan struct{}
	mu   sync.Mutex
	body []byte // the last feed fetched, for due landings between fetches
}

func (c *Calendar) init() { c.once.Do(func() { c.kick = make(chan struct{}, 1) }) }

// Connect checks the URL with one fetch, keeps it and restarts the feed with it.
func (c *Calendar) Connect(ctx context.Context, url string) error {
	c.init()
	if !strings.HasPrefix(url, "https://") && !strings.HasPrefix(url, "http://") && !strings.HasPrefix(url, "webcal://") {
		return fmt.Errorf("not an iCalendar URL")
	}
	url = strings.Replace(url, "webcal://", "https://", 1)
	body, _, err := c.fetch(ctx, url, calCursor{})
	if err != nil {
		return err
	}
	if _, err := ical.NewDecoder(bytes.NewReader(body)).Decode(); err != nil {
		return fmt.Errorf("not an iCalendar feed: %w", err)
	}
	if err := c.Secrets.Set(calendarSecret, url); err != nil {
		return err
	}
	setCursor(ctx, c.L.S, "calendar", "{}")
	select {
	case c.kick <- struct{}{}:
	default:
	}
	return nil
}

type calCursor struct {
	ETag         string `json:"etag"`
	LastModified string `json:"last_modified"`
}

func (c *Calendar) fetch(ctx context.Context, url string, cur calCursor) ([]byte, calCursor, error) {
	req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err != nil {
		return nil, cur, err
	}
	if cur.ETag != "" {
		req.Header.Set("If-None-Match", cur.ETag)
	}
	if cur.LastModified != "" {
		req.Header.Set("If-Modified-Since", cur.LastModified)
	}
	resp, err := c.HTTP.Do(req)
	if err != nil {
		return nil, cur, err
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusNotModified {
		return nil, cur, nil
	}
	if resp.StatusCode != 200 {
		return nil, cur, fmt.Errorf("calendar feed answered %d", resp.StatusCode)
	}
	b, err := store.ReadAllLimit(resp.Body, 64<<20)
	if err != nil {
		return nil, cur, err
	}
	io.Copy(io.Discard, resp.Body)
	return b, calCursor{ETag: resp.Header.Get("ETag"), LastModified: resp.Header.Get("Last-Modified")}, nil
}

// Run fetches and lands until ctx ends.
func (c *Calendar) Run(ctx context.Context) {
	c.init()
	for ctx.Err() == nil {
		url := c.Secrets.Get(calendarSecret)
		if url == "" {
			select {
			case <-ctx.Done():
				return
			case <-c.kick:
			}
			continue
		}
		if err := c.Tick(ctx, url); err != nil && c.Log != nil {
			c.Log.Printf("calendar: %v", err)
		}
		select {
		case <-ctx.Done():
			return
		case <-c.kick:
		case <-c.L.Clock.After(c.L.Clock.Now().Add(c.Usual(ctx) / 2)):
		}
	}
}

// Tick is one fetch: what changed lands, then whatever comes due lands.
func (c *Calendar) Tick(ctx context.Context, url string) error {
	var cur calCursor
	json.Unmarshal([]byte(cursor(ctx, c.L.S, "calendar")), &cur)
	body, next, err := c.fetch(ctx, url, cur)
	if err != nil {
		return err
	}
	if body != nil {
		c.mu.Lock()
		c.body = body
		c.mu.Unlock()
		if err := c.written(ctx, body); err != nil {
			return err
		}
		b, _ := json.Marshal(next)
		setCursor(ctx, c.L.S, "calendar", string(b))
	}
	c.mu.Lock()
	body = c.body
	c.mu.Unlock()
	if body == nil {
		// After a restart the last feed is the newest calendar original landed.
		var sha string
		c.L.S.DB.QueryRowContext(ctx, `SELECT blob FROM heard WHERE source='calendar' ORDER BY id DESC LIMIT 1`).Scan(&sha)
		if sha != "" {
			body, _ = c.L.S.Blobs.Get(ctx, sha)
		}
	}
	if body == nil {
		return nil
	}
	return c.due(ctx, body)
}

type entry struct {
	key     string // UID and RECURRENCE-ID
	start   time.Time
	end     time.Time
	ev      ical.Event
	allDay  bool
	text    string
	speaker string
}

func (c *Calendar) entries(ctx context.Context, body []byte) ([]entry, error) {
	cal, err := ical.NewDecoder(bytes.NewReader(body)).Decode()
	if err != nil {
		return nil, err
	}
	loc := c.Loc(ctx)
	var out []entry
	for _, ev := range cal.Events() {
		uid := ev.Props.Get(ical.PropUID)
		if uid == nil {
			continue
		}
		key := uid.Value
		if rid := ev.Props.Get(ical.PropRecurrenceID); rid != nil {
			key += "/" + rid.Value
		}
		start, err := ev.DateTimeStart(loc)
		if err != nil {
			continue
		}
		end, err := ev.DateTimeEnd(loc)
		if err != nil || end.IsZero() {
			end = start
		}
		ds := ev.Props.Get(ical.PropDateTimeStart)
		allDay := ds != nil && (ds.ValueType() == ical.ValueDate || len(ds.Value) == len("20060102"))
		e := entry{key: key, start: start, end: end, ev: ev, allDay: allDay, speaker: "calendar"}
		if o := ev.Props.Get(ical.PropOrganizer); o != nil {
			e.speaker = strings.TrimPrefix(strings.TrimPrefix(o.Value, "mailto:"), "MAILTO:")
			if cn := o.Params.Get(ical.ParamCommonName); cn != "" {
				e.speaker = cn + " <" + e.speaker + ">"
			}
		}
		e.text = describe(ev, start, end, allDay, loc)
		out = append(out, e)
	}
	return out, nil
}

func text(ev ical.Event, name string) string {
	if p := ev.Props.Get(name); p != nil {
		if s, err := p.Text(); err == nil {
			return s
		}
		return p.Value
	}
	return ""
}

func describe(ev ical.Event, start, end time.Time, allDay bool, loc *time.Location) string {
	var b strings.Builder
	b.WriteString(text(ev, ical.PropSummary) + "\n")
	if allDay {
		fmt.Fprintf(&b, "when: %s (all day)\n", start.Format("2006-01-02"))
	} else {
		fmt.Fprintf(&b, "when: %s – %s\n", start.In(loc).Format(time.RFC3339), end.In(loc).Format(time.RFC3339))
	}
	if s := text(ev, ical.PropLocation); s != "" {
		fmt.Fprintf(&b, "where: %s\n", s)
	}
	if r := ev.Props.Get(ical.PropRecurrenceRule); r != nil {
		fmt.Fprintf(&b, "repeats: %s\n", r.Value)
	}
	if s, _ := ev.Status(); s != "" {
		fmt.Fprintf(&b, "status: %s\n", strings.ToLower(string(s)))
	}
	if s := text(ev, ical.PropDescription); s != "" {
		b.WriteString(s + "\n")
	}
	return strings.TrimRight(b.String(), "\n")
}

// written lands each entry as written; an edit lands as a newer row covering the old.
func (c *Calendar) written(ctx context.Context, body []byte) error {
	es, err := c.entries(ctx, body)
	if err != nil {
		return err
	}
	for _, e := range es {
		row := underlay.Row{Source: "calendar", Lines: []underlay.Line{{Speaker: e.speaker, At: e.start.UTC().Format(time.RFC3339Nano), Text: e.text}}}
		if _, _, err := c.L.Put(ctx, c.Watch, "calendar", "entry/"+e.key, e.start, e.end, row, body); err != nil {
			return err
		}
	}
	return nil
}

// due lands every instance whose start is within the owner's usual time to answer.
func (c *Calendar) due(ctx context.Context, body []byte) error {
	es, err := c.entries(ctx, body)
	if err != nil {
		return err
	}
	now := c.L.Clock.Now()
	until := now.Add(c.Usual(ctx))
	loc := c.Loc(ctx)
	for _, e := range es {
		if s, _ := e.ev.Status(); s == ical.EventCancelled {
			continue
		}
		starts := []time.Time{e.start}
		if rs, err := e.ev.RecurrenceSet(loc); err == nil && rs != nil {
			starts = rs.Between(now, until, true)
		}
		for _, st := range starts {
			if st.Before(now) || st.After(until) {
				continue
			}
			en := st.Add(e.end.Sub(e.start))
			row := underlay.Row{Source: "calendar", Lines: []underlay.Line{{Speaker: e.speaker, At: st.UTC().Format(time.RFC3339Nano),
				Text: "due: " + describe(e.ev, st, en, e.allDay, loc)}}}
			if _, _, err := c.L.Put(ctx, c.Watch, "calendar", "due/"+e.key+"/"+st.UTC().Format(time.RFC3339), st, en, row, body); err != nil {
				return err
			}
		}
	}
	return nil
}
