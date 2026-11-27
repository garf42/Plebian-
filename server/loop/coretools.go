package loop

import (
	"context"
	"encoding/json"
	"strings"
	"time"

	"plebian/server/models"
	"plebian/server/store"
	"plebian/server/tools"
	"plebian/server/underlay"
)

// CoreTools are the band's harness-level tools: ask, keep and lookup. They write no
// memory table; the harness records each call like any call.
func CoreTools(s *store.Store, nameVoice func(ctx context.Context, id int64, words string, owner bool) error) []tools.Tool {
	return []tools.Tool{
		tools.Func{
			D: models.Tool{Name: "ask", Description: "Ask the owner a question; it stays open until the owner's reply retraces it.",
				Schema: tools.Schema([]string{"question"}, map[string][2]string{"question": {"string", "the question, in your words"}})},
			C: tools.Read,
			RunFunc: func(ctx context.Context, in json.RawMessage) (tools.Result, error) {
				var q struct{ Question string }
				if json.Unmarshal(in, &q) != nil || strings.TrimSpace(q.Question) == "" {
					return tools.Result{Reading: []byte("ask needs a question"), Exit: 2}, nil
				}
				var turn int64
				s.DB.QueryRowContext(ctx, `SELECT MAX(id) FROM turns`).Scan(&turn)
				res, err := s.DB.ExecContext(ctx, `INSERT INTO loops(kind, text, opened_turn) VALUES('question', ?, ?)`, q.Question, turn)
				if err != nil {
					return tools.Result{}, err
				}
				id, _ := res.LastInsertId()
				s.Emit(ctx, time.Now().UTC().Format(time.RFC3339Nano), "question", map[string]any{"question_id": id, "text": q.Question, "state": "open"}, turn)
				return tools.Result{Reading: []byte("asked; the owner's reply arrives as their next message")}, nil
			},
		},
		tools.Func{
			D: models.Tool{Name: "keep", Description: "Keep or release an order, or name a voice, by pointing at the owner's own words (quoted exactly).",
				Schema: tools.Schema([]string{"words"}, map[string][2]string{
					"words":   {"string", "the owner's words, exactly as the owner wrote them"},
					"release": {"boolean", "release the order these words gave"},
					"at":      {"string", "RFC 3339 time the words name, if any"},
					"voice":   {"integer", "the voice number these words name (from \"voice N\")"},
					"owner":   {"boolean", "the words name the voice as the owner's own"},
				})},
			C: tools.Read,
			RunFunc: func(ctx context.Context, in json.RawMessage) (tools.Result, error) {
				var k struct {
					Words   string
					Release bool
					At      string
					Voice   int64
					Owner   bool
				}
				if json.Unmarshal(in, &k) != nil || strings.TrimSpace(k.Words) == "" {
					return tools.Result{Reading: []byte("keep needs the owner's words"), Exit: 2}, nil
				}
				var n int
				s.DB.QueryRowContext(ctx, `SELECT COUNT(*) FROM owner_messages WHERE instr(text, ?) > 0`, k.Words).Scan(&n)
				if n == 0 {
					return tools.Result{Reading: []byte("not the owner's words: keep points only at words the owner wrote"), Exit: 1}, nil
				}
				var turn int64
				s.DB.QueryRowContext(ctx, `SELECT MAX(id) FROM turns`).Scan(&turn)
				switch {
				case k.Voice != 0:
					if nameVoice == nil {
						return tools.Result{Reading: []byte("no voices are heard on this box"), Exit: 1}, nil
					}
					if err := nameVoice(ctx, k.Voice, k.Words, k.Owner); err != nil {
						return tools.Result{Reading: []byte(err.Error()), Exit: 1}, nil
					}
					return tools.Result{Reading: []byte("named")}, nil
				case k.Release:
					res, _ := s.DB.ExecContext(ctx, `UPDATE loops SET closed_turn=? WHERE kind='order' AND closed_turn IS NULL AND instr(?, text) > 0`, turn, k.Words)
					if c, _ := res.RowsAffected(); c == 0 {
						return tools.Result{Reading: []byte("no kept order those words gave"), Exit: 1}, nil
					}
					return tools.Result{Reading: []byte("released")}, nil
				default:
					var due any
					if t, err := time.Parse(time.RFC3339Nano, k.At); err == nil {
						due = t.UTC().Format(time.RFC3339Nano)
					}
					s.DB.ExecContext(ctx, `INSERT INTO loops(kind, text, opened_turn, due_at) VALUES('order', ?, ?, ?)`, k.Words, turn, due)
					return tools.Result{Reading: []byte("kept")}, nil
				}
			},
		},
		underlay.Lookup(s),
	}
}
