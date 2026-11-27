// Package watch is the silence watch every feed shares (AG-06): a quiet longer than the
// longest that feed has ever had opens one question, so a dead sign-in can't pass for a
// slow week. No fixed timeout: the line is the feed's own history.
package watch

import (
	"context"
	"database/sql"
	"fmt"
	"sync"
	"time"

	"plebian/server/clock"
	"plebian/server/store"
)

type Watch struct {
	S     *store.Store
	Clock clock.Clock
	Feed  string // omi | mail | calendar | texts
	Ask   func(ctx context.Context, text string) error

	once sync.Once
	kick chan struct{}
}

func (w *Watch) init() { w.once.Do(func() { w.kick = make(chan struct{}, 1) }) }

func parse(s sql.NullString) (time.Time, bool) {
	if !s.Valid {
		return time.Time{}, false
	}
	t, err := time.Parse(time.RFC3339Nano, s.String)
	return t, err == nil
}

func (w *Watch) state(ctx context.Context) (last time.Time, ok bool, longest time.Duration, asked bool) {
	var l sql.NullString
	var ms int64
	var a int
	err := w.S.DB.QueryRowContext(ctx, `SELECT last_landing, longest_quiet_ms, asked_quiet FROM feeds WHERE feed=?`, w.Feed).Scan(&l, &ms, &a)
	if err != nil {
		return time.Time{}, false, 0, false
	}
	last, ok = parse(l)
	return last, ok, time.Duration(ms) * time.Millisecond, a == 1
}

// Arrived records that the feed delivered something at t; the quiet it ended joins the
// feed's history.
func (w *Watch) Arrived(ctx context.Context, t time.Time) {
	w.init()
	last, ok, longest, _ := w.state(ctx)
	if ok && t.Before(last) {
		return // a late arrival ends no quiet
	}
	if ok {
		if q := t.Sub(last); q > longest {
			longest = q
		}
	}
	w.S.DB.ExecContext(ctx, `INSERT INTO feeds(feed, last_landing, longest_quiet_ms, asked_quiet) VALUES(?,?,?,0)
		ON CONFLICT(feed) DO UPDATE SET last_landing=excluded.last_landing, longest_quiet_ms=excluded.longest_quiet_ms, asked_quiet=0`,
		w.Feed, t.UTC().Format(time.RFC3339Nano), longest.Milliseconds())
	select {
	case w.kick <- struct{}{}:
	default:
	}
}

// Run waits for the current quiet to pass the longest and then asks, once per quiet.
func (w *Watch) Run(ctx context.Context) {
	w.init()
	for {
		last, ok, longest, asked := w.state(ctx)
		var fire <-chan time.Time
		if ok && longest > 0 && !asked {
			fire = w.Clock.After(last.Add(longest).Add(time.Millisecond))
		}
		select {
		case <-ctx.Done():
			return
		case <-w.kick:
		case now := <-fire:
			l2, _, _, asked2 := w.state(ctx)
			if asked2 || !l2.Equal(last) {
				continue
			}
			w.S.DB.ExecContext(ctx, `UPDATE feeds SET asked_quiet=1 WHERE feed=?`, w.Feed)
			if w.Ask != nil {
				w.Ask(ctx, fmt.Sprintf("feed %s has been quiet since %s (%s), longer than its longest quiet so far (%s)",
					w.Feed, last.UTC().Format(time.RFC3339), now.Sub(last).Round(time.Second), longest.Round(time.Second)))
			}
		}
	}
}
