// Package browser is n.browser: one Chromium per box with a persistent profile, driven by
// the browse sub-agent through Playwright MCP over CDP. A guard on the same CDP endpoint
// lets reads through, drops beacons, and holds every other request an agent action sets
// off as a draft whose release sends exactly the held bytes, once. The owner can take
// the wheel in a live view; nothing the owner types is recorded.
package browser

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sync"

	"github.com/coder/websocket"
)

// CDP is a Chrome DevTools Protocol connection (flat sessions).
type CDP struct {
	c        *websocket.Conn
	mu       sync.Mutex
	id       int64
	pending  map[int64]chan reply
	handlers map[string][]func(session string, params json.RawMessage)
	done     chan struct{}
}

type reply struct {
	Result json.RawMessage
	Err    error
}

type frame struct {
	ID        int64           `json:"id,omitempty"`
	Method    string          `json:"method,omitempty"`
	Params    json.RawMessage `json:"params,omitempty"`
	SessionID string          `json:"sessionId,omitempty"`
	Result    json.RawMessage `json:"result,omitempty"`
	Error     *struct {
		Code    int    `json:"code"`
		Message string `json:"message"`
	} `json:"error,omitempty"`
}

// Dial connects to a DevTools WebSocket URL.
func Dial(ctx context.Context, url string) (*CDP, error) {
	c, _, err := websocket.Dial(ctx, url, nil)
	if err != nil {
		return nil, err
	}
	c.SetReadLimit(256 << 20) // screencast frames and large bodies
	x := &CDP{c: c, pending: map[int64]chan reply{}, handlers: map[string][]func(string, json.RawMessage){}, done: make(chan struct{})}
	go x.read()
	return x, nil
}

func (x *CDP) read() {
	defer close(x.done)
	for {
		_, b, err := x.c.Read(context.Background())
		if err != nil {
			x.mu.Lock()
			for _, ch := range x.pending {
				ch <- reply{Err: err}
			}
			x.pending = map[int64]chan reply{}
			x.mu.Unlock()
			return
		}
		var f frame
		if json.Unmarshal(b, &f) != nil {
			continue
		}
		if f.ID != 0 {
			x.mu.Lock()
			ch := x.pending[f.ID]
			delete(x.pending, f.ID)
			x.mu.Unlock()
			if ch != nil {
				r := reply{Result: f.Result}
				if f.Error != nil {
					r.Err = fmt.Errorf("cdp %d: %s", f.Error.Code, f.Error.Message)
				}
				ch <- r
			}
			continue
		}
		x.mu.Lock()
		hs := append([]func(string, json.RawMessage){}, x.handlers[f.Method]...)
		x.mu.Unlock()
		for _, h := range hs {
			go h(f.SessionID, f.Params)
		}
	}
}

// On registers a handler for an event method.
func (x *CDP) On(method string, fn func(session string, params json.RawMessage)) {
	x.mu.Lock()
	x.handlers[method] = append(x.handlers[method], fn)
	x.mu.Unlock()
}

// Call sends one command on a session ("" is the browser) and decodes its result.
func (x *CDP) Call(ctx context.Context, session, method string, params any, out any) error {
	x.mu.Lock()
	x.id++
	id := x.id
	ch := make(chan reply, 1)
	x.pending[id] = ch
	x.mu.Unlock()
	p, _ := json.Marshal(params)
	if params == nil {
		p = []byte("{}")
	}
	b, _ := json.Marshal(frame{ID: id, Method: method, Params: p, SessionID: session})
	if err := x.c.Write(ctx, websocket.MessageText, b); err != nil {
		return err
	}
	select {
	case <-ctx.Done():
		return ctx.Err()
	case r := <-ch:
		if r.Err != nil {
			return r.Err
		}
		if out != nil && len(r.Result) > 0 {
			return json.Unmarshal(r.Result, out)
		}
		return nil
	case <-x.done:
		return errors.New("cdp: connection closed")
	}
}

func (x *CDP) Close() error { return x.c.CloseNow() }
