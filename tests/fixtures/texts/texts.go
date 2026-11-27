// Package texts stands in for the Google Messages link at the texts
// library's interface: pairing by emoji and phone tap, a long-lived NDJSON
// stream of inbound messages (backlog, then live) and sending.
package texts

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"net/http"
	"sync"
	"time"

	"plebian/tests/fixtures"
)

// Message is one inbound text.
type Message struct {
	ConversationID string `json:"conversation_id"`
	MessageID      string `json:"message_id"`
	Sender         string `json:"sender"`
	Text           string `json:"text"`
	TsMs           int64  `json:"ts_ms"`
}

// Sent is one text the library sent.
type Sent struct {
	ConversationID string    `json:"conversation_id"`
	Text           string    `json:"text"`
	MessageID      string    `json:"message_id"`
	Time           time.Time `json:"time"`
}

// Texts is the fixture's state.
type Texts struct {
	mu      sync.Mutex
	pending map[string]string // cookies awaiting the phone tap
	paired  bool
	inbox   []Message
	sent    []Sent
	seq     int
	changed chan struct{}
	log     *fixtures.Log
}

// New returns an unpaired link.
func New() *Texts { return &Texts{changed: make(chan struct{}), log: fixtures.NewLog()} }

// Inject delivers an inbound message to every stream, now and on later backlogs.
func (t *Texts) Inject(m Message) Message {
	t.mu.Lock()
	t.seq++
	if m.MessageID == "" {
		m.MessageID = fmt.Sprintf("in-%d", t.seq)
	}
	if m.TsMs == 0 {
		m.TsMs = time.Now().UnixMilli()
	}
	t.inbox = append(t.inbox, m)
	close(t.changed)
	t.changed = make(chan struct{})
	t.mu.Unlock()
	return m
}

// emoji derives the pairing emoji from the cookies, within Unicode's
// Emoticons block (U+1F600..U+1F64F, 80 code points).
func emoji(cookies map[string]string) string {
	b, _ := json.Marshal(cookies) // map keys marshal sorted: same cookies, same emoji
	h := sha256.Sum256(b)
	return string(rune(0x1F600 + int(h[0])%80))
}

func (t *Texts) isPaired(w http.ResponseWriter) bool {
	t.mu.Lock()
	ok := t.paired
	t.mu.Unlock()
	if !ok {
		fixtures.WriteJSON(w, 401, map[string]string{"error": "not paired"})
	}
	return ok
}

// Handler serves the library interface and the control API.
func (t *Texts) Handler() http.Handler {
	mux := http.NewServeMux()
	rec := func(h http.HandlerFunc) http.Handler { return t.log.Recording(h) }
	mux.Handle("POST /v1/pair", rec(func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			Cookies map[string]string `json:"cookies"`
		}
		if !fixtures.ReadJSON(w, r, &req) {
			return
		}
		if len(req.Cookies) == 0 {
			fixtures.WriteJSON(w, 400, map[string]string{"error": "cookies are required"})
			return
		}
		t.mu.Lock()
		t.pending, t.paired = req.Cookies, false
		t.mu.Unlock()
		fixtures.WriteJSON(w, 200, map[string]string{"emoji": emoji(req.Cookies)})
	}))
	mux.Handle("GET /v1/status", rec(func(w http.ResponseWriter, r *http.Request) {
		t.mu.Lock()
		p := t.paired
		t.mu.Unlock()
		fixtures.WriteJSON(w, 200, map[string]bool{"paired": p})
	}))
	mux.Handle("GET /v1/stream", rec(func(w http.ResponseWriter, r *http.Request) {
		if !t.isPaired(w) {
			return
		}
		w.Header().Set("Content-Type", "application/x-ndjson")
		w.WriteHeader(200)
		f, _ := w.(http.Flusher)
		enc := json.NewEncoder(w)
		next := 0
		for {
			t.mu.Lock()
			batch, wait := append([]Message{}, t.inbox[next:]...), t.changed
			next = len(t.inbox)
			t.mu.Unlock()
			for _, m := range batch {
				if enc.Encode(m) != nil {
					return
				}
			}
			if f != nil {
				f.Flush()
			}
			select {
			case <-wait:
			case <-r.Context().Done():
				return
			}
		}
	}))
	mux.Handle("POST /v1/send", rec(func(w http.ResponseWriter, r *http.Request) {
		if !t.isPaired(w) {
			return
		}
		var req struct {
			ConversationID string `json:"conversation_id"`
			Text           string `json:"text"`
		}
		if !fixtures.ReadJSON(w, r, &req) {
			return
		}
		if req.ConversationID == "" || req.Text == "" {
			fixtures.WriteJSON(w, 400, map[string]string{"error": "conversation_id and text are required"})
			return
		}
		t.mu.Lock()
		t.seq++
		s := Sent{req.ConversationID, req.Text, fmt.Sprintf("out-%d", t.seq), time.Now().UTC()}
		t.sent = append(t.sent, s)
		t.mu.Unlock()
		fixtures.WriteJSON(w, 200, map[string]string{"message_id": s.MessageID})
	}))
	mux.HandleFunc("POST /control/phone_tap", func(w http.ResponseWriter, r *http.Request) {
		t.mu.Lock()
		ok := t.pending != nil
		t.paired = t.paired || ok
		t.mu.Unlock()
		if !ok {
			fixtures.WriteJSON(w, 409, map[string]string{"error": "no pairing is waiting for a tap"})
			return
		}
		fixtures.WriteJSON(w, 200, map[string]bool{"paired": true})
	})
	mux.HandleFunc("POST /control/inject", func(w http.ResponseWriter, r *http.Request) {
		var m Message
		if !fixtures.ReadJSON(w, r, &m) {
			return
		}
		if m.ConversationID == "" || m.Sender == "" || m.Text == "" {
			fixtures.WriteJSON(w, 400, map[string]string{"error": "conversation_id, sender and text are required"})
			return
		}
		fixtures.WriteJSON(w, 200, t.Inject(m))
	})
	mux.HandleFunc("GET /control/sent", func(w http.ResponseWriter, r *http.Request) {
		t.mu.Lock()
		out := append([]Sent{}, t.sent...)
		t.mu.Unlock()
		fixtures.WriteJSON(w, 200, out)
	})
	mux.HandleFunc("GET /control/requests", func(w http.ResponseWriter, r *http.Request) {
		fixtures.WriteJSON(w, 200, t.log.List())
	})
	return mux
}
