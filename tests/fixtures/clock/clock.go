// Package clock is a controllable clock: set, advance, and optionally run
// with real time from the set point.
package clock

import (
	"net/http"
	"sync"
	"time"

	"plebian/tests/fixtures"
)

// Clock is the fixture's state.
type Clock struct {
	mu      sync.Mutex
	baseMs  int64     // the clock's reading at since
	since   time.Time // real time of the last set or advance
	running bool
}

// New returns a clock reading real time, running.
func New() *Clock { return &Clock{baseMs: time.Now().UnixMilli(), since: time.Now(), running: true} }

// Now returns the clock's reading in unix milliseconds.
func (c *Clock) Now() int64 {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.nowLocked()
}

func (c *Clock) nowLocked() int64 {
	if c.running {
		return c.baseMs + time.Since(c.since).Milliseconds()
	}
	return c.baseMs
}

// Set reads ms from now on, advancing with real time when running.
func (c *Clock) Set(ms int64, running bool) {
	c.mu.Lock()
	c.baseMs, c.since, c.running = ms, time.Now(), running
	c.mu.Unlock()
}

// Advance moves the clock forward by ms.
func (c *Clock) Advance(ms int64) {
	c.mu.Lock()
	c.baseMs, c.since = c.nowLocked()+ms, time.Now()
	c.mu.Unlock()
}

// Handler serves GET /now and the control API.
func (c *Clock) Handler() http.Handler {
	mux := http.NewServeMux()
	now := func(w http.ResponseWriter, r *http.Request) {
		fixtures.WriteJSON(w, 200, map[string]int64{"unix_ms": c.Now()})
	}
	mux.HandleFunc("GET /now", now)
	mux.HandleFunc("POST /control/set", func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			UnixMs  *int64 `json:"unix_ms"`
			Running bool   `json:"running"`
		}
		if !fixtures.ReadJSON(w, r, &req) {
			return
		}
		if req.UnixMs == nil {
			fixtures.WriteJSON(w, 400, map[string]string{"error": "unix_ms is required"})
			return
		}
		c.Set(*req.UnixMs, req.Running)
		now(w, r)
	})
	mux.HandleFunc("POST /control/advance", func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			Ms int64 `json:"ms"`
		}
		if !fixtures.ReadJSON(w, r, &req) {
			return
		}
		if req.Ms < 0 {
			fixtures.WriteJSON(w, 400, map[string]string{"error": "ms must not be negative"})
			return
		}
		c.Advance(req.Ms)
		now(w, r)
	})
	return mux
}
