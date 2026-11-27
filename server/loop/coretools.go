package loop

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"plebian/server/models"
	"plebian/server/store"
	"plebian/server/tools"
	"plebian/server/underlay"
)

// CoreTools are the band's harness-level tools: ask, keep and lookup. They write no
// memory table; the harness records each call like any call.
// putOut, given a message's row id, puts its attachments in the outside folder and
// returns the paths it put there (nil on a box with no feeds).
func CoreTools(s *store.Store, nameVoice func(ctx context.Context, id int64, words string, owner bool) error, putOut func(ctx context.Context, row int64) ([]string, error)) []tools.Tool {
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
				var at string // the sheet that holds this call: the loop's clock, not the wall
				s.DB.QueryRowContext(ctx, `SELECT id, at FROM turns ORDER BY id DESC LIMIT 1`).Scan(&turn, &at)
				res, err := s.DB.ExecContext(ctx, `INSERT INTO loops(kind, text, opened_turn) VALUES('question', ?, ?)`, q.Question, turn)
				if err != nil {
					return tools.Result{}, err
				}
				id, _ := res.LastInsertId()
				s.Emit(ctx, at, "question", map[string]any{"question_id": id, "text": q.Question, "state": "open"}, turn)
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
		lookup(s, putOut),
		entry(s),
	}
}

// lookup is the one read on the underlay: by words, who, source and time. It returns
// row ids; the rows are quoted for the next request only and never copied into the stack.
func lookup(s *store.Store, putOut func(ctx context.Context, row int64) ([]string, error)) tools.Tool {
	return tools.Func{
		D: models.Tool{Name: "lookup", Description: "Read the underlay (talk, mail, texts, calendar overheard) by words, who, source and time; returns row ids, and the rows are quoted beside the result.",
			Schema: tools.Schema(nil, map[string][2]string{
				"words":  {"string", "words to find"},
				"who":    {"string", "speaker or sender"},
				"source": {"string", "talk, mail, text or calendar"},
				"from":   {"string", "RFC 3339 start"},
				"to":     {"string", "RFC 3339 end"},
				"row":    {"integer", "a message's row id: its attachments are put in the outside folder as files"},
			})},
		C: tools.Read,
		RunFunc: func(ctx context.Context, in json.RawMessage) (tools.Result, error) {
			var q struct {
				Words, Who, Source, From, To string
				Row                          int64
			}
			if err := json.Unmarshal(in, &q); err != nil {
				return tools.Result{Reading: []byte("bad input: " + err.Error()), Exit: 2}, nil
			}
			if q.Row != 0 {
				if putOut == nil {
					return tools.Result{Reading: []byte("no feeds on this box"), Exit: 1}, nil
				}
				paths, err := putOut(ctx, q.Row)
				if err != nil {
					return tools.Result{Reading: []byte(err.Error()), Exit: 1}, nil
				}
				var b strings.Builder
				fmt.Fprintf(&b, "rows: 1\n%d\nput out:\n", q.Row)
				for _, p := range paths {
					b.WriteString(p + "\n")
				}
				return tools.Result{Reading: []byte(b.String())}, nil
			}
			ids, err := underlay.Find(ctx, s, q.Words, q.Who, q.Source, q.From, q.To)
			if err != nil {
				return tools.Result{}, err
			}
			var b strings.Builder
			fmt.Fprintf(&b, "rows: %d\n", len(ids))
			for _, id := range ids {
				fmt.Fprintf(&b, "%d\n", id)
			}
			return tools.Result{Reading: []byte(b.String())}, nil
		},
	}
}

// entry is the night's read written down: the day's lines, each held (at ½, covering
// nothing) until the owner answers it; a line naming a tap becomes that tap's title.
func entry(s *store.Store) tools.Tool {
	return tools.Func{
		D: models.Tool{Name: "entry", Description: "Write the day's entry: short lines on what the day's talk and mail answered or set off, what the owner committed to, and what needs the owner's word; a line may name the tap it reports.",
			Schema: tools.Schema([]string{"lines"}, map[string][2]string{
				"lines": {"array", "the lines, each {\"text\": ..., \"tap\": <tap id, if it reports one>, \"heard_by\": <speaker, if a heard line>}"},
			})},
		C: tools.Read, // it writes only the owner's page, held until the owner answers
		RunFunc: func(ctx context.Context, in json.RawMessage) (tools.Result, error) {
			var e struct {
				Lines []struct {
					Text    string `json:"text"`
					Tap     string `json:"tap,omitempty"`
					HeardBy string `json:"heard_by,omitempty"`
				} `json:"lines"`
			}
			if err := json.Unmarshal(in, &e); err != nil || len(e.Lines) == 0 {
				return tools.Result{Reading: []byte("entry needs lines"), Exit: 2}, nil
			}
			for _, l := range e.Lines {
				if strings.TrimSpace(l.Text) == "" {
					return tools.Result{Reading: []byte("an entry line is empty"), Exit: 2}, nil
				}
			}
			// The entry is stamped at the sheet that holds this call (the loop's clock).
			var turn int64
			var now string
			s.DB.QueryRowContext(ctx, `SELECT id, at FROM turns ORDER BY id DESC LIMIT 1`).Scan(&turn, &now)
			if _, err := s.Emit(ctx, now, "entry", e.Lines, turn); err != nil {
				return tools.Result{}, err
			}
			for _, l := range e.Lines {
				if l.Tap != "" {
					s.DB.ExecContext(ctx, `UPDATE taps SET title=? WHERE id=?`, l.Text, l.Tap)
				}
			}
			return tools.Result{Reading: []byte(fmt.Sprintf("entry: %d lines on the page, held until the owner answers them", len(e.Lines)))}, nil
		},
	}
}
