package store

import (
	"context"
	"encoding/json"
	"sync"
)

// Event is one entry of the thread's event log, which the API streams to the app.
type Event struct {
	ID   int64           `json:"id"`
	At   string          `json:"at"`
	Type string          `json:"type"`
	Body json.RawMessage `json:"body"`
	Turn int64           `json:"turn,omitempty"`
}

// Hub fans new event ids out to live subscribers.
type Hub struct {
	mu   sync.Mutex
	subs map[chan int64]struct{}
}

func (h *Hub) Subscribe() (chan int64, func()) {
	ch := make(chan int64, 256)
	h.mu.Lock()
	if h.subs == nil {
		h.subs = map[chan int64]struct{}{}
	}
	h.subs[ch] = struct{}{}
	h.mu.Unlock()
	return ch, func() {
		h.mu.Lock()
		delete(h.subs, ch)
		h.mu.Unlock()
	}
}

func (h *Hub) publish(id int64) {
	h.mu.Lock()
	defer h.mu.Unlock()
	for ch := range h.subs {
		select {
		case ch <- id:
		default: // a slow subscriber re-reads the backlog from its last id
		}
	}
}

// Emit appends an event durably and wakes subscribers.
func (s *Store) Emit(ctx context.Context, at, typ string, body any, turn int64) (int64, error) {
	b, err := json.Marshal(body)
	if err != nil {
		return 0, err
	}
	var t any
	if turn > 0 {
		t = turn
	}
	res, err := s.DB.ExecContext(ctx, `INSERT INTO events(at, type, body, turn) VALUES(?,?,?,?)`, at, typ, string(b), t)
	if err != nil {
		return 0, err
	}
	id, _ := res.LastInsertId()
	s.Hub.publish(id)
	return id, nil
}

// EventsAfter returns events with id > after, oldest first.
func (s *Store) EventsAfter(ctx context.Context, after int64, limit int) ([]Event, error) {
	rows, err := s.DB.QueryContext(ctx, `SELECT id, at, type, body, COALESCE(turn,0) FROM events WHERE id > ? ORDER BY id LIMIT ?`, after, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Event
	for rows.Next() {
		var e Event
		var body string
		if err := rows.Scan(&e.ID, &e.At, &e.Type, &body, &e.Turn); err != nil {
			return nil, err
		}
		e.Body = json.RawMessage(body)
		out = append(out, e)
	}
	return out, rows.Err()
}
