// Package ical serves one iCalendar feed with ETag and Last-Modified and
// answers conditional GETs with 304, counting every fetch.
package ical

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"io"
	"net/http"
	"sync"
	"time"

	"plebian/tests/fixtures"
)

// Empty is the feed served until a body is set: a calendar with no events.
const Empty = "BEGIN:VCALENDAR\r\nVERSION:2.0\r\nPRODID:-//Plebian//Fixture//EN\r\nEND:VCALENDAR\r\n"

// Stats counts feed fetches by answer.
type Stats struct {
	Fetches     int `json:"fetches"`
	OK          int `json:"ok"`
	NotModified int `json:"not_modified"`
}

// Feed is the fixture's state.
type Feed struct {
	mu       sync.Mutex
	body     []byte
	modified time.Time
	stats    Stats
	log      *fixtures.Log
}

// New returns a feed serving Empty.
func New() *Feed {
	f := &Feed{log: fixtures.NewLog()}
	f.Set([]byte(Empty))
	return f
}

// Set replaces the feed; Last-Modified becomes now (whole seconds, as HTTP dates are).
func (f *Feed) Set(body []byte) {
	f.mu.Lock()
	f.body, f.modified = body, time.Now().UTC().Truncate(time.Second)
	f.mu.Unlock()
}

// Stats returns the counts so far.
func (f *Feed) Stats() Stats {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.stats
}

type status struct {
	http.ResponseWriter
	code int
}

func (s *status) WriteHeader(c int) { s.code = c; s.ResponseWriter.WriteHeader(c) }

// Handler serves GET /calendar.ics and the control API.
func (f *Feed) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.Handle("GET /calendar.ics", f.log.Recording(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		f.mu.Lock()
		body, mod := f.body, f.modified
		f.mu.Unlock()
		sum := sha256.Sum256(body)
		w.Header().Set("ETag", `"`+hex.EncodeToString(sum[:])+`"`)
		w.Header().Set("Content-Type", "text/calendar; charset=utf-8")
		st := &status{ResponseWriter: w, code: 200}
		// ServeContent applies RFC 9110 conditionals: If-None-Match first, else If-Modified-Since.
		http.ServeContent(st, r, "calendar.ics", mod, bytes.NewReader(body))
		f.mu.Lock()
		f.stats.Fetches++
		switch st.code {
		case 200:
			f.stats.OK++
		case 304:
			f.stats.NotModified++
		}
		f.mu.Unlock()
	})))
	mux.HandleFunc("POST /control/body", func(w http.ResponseWriter, r *http.Request) {
		b, err := io.ReadAll(r.Body)
		if err != nil || len(b) == 0 {
			fixtures.WriteJSON(w, 400, map[string]string{"error": "the request body is the feed"})
			return
		}
		f.Set(b)
		fixtures.WriteJSON(w, 200, map[string]int{"bytes": len(b)})
	})
	mux.HandleFunc("GET /control/stats", func(w http.ResponseWriter, r *http.Request) {
		fixtures.WriteJSON(w, 200, f.Stats())
	})
	mux.HandleFunc("GET /control/requests", func(w http.ResponseWriter, r *http.Request) {
		fixtures.WriteJSON(w, 200, f.log.List())
	})
	return mux
}
