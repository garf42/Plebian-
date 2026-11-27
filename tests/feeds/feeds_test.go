package feeds_test

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"plebian/server/config"
	"plebian/server/feeds"
	"plebian/server/grammar"
	"plebian/server/store"
	"plebian/server/watch"
	"plebian/tests/fixtures/ical"
	fmail "plebian/tests/fixtures/mail"
	"plebian/tests/kit"
)

var ctx = context.Background()

type env struct {
	s      *store.Store
	clock  *kit.Clock
	sec    *config.Secrets
	l      *feeds.Lander
	landed []int64
}

func newEnv(t *testing.T) *env {
	dir := t.TempDir()
	s, err := store.Open(filepath.Join(dir, "store.db"), store.DirBlobs{Dir: filepath.Join(dir, "blobs")})
	if err != nil {
		t.Fatal(err)
	}
	g, err := grammar.Open(ctx, s)
	if err != nil {
		t.Fatal(err)
	}
	s.SetSpeller(g)
	e := &env{s: s, clock: kit.NewClock(time.Date(2026, 12, 3, 13, 0, 0, 0, time.UTC)), sec: &config.Secrets{Path: filepath.Join(dir, "secrets.json")}}
	e.l = &feeds.Lander{S: s, Clock: e.clock, Land: func(_ context.Context, id int64) { e.landed = append(e.landed, id) }}
	return e
}

func hour(context.Context) time.Duration { return time.Hour }

type row struct {
	ext, at string
	b       []byte
}

func rows(t *testing.T, s *store.Store, source string) []row {
	r, err := s.DB.Query(`SELECT id, ext, at FROM heard WHERE source=? AND id NOT IN (SELECT covers FROM heard WHERE covers IS NOT NULL) ORDER BY at, ext`, source)
	if err != nil {
		t.Fatal(err)
	}
	type idrow struct {
		id int64
		row
	}
	var ids []idrow
	for r.Next() {
		var x idrow
		r.Scan(&x.id, &x.ext, &x.at)
		ids = append(ids, x)
	}
	r.Close()
	var out []row
	for _, x := range ids {
		b, err := s.HeardBytes(ctx, x.id)
		if err != nil {
			t.Fatal(err)
		}
		x.b = b
		out = append(out, x.row)
	}
	return out
}

const cal = "BEGIN:VCALENDAR\r\nVERSION:2.0\r\nPRODID:-//Plebian//Fixture//EN\r\n" +
	"BEGIN:VEVENT\r\nUID:gravel-1@fixture\r\nDTSTAMP:20261120T090000Z\r\nDTSTART:20261203T133000Z\r\nDTEND:20261203T143000Z\r\nSUMMARY:Gravel delivery\r\nLOCATION:the culvert\r\nEND:VEVENT\r\n" +
	"BEGIN:VEVENT\r\nUID:standup@fixture\r\nDTSTAMP:20261120T090000Z\r\nDTSTART;TZID=America/Chicago:20261201T090000\r\nDTEND;TZID=America/Chicago:20261201T091500\r\nRRULE:FREQ=DAILY;COUNT=10\r\nSUMMARY:Crew call\r\nEND:VEVENT\r\n" +
	"END:VCALENDAR\r\n"

func calendar(e *env, url string) *feeds.Calendar {
	c := &feeds.Calendar{L: e.l, Secrets: e.sec, HTTP: httptest.NewServer(nil).Client(), Usual: hour,
		Loc: func(context.Context) *time.Location { l, _ := time.LoadLocation("America/Chicago"); return l }}
	return c
}

// D1: an entry lands when written and again as it comes due (early by the owner's usual
// time to answer); a recurring entry lands per instance; nothing lands twice.
func TestCalendarDueLandsAgain(t *testing.T) {
	e := newEnv(t)
	f := ical.New()
	f.Set([]byte(cal))
	srv := httptest.NewServer(f.Handler())
	defer srv.Close()
	c := calendar(e, srv.URL)
	c.HTTP = srv.Client()
	if err := c.Tick(ctx, srv.URL+"/calendar.ics"); err != nil {
		t.Fatal(err)
	}
	var written, due int
	for _, r := range rows(t, e.s, "calendar") {
		switch {
		case strings.HasPrefix(r.ext, "entry/"):
			written++
		case strings.HasPrefix(r.ext, "due/"):
			due++
		}
	}
	// 13:00 UTC: the gravel delivery at 13:30 is within the hour; the 09:00 Chicago crew
	// call (15:00 UTC) is not yet.
	if written != 2 || due != 1 {
		t.Fatalf("written %d, due %d; want 2 and 1", written, due)
	}
	e.clock.Advance(90 * time.Minute) // 14:30 UTC: the crew call at 15:00 comes due
	if err := c.Tick(ctx, srv.URL+"/calendar.ics"); err != nil {
		t.Fatal(err)
	}
	due = 0
	for _, r := range rows(t, e.s, "calendar") {
		if strings.HasPrefix(r.ext, "due/standup@fixture") {
			due++
			if !bytes.Contains(r.b, []byte("Crew call")) || r.at != "2026-12-03T15:00:00Z" {
				t.Fatalf("due row %q at %s", r.b, r.at)
			}
		}
	}
	if due != 1 {
		t.Fatalf("the crew call came due %d times", due)
	}
	n := len(e.landed)
	if err := c.Tick(ctx, srv.URL+"/calendar.ics"); err != nil { // MR-2: the same feed again lands nothing
		t.Fatal(err)
	}
	if len(e.landed) != n {
		t.Fatalf("an unchanged feed landed %d rows", len(e.landed)-n)
	}
	if st := f.Stats(); st.NotModified == 0 {
		t.Fatalf("the feed was not fetched conditionally: %+v", st)
	}
}

// An edit lands as a newer row covering the old one.
func TestCalendarEditCovers(t *testing.T) {
	e := newEnv(t)
	f := ical.New()
	f.Set([]byte(cal))
	srv := httptest.NewServer(f.Handler())
	defer srv.Close()
	c := calendar(e, srv.URL)
	c.HTTP = srv.Client()
	c.Tick(ctx, srv.URL+"/calendar.ics")
	f.Set([]byte(strings.Replace(cal, "the culvert", "the north gate", 1)))
	c.Tick(ctx, srv.URL+"/calendar.ics")
	var covered int
	e.s.DB.QueryRow(`SELECT COUNT(*) FROM heard WHERE source='calendar' AND covers IS NOT NULL AND ext LIKE 'entry/%'`).Scan(&covered)
	if covered != 1 {
		t.Fatalf("an edit covered %d rows", covered)
	}
}

// D3: the silence line is the feed's own longest quiet, never a fixed timeout.
func TestSilenceFromHistory(t *testing.T) {
	for _, gap := range []time.Duration{5 * time.Minute, 50 * time.Minute} {
		e := newEnv(t)
		var asked []string
		w := &watch.Watch{S: e.s, Clock: e.clock, Feed: "mail", Ask: func(_ context.Context, q string) error { asked = append(asked, q); return nil }}
		wctx, cancel := context.WithCancel(ctx)
		go w.Run(wctx)
		for i := 0; i < 3; i++ {
			w.Arrived(ctx, e.clock.Now())
			e.clock.Advance(gap)
		}
		w.Arrived(ctx, e.clock.Now())
		time.Sleep(50 * time.Millisecond)
		e.clock.Advance(gap + time.Minute) // a quiet one minute past the longest
		time.Sleep(100 * time.Millisecond)
		cancel()
		if len(asked) != 1 {
			t.Fatalf("history of %v quiets: asked %d times", gap, len(asked))
		}
	}
}

func message(from, subject, body string) string {
	return fmt.Sprintf("From: %s\r\nTo: owner@example.com\r\nSubject: %s\r\nMessage-ID: <%s@example.com>\r\nDate: Mon, 1 Jan 2001 00:00:00 +0000\r\n\r\n%s\r\n",
		from, subject, strings.ReplaceAll(subject, " ", "-"), body)
}

// MR-1: the same mailbox delivered in two orders lands identical rows at the same wall
// times (INTERNALDATE, not the Date header, which lies here).
func TestMailboxOrders(t *testing.T) {
	msgs := []struct {
		raw string
		at  time.Time
	}{
		{message("Dale <dale@quarry.example>", "Gravel Wednesday", "Fifteen yards, by the culvert."), time.Date(2026, 12, 1, 14, 0, 0, 0, time.UTC)},
		{message("Vendor <ap@vendor.example>", "Invoice 1042", "Please find the invoice attached."), time.Date(2026, 12, 2, 9, 30, 0, 0, time.UTC)},
	}
	var got [2][]row
	for k, order := range [][]int{{0, 1}, {1, 0}} {
		e := newEnv(t)
		m := fmail.New("owner@example.com", "pw")
		srv, err := m.Start("127.0.0.1:0", "127.0.0.1:0", "127.0.0.1:0", false)
		if err != nil {
			t.Fatal(err)
		}
		for _, i := range order {
			if _, err := m.Append("INBOX", []byte(msgs[i].raw), msgs[i].at); err != nil {
				t.Fatal(err)
			}
		}
		mf := &feeds.Mail{L: e.l, Secrets: e.sec, Usual: hour}
		if err := mf.Connect(ctx, feeds.Account{IMAP: srv.Info["imap"].(string), SMTP: srv.Info["smtp"].(string), User: "owner@example.com", Password: "pw"}); err != nil {
			t.Fatal(err)
		}
		rctx, cancel := context.WithCancel(ctx)
		done := make(chan struct{})
		go func() { mf.Run(rctx); close(done) }()
		deadline := time.Now().Add(10 * time.Second)
		for len(rows(t, e.s, "mail")) < 2 && time.Now().Before(deadline) {
			time.Sleep(50 * time.Millisecond)
		}
		cancel()
		<-done
		srv.Close()
		got[k] = rows(t, e.s, "mail")
		for i := range got[k] {
			got[k][i].ext = "" // UIDs differ with the order; rows and times must not
		}
	}
	if len(got[0]) != 2 || fmt.Sprint(got[0]) != fmt.Sprint(got[1]) {
		t.Fatalf("orders landed differently:\n%v\n%v", got[0], got[1])
	}
	if got[0][0].at != "2026-12-01T14:00:00Z" || !bytes.Contains(got[0][0].b, []byte("Dale%20<dale@quarry.example>")) {
		t.Fatalf("first row at %s: %q", got[0][0].at, got[0][0].b)
	}
}

// A mail draft is the complete message, Date and Message-ID included; sent, its bytes
// reach SMTP exactly.
func TestDraftIsTheMessage(t *testing.T) {
	e := newEnv(t)
	m := fmail.New("owner@example.com", "pw")
	srv, err := m.Start("127.0.0.1:0", "127.0.0.1:0", "127.0.0.1:0", false)
	if err != nil {
		t.Fatal(err)
	}
	defer srv.Close()
	mf := &feeds.Mail{L: e.l, Secrets: e.sec, Usual: hour}
	if err := mf.Connect(ctx, feeds.Account{IMAP: srv.Info["imap"].(string), SMTP: srv.Info["smtp"].(string), User: "owner@example.com", Password: "pw"}); err != nil {
		t.Fatal(err)
	}
	tool := feeds.SendTool{M: mf, Clock: e.clock}
	payload, shown, err := tool.Compose(ctx, json.RawMessage(`{"to":"Dale <dale@quarry.example>","subject":"Gravel","body":"Wednesday works."}`))
	if err != nil {
		t.Fatal(err)
	}
	for _, h := range []string{"Date: ", "Message-ID: <", "To: Dale <dale@quarry.example>\r\n"} {
		if !strings.Contains(shown, h) {
			t.Fatalf("the draft lacks %q:\n%s", h, shown)
		}
	}
	e.clock.Advance(time.Hour) // approved later: the Date shown is the Date sent
	if res, err := tool.Run(ctx, payload); err != nil || res.Exit != 0 {
		t.Fatalf("send: %v %s", err, res.Reading)
	}
	sent := m.SentMessages()
	if len(sent) != 1 || string(sent[0].Data) != shown {
		t.Fatalf("sent %d messages; bytes equal: %v", len(sent), len(sent) == 1 && string(sent[0].Data) == shown)
	}
}
