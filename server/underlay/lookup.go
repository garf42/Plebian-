package underlay

import (
	"context"
	"fmt"
	"strconv"
	"strings"

	"plebian/server/store"
)

// Find returns heard row ids matching all given filters. shortcut: scans rows in time
// order and matches words by substring; the underlay lamp (TP-01) replaces the words
// match with surprise scoring.
func Find(ctx context.Context, s *store.Store, words, who, source, from, to string) ([]int64, error) {
	q := `SELECT id FROM heard WHERE id NOT IN (SELECT covers FROM heard WHERE covers IS NOT NULL)`
	var args []any
	if source != "" {
		q += ` AND source=?`
		args = append(args, source)
	}
	if from != "" {
		q += ` AND at >= ?`
		args = append(args, from)
	}
	if to != "" {
		q += ` AND at < ?`
		args = append(args, to)
	}
	rows, err := s.DB.QueryContext(ctx, q+` ORDER BY at, id`, args...)
	if err != nil {
		return nil, err
	}
	var all []int64
	for rows.Next() {
		var id int64
		rows.Scan(&id)
		all = append(all, id)
	}
	rows.Close()
	ws := strings.Fields(strings.ToLower(words))
	var out []int64
	for _, id := range all {
		b, err := s.HeardBytes(ctx, id)
		if err != nil {
			return nil, err
		}
		r, err := Parse(b)
		if err != nil {
			return nil, err
		}
		hit := false
		for _, l := range r.Lines {
			if who != "" && !strings.EqualFold(l.Speaker, who) {
				continue
			}
			t := strings.ToLower(l.Text)
			all := true
			for _, w := range ws {
				if !strings.Contains(t, w) {
					all = false
				}
			}
			if all {
				hit = true
			}
		}
		if hit {
			out = append(out, id)
		}
	}
	return out, nil
}

// Quote renders heard rows for one request: who said each line and when.
func Quote(ctx context.Context, s *store.Store, ids []int64) []byte {
	var b strings.Builder
	for _, id := range ids {
		raw, err := s.HeardBytes(ctx, id)
		if err != nil {
			continue
		}
		r, err := Parse(raw)
		if err != nil {
			continue
		}
		fmt.Fprintf(&b, "<heard id=\"%d\" source=\"%s\">\n", id, r.Source)
		for _, l := range r.Lines {
			fmt.Fprintf(&b, "%s · %s: %s\n", l.Speaker, l.At, l.Text)
		}
		b.WriteString("</heard>\n")
	}
	return []byte(b.String())
}

// IDs reads the row ids out of a lookup reading.
func IDs(reading []byte) []int64 {
	var out []int64
	for _, ln := range strings.Split(string(reading), "\n")[1:] {
		if id, err := strconv.ParseInt(strings.TrimSpace(ln), 10, 64); err == nil {
			out = append(out, id)
		}
	}
	return out
}
