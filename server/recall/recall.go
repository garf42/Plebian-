// Package recall is n.recall: visibility v = S × L × τ, ranked by v × λ (TP-01). S is ink
// on the wording, L the light later sheets leave, τ the record outcomes write, λ the lamp
// a cue lights. Nothing here reads the wall clock: the clock is turns (TP-T15).
package recall

import (
	"context"
	"math"
	"sort"
	"strings"

	"plebian/server/core"
	"plebian/server/grammar"
	"plebian/server/store"
)

type R struct {
	S *store.Store
	G *grammar.G
	P core.Params
}

// Info is a word's information in a body: −log2 of its frequency there, from the body's
// own uses once it has ColdStart of them, else from ordinary English (TP schedule).
func (r *R) Info(ctx context.Context, sym int64, body string) float64 {
	var total int
	r.S.DB.QueryRowContext(ctx, `SELECT COUNT(*) FROM uses WHERE body=?`, body).Scan(&total)
	if total < r.P.ColdStart {
		return r.G.EnglishBits(string(r.G.Expansion(sym)))
	}
	n := r.G.Uses(ctx, sym, body)
	var vocab int
	r.S.DB.QueryRowContext(ctx, `SELECT COUNT(*) FROM inks WHERE body=?`, body).Scan(&vocab)
	return -math.Log2(float64(n+1) / float64(total+vocab+1))
}

// word is one word of a fragment with the smallest label around it.
type word struct {
	sym, around int64
	info        float64
}

// words segments a fragment and pairs each word with the smallest label around it.
func (r *R) words(ctx context.Context, text []byte, body string) []word {
	ids, err := r.G.Segment(ctx, text)
	if err != nil {
		return nil
	}
	var out []word
	var walk func(id, around int64)
	walk = func(id, around int64) {
		l, rr, isWord := r.G.Node(id)
		if isWord {
			a := around
			if a == 0 {
				a = id
			}
			out = append(out, word{sym: id, around: a, info: r.Info(ctx, id, body)})
			return
		}
		if l == 0 {
			return // white space or punctuation carries no information
		}
		walk(l, id)
		walk(rr, id)
	}
	for _, id := range ids {
		walk(id, 0)
	}
	return out
}

// Ink is S for a fragment's text: the information-weighted average over its words of
// the ink of the smallest label around each. Reading it adds no ink (TP-T5).
func (r *R) Ink(ctx context.Context, text []byte, body string, now float64) float64 {
	ws := r.words(ctx, text, body)
	var num, den float64
	for _, w := range ws {
		num += w.info * r.G.Ink(ctx, w.around, body, now)
		den += w.info
	}
	if den == 0 {
		return 0
	}
	return num / den
}

// InfoOf is a fragment's information: the sum over its words.
func (r *R) InfoOf(ctx context.Context, text []byte, body string) float64 {
	var s float64
	for _, w := range r.words(ctx, text, body) {
		s += w.info
	}
	return s
}

// Trust is τ for a call's wording: each word's Beta record (1+a)/(2+a+b), read from the
// innermost symbol around it that has a record (labels formed later carry none), and
// weighted by the word's ordinary-English information. The store's own frequencies move
// with every repeat, so weighting by them would let repetition move trust; these don't
// (TP-T6). Untested wording sits at ½. Only outcomes write records.
func (r *R) Trust(ctx context.Context, wording []byte) float64 {
	var num, den float64
	for _, path := range r.paths(ctx, wording) {
		a, b := 0.0, 0.0
		for _, id := range path.syms { // innermost first
			var x, y float64
			r.S.DB.QueryRowContext(ctx, `SELECT rec_a, rec_b FROM symbols WHERE id=?`, id).Scan(&x, &y)
			if x+y > 0 {
				a, b = x, y
				break
			}
		}
		w := r.G.EnglishBits(string(r.G.Expansion(path.syms[0])))
		num += w * (1 + a) / (2 + a + b)
		den += w
	}
	if den == 0 {
		return 0.5
	}
	return num / den
}

// path is a word with every symbol around it, innermost first.
type path struct {
	syms []int64
	info float64
}

func (r *R) paths(ctx context.Context, text []byte) []path {
	ids, err := r.G.Segment(ctx, text)
	if err != nil {
		return nil
	}
	var out []path
	var walk func(id int64, up []int64)
	walk = func(id int64, up []int64) {
		l, rr, isWord := r.G.Node(id)
		if isWord {
			syms := append([]int64{id}, up...)
			out = append(out, path{syms: syms, info: r.Info(ctx, id, grammar.Thread)})
			return
		}
		if l == 0 {
			return
		}
		next := append([]int64{id}, up...)
		walk(l, next)
		walk(rr, next)
	}
	for _, id := range ids {
		walk(id, nil)
	}
	return out
}

// Struck reports wording under the strike line.
func (r *R) Struck(ctx context.Context, wording []byte) bool {
	return r.Trust(ctx, wording) < r.P.StrikeLine
}

// Charge settles a call: its outcome is charged to every label and word of its wording.
func (r *R) Charge(ctx context.Context, turn int64, callID string, wording []byte, success bool, kind string) error {
	col := "rec_b"
	if success {
		col = "rec_a"
	}
	seen := map[int64]bool{}
	for _, p := range r.paths(ctx, wording) {
		for _, id := range p.syms {
			if seen[id] {
				continue
			}
			seen[id] = true
			if _, err := r.S.DB.ExecContext(ctx, `UPDATE symbols SET `+col+`=`+col+`+1 WHERE id=?`, id); err != nil {
				return err
			}
		}
	}
	s := 0
	if success {
		s = 1
	}
	_, err := r.S.DB.ExecContext(ctx, `INSERT INTO outcomes(turn, call_turn, call_id, kind, success) VALUES(?,?,?,?,?)`, turn, turn, callID, kind, s)
	return err
}

// Cover lays a new sheet's fragments over earlier ones: each earlier fragment sharing
// labels loses light by the share of its information retraced while the rest changed,
// zeroed at the store's median share. A whole repeat is a use, not a cover; a tool
// repeating a reading covers it. Nothing heard covers a line of the thread.
func (r *R) Cover(ctx context.Context, frs []grammar.Fragment, turn int64) error {
	median := r.medianShare(ctx)
	for _, f := range frs {
		if f.Writer.Heard() {
			continue
		}
		info := map[int64]float64{}
		for _, s := range f.Symbols {
			info[s] = 1
		}
		// Earlier fragments the new one's distinctive labels reach (bounded per label).
		cands := map[int64]bool{}
		for _, s := range f.Symbols {
			rows, err := r.S.DB.QueryContext(ctx, `SELECT fs.fragment FROM fragment_symbols fs JOIN fragments fr ON fr.id=fs.fragment
				WHERE fs.symbol=? AND fr.turn IS NOT NULL AND fr.turn < ? AND fr.open=0 ORDER BY fs.fragment DESC LIMIT 32`, s, turn)
			if err != nil {
				return err
			}
			for rows.Next() {
				var id int64
				rows.Scan(&id)
				cands[id] = true
			}
			rows.Close()
		}
		for id := range cands {
			var start, stop int
			var tn int64
			var writer, kind string
			var light float64
			if r.S.DB.QueryRowContext(ctx, `SELECT turn, start, stop, writer, kind, light FROM fragments WHERE id=?`, id).Scan(&tn, &start, &stop, &writer, &kind, &light) != nil {
				continue
			}
			b, err := r.S.TurnBytes(ctx, tn)
			if err != nil || stop > len(b) {
				continue
			}
			old := b[start:stop]
			if string(old) == string(f.Text) {
				if f.Writer == core.Tool && kind == core.PartReading {
					r.S.DB.ExecContext(ctx, `UPDATE fragments SET light=0 WHERE id=?`, id) // the newer reading covers the older
				} else {
					r.S.DB.ExecContext(ctx, `UPDATE fragments SET light=1 WHERE id=?`, id) // a whole repeat: only later sheets count
				}
				continue
			}
			c := r.retraced(ctx, old, info)
			if c <= median {
				continue
			}
			c = (c - median) / (1 - median)
			r.S.DB.ExecContext(ctx, `UPDATE fragments SET light=light*? WHERE id=?`, 1-c, id)
		}
	}
	return nil
}

// retraced is the share of a fragment's information whose words sit in labels the new
// fragment also holds. A lone shared word is not a retrace (its label is itself).
func (r *R) retraced(ctx context.Context, text []byte, labels map[int64]float64) float64 {
	ws := r.words(ctx, text, grammar.Thread)
	var hit, all float64
	for _, w := range ws {
		all += w.info
		if w.around != w.sym && labels[w.around] > 0 {
			hit += w.info
		}
	}
	if all == 0 {
		return 0
	}
	return hit / all
}

// medianShare is the covering zero point: the median retraced share between random pairs
// of fragments, resampled in the sleep pass (kept in settings); 0 until the first pass.
func (r *R) medianShare(ctx context.Context) float64 {
	var v float64
	r.S.DB.QueryRowContext(ctx, `SELECT CAST(value AS REAL) FROM settings WHERE key='cover_zero'`).Scan(&v)
	if v < 0 || v >= 1 {
		return 0
	}
	return v
}

// Lit is a fragment the lamp lights.
type Lit struct {
	Fragment int64
	Turn     int64
	Text     string
	Lambda   float64
	V        float64
}

// Lamp lights fragments by a cue: λ = N^r, r the share of the fragment's information
// found in the cue, counted against at least the store's median line, so two common
// words can't pin a two-word line. Reached only through the cue's own symbols.
func (r *R) Lamp(ctx context.Context, cue []byte, now float64, exclude map[int64]bool) []Lit {
	cueWords := r.words(ctx, cue, grammar.Thread)
	keys := map[int64]bool{}
	for _, w := range cueWords {
		keys[w.sym] = true
	}
	var N int
	r.S.DB.QueryRowContext(ctx, `SELECT COUNT(*) FROM fragments WHERE turn IS NOT NULL`).Scan(&N)
	if N < 2 || len(keys) == 0 {
		return nil
	}
	medLine := float64(r.P.MedianLineWords) * r.meanInfo(ctx, cueWords)
	cands := map[int64]bool{}
	for k := range keys {
		rows, err := r.S.DB.QueryContext(ctx, `SELECT fs.fragment FROM fragment_symbols fs JOIN fragments fr ON fr.id=fs.fragment WHERE fs.symbol=? AND fr.turn IS NOT NULL ORDER BY fs.fragment DESC LIMIT 64`, k)
		if err != nil {
			continue
		}
		for rows.Next() {
			var id int64
			rows.Scan(&id)
			if !exclude[id] {
				cands[id] = true
			}
		}
		rows.Close()
	}
	var out []Lit
	for id := range cands {
		var tn int64
		var start, stop int
		var writer string
		var light float64
		if r.S.DB.QueryRowContext(ctx, `SELECT turn, start, stop, writer, light FROM fragments WHERE id=?`, id).Scan(&tn, &start, &stop, &writer, &light) != nil {
			continue
		}
		b, err := r.S.TurnBytes(ctx, tn)
		if err != nil || stop > len(b) {
			continue
		}
		text := b[start:stop]
		ws := r.words(ctx, text, grammar.Thread)
		var found, all float64
		for _, w := range ws {
			all += w.info
			if keys[w.sym] {
				found += w.info
			}
		}
		den := math.Max(all, medLine)
		if den == 0 || found == 0 {
			continue
		}
		lr := found / den
		lam := math.Pow(float64(N), lr)
		v := r.Ink(ctx, text, grammar.Thread, now) * light
		out = append(out, Lit{Fragment: id, Turn: tn, Text: string(text), Lambda: lam, V: v})
	}
	sort.Slice(out, func(i, j int) bool {
		a, b := out[i].V*out[i].Lambda, out[j].V*out[j].Lambda
		if a != b {
			return a > b
		}
		return out[i].Fragment < out[j].Fragment
	})
	return out
}

func (r *R) meanInfo(ctx context.Context, ws []word) float64 {
	if len(ws) == 0 {
		return 0
	}
	var s float64
	for _, w := range ws {
		s += w.info
	}
	return s / float64(len(ws))
}

// Counts is how often a word was ever used in each body, with first and last turns
// ("never said here, heard once").
type Counts struct {
	Word                string
	Thread, Heard       int
	FirstTurn, LastTurn int64
}

func (r *R) Counts(ctx context.Context, w string) Counts {
	c := Counts{Word: w}
	id, ok := r.G.Terminal(w)
	if !ok {
		id, ok = r.G.Terminal(strings.ToLower(w))
	}
	if !ok {
		return c
	}
	c.Thread = r.G.Uses(ctx, id, grammar.Thread)
	c.Heard = r.G.Uses(ctx, id, grammar.Heard)
	r.S.DB.QueryRowContext(ctx, `SELECT COALESCE(MIN(turn),0), COALESCE(MAX(turn),0) FROM uses WHERE symbol=? AND body='thread'`, id).Scan(&c.FirstTurn, &c.LastTurn)
	return c
}
