package loop

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"plebian/server/assemble"
	"plebian/server/core"
	"plebian/server/models"
	"plebian/server/store"
	"plebian/server/tools"
	"plebian/server/underlay"
)

// view is what someone else's words put in view for the draft rule (AG-05).
type view struct {
	heard   bool // a heard row: a landing, a lookup's rows, a lit heard line
	outside bool // an outside reading: a page, a message body, a file from the outside folder
}

func (v view) someone() bool { return v.heard || v.outside }

// exchange runs one exchange: rounds of request → model → seal → calls, until the model
// answers without a call.
func (l *Loop) exchange(ctx context.Context, opener core.Opener, parts []core.Part, ownerIDs []int64) error {
	routes, err := l.Routes(ctx)
	if err != nil {
		return err
	}
	if len(routes) == 0 {
		return errors.New("no model configured")
	}
	l.emit(ctx, "presence", map[string]any{"working": true}, 0)
	defer l.emit(ctx, "presence", map[string]any{"working": false}, 0)

	// The budget folds the tail only as the exchange opens, counted with the spotlight the
	// opening request will carry (TP-T4).
	open, openShown := l.spotlight(ctx, routes[0], parts)
	if l.foldOnOpen(ctx, routes[0], parts, open) {
		open, openShown = l.spotlight(ctx, routes[0], parts)
	}
	start := len(l.tail)
	thinking := map[int][]models.Block{}
	var spots [][]byte
	sheetOpener := opener
	var lastFailed *core.Part
	var lastFailedHeld bool
	ownerAt := ""
	for _, p := range parts {
		if p.Kind == core.PartOwner && p.At > ownerAt {
			ownerAt = p.At
		}
	}
	var nextSpot []byte
	for round := 0; ; round++ {
		v := l.inView(start, opener, parts)
		l.settle(ctx, parts)
		spot, shown := open, openShown
		if round > 0 {
			spot, shown = l.spotlight(ctx, routes[0], parts)
		}
		spots = append(spots, append(nextSpot, spot...))
		nextSpot = nil
		resp, rt, err := l.call(ctx, routes, start, spots, thinking, parts)
		if err != nil {
			l.emit(ctx, "step", map[string]any{"tool": "model", "summary": err.Error(), "state": "failed"}, 0)
			return err
		}
		out := outputParts(resp, l.now())
		sh := core.Sheet{Opener: sheetOpener, Parts: append(append([]core.Part{}, parts...), out...)}
		if shown == nil {
			shown = []int64{}
		}
		shownJSON, _ := json.Marshal(shown)
		meta := store.TurnMeta{At: l.now(), Shown: string(shownJSON), HeardOpened: opener == core.ByLanding || opener == core.ByQuestion, OutsideInView: v.outside, Model: rt.Model.ID, RequestID: resp.RequestID}
		n, err := l.S.Seal(ctx, sh, meta)
		if err != nil {
			return err
		}
		if l.tailFrom == 0 {
			l.tailFrom = n
		}
		if l.Index != nil {
			if err := l.Index(ctx, n, sh, meta.HeardOpened); err != nil {
				l.Log.Printf("index turn %d: %v", n, err)
			}
		}
		for _, id := range ownerIDs {
			l.S.DB.ExecContext(ctx, `UPDATE owner_messages SET turn=? WHERE id=?`, n, id)
		}
		ownerIDs = nil
		l.spend(ctx, rt.Model.ID, resp.Usage)
		l.tail = append(l.tail, sh)
		var th []models.Block
		for _, b := range resp.Content {
			if b.Type == "thinking" {
				th = append(th, b) // passed back within this exchange only; never stored
			}
		}
		thinking[len(l.tail)-1] = th
		for _, p := range out {
			if p.Kind == core.PartAgent && len(bytes.TrimSpace(p.Text)) > 0 {
				l.emit(ctx, "message", map[string]any{"from": "agent", "text": string(p.Text), "notify": opener != core.ByOwner}, n)
			}
		}
		var calls []core.Part
		for _, p := range out {
			if p.Kind == core.PartCall {
				calls = append(calls, p)
			}
		}
		if len(calls) == 0 {
			return nil
		}
		var readings []core.Part
		for _, c := range calls {
			id, name, _ := strings.Cut(c.Meta, " ")
			l.S.DB.ExecContext(ctx, `INSERT INTO calls(turn, call_id, tool, class, state) VALUES(?,?,?,?, 'sealed')`, n, id, name, string(l.Tools.ClassOf(name, c.Text)))
			// Held once: a call repeating word for word the one whose failure it answers, while
			// its wording is struck (TP-07 circuit breaker).
			held := lastFailed != nil && !lastFailedHeld && sameCall(*lastFailed, c) && (l.R == nil || l.R.Struck(ctx, c.Text))
			rd := l.runOrDraft(ctx, n, opener, ownerAt, v, c, held)
			if held {
				lastFailedHeld = true
			} else {
				lastFailedHeld = false
			}
			readings = append(readings, rd)
			if f := strings.Fields(rd.Meta); len(f) > 1 && f[1] != "0" && f[1] != "-" {
				cc := c
				lastFailed = &cc
			} else if !held {
				lastFailed = nil
			}
		}
		// A lookup's rows are quoted beside its result for this exchange only; the sheet keeps the ids.
		for i, rd := range readings {
			if _, name, _ := strings.Cut(calls[i].Meta, " "); name == "lookup" {
				nextSpot = append(nextSpot, underlay.Quote(ctx, l.S, underlay.IDs(rd.Text))...)
			}
		}
		parts = readings
		sheetOpener = core.ByTool
	}
}

func sameCall(a, b core.Part) bool {
	_, na, _ := strings.Cut(a.Meta, " ")
	_, nb, _ := strings.Cut(b.Meta, " ")
	return na == nb && bytes.Equal(a.Text, b.Text)
}

// inView reads the draft rule's inputs from the request about to be sent: outside
// readings anywhere in the tail or this inbound; heard rows when a landing or question
// opened the exchange or a lookup's rows are in this exchange.
func (l *Loop) inView(start int, opener core.Opener, parts []core.Part) view {
	v := view{heard: opener == core.ByLanding}
	check := func(ps []core.Part, inExchange bool) {
		for _, p := range ps {
			if p.Kind != core.PartReading {
				continue
			}
			f := strings.Fields(p.Meta)
			if len(f) > 3 && f[3] == "1" {
				v.outside = true
			}
			if inExchange && len(f) > 4 && f[4] == "heard" {
				v.heard = true
			}
		}
	}
	for i, sh := range l.tail {
		check(sh.Parts, i >= start)
	}
	check(parts, true)
	return v
}

// call assembles the request for each route in order and sends it within the retry
// window; past it, the next route takes the turn, re-assembled within its own budget.
func (l *Loop) call(ctx context.Context, routes []models.Route, start int, spots [][]byte, thinking map[int][]models.Block, parts []core.Part) (models.Response, models.Route, error) {
	var lastErr error
	for _, rt := range routes {
		rt := rt
		B := l.budget(ctx, rt)
		in := assemble.Input{Model: rt.Model.ID, Tools: l.Tools.Band(), Tail: l.tail, Exchange: start, Spots: spots, Thinking: thinking, Inbound: parts}
		if l.A != nil {
			in.Deep, in.Recent = l.A.Bands(ctx)
		}
		req := assemble.Build(in, l.P)
		n, err := l.Counter.Count(ctx, &rt.Provider, req)
		if err != nil {
			lastErr = err
			continue
		}
		ceiling := l.P.BudgetCeiling
		if lim := l.Counter.ContextLimit(rt.Model); lim > 0 && lim < ceiling {
			ceiling = lim
		}
		if n > ceiling {
			lastErr = fmt.Errorf("request of %d tokens passes the ceiling %d", n, ceiling)
			continue
		}
		req.MaxTokens = B - n
		if req.MaxTokens <= 0 || n > B {
			// Within an exchange the request only grows and may pass B; the output room is
			// what the ceiling leaves.
			req.MaxTokens = ceiling - n
		}
		if rt.Model.MaxOut > 0 && req.MaxTokens > rt.Model.MaxOut {
			req.MaxTokens = rt.Model.MaxOut
		}
		body, _ := rt.Provider.Body(req)
		resp, err := models.SendWithin(ctx, &rt.Provider, req, l.window(ctx), l.Clock, nil)
		if err != nil {
			var me *models.Error
			if errors.As(err, &me) && me.Class == "overflow" {
				l.Counter.Overflowed(rt.Model.ID, n)
			}
			lastErr = err
			continue
		}
		l.Counter.Observe(rt.Model.ID, len(body), 0, resp.Usage)
		l.S.SetSetting(ctx, "counter", string(l.Counter.MarshalState()))
		return resp, rt, nil
	}
	return models.Response{}, models.Route{}, lastErr
}

// foldOnOpen folds the tail into recent strata as an exchange opens, when the request
// would pass B; the newest sheet never folds (TP-05). Within an exchange nothing folds.
func (l *Loop) foldOnOpen(ctx context.Context, rt models.Route, parts []core.Part, spot []byte) bool {
	B := l.budget(ctx, rt)
	req := l.build(ctx, rt, len(l.tail), [][]byte{spot}, nil, parts)
	n, err := l.Counter.Count(ctx, &rt.Provider, req)
	if err != nil || n <= B || len(l.tail) < 2 {
		return false
	}
	if l.A == nil {
		for len(l.tail) > 1 && n > B {
			l.tail = l.tail[1:]
			l.tailFrom++
			req = l.build(ctx, rt, len(l.tail), [][]byte{spot}, nil, parts)
			n, _ = l.Counter.Count(ctx, &rt.Provider, req)
		}
		l.S.SetSetting(ctx, "tail_from", fmt.Sprint(l.tailFrom))
		return true
	}
	var turns []int64
	for i := range l.tail {
		turns = append(turns, l.tailFrom+int64(i))
	}
	fixed := l.Counter.CountText(rt.Model.ID, []byte(assemble.System)) + int(float64(B)*l.P.SpotlightShare)
	for _, t := range l.Tools.Band() {
		fixed += l.Counter.CountText(rt.Model.ID, append([]byte(t.Name+t.Description), t.Schema...))
	}
	k := l.A.Merge(ctx, rt.Model.ID, B, fixed, turns, float64(l.tailFrom+int64(len(l.tail))))
	l.tail = l.tail[k:]
	l.tailFrom += int64(k)
	l.S.SetSetting(ctx, "tail_from", fmt.Sprint(l.tailFrom))
	return k > 0
}

func (l *Loop) build(ctx context.Context, rt models.Route, start int, spots [][]byte, thinking map[int][]models.Block, parts []core.Part) models.Request {
	in := assemble.Input{Model: rt.Model.ID, Tools: l.Tools.Band(), Tail: l.tail, Exchange: start, Spots: spots, Thinking: thinking, Inbound: parts}
	if l.A != nil {
		in.Deep, in.Recent = l.A.Bands(ctx)
	}
	return assemble.Build(in, l.P)
}

// spotlight builds the quoted record beside this round's inbound.
func (l *Loop) spotlight(ctx context.Context, rt models.Route, parts []core.Part) ([]byte, []int64) {
	if l.A == nil {
		return nil, nil
	}
	var cue, last, landed []byte
	for _, p := range parts {
		if p.Kind == core.PartLanding {
			// A landing's rows are quoted for its own turn, with who said each and when.
			first, _, _ := strings.Cut(string(p.Text), "\n")
			var ids []int64
			for _, f := range strings.Fields(strings.TrimPrefix(first, "rows:")) {
				var id int64
				if _, err := fmt.Sscan(f, &id); err == nil {
					ids = append(ids, id)
				}
			}
			landed = append(landed, underlay.Quote(ctx, l.S, ids)...)
		}
		if p.Kind == core.PartOwner || p.Kind == core.PartReading || p.Kind == core.PartQuestion || p.Kind == core.PartClock {
			cue = append(append(cue, p.Text...), ' ')
		}
	}
	for i := len(l.tail) - 1; i >= 0 && last == nil; i-- {
		for _, p := range l.tail[i].Parts {
			if p.Kind == core.PartAgent {
				last = append(last, p.Text...)
			}
		}
	}
	inView := map[int64]bool{}
	rows, err := l.S.DB.QueryContext(ctx, `SELECT id FROM fragments WHERE turn >= ?`, l.tailFrom)
	if err == nil {
		for rows.Next() {
			var id int64
			rows.Scan(&id)
			inView[id] = true
		}
		rows.Close()
	}
	spot, shown := l.A.Spotlight(ctx, rt.Model.ID, l.budget(ctx, rt), cue, last, inView, l.Heard, float64(l.tailFrom+int64(len(l.tail))))
	return append(landed, spot...), shown
}

// settle is the outcome ledger, first in each turn: each reading settles its call by its
// exit code; a failure is a redline (an open loop) that a success of the same step closes.
func (l *Loop) settle(ctx context.Context, parts []core.Part) {
	if l.R == nil {
		return
	}
	n := l.tailFrom + int64(len(l.tail)) - 1
	for _, p := range parts {
		if p.Kind != core.PartReading {
			continue
		}
		f := strings.Fields(p.Meta)
		if len(f) < 2 {
			continue
		}
		id, exit := f[0], f[1]
		var call core.Part
		for i := len(l.tail) - 1; i >= 0; i-- {
			for _, c := range l.tail[i].Parts {
				if c.Kind == core.PartCall && strings.HasPrefix(c.Meta, id+" ") {
					call = c
				}
			}
			if call.Meta != "" {
				break
			}
		}
		if call.Meta == "" || strings.HasPrefix(string(p.Text), "drafted") || strings.HasPrefix(string(p.Text), "held") {
			continue
		}
		_, name, _ := strings.Cut(call.Meta, " ")
		st := step(name, call.Text)
		ok := exit == "0"
		l.R.Charge(ctx, n, id, call.Text, ok, "exit")
		if ok {
			l.S.DB.ExecContext(ctx, `UPDATE loops SET closed_turn=? WHERE kind='redline' AND step=? AND closed_turn IS NULL`, n, st)
		} else {
			text := string(p.Text)
			if len(text) > 400 {
				text = text[:400]
			}
			l.S.DB.ExecContext(ctx, `UPDATE loops SET closed_turn=? WHERE kind='redline' AND step=? AND closed_turn IS NULL`, n, st) // a newer reading of the same failure takes its place
			l.S.DB.ExecContext(ctx, `INSERT INTO loops(kind, step, text, opened_turn, call_turn, call_id) VALUES('redline', ?, ?, ?, ?, ?)`, st, name+" "+string(call.Text)+" → "+text, n, n, id)
		}
	}
}

func outputParts(r models.Response, at string) []core.Part {
	var out []core.Part
	for _, b := range r.Content {
		switch b.Type {
		case "text":
			out = append(out, core.Part{Kind: core.PartAgent, Writer: core.Agent, At: at, Text: []byte(b.Text)})
		case "tool_use":
			in := b.Input
			if len(in) == 0 {
				in = json.RawMessage("{}")
			}
			out = append(out, core.Part{Kind: core.PartCall, Writer: core.Agent, At: at, Meta: b.ID + " " + b.Name, Text: in})
		}
	}
	return out
}

func (l *Loop) spend(ctx context.Context, model string, u models.Usage) {
	day := l.Clock.Now().UTC().Format("2006-01-02")
	l.S.DB.ExecContext(ctx, `INSERT INTO spend(model, day, tokens_in, tokens_out, tokens_cached) VALUES(?,?,?,?,?)
		ON CONFLICT(model, day) DO UPDATE SET tokens_in=tokens_in+excluded.tokens_in, tokens_out=tokens_out+excluded.tokens_out, tokens_cached=tokens_cached+excluded.tokens_cached`,
		model, day, u.In+u.CacheWrite, u.Out, u.CacheRead)
}

// reading builds a reading part: Meta is "<call id> <exit> <class> <outside 0|1> <heard|->".
func reading(at, id string, exit int, class tools.Class, outside, heard bool, text []byte) core.Part {
	o, h := "0", "-"
	if outside {
		o = "1"
	}
	if heard {
		h = "heard"
	}
	return core.Part{Kind: core.PartReading, Writer: core.Tool, At: at, Meta: fmt.Sprintf("%s %d %s %s %s", id, exit, strings.ReplaceAll(string(class), " ", "_"), o, h), Text: text}
}

// runOrDraft applies the draft rule to one sealed call and returns its reading.
func (l *Loop) runOrDraft(ctx context.Context, turn int64, opener core.Opener, ownerAt string, v view, c core.Part, held bool) core.Part {
	id, name, _ := strings.Cut(c.Meta, " ")
	class := l.Tools.ClassOf(name, c.Text)
	at := l.now()
	if held {
		l.S.DB.ExecContext(ctx, `UPDATE calls SET state='held' WHERE turn=? AND call_id=?`, turn, id)
		l.emit(ctx, "step", map[string]any{"tool": name, "summary": "held: the same call just failed", "state": "held"}, turn)
		return reading(at, id, 1, class, false, false, []byte("held: this is the call that just failed, word for word; it did not run. Issued once more, it runs."))
	}
	// A repeat of a draft, word for word, in an exchange an owner message opened after it was shown, runs the stored bytes.
	if d, ok := l.matchingDraft(ctx, name, c.Text); ok {
		if opener == core.ByOwner && ownerAt > d.shownAt {
			return l.runDraft(ctx, turn, id, name, class, d)
		}
	}
	reaches := l.Tools.ReachesOut(name, c.Text)
	draft := false
	switch class {
	case tools.Read:
		draft = reaches && !opener.WordToGoOut()
	case tools.Local:
		draft = v.someone()
	default:
		draft = !(l.hasStanding(ctx, name) && !v.someone())
	}
	if draft {
		return l.makeDraft(ctx, turn, id, name, class, c.Text)
	}
	return l.run(ctx, turn, id, name, class, c.Text)
}

func (l *Loop) run(ctx context.Context, turn int64, id, name string, class tools.Class, input []byte) core.Part {
	t, ok := l.Tools.Get(name)
	if !ok {
		return reading(l.now(), id, 127, class, false, false, []byte("no such tool: "+name))
	}
	// Write-ahead: the sheet is sealed; mark the call running durably before it runs.
	l.S.Durable(ctx, `UPDATE calls SET state='running' WHERE turn=? AND call_id=?`, turn, id)
	l.emit(ctx, "step", map[string]any{"tool": name, "summary": summarize(input), "state": "running", "browser": name == "browse"}, turn)
	res, err := t.Run(ctx, input)
	state := "done"
	if err != nil {
		res = tools.Result{Reading: []byte(err.Error()), Exit: 1}
	}
	if res.Exit != 0 {
		state = "failed"
	}
	l.S.DB.ExecContext(ctx, `UPDATE calls SET state=?, exit_code=? WHERE turn=? AND call_id=?`, state, res.Exit, turn, id)
	l.emit(ctx, "step", map[string]any{"tool": name, "summary": summarize(input), "state": state, "browser": name == "browse"}, turn)
	return reading(l.now(), id, res.Exit, class, res.Outside, name == "lookup" && res.Exit == 0, res.Reading)
}

func summarize(input []byte) string {
	s := string(input)
	if len(s) > 200 {
		s = s[:200]
	}
	return s
}

type draftRow struct {
	id      int64
	shownAt string
	payload []byte
}

func (l *Loop) matchingDraft(ctx context.Context, name string, input []byte) (draftRow, bool) {
	var d draftRow
	err := l.S.DB.QueryRowContext(ctx, `SELECT id, shown_at, payload FROM drafts WHERE tool=? AND call=? AND state='waiting' ORDER BY id DESC LIMIT 1`, name, input).Scan(&d.id, &d.shownAt, &d.payload)
	return d, err == nil
}

func (l *Loop) makeDraft(ctx context.Context, turn int64, id, name string, class tools.Class, input []byte) core.Part {
	shown := string(input)
	at := l.now()
	// The repeat that is drafted again supersedes the earlier waiting draft of the same call.
	l.S.DB.ExecContext(ctx, `UPDATE drafts SET state='superseded' WHERE tool=? AND call=? AND state='waiting'`, name, input)
	res, err := l.S.DB.ExecContext(ctx, `INSERT INTO drafts(turn, call_id, step, tool, call, payload, shown, state, shown_at) VALUES(?,?,?,?,?,?,?, 'waiting', ?)`,
		turn, id, step(name, input), name, input, input, shown, at)
	if err != nil {
		return reading(at, id, 1, class, false, false, []byte("draft not recorded: "+err.Error()))
	}
	did, _ := res.LastInsertId()
	l.S.DB.ExecContext(ctx, `UPDATE calls SET state='drafted' WHERE turn=? AND call_id=?`, turn, id)
	l.S.DB.ExecContext(ctx, `INSERT INTO standing(step) VALUES(?) ON CONFLICT(step) DO UPDATE SET drafts=drafts+1`, step(name, input))
	l.emit(ctx, "draft", map[string]any{"draft_id": did, "step": step(name, input), "shown": shown, "state": "waiting", "record": l.record(ctx, step(name, input))}, turn)
	return reading(at, id, 0, class, false, false, []byte(fmt.Sprintf("drafted (draft %d): shown to the owner exactly as it would run; it waits for the owner's word", did)))
}

func (l *Loop) runDraft(ctx context.Context, turn int64, id, name string, class tools.Class, d draftRow) core.Part {
	rd := l.run(ctx, turn, id, name, class, d.payload)
	state := "ran"
	if f := strings.Fields(rd.Meta); len(f) > 1 && f[1] != "0" {
		state = "failed"
	}
	l.S.DB.ExecContext(ctx, `UPDATE drafts SET state=?, settled_turn=? WHERE id=?`, state, turn, d.id)
	l.S.DB.ExecContext(ctx, `UPDATE standing SET as_drafted=as_drafted+1 WHERE step=?`, step(name, d.payload))
	l.emit(ctx, "draft", map[string]any{"draft_id": d.id, "state": state}, turn)
	return rd
}

// step names a call's step: the tool and its first argument (TP-07).
func step(name string, input []byte) string {
	var m map[string]json.RawMessage
	if json.Unmarshal(input, &m) != nil {
		return name
	}
	// The first argument as written in the call's own bytes.
	dec := json.NewDecoder(bytes.NewReader(input))
	dec.Token()
	if tok, err := dec.Token(); err == nil {
		if k, ok := tok.(string); ok {
			var v any
			json.Unmarshal(m[k], &v)
			if s, ok := v.(string); ok {
				f := strings.Fields(s)
				if len(f) > 0 {
					return name + " " + f[0]
				}
			}
		}
	}
	return name
}

func (l *Loop) record(ctx context.Context, st string) map[string]int {
	var a, c, t int
	l.S.DB.QueryRowContext(ctx, `SELECT as_drafted, changed, turned_down FROM standing WHERE step=?`, st).Scan(&a, &c, &t)
	return map[string]int{"as_drafted": a, "changed": c, "turned_down": t}
}

func (l *Loop) hasStanding(ctx context.Context, name string) bool {
	var n int
	l.S.DB.QueryRowContext(ctx, `SELECT COUNT(*) FROM standing WHERE has_standing=1 AND (step=? OR step LIKE ?)`, name, name+" %").Scan(&n)
	return n > 0
}

var _ = time.Second
