package grammar

import (
	"bytes"
	"context"
	"fmt"
	"sort"

	"plebian/server/core"
)

type sleepRow struct {
	table string
	id    int64
	body  string
	sha   []byte
	ids   []int64
}

// Sleep is the grammar's part of the sleep pass (TP-08): it rebuilds the labels from
// the rows themselves by Re-Pair (Larsson & Moffat 1999; Sequitur's offline twin):
// starting from the words, every non-overlapping occurrence of the most frequent pair
// becomes one label, again and again, until no pair of symbols appears twice. Each label
// is born with at least two occurrences and keeps them through the labels that hold it
// (TP-T12); labels the rows no longer need are retired, never deleted. The result
// depends only on the rows, so the same store always sleeps into the same grammar. Every
// respelling must rebuild the bytes its hash fixed (TP-T14) before it is written. It
// records no uses and never runs on the reply path.
// shortcut: each round rescans every row (O(rows × labels)); Re-Pair's linear-time
// priority queue replaces it when stores reach millions of turns.
func (g *G) Sleep(ctx context.Context) error {
	var rows []sleepRow
	for _, t := range []string{"turns", "heard"} {
		r, err := g.S.DB.QueryContext(ctx, `SELECT id, spelling, sha FROM `+t+` ORDER BY id`)
		if err != nil {
			return err
		}
		type raw struct {
			x  sleepRow
			sp []byte
		}
		var raws []raw
		for r.Next() {
			x := sleepRow{table: t, body: Thread}
			var sp []byte
			if err := r.Scan(&x.id, &sp, &x.sha); err != nil {
				r.Close()
				return err
			}
			if t == "heard" {
				x.body = Heard
			}
			raws = append(raws, raw{x, sp})
		}
		r.Close()
		for _, w := range raws {
			b, err := g.Expand(w.x.body, w.sp)
			if err != nil {
				return err
			}
			x := w.x
			for _, tok := range Tokenize(b) {
				g.mu.Lock()
				id, err := g.terminal(ctx, tok)
				g.mu.Unlock()
				if err != nil {
					return err
				}
				x.ids = append(x.ids, id)
			}
			rows = append(rows, x)
		}
	}
	active := map[int64]bool{}
	for {
		counts := map[[2]int64]int{}
		for _, x := range rows {
			for i := 0; i+1 < len(x.ids); i++ {
				if i > 0 && x.ids[i-1] == x.ids[i] && x.ids[i] == x.ids[i+1] {
					continue // an overlapping run counts once
				}
				counts[[2]int64{x.ids[i], x.ids[i+1]}]++
			}
		}
		var best [2]int64
		bestN := 1
		for p, n := range counts {
			if n > bestN || (n == bestN && n >= 2 && (p[0] < best[0] || (p[0] == best[0] && p[1] < best[1]))) {
				best, bestN = p, n
			}
		}
		if bestN < 2 {
			break
		}
		g.mu.Lock()
		id, ok := g.label[best]
		if !ok {
			var err error
			if id, err = g.newLabel(ctx, best[0], best[1]); err != nil {
				g.mu.Unlock()
				return err
			}
		}
		g.mu.Unlock()
		active[id] = true
		for k := range rows {
			ids := rows[k].ids
			out := ids[:0:0]
			for i := 0; i < len(ids); i++ {
				if i+1 < len(ids) && ids[i] == best[0] && ids[i+1] == best[1] {
					out = append(out, id)
					i++
					continue
				}
				out = append(out, ids[i])
			}
			rows[k].ids = out
		}
	}
	g.mu.Lock()
	for p, id := range g.label {
		if !active[id] {
			delete(g.label, p)
			g.S.DB.ExecContext(ctx, `UPDATE symbols SET retired=1 WHERE id=?`, id)
		}
	}
	for id := range active {
		g.S.DB.ExecContext(ctx, `UPDATE symbols SET retired=0 WHERE id=?`, id)
	}
	g.mu.Unlock()
	if _, err := g.S.DB.ExecContext(ctx, `DELETE FROM pairs`); err != nil {
		return err
	}
	for _, x := range rows {
		b, err := g.expandIDs(x.ids)
		if err != nil {
			return err
		}
		if h := core.Hash(b); !bytes.Equal(h[:], x.sha) {
			return fmt.Errorf("grammar: respelling %s %d does not rebuild its bytes", x.table, x.id)
		}
		if _, err := g.S.DB.ExecContext(ctx, `UPDATE `+x.table+` SET spelling=? WHERE id=?`, encode(x.ids), x.id); err != nil {
			return err
		}
		for i := 0; i+1 < len(x.ids); i++ {
			g.S.DB.ExecContext(ctx, `INSERT INTO pairs(left_sym, right_sym, row_sha, pos) VALUES(?,?,?,?)`, x.ids[i], x.ids[i+1], x.sha, i)
		}
	}
	if g.S.Cache != nil {
		g.S.Cache()
	}
	return nil
}

func (g *G) respell(ctx context.Context, b []byte) []int64 {
	g.mu.Lock()
	defer g.mu.Unlock()
	ids, _, _ := g.spell(ctx, Tokenize(b), false)
	return ids
}

// occurrences counts each active label's appearances: in the rows, and through every
// active label that holds it (parents are formed after their parts, so higher ids first).
func (g *G) occurrences(rows []sleepRow) map[int64]int {
	occ := map[int64]int{}
	for _, x := range rows {
		for _, id := range x.ids {
			occ[id]++
		}
	}
	var labels []int64
	for _, id := range g.label {
		labels = append(labels, id)
	}
	sort.Slice(labels, func(i, j int) bool { return labels[i] > labels[j] })
	for _, id := range labels {
		sy := g.syms[id]
		if sy == nil || occ[id] == 0 {
			continue
		}
		occ[sy.left] += occ[id]
		occ[sy.right] += occ[id]
	}
	return occ
}

func (g *G) expandIDs(ids []int64) ([]byte, error) {
	g.mu.Lock()
	defer g.mu.Unlock()
	var out bytes.Buffer
	for _, id := range ids {
		if err := g.expand(id, &out, 0); err != nil {
			return nil, err
		}
	}
	return out.Bytes(), nil
}

// Check reports the grammar's invariants as the rows spell it: no pair of symbols twice,
// no active label occurring fewer than twice (TP-T12).
func (g *G) Check(ctx context.Context) error {
	var rows []sleepRow
	for _, t := range []string{"turns", "heard"} {
		r, err := g.S.DB.QueryContext(ctx, `SELECT spelling FROM `+t)
		if err != nil {
			return err
		}
		for r.Next() {
			var sp []byte
			r.Scan(&sp)
			ids, err := decode(sp)
			if err != nil {
				r.Close()
				return err
			}
			rows = append(rows, sleepRow{ids: ids})
		}
		r.Close()
	}
	counts := map[[2]int64]int{}
	for _, x := range rows {
		for i := 0; i+1 < len(x.ids); i++ {
			if i > 0 && x.ids[i-1] == x.ids[i] && x.ids[i] == x.ids[i+1] {
				continue
			}
			p := [2]int64{x.ids[i], x.ids[i+1]}
			if counts[p]++; counts[p] > 1 {
				return fmt.Errorf("pair %v appears twice", p)
			}
		}
	}
	g.mu.Lock()
	defer g.mu.Unlock()
	occ := g.occurrences(rows)
	for _, id := range g.label {
		if occ[id] == 1 {
			return fmt.Errorf("label %d (%q) occurs once", id, g.syms[id].exp)
		}
	}
	return nil
}
