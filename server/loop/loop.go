// Package loop is n.loop: the turn (TP-02). The harness owns every control: it gathers
// the inbound, assembles the request, calls the model, seals the sheet before any call
// runs, and runs or drafts each call by the draft rule.
package loop

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"strings"
	"sync"
	"time"

	"plebian/server/clock"
	"plebian/server/config"
	"plebian/server/core"
	"plebian/server/models"
	"plebian/server/store"
	"plebian/server/tools"
)

type Loop struct {
	S       *store.Store
	Clock   clock.Clock
	P       core.Params
	Tools   *tools.Registry
	Counter *models.Counter
	Secrets *config.Secrets
	Log     *log.Logger

	wake     chan struct{}
	mu       sync.Mutex
	tail     []core.Sheet // sheets since the last merge, oldest first
	tailFrom int64
	busy     bool
}

func (l *Loop) Init(ctx context.Context) error {
	l.wake = make(chan struct{}, 1)
	if l.Log == nil {
		l.Log = log.Default()
	}
	if err := l.loadTail(ctx); err != nil {
		return err
	}
	return l.recoverInterrupted(ctx)
}

// Wake asks the loop to look for inbounds now.
func (l *Loop) Wake() {
	select {
	case l.wake <- struct{}{}:
	default:
	}
}

func (l *Loop) loadTail(ctx context.Context) error {
	from := l.S.Setting(ctx, "tail_from")
	fmt.Sscan(from, &l.tailFrom)
	rows, err := l.S.DB.QueryContext(ctx, `SELECT id FROM turns WHERE id >= ? ORDER BY id`, l.tailFrom)
	if err != nil {
		return err
	}
	var ids []int64
	for rows.Next() {
		var id int64
		rows.Scan(&id)
		ids = append(ids, id)
	}
	rows.Close()
	l.tail = nil
	for _, id := range ids {
		sh, err := l.S.Turn(ctx, id)
		if err != nil {
			return err
		}
		l.tail = append(l.tail, sh)
	}
	if len(ids) > 0 && l.tailFrom == 0 {
		l.tailFrom = ids[0]
	}
	return nil
}

// Run is the loop's main cycle.
func (l *Loop) Run(ctx context.Context) {
	for {
		next, err := l.step(ctx)
		if err != nil {
			l.Log.Printf("loop: %v", err)
		}
		var timer <-chan time.Time
		if !next.IsZero() {
			timer = l.Clock.After(next)
		}
		select {
		case <-ctx.Done():
			return
		case <-l.wake:
		case <-timer:
		}
	}
}

// step opens one exchange if an inbound is ready; it returns when to look again.
func (l *Loop) step(ctx context.Context) (time.Time, error) {
	// A reading answering the newest sheet's calls goes first: the provider needs each
	// call's result in the very next request.
	if in, err := l.pendingInbound(ctx, "kind='readings'"); err != nil || in != nil {
		if err != nil {
			return time.Time{}, err
		}
		return time.Time{}, l.exchange(ctx, in.opener, in.parts, nil)
	}
	msgs, last, err := l.pendingOwner(ctx)
	if err != nil {
		return time.Time{}, err
	}
	if len(msgs) > 0 {
		gap := l.usualGap(ctx)
		ready := last.Add(gap)
		if l.Clock.Now().Before(ready) {
			return ready, nil
		}
		var parts []core.Part
		var ids []int64
		for _, m := range msgs {
			parts = append(parts, core.Part{Kind: core.PartOwner, Writer: core.Owner, At: m.at, Meta: m.client, Text: []byte(m.text)})
			ids = append(ids, m.id)
		}
		return time.Time{}, l.exchange(ctx, core.ByOwner, parts, ids)
	}
	in, err := l.pendingInbound(ctx, "1=1")
	if err != nil || in == nil {
		return time.Time{}, err
	}
	return time.Time{}, l.exchange(ctx, in.opener, in.parts, nil)
}

type ownerMsg struct {
	id           int64
	client, text string
	at           string
}

func (l *Loop) pendingOwner(ctx context.Context) ([]ownerMsg, time.Time, error) {
	rows, err := l.S.DB.QueryContext(ctx, `SELECT id, client_msg_id, text, sent_at, received_at FROM owner_messages WHERE turn IS NULL ORDER BY id`)
	if err != nil {
		return nil, time.Time{}, err
	}
	defer rows.Close()
	var out []ownerMsg
	var last time.Time
	for rows.Next() {
		var m ownerMsg
		var recv string
		if err := rows.Scan(&m.id, &m.client, &m.text, &m.at, &recv); err != nil {
			return nil, time.Time{}, err
		}
		if t, err := time.Parse(time.RFC3339Nano, recv); err == nil && t.After(last) {
			last = t
		}
		out = append(out, m)
	}
	return out, last, rows.Err()
}

type inbound struct {
	opener core.Opener
	parts  []core.Part
}

// pendingInbound takes the oldest queued inbound that isn't an owner message: landings,
// questions code raises, clock and order turns, job results.
func (l *Loop) pendingInbound(ctx context.Context, filter string) (*inbound, error) {
	var id int64
	var kind, body, at string
	err := l.S.DB.QueryRowContext(ctx, `SELECT id, kind, body, at FROM inbounds WHERE done=0 AND `+filter+` ORDER BY id LIMIT 1`).Scan(&id, &kind, &body, &at)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	if _, err := l.S.DB.ExecContext(ctx, `UPDATE inbounds SET done=1 WHERE id=?`, id); err != nil {
		return nil, err
	}
	var in inbound
	switch kind {
	case "landing":
		in.opener = core.ByLanding
		in.parts = []core.Part{{Kind: core.PartLanding, Writer: core.Harness, At: at, Text: []byte(body)}}
	case "question":
		in.opener = core.ByQuestion
		in.parts = []core.Part{{Kind: core.PartQuestion, Writer: core.Harness, At: at, Text: []byte(body)}}
	case "clock":
		in.opener = core.ByClock
		in.parts = []core.Part{{Kind: core.PartClock, Writer: core.Harness, At: at, Text: []byte(body)}}
	case "readings":
		in.opener = core.ByJob
		if err := json.Unmarshal([]byte(body), &in.parts); err != nil {
			return nil, err
		}
	case "order":
		in.opener = core.ByOrder
		in.parts = []core.Part{{Kind: core.PartClock, Writer: core.Harness, At: at, Text: []byte(body)}}
	default:
		return nil, fmt.Errorf("unknown inbound kind %q", kind)
	}
	return &in, nil
}

// Enqueue adds an inbound for the loop (landings, questions code raises, clock turns).
func (l *Loop) Enqueue(ctx context.Context, kind, body string) error {
	_, err := l.S.DB.ExecContext(ctx, `INSERT INTO inbounds(kind, body, at) VALUES(?,?,?)`, kind, body, l.now())
	l.Wake()
	return err
}

func (l *Loop) now() string { return l.Clock.Now().UTC().Format(time.RFC3339Nano) }

// usualGap is the owner's usual gap between messages: the median of gaps between
// consecutive owner messages gathered into one turn, once ColdStart samples exist.
func (l *Loop) usualGap(ctx context.Context) time.Duration {
	rows, err := l.S.DB.QueryContext(ctx, `SELECT a.received_at, b.received_at FROM owner_messages a JOIN owner_messages b ON b.id = (SELECT MIN(id) FROM owner_messages WHERE id > a.id) WHERE a.turn IS NOT NULL AND a.turn = b.turn`)
	if err != nil {
		return l.P.UsualGap
	}
	defer rows.Close()
	var gaps []time.Duration
	for rows.Next() {
		var a, b string
		rows.Scan(&a, &b)
		ta, e1 := time.Parse(time.RFC3339Nano, a)
		tb, e2 := time.Parse(time.RFC3339Nano, b)
		if e1 == nil && e2 == nil && tb.After(ta) {
			gaps = append(gaps, tb.Sub(ta))
		}
	}
	if len(gaps) < l.P.ColdStart {
		return l.P.UsualGap
	}
	return median(gaps)
}

func median(d []time.Duration) time.Duration {
	s := append([]time.Duration(nil), d...)
	for i := 1; i < len(s); i++ {
		for j := i; j > 0 && s[j] < s[j-1]; j-- {
			s[j], s[j-1] = s[j-1], s[j]
		}
	}
	return s[len(s)/2]
}

// Routes reads the owner's ordered provider list from settings and secrets.
func (l *Loop) Routes(ctx context.Context) ([]models.Route, error) {
	var rs []models.Route
	if v := l.S.Setting(ctx, "routes"); v != "" {
		if err := json.Unmarshal([]byte(v), &rs); err != nil {
			return nil, err
		}
	}
	for i := range rs {
		rs[i].Provider.Key = l.Secrets.Get("provider:" + rs[i].Provider.ID)
	}
	return rs, nil
}

// budget is B for a model (TP-06), capped at its context limit.
func (l *Loop) budget(ctx context.Context, rt models.Route) int {
	b := l.P.BudgetStart
	var v int
	if err := l.S.DB.QueryRowContext(ctx, `SELECT b FROM budget WHERE model=?`, rt.Model.ID).Scan(&v); err == nil && v > 0 {
		b = v
	}
	if lim := l.Counter.ContextLimit(rt.Model); lim > 0 && b > lim {
		b = lim
	}
	return b
}

// window is the owner's usual time to answer the agent; until the store has its own fit
// the usual gap stands in for it (Parameter schedule, cold start).
func (l *Loop) window(ctx context.Context) time.Duration { return l.usualGap(ctx) }

func (l *Loop) emit(ctx context.Context, typ string, body any, turn int64) {
	if _, err := l.S.Emit(ctx, l.now(), typ, body, turn); err != nil {
		l.Log.Printf("emit %s: %v", typ, err)
	}
}

// recoverInterrupted runs once at start. If the newest sheet holds calls and no later
// sheet answers them, the server stopped between seal and reading: each such call is
// reported to the agent as interrupted (a reading) and never re-run on its own (AG-06).
func (l *Loop) recoverInterrupted(ctx context.Context) error {
	if len(l.tail) == 0 {
		return nil
	}
	last := l.tail[len(l.tail)-1]
	ids := callIDs(last.Parts)
	if len(ids) == 0 {
		return nil
	}
	var n int64
	l.S.DB.QueryRowContext(ctx, `SELECT MAX(id) FROM turns`).Scan(&n)
	var parts []core.Part
	for _, p := range last.Parts {
		if p.Kind != core.PartCall {
			continue
		}
		id, tool, _ := strings.Cut(p.Meta, " ")
		var state string
		l.S.DB.QueryRowContext(ctx, `SELECT state FROM calls WHERE turn=? AND call_id=?`, n, id).Scan(&state)
		if state == "done" || state == "failed" || state == "drafted" || state == "held" {
			continue // its reading was written; the reading turn below carries it
		}
		l.S.DB.ExecContext(ctx, `INSERT INTO calls(turn, call_id, tool, class, state) VALUES(?,?,?,?, 'interrupted')
			ON CONFLICT(turn, call_id) DO UPDATE SET state='interrupted'`, n, id, tool, string(l.Tools.ClassOf(tool, p.Text)))
		l.emit(ctx, "step", map[string]any{"tool": tool, "summary": "interrupted", "state": "interrupted"}, n)
		parts = append(parts, core.Part{Kind: core.PartReading, Writer: core.Harness, At: l.now(), Meta: id + " -1 " + strings.ReplaceAll(string(l.Tools.ClassOf(tool, p.Text)), " ", "_") + " 0",
			Text: []byte("interrupted: the server stopped while this call was running; it was not re-run")})
	}
	if len(parts) == 0 {
		return nil
	}
	b, _ := json.Marshal(parts)
	_, err := l.S.DB.ExecContext(ctx, `INSERT INTO inbounds(kind, body, at) VALUES('readings', ?, ?)`, string(b), l.now())
	return err
}

func callIDs(parts []core.Part) []string {
	var out []string
	for _, p := range parts {
		if p.Kind == core.PartCall {
			id, _, _ := strings.Cut(p.Meta, " ")
			out = append(out, id)
		}
	}
	return out
}
