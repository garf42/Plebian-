package api

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/coder/websocket"

	"plebian/server/store"
	"plebian/server/underlay"
)

// Projections are pure functions of the store: the owner's time zone comes from the
// store, never from the host, and nothing reads the wall clock except /v1/today, which
// only chooses which day to project.

// Location is the owner's time zone, set at install.
func Location(ctx context.Context, s *store.Store) *time.Location {
	if loc, err := time.LoadLocation(s.Setting(ctx, "tz")); err == nil {
		return loc
	}
	return time.UTC
}

// DayCut is the hour a page starts (PL-03).
const DayCut = 4 * time.Hour

// DayOf is the page a moment belongs to.
func DayOf(t time.Time, loc *time.Location) string {
	return t.In(loc).Add(-DayCut).Format("2006-01-02")
}

// DayRange is a page's [start, end) in UTC.
func DayRange(date string, loc *time.Location) (time.Time, time.Time, error) {
	d, err := time.ParseInLocation("2006-01-02", date, loc)
	if err != nil {
		return time.Time{}, time.Time{}, err
	}
	start := time.Date(d.Year(), d.Month(), d.Day(), 4, 0, 0, 0, loc)
	end := time.Date(d.Year(), d.Month(), d.Day()+1, 4, 0, 0, 0, loc)
	return start.UTC(), end.UTC(), nil
}

func rfc(t time.Time) string { return t.UTC().Format(time.RFC3339Nano) }

type eventOut struct {
	ID   string          `json:"id"`
	At   string          `json:"at"`
	Type string          `json:"type"`
	Body json.RawMessage `json:"-"`
}

// flatten merges an event's body into its envelope: {"id","at","type",...body}.
func flatten(e store.Event) map[string]any {
	m := map[string]any{}
	json.Unmarshal(e.Body, &m)
	m["id"] = strconv.FormatInt(e.ID, 10)
	m["at"] = e.At
	m["type"] = e.Type
	return m
}

var threadTypes = map[string]bool{"message": true, "draft": true, "question": true, "step": true}

func (a *Server) threadEvents(ctx context.Context, where string, args ...any) ([]map[string]any, error) {
	rows, err := a.S.DB.QueryContext(ctx, `SELECT id, at, type, body, COALESCE(turn,0) FROM events WHERE type IN ('message','draft','question','step') AND `+where+` ORDER BY id`, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []map[string]any{}
	for rows.Next() {
		var e store.Event
		var body string
		if err := rows.Scan(&e.ID, &e.At, &e.Type, &body, &e.Turn); err != nil {
			return nil, err
		}
		e.Body = json.RawMessage(body)
		out = append(out, flatten(e))
	}
	return out, rows.Err()
}

func (a *Server) thread(w http.ResponseWriter, r *http.Request, _ string) {
	limit := 100
	if v, err := strconv.Atoi(r.URL.Query().Get("limit")); err == nil && v > 0 && v <= 1000 {
		limit = v
	}
	before := int64(1 << 62)
	if v, err := strconv.ParseInt(r.URL.Query().Get("before"), 10, 64); err == nil {
		before = v
	}
	evs, err := a.threadEvents(r.Context(), `id IN (SELECT id FROM events WHERE id < ? AND type IN ('message','draft','question','step') ORDER BY id DESC LIMIT ?)`, before, limit)
	if err != nil {
		http.Error(w, "", http.StatusInternalServerError)
		return
	}
	writeJSON(w, map[string]any{"events": evs})
}

// events streams the event log: the backlog after ?after=, then live.
func (a *Server) events(w http.ResponseWriter, r *http.Request, _ string) {
	after, _ := strconv.ParseInt(r.URL.Query().Get("after"), 10, 64)
	c, err := websocket.Accept(w, r, &websocket.AcceptOptions{InsecureSkipVerify: true})
	if err != nil {
		return
	}
	defer c.CloseNow()
	ctx := c.CloseRead(r.Context())
	sub, cancel := a.S.Hub.Subscribe()
	defer cancel()
	for {
		evs, err := a.S.EventsAfter(ctx, after, 500)
		if err != nil {
			return
		}
		for _, e := range evs {
			b, _ := json.Marshal(flatten(e))
			wctx, wcancel := context.WithTimeout(ctx, 30*time.Second)
			err := c.Write(wctx, websocket.MessageText, b)
			wcancel()
			if err != nil {
				return
			}
			after = e.ID
		}
		if len(evs) == 500 {
			continue
		}
		select {
		case <-ctx.Done():
			return
		case <-sub:
		}
	}
}

// HeardOut is a heard row as the API shows it.
type HeardOut struct {
	ID      string          `json:"id"`
	Source  string          `json:"source"`
	At      string          `json:"at"`
	End     string          `json:"end"`
	Lines   []underlay.Line `json:"lines"`
	Settled bool            `json:"settled"`
	Blob    string          `json:"blob"`
}

func (a *Server) heardIn(ctx context.Context, start, end time.Time) ([]HeardOut, error) {
	rows, err := a.S.DB.QueryContext(ctx, `SELECT id, at, end_at, blob, settled FROM heard WHERE at >= ? AND at < ? AND id NOT IN (SELECT covers FROM heard WHERE covers IS NOT NULL) ORDER BY at, id`, rfc(start), rfc(end))
	if err != nil {
		return nil, err
	}
	type rr struct {
		id            int64
		at, end, blob string
		settled       bool
	}
	var rs []rr
	for rows.Next() {
		var x rr
		rows.Scan(&x.id, &x.at, &x.end, &x.blob, &x.settled)
		rs = append(rs, x)
	}
	rows.Close()
	out := []HeardOut{}
	for _, x := range rs {
		b, err := a.S.HeardBytes(ctx, x.id)
		if err != nil {
			return nil, err
		}
		row, err := underlay.Parse(b)
		if err != nil {
			return nil, err
		}
		out = append(out, HeardOut{ID: strconv.FormatInt(x.id, 10), Source: row.Source, At: x.at, End: x.end, Lines: row.Lines, Settled: x.settled, Blob: x.blob})
	}
	return out, nil
}

type TapOut struct {
	ID        string    `json:"id"`
	At        string    `json:"at"`
	How       string    `json:"how"`
	Title     string    `json:"title"`
	Note      string    `json:"note"`
	StretchID *string   `json:"stretch_id"`
	Stretch   *HeardOut `json:"stretch,omitempty"`
}

func (a *Server) tapsWhere(ctx context.Context, where string, args ...any) ([]TapOut, error) {
	rows, err := a.S.DB.QueryContext(ctx, `SELECT id, at, how, title, note, stretch FROM taps WHERE `+where, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []TapOut{}
	for rows.Next() {
		var t TapOut
		var id int64
		var st *int64
		if err := rows.Scan(&id, &t.At, &t.How, &t.Title, &t.Note, &st); err != nil {
			return nil, err
		}
		t.ID = strconv.FormatInt(id, 10)
		if st != nil {
			s := strconv.FormatInt(*st, 10)
			t.StretchID = &s
		}
		out = append(out, t)
	}
	return out, rows.Err()
}

// Page renders a day's page as the API serves it (the update replay compares these).
func (a *Server) Page(ctx context.Context, date string) (map[string]any, error) {
	return a.page(ctx, date)
}

func (a *Server) page(ctx context.Context, date string) (map[string]any, error) {
	loc := Location(ctx, a.S)
	start, end, err := DayRange(date, loc)
	if err != nil {
		return nil, err
	}
	thread, err := a.threadEvents(ctx, `at >= ? AND at < ?`, rfc(start), rfc(end))
	if err != nil {
		return nil, err
	}
	heard, err := a.heardIn(ctx, start, end)
	if err != nil {
		return nil, err
	}
	taps, err := a.tapsWhere(ctx, `at >= ? AND at < ? ORDER BY at`, rfc(start), rfc(end))
	if err != nil {
		return nil, err
	}
	hours := make([]map[string]any, 24)
	secs := make([]float64, 24)
	rows, err := a.S.DB.QueryContext(ctx, `SELECT minute, seconds FROM speech WHERE minute >= ? AND minute < ?`, rfc(start), rfc(end))
	if err == nil {
		for rows.Next() {
			var m string
			var s float64
			rows.Scan(&m, &s)
			if t, err := time.Parse(time.RFC3339Nano, m); err == nil {
				secs[t.In(loc).Hour()] += s
			}
		}
		rows.Close()
	}
	for h := range hours {
		hours[h] = map[string]any{"hour": h, "seconds": secs[h]}
	}
	tapTimes := []string{}
	for _, t := range taps {
		tapTimes = append(tapTimes, t.At)
	}
	entry, err := a.entry(ctx, start, end)
	if err != nil {
		return nil, err
	}
	return map[string]any{"date": date, "cut": rfc(start), "entry": entry, "thread": thread, "taps": taps, "heard": heard,
		"ribbon": map[string]any{"hours": hours, "taps": tapTimes}}, nil
}

// entry is the night's read lines stamped that day (n.loop writes them as entry events).
func (a *Server) entry(ctx context.Context, start, end time.Time) ([]map[string]any, error) {
	rows, err := a.S.DB.QueryContext(ctx, `SELECT id, body FROM events WHERE type='entry' AND at >= ? AND at < ? ORDER BY id`, rfc(start), rfc(end))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []map[string]any{}
	for rows.Next() {
		var id int64
		var body string
		rows.Scan(&id, &body)
		var lines []map[string]any
		json.Unmarshal([]byte(body), &lines)
		for i, l := range lines {
			l["id"] = fmt.Sprintf("%d.%d", id, i)
			if _, ok := l["heard_by"]; !ok {
				l["heard_by"] = nil
			}
			out = append(out, l)
		}
	}
	rows.Close()
	// A line is held until the owner answers it: a message quoting it settles it.
	for _, l := range out {
		var n int
		a.S.DB.QueryRowContext(ctx, `SELECT COUNT(*) FROM owner_messages WHERE quote != '' AND json_extract(quote, '$.kind')='line' AND json_extract(quote, '$.id')=?`, l["id"]).Scan(&n)
		l["held"] = n == 0
	}
	return out, nil
}

func (a *Server) day(w http.ResponseWriter, r *http.Request, _ string) {
	p, err := a.page(r.Context(), r.PathValue("date"))
	if err != nil {
		http.Error(w, "bad date", http.StatusBadRequest)
		return
	}
	writeJSON(w, p)
}

func (a *Server) today(w http.ResponseWriter, r *http.Request, _ string) {
	p, err := a.page(r.Context(), DayOf(a.Clock.Now(), Location(r.Context(), a.S)))
	if err != nil {
		http.Error(w, "", http.StatusInternalServerError)
		return
	}
	writeJSON(w, p)
}

func (a *Server) days(w http.ResponseWriter, r *http.Request, _ string) {
	ctx := r.Context()
	loc := Location(ctx, a.S)
	counts := map[string]map[string]int{}
	bump := func(at, k string) {
		t, err := time.Parse(time.RFC3339Nano, at)
		if err != nil {
			return
		}
		d := DayOf(t, loc)
		if counts[d] == nil {
			counts[d] = map[string]int{"heard": 0, "taps": 0, "messages": 0, "open": 0}
		}
		counts[d][k]++
	}
	for _, q := range []struct{ sql, k string }{
		{`SELECT at FROM heard`, "heard"}, {`SELECT at FROM taps`, "taps"},
		{`SELECT at FROM events WHERE type='message'`, "messages"},
		{`SELECT shown_at FROM drafts WHERE state='waiting'`, "open"},
	} {
		rows, err := a.S.DB.QueryContext(ctx, q.sql)
		if err != nil {
			continue
		}
		for rows.Next() {
			var at string
			rows.Scan(&at)
			bump(at, q.k)
		}
		rows.Close()
	}
	var ds []string
	for d := range counts {
		if b := r.URL.Query().Get("before"); b == "" || d < b {
			ds = append(ds, d)
		}
	}
	sort.Sort(sort.Reverse(sort.StringSlice(ds)))
	if v, err := strconv.Atoi(r.URL.Query().Get("limit")); err == nil && v > 0 && v < len(ds) {
		ds = ds[:v]
	}
	out := []map[string]any{}
	for _, d := range ds {
		c := counts[d]
		out = append(out, map[string]any{"date": d, "heard": c["heard"], "taps": c["taps"], "messages": c["messages"], "open": c["open"]})
	}
	writeJSON(w, map[string]any{"days": out})
}

func (a *Server) taps(w http.ResponseWriter, r *http.Request, _ string) {
	limit := 100
	if v, err := strconv.Atoi(r.URL.Query().Get("limit")); err == nil && v > 0 && v <= 1000 {
		limit = v
	}
	before := int64(1 << 62)
	if v, err := strconv.ParseInt(r.URL.Query().Get("before"), 10, 64); err == nil {
		before = v
	}
	ts, err := a.tapsWhere(r.Context(), `id < ? ORDER BY at DESC, id DESC LIMIT ?`, before, limit)
	if err != nil {
		http.Error(w, "", http.StatusInternalServerError)
		return
	}
	writeJSON(w, map[string]any{"taps": ts})
}

func (a *Server) tap(w http.ResponseWriter, r *http.Request, _ string) {
	ts, err := a.tapsWhere(r.Context(), `id = ?`, r.PathValue("id"))
	if err != nil || len(ts) == 0 {
		http.NotFound(w, r)
		return
	}
	t := ts[0]
	if t.StretchID != nil {
		var at string
		a.S.DB.QueryRowContext(r.Context(), `SELECT at FROM heard WHERE id=?`, *t.StretchID).Scan(&at)
		if st, err := time.Parse(time.RFC3339Nano, at); err == nil {
			hs, _ := a.heardIn(r.Context(), st, st.Add(time.Nanosecond))
			for i := range hs {
				if hs[i].ID == *t.StretchID {
					t.Stretch = &hs[i]
				}
			}
		}
	}
	writeJSON(w, t)
}

func (a *Server) postTap(w http.ResponseWriter, r *http.Request, _ string) {
	var t struct {
		At   string `json:"at"`
		How  string `json:"how"`
		Line *struct {
			Kind string `json:"kind"`
			ID   string `json:"id"`
		} `json:"line"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, maxBody)).Decode(&t); err != nil || (t.How != "omi" && t.How != "app") {
		http.Error(w, "bad tap", http.StatusBadRequest)
		return
	}
	at, err := time.Parse(time.RFC3339Nano, t.At)
	if err != nil {
		http.Error(w, "bad time", http.StatusBadRequest)
		return
	}
	line := ""
	if t.Line != nil {
		b, _ := json.Marshal(t.Line)
		line = string(b)
	}
	ctx := r.Context()
	res, err := a.S.DB.ExecContext(ctx, `INSERT INTO taps(at, how, line) VALUES(?,?,?)`, rfc(at), t.How, line)
	if err != nil {
		http.Error(w, "", http.StatusInternalServerError)
		return
	}
	id, _ := res.LastInsertId()
	ts, _ := a.tapsWhere(ctx, `id = ?`, id)
	if len(ts) == 1 {
		a.S.Emit(ctx, a.now(), "tap", ts[0], 0)
		writeJSON(w, ts[0])
	}
}

func (a *Server) patchTap(w http.ResponseWriter, r *http.Request, _ string) {
	var t struct {
		Note string `json:"note"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, maxBody)).Decode(&t); err != nil {
		http.Error(w, "", http.StatusBadRequest)
		return
	}
	a.S.DB.ExecContext(r.Context(), `UPDATE taps SET note=? WHERE id=?`, t.Note, r.PathValue("id"))
	ts, err := a.tapsWhere(r.Context(), `id = ?`, r.PathValue("id"))
	if err != nil || len(ts) == 0 {
		http.NotFound(w, r)
		return
	}
	writeJSON(w, ts[0])
}

func (a *Server) open(w http.ResponseWriter, r *http.Request, _ string) {
	ctx := r.Context()
	items := func(q string) []map[string]any {
		out := []map[string]any{}
		rows, err := a.S.DB.QueryContext(ctx, q)
		if err != nil {
			return out
		}
		defer rows.Close()
		for rows.Next() {
			var id int64
			var kind, text, at string
			rows.Scan(&id, &kind, &text, &at)
			m := map[string]any{"id": strconv.FormatInt(id, 10), "kind": kind, "text": text, "at": at}
			if kind == "draft" {
				m["shown"] = text
			}
			out = append(out, m)
		}
		return out
	}
	writeJSON(w, map[string]any{
		"questions": items(`SELECT id, 'question', text, (SELECT at FROM turns WHERE id=opened_turn) FROM loops WHERE kind='question' AND closed_turn IS NULL ORDER BY id`),
		"drafts":    items(`SELECT id, 'draft', shown, shown_at FROM drafts WHERE state='waiting' ORDER BY id`),
		"orders":    items(`SELECT id, 'order', text, (SELECT at FROM turns WHERE id=opened_turn) FROM loops WHERE kind='order' AND closed_turn IS NULL ORDER BY id`),
	})
}

// search counts per body. shortcut: scans every row; the lamp (n.recall/n.underlay)
// replaces it with the label index once built.
func (a *Server) search(w http.ResponseWriter, r *http.Request, _ string) {
	q := strings.ToLower(strings.TrimSpace(r.URL.Query().Get("q")))
	ctx := r.Context()
	type body struct {
		Count int              `json:"count"`
		First string           `json:"first"`
		Last  string           `json:"last"`
		Hits  []map[string]any `json:"hits"`
	}
	th, he := body{Hits: []map[string]any{}}, body{Hits: []map[string]any{}}
	note := func(b *body, hit map[string]any) {
		b.Count++
		at := hit["at"].(string)
		if b.First == "" || at < b.First {
			b.First = at
		}
		if at > b.Last {
			b.Last = at
		}
		b.Hits = append(b.Hits, hit)
	}
	if q != "" {
		evs, _ := a.threadEvents(ctx, `type='message'`)
		for _, e := range evs {
			if t, _ := e["text"].(string); strings.Contains(strings.ToLower(t), q) {
				note(&th, map[string]any{"id": e["id"], "kind": "message", "text": t, "at": e["at"], "who": e["from"]})
			}
		}
		hs, _ := a.heardIn(ctx, time.Unix(0, 0), time.Unix(1<<40, 0))
		for _, h := range hs {
			for _, l := range h.Lines {
				if strings.Contains(strings.ToLower(l.Text), q) {
					note(&he, map[string]any{"id": h.ID, "kind": h.Source, "text": l.Text, "at": h.At, "who": l.Speaker})
				}
			}
		}
	}
	writeJSON(w, map[string]any{"thread": th, "heard": he})
}
