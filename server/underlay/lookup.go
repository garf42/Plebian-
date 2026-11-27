package underlay

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"

	"plebian/server/models"
	"plebian/server/store"
	"plebian/server/tools"
)

// Lookup is the one read on the underlay: by words, who, source and time. It returns
// row ids; the rows are quoted for the next request only (Quote) and never copied into
// the stack.
func Lookup(s *store.Store) tools.Tool {
	return tools.Func{
		D: models.Tool{Name: "lookup", Description: "Read the underlay (talk, mail, texts, calendar overheard) by words, who, source and time; returns row ids, and the rows are quoted beside the result.",
			Schema: tools.Schema(nil, map[string][2]string{
				"words":  {"string", "words to find"},
				"who":    {"string", "speaker or sender"},
				"source": {"string", "talk, mail, text or calendar"},
				"from":   {"string", "RFC 3339 start"},
				"to":     {"string", "RFC 3339 end"},
			})},
		C: tools.Read,
		RunFunc: func(ctx context.Context, in json.RawMessage) (tools.Result, error) {
			var q struct{ Words, Who, Source, From, To string }
			if err := json.Unmarshal(in, &q); err != nil {
				return tools.Result{Reading: []byte("bad input: " + err.Error()), Exit: 2}, nil
			}
			ids, err := Find(ctx, s, q.Words, q.Who, q.Source, q.From, q.To)
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
