// Package feeds is n.feeds: mail over IMAP IDLE, calendar entries from an iCalendar
// feed, texts through a linked-device link; every message or entry lands as received in
// the blob store and as a heard row with its sender and wall time; the send paths run
// only on an approved draft.
package feeds

import (
	"bytes"
	"context"
	"database/sql"
	"fmt"
	"time"

	"plebian/server/clock"
	"plebian/server/store"
	"plebian/server/underlay"
	"plebian/server/watch"
)

// Lander puts what a feed received into the store and hands it to the underlay.
type Lander struct {
	S     *store.Store
	Clock clock.Clock
	Land  func(ctx context.Context, id int64) // the underlay's landing
}

// Put lands one message or entry: the original goes to the blob store by its sha256, the
// row to the heard table. ext is the source's own key; a row already landed under it
// with the same bytes lands nothing (MR-2), with other bytes the new row covers it (an
// edit). It returns the row id and whether anything landed.
func (l *Lander) Put(ctx context.Context, w *watch.Watch, source, ext string, at, end time.Time, row underlay.Row, original []byte) (int64, bool, error) {
	b := row.Bytes()
	var prev int64
	err := l.S.DB.QueryRowContext(ctx, `SELECT id FROM heard WHERE source=? AND ext=? AND id NOT IN (SELECT covers FROM heard WHERE covers IS NOT NULL) ORDER BY id DESC LIMIT 1`, source, ext).Scan(&prev)
	if err != nil && err != sql.ErrNoRows {
		return 0, false, err
	}
	if prev != 0 {
		old, err := l.S.HeardBytes(ctx, prev)
		if err != nil {
			return 0, false, err
		}
		if bytes.Equal(old, b) {
			return prev, false, nil
		}
	}
	sha, err := l.S.Blobs.Put(ctx, original)
	if err != nil {
		return 0, false, err
	}
	now := l.Clock.Now().UTC()
	id, err := l.S.AddHeard(ctx, store.HeardRow{Source: source, At: at.UTC().Format(time.RFC3339Nano), End: end.UTC().Format(time.RFC3339Nano),
		Bytes: b, Blob: sha, Ext: ext, Covers: prev, Landed: now.Format(time.RFC3339Nano)})
	if err != nil {
		return 0, false, err
	}
	l.S.Emit(ctx, now.Format(time.RFC3339Nano), "heard", map[string]any{"id": fmt.Sprint(id), "source": source, "at": at.UTC().Format(time.RFC3339Nano),
		"end": end.UTC().Format(time.RFC3339Nano), "lines": row.Lines, "settled": false, "blob": sha}, 0)
	if w != nil {
		w.Arrived(ctx, now)
	}
	if l.Land != nil {
		l.Land(ctx, id)
	}
	return id, true, nil
}

// cursor reads and writes a feed's position (UIDs, ETags, stream offsets).
func cursor(ctx context.Context, s *store.Store, feed string) string {
	var c string
	s.DB.QueryRowContext(ctx, `SELECT cursor FROM feeds WHERE feed=?`, feed).Scan(&c)
	return c
}

func setCursor(ctx context.Context, s *store.Store, feed, c string) error {
	_, err := s.DB.ExecContext(ctx, `INSERT INTO feeds(feed, cursor) VALUES(?,?) ON CONFLICT(feed) DO UPDATE SET cursor=excluded.cursor`, feed, c)
	return err
}

// retry is how long a feed waits after its connection fails: half the owner's usual
// time to answer, the cadence the calendar fetch keeps (gap G026).
func retry(usual time.Duration) time.Duration { return usual / 2 }
