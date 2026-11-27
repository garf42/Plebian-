package loop

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"time"

	"plebian/server/core"
)

// The clock (TP-07, TP-11): a step the owner asked for ran in this window in k of the n
// periods since it began; when (k+1)/(n+2) > ½ with k ≥ 2 (Laplace's rule of
// succession) and it hasn't run in the window yet, the clock opens a turn for it. A run
// a clock turn made counts only when the owner answered that turn.
//
// shortcut: daily windows only (the same time of day within 90 minutes, a page day
// being a period); the weekday window and the date window join when a step has two
// weekly periods of its own.

const clockWidth = 90 * time.Minute // Default widths (TP-11)

// ranStep records a step that ran on the owner's word or on a clock turn.
func (l *Loop) ranStep(ctx context.Context, turn int64, opener core.Opener, name string, input []byte, rd core.Part) {
	if opener != core.ByOwner && opener != core.ByClock {
		return
	}
	var exit int
	if f := strings.Fields(rd.Meta); len(f) > 1 {
		fmt.Sscan(f[1], &exit)
	}
	if exit != 0 {
		return
	}
	l.S.DB.ExecContext(ctx, `INSERT INTO step_runs(step, tool, at, opener, turn) VALUES(?,?,?,?,?)`, step(name, input), name, l.now(), string(opener), turn)
}

func (l *Loop) loc(ctx context.Context) *time.Location {
	if z, err := time.LoadLocation(l.S.Setting(ctx, "tz")); err == nil {
		return z
	}
	return time.UTC
}

// pageDay is the page a time falls on (cut at 4 am) and its offset into that page.
func pageDay(t time.Time, loc *time.Location) (string, time.Duration) {
	lt := t.In(loc)
	cut := time.Date(lt.Year(), lt.Month(), lt.Day(), 4, 0, 0, 0, loc)
	if lt.Before(cut) {
		cut = cut.AddDate(0, 0, -1)
	}
	return cut.Format("2006-01-02"), lt.Sub(cut)
}

func dayStart(day string, loc *time.Location) time.Time {
	d, _ := time.ParseInLocation("2006-01-02", day, loc)
	return d.Add(4 * time.Hour)
}

type clockDue struct {
	step, tool string
	at         time.Time
	k, n       int
}

// clockNext finds the earliest window the clock will open (or has to open now) for any
// step the owner asked for twice.
func (l *Loop) clockNext(ctx context.Context) (clockDue, bool) {
	loc := l.loc(ctx)
	now := l.Clock.Now()
	today, _ := pageDay(now, loc)
	type run struct {
		day string
		off time.Duration
	}
	runs := map[string][]run{}
	tools := map[string]string{}
	rows, err := l.S.DB.QueryContext(ctx, `SELECT sr.step, sr.tool, sr.at, sr.opener FROM step_runs sr WHERE sr.opener='owner'
		OR EXISTS (SELECT 1 FROM owner_messages m WHERE m.received_at > sr.at AND m.received_at < strftime('%Y-%m-%dT%H:%M:%fZ', sr.at, '+1 day'))
		ORDER BY sr.at`)
	if err != nil {
		return clockDue{}, false
	}
	for rows.Next() {
		var st, tool, at, op string
		rows.Scan(&st, &tool, &at, &op)
		t, err := time.Parse(time.RFC3339Nano, at)
		if err != nil {
			continue
		}
		d, off := pageDay(t, loc)
		runs[st] = append(runs[st], run{d, off})
		tools[st] = tool
	}
	rows.Close()
	var best clockDue
	found := false
	steps := make([]string, 0, len(runs))
	for st := range runs {
		steps = append(steps, st)
	}
	sort.Strings(steps)
	for _, st := range steps {
		rs := runs[st]
		var opened int
		l.S.DB.QueryRowContext(ctx, `SELECT COUNT(*) FROM clock_opened WHERE step=? AND day=?`, st, today).Scan(&opened)
		if opened > 0 {
			continue
		}
		first := rs[0].day
		n := 0
		for d := first; d < today; d = dayStart(d, loc).AddDate(0, 0, 1).Format("2006-01-02") {
			n++
		}
		for _, cand := range rs {
			if cand.day == today {
				continue
			}
			days := map[string]bool{}
			ranToday := false
			for _, r := range rs {
				if diff := r.off - cand.off; diff <= clockWidth && diff >= -clockWidth {
					if r.day == today {
						ranToday = true
					} else {
						days[r.day] = true
					}
				}
			}
			k := len(days)
			if ranToday || k < 2 || 2*(k+1) <= n+2 {
				continue
			}
			at := dayStart(today, loc).Add(cand.off)
			if now.After(at.Add(clockWidth)) {
				continue // this window has passed today
			}
			if !found || at.Before(best.at) {
				best, found = clockDue{step: st, tool: tools[st], at: at, k: k, n: n}, true
			}
		}
	}
	return best, found
}

// clockTick opens the clock's turn when its window has come; it returns when to look again.
func (l *Loop) clockTick(ctx context.Context) time.Time {
	d, ok := l.clockNext(ctx)
	if !ok {
		return time.Time{}
	}
	if l.Clock.Now().Before(d.at) {
		return d.at
	}
	today, _ := pageDay(l.Clock.Now(), l.loc(ctx))
	res, err := l.S.DB.ExecContext(ctx, `INSERT OR IGNORE INTO clock_opened(step, day, at) VALUES(?,?,?)`, d.step, today, l.now())
	if err != nil {
		return time.Time{}
	}
	if n, _ := res.RowsAffected(); n == 1 {
		l.Enqueue(ctx, "clock", fmt.Sprintf("the owner asked for %q in this window (around %s) on %d of the %d days since it began; it has not run today",
			d.step, d.at.In(l.loc(ctx)).Format("15:04"), d.k, d.n))
	}
	return time.Time{}
}
