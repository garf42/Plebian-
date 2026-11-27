package browser

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"log"
	"net/http"
	"sync"
	"time"

	"github.com/coder/websocket"
)

// Live serves the live view: the page's screencast to the app, the owner's input back.
// While the owner holds the wheel the guard stands aside and the agent waits; nothing
// the owner types is kept, logged or read.
type Live struct {
	G   *Guard
	Log *log.Logger // errors only; never input

	once sync.Once
	mu   sync.Mutex
	subs map[string]chan []byte // page session → frames for the one viewer
}

func (l *Live) init() {
	l.once.Do(func() {
		l.subs = map[string]chan []byte{}
		l.G.C.On("Page.screencastFrame", func(session string, p json.RawMessage) {
			var f struct {
				Data      string `json:"data"`
				SessionID int    `json:"sessionId"`
			}
			if json.Unmarshal(p, &f) != nil {
				return
			}
			l.G.C.Call(context.Background(), session, "Page.screencastFrameAck", map[string]any{"sessionId": f.SessionID}, nil)
			l.mu.Lock()
			ch := l.subs[session]
			l.mu.Unlock()
			if ch == nil {
				return
			}
			b, err := base64.StdEncoding.DecodeString(f.Data)
			if err != nil {
				return
			}
			select {
			case ch <- b:
			default: // a slow viewer skips frames
			}
		})
	})
}

type input struct {
	Type  string  `json:"type"` // take | give | mouse | key
	X     float64 `json:"x"`
	Y     float64 `json:"y"`
	Event string  `json:"event"` // down | up | move
	Text  string  `json:"text"`
	Key   string  `json:"key"` // a named key: Tab, Enter, Backspace, Escape
}

var keys = map[string]int{"Tab": 9, "Enter": 13, "Backspace": 8, "Escape": 27}

// Handler serves GET /v1/browser/live.
func (l *Live) Handler(w http.ResponseWriter, r *http.Request) {
	l.init()
	session := l.G.Page()
	if session == "" {
		http.Error(w, "no page is open", http.StatusConflict)
		return
	}
	c, err := websocket.Accept(w, r, &websocket.AcceptOptions{InsecureSkipVerify: true})
	if err != nil {
		return
	}
	defer c.CloseNow()
	ctx, cancel := context.WithCancel(r.Context())
	defer cancel()
	frames := make(chan []byte, 4)
	l.mu.Lock()
	l.subs[session] = frames
	l.mu.Unlock()
	defer func() {
		l.mu.Lock()
		delete(l.subs, session)
		l.mu.Unlock()
		l.G.Owner(false) // a dropped view hands the wheel back
		l.G.C.Call(context.Background(), session, "Page.stopScreencast", nil, nil)
	}()
	if err := l.G.C.Call(ctx, session, "Page.startScreencast", map[string]any{"format": "jpeg", "quality": 60, "everyNthFrame": 1}, nil); err != nil {
		if l.Log != nil {
			l.Log.Printf("live view: %v", err)
		}
		return
	}
	go func() {
		for {
			select {
			case <-ctx.Done():
				return
			case b := <-frames:
				wctx, wc := context.WithTimeout(ctx, 10*time.Second)
				c.Write(wctx, websocket.MessageBinary, b)
				wc()
			}
		}
	}()
	for {
		_, b, err := c.Read(ctx)
		if err != nil {
			return
		}
		var in input
		if json.Unmarshal(b, &in) != nil {
			continue
		}
		switch in.Type {
		case "take":
			l.G.Owner(true)
		case "give":
			l.G.Owner(false)
		case "mouse":
			if !l.G.Holding() {
				continue // input reaches the page only while the owner holds the wheel
			}
			t := map[string]string{"down": "mousePressed", "up": "mouseReleased", "move": "mouseMoved"}[in.Event]
			if t == "" {
				continue
			}
			p := map[string]any{"type": t, "x": in.X, "y": in.Y}
			if t != "mouseMoved" {
				p["button"], p["clickCount"] = "left", 1
			}
			l.G.C.Call(ctx, session, "Input.dispatchMouseEvent", p, nil)
		case "key":
			if !l.G.Holding() {
				continue
			}
			if code, ok := keys[in.Key]; ok {
				for _, t := range []string{"keyDown", "keyUp"} {
					p := map[string]any{"type": t, "key": in.Key, "code": in.Key, "windowsVirtualKeyCode": code}
					if in.Key == "Enter" && t == "keyDown" {
						p["text"] = "\r"
					}
					l.G.C.Call(ctx, session, "Input.dispatchKeyEvent", p, nil)
				}
			} else if in.Text != "" {
				l.G.C.Call(ctx, session, "Input.insertText", map[string]any{"text": in.Text}, nil)
			}
		}
	}
}
