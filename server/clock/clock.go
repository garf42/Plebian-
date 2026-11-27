// Package clock is the server's one source of wall time. Wall time only addresses the
// underlay, cuts pages and times turns and stop windows (TP-T15); nothing fades by it.
// The source is a setting: the system clock, or a time endpoint (a fixture in tests).
package clock

import (
	"encoding/json"
	"net/http"
	"sync"
	"time"
)

type Clock interface {
	Now() time.Time
	// After fires once the clock reaches t.
	After(t time.Time) <-chan time.Time
}

type System struct{}

func (System) Now() time.Time { return time.Now().UTC() }
func (System) After(t time.Time) <-chan time.Time {
	return time.After(time.Until(t))
}

// URL reads time from an endpoint answering {"unix_ms": n}.
type URL struct {
	Endpoint string
	client   http.Client
	mu       sync.Mutex
	last     time.Time
}

func NewURL(endpoint string) *URL {
	return &URL{Endpoint: endpoint, client: http.Client{Timeout: 5 * time.Second}}
}

func (u *URL) Now() time.Time {
	resp, err := u.client.Get(u.Endpoint)
	if err == nil {
		defer resp.Body.Close()
		var v struct {
			UnixMs int64 `json:"unix_ms"`
		}
		if json.NewDecoder(resp.Body).Decode(&v) == nil {
			t := time.UnixMilli(v.UnixMs).UTC()
			u.mu.Lock()
			u.last = t
			u.mu.Unlock()
			return t
		}
	}
	u.mu.Lock()
	defer u.mu.Unlock()
	return u.last // the endpoint is a fixture on localhost; a failed read repeats the last time
}

// After polls the endpoint. shortcut: polls every 50 ms, which only test clocks use; a
// push from the time source would replace it if a non-test URL source ever exists.
func (u *URL) After(t time.Time) <-chan time.Time {
	ch := make(chan time.Time, 1)
	go func() {
		for {
			if n := u.Now(); !n.Before(t) {
				ch <- n
				return
			}
			time.Sleep(50 * time.Millisecond)
		}
	}()
	return ch
}

// New returns the system clock for "" and a URL clock otherwise.
func New(source string) Clock {
	if source == "" {
		return System{}
	}
	return NewURL(source)
}
