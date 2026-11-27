package loop

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"plebian/server/core"
	"plebian/server/models"
	"plebian/server/tools"
)

// Grant gives a step standing. It is reached only from a signed admin action (This week,
// behind the owner's fingerprint); no tool the agent holds calls it (TP-T9). The grant
// records the step's record τ at the moment, the line its standing holds above.
func (l *Loop) Grant(ctx context.Context, step, grantID string) error {
	step = strings.TrimSpace(step)
	if step == "" {
		return errors.New("grant needs a step")
	}
	tau := 0.5
	if l.R != nil {
		tau = l.R.Trust(ctx, []byte(step))
	}
	_, err := l.S.DB.ExecContext(ctx, `INSERT INTO standing(step, has_standing, granted_tau, grant_id) VALUES(?, 1, ?, ?)
		ON CONFLICT(step) DO UPDATE SET has_standing=1, granted_tau=excluded.granted_tau, grant_id=excluded.grant_id`, step, tau, grantID)
	if err == nil {
		l.emit(ctx, "step", map[string]any{"tool": step, "summary": "standing granted", "state": "done"}, 0)
	}
	return err
}

// lowerStanding takes standing back: a run the owner stopped or had redone, a failure.
func (l *Loop) lowerStanding(ctx context.Context, step string) {
	l.S.DB.ExecContext(ctx, `UPDATE standing SET has_standing=0 WHERE step=?`, step)
}

// hasStandingFor reports standing for a call's step, held while the step's record stays
// at or above where it was granted.
func (l *Loop) hasStandingFor(ctx context.Context, name string, input []byte) bool {
	st := step(name, input)
	var has int
	var granted float64
	if l.S.DB.QueryRowContext(ctx, `SELECT has_standing, COALESCE(granted_tau, 0) FROM standing WHERE step=?`, st).Scan(&has, &granted) != nil || has != 1 {
		return false
	}
	if l.R != nil && l.R.Trust(ctx, []byte(st)) < granted {
		l.lowerStanding(ctx, st)
		return false
	}
	return true
}

// standingRun announces a step with standing and runs it after a stop window as long as
// the owner's usual time to answer the agent; an owner message in the window stops it.
func (l *Loop) standingRun(ctx context.Context, turn int64, id, name string, class tools.Class, input []byte) core.Part {
	window := l.window(ctx)
	at := l.Clock.Now()
	ev, _ := l.S.Emit(ctx, l.now(), "step", map[string]any{"tool": name, "summary": "runs at " + at.Add(window).UTC().Format(time.RFC3339) + ": " + summarize(input), "state": "announced"}, turn)
	b, _ := json.Marshal(map[string]any{"step": step(name, input), "announced_event": ev, "runs_at": at.Add(window).UTC().Format(time.RFC3339Nano)})
	l.S.DB.ExecContext(ctx, `UPDATE calls SET state='announced', job=? WHERE turn=? AND call_id=?`, string(b), turn, id)
	select {
	case <-ctx.Done():
		return reading(l.now(), id, 1, class, false, false, []byte("stopped: the server stopped during the stop window; it did not run"))
	case <-l.Clock.After(at.Add(window)):
	}
	var stop int
	l.S.DB.QueryRowContext(ctx, `SELECT COUNT(*) FROM owner_messages WHERE event > ? AND turn IS NULL`, ev).Scan(&stop)
	if stop > 0 {
		l.lowerStanding(ctx, step(name, input))
		l.emit(ctx, "step", map[string]any{"tool": name, "summary": "stopped by the owner", "state": "failed"}, turn)
		return reading(l.now(), id, 1, class, false, false, []byte(fmt.Sprintf("stopped: the owner wrote during the stop window (%s); it did not run", window)))
	}
	return l.run(ctx, turn, id, name, class, input)
}

// HeldDraft records requests the browser holds as one draft. Its call is
// `browser_send {"held": <id>}`: the agent repeating that call in an exchange the
// owner's word opened after the draft was shown releases exactly what is held.
func (l *Loop) HeldDraft(ctx context.Context, shown string) (int64, error) {
	var turn int64
	l.S.DB.QueryRowContext(ctx, `SELECT COALESCE(MAX(id),0) FROM turns`).Scan(&turn)
	res, err := l.S.DB.ExecContext(ctx, `INSERT INTO drafts(turn, call_id, step, tool, call, payload, shown, state, shown_at) VALUES(?, '', 'browser_send', 'browser_send', '{}', '{}', ?, 'waiting', ?)`,
		turn, shown, l.now())
	if err != nil {
		return 0, err
	}
	id, _ := res.LastInsertId()
	call := fmt.Sprintf(`{"held":%d}`, id)
	l.S.DB.ExecContext(ctx, `UPDATE drafts SET call=?, payload=? WHERE id=?`, call, call, id)
	ev, _ := l.S.Emit(ctx, l.now(), "draft", map[string]any{"draft_id": id, "step": "browser_send", "shown": shown, "state": "waiting", "record": l.record(ctx, "browser_send")}, turn)
	l.S.DB.ExecContext(ctx, `UPDATE drafts SET event=? WHERE id=?`, ev, id)
	return id, nil
}

// SubCall is one call of a sub-agent on the agent's routes, held within the model's B.
func (l *Loop) SubCall(ctx context.Context, r models.Request) (models.Response, error) {
	routes, err := l.Routes(ctx)
	if err != nil {
		return models.Response{}, err
	}
	var last error = errors.New("no model configured")
	for _, rt := range routes {
		rt := rt
		r.Model = rt.Model.ID
		n, err := l.Counter.Count(ctx, &rt.Provider, r)
		if err != nil {
			last = err
			continue
		}
		B := l.budget(ctx, rt)
		if n >= B {
			return models.Response{}, fmt.Errorf("the sub-agent's context reached its B (%d tokens)", B)
		}
		r.MaxTokens = B - n
		if rt.Model.MaxOut > 0 && r.MaxTokens > rt.Model.MaxOut {
			r.MaxTokens = rt.Model.MaxOut
		}
		resp, err := models.SendWithin(ctx, &rt.Provider, r, l.window(ctx), l.Clock, nil)
		if err != nil {
			last = err
			continue
		}
		l.spend(ctx, rt.Model.ID, resp.Usage)
		return resp, nil
	}
	return models.Response{}, last
}
