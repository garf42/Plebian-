package underlay

import (
	"context"
	"fmt"
	"math"
	"sort"
	"strings"

	"plebian/server/core"
	"plebian/server/grammar"
	"plebian/server/store"
)

// U is the underlay's lamp and landings. It reads the thread only for word information;
// nothing heard writes a use, cover or outcome on a sheet (TP-T5, TP-T7).
type U struct {
	S *store.Store
	G *grammar.G
	P core.Params
	// Info is a word's information in the thread (n.recall).
	Info func(ctx context.Context, sym int64) float64
	// Open receives a landing that lit something: its rows and what they lit.
	Open func(ctx context.Context, body string) error
}

// Index indexes a landed heard row on the underlay's own clock (one tick per stretch
// or message): fragments per line, then uses in the heard body only.
func (u *U) Index(ctx context.Context, id int64) error {
	syms, err := u.fragments(ctx, id)
	if err != nil {
		return err
	}
	return u.uses(ctx, id, syms)
}

func (u *U) uses(ctx context.Context, id int64, syms []int64) error {
	var tick int64
	u.S.DB.QueryRowContext(ctx, `SELECT tick FROM heard WHERE id=?`, id).Scan(&tick)
	for _, x := range syms {
		if err := u.G.UseHeard(ctx, x, float64(tick), u.P); err != nil {
			return err
		}
	}
	return nil
}

// fragments writes a heard row's line fragments and returns the symbols they hold.
func (u *U) fragments(ctx context.Context, id int64) ([]int64, error) {
	b, err := u.S.HeardBytes(ctx, id)
	if err != nil {
		return nil, err
	}
	row, err := Parse(b)
	if err != nil {
		return nil, err
	}
	var all []int64
	off := lineOffsets(b, row)
	for i, l := range row.Lines {
		ids, err := u.G.Segment(ctx, []byte(l.Text))
		if err != nil {
			return nil, err
		}
		words := 0
		for _, t := range grammar.Tokenize([]byte(l.Text)) {
			if grammar.IsWord(t) {
				words++
			}
		}
		res, err := u.S.DB.ExecContext(ctx, `INSERT INTO fragments(heard, part, start, stop, tokens, writer, kind) VALUES(?,?,?,?,?,?, 'heard')`,
			id, i, off[i], off[i]+len(l.Text), words, string(core.HeardWriter(row.Source, l.Speaker)))
		if err != nil {
			return nil, err
		}
		fid, _ := res.LastInsertId()
		seen := map[int64]bool{}
		for _, s := range ids {
			u.G.Tree(s, func(x int64, _ bool) {
				if !seen[x] {
					seen[x] = true
				}
			})
		}
		for x := range seen {
			u.S.DB.ExecContext(ctx, `INSERT OR IGNORE INTO fragment_symbols(fragment, symbol) VALUES(?,?)`, fid, x)
			all = append(all, x)
		}
	}
	return all, nil
}

// lineOffsets finds each line's text inside the row's exact bytes.
func lineOffsets(b []byte, r Row) []int {
	out := make([]int, len(r.Lines))
	pos := len(magic) + len("source "+r.Source+"\n")
	for i, l := range r.Lines {
		pos += len(fmt.Sprintf("line %s %s %d\n", esc(l.Speaker), esc(l.At), len(l.Text)))
		out[i] = pos
		pos += len(l.Text) + 1
	}
	return out
}

// chance is −log2 of a word turning up by chance in a heard line of n words: the line
// holds it with probability 1 − (1 − p)^n, p its frequency in the heard body.
func (u *U) chance(ctx context.Context, sym int64, n int) float64 {
	total, vocab := u.G.Totals(grammar.Heard)
	p := float64(u.G.Uses(ctx, sym, grammar.Heard)+1) / float64(total+vocab+1)
	q := 1 - math.Pow(1-p, float64(n))
	if q <= 0 {
		return 64
	}
	return -math.Log2(q)
}

// wordBits is what a cue word says in a line: the lesser of its thread information and
// how unlikely it is in a line that long.
func (u *U) wordBits(ctx context.Context, sym int64, lineWords int) float64 {
	return math.Min(u.Info(ctx, sym), u.chance(ctx, sym, lineWords))
}

type hit struct {
	row, frag  int64
	line       int
	bits, best float64
}

// light scores each heard line holding the cue's words: it is lit when the words carry
// at least LampBits and one of them at least LampWordBits (BLAST-style chance scores).
func (u *U) light(ctx context.Context, cue []byte, rows map[int64]bool) []hit {
	keys := map[string]int64{}
	for _, t := range grammar.Tokenize(cue) {
		if grammar.IsWord(t) {
			if id, ok := u.G.Terminal(string(t)); ok {
				keys[grammar.Key(string(t))] = id
			}
			if id, ok := u.G.Terminal(strings.ToLower(string(t))); ok {
				keys[grammar.Key(string(t))] = id
			}
		}
	}
	var out []hit
	for _, sym := range keys {
		frs, err := u.S.DB.QueryContext(ctx, `SELECT fr.id, fr.heard, fr.part, fr.tokens FROM fragment_symbols fs JOIN fragments fr ON fr.id=fs.fragment WHERE fs.symbol=? AND fr.heard IS NOT NULL ORDER BY fr.id DESC LIMIT 256`, sym)
		if err != nil {
			continue
		}
		type f struct {
			id, row      int64
			line, tokens int
		}
		var fs []f
		for frs.Next() {
			var x f
			frs.Scan(&x.id, &x.row, &x.line, &x.tokens)
			fs = append(fs, x)
		}
		frs.Close()
		for _, x := range fs {
			if rows != nil && !rows[x.row] {
				continue
			}
			out = append(out, hit{row: x.row, frag: x.id, line: x.line})
		}
	}
	// Score each distinct line against all the cue's words it holds.
	byFrag := map[int64]*hit{}
	for i := range out {
		h := out[i]
		if _, ok := byFrag[h.frag]; ok {
			continue
		}
		hh := h
		byFrag[h.frag] = &hh
	}
	var lit []hit
	for _, h := range byFrag {
		var words, has []int64
		r, _ := u.S.DB.QueryContext(ctx, `SELECT symbol FROM fragment_symbols WHERE fragment=?`, h.frag)
		for r.Next() {
			var s int64
			r.Scan(&s)
			words = append(words, s)
		}
		r.Close()
		set := map[int64]bool{}
		for _, w := range words {
			set[w] = true
		}
		var n int
		u.S.DB.QueryRowContext(ctx, `SELECT tokens FROM fragments WHERE id=?`, h.frag).Scan(&n)
		for _, sym := range keys {
			if set[sym] {
				has = append(has, sym)
			}
		}
		for _, sym := range has {
			b := u.wordBits(ctx, sym, max(1, n))
			h.bits += b
			if b > h.best {
				h.best = b
			}
		}
		if h.bits >= u.P.LampBits && h.best >= u.P.LampWordBits {
			lit = append(lit, *h)
		}
	}
	sort.Slice(lit, func(i, j int) bool {
		if lit[i].bits != lit[j].bits {
			return lit[i].bits > lit[j].bits
		}
		return lit[i].frag < lit[j].frag
	})
	return lit
}

// Lit quotes the heard lines a cue lights past chance, each with the talk or thread
// around it, who said it and when, for one request only (assemble.Heard).
func (u *U) Lit(ctx context.Context, cue []byte) []byte {
	hs := u.light(ctx, cue, nil)
	var ids []int64
	seen := map[int64]bool{}
	for _, h := range hs {
		if !seen[h.row] {
			seen[h.row] = true
			ids = append(ids, h.row)
		}
		if len(ids) >= 3 {
			break
		}
	}
	return Quote(ctx, u.S, ids)
}

// Land handles a row that landed: it is indexed, and it lights open loops and kept
// orders whose words three of its lines hold past the wake line. A landing that lights
// nothing opens no turn; one that lights several opens one (TP-01).
func (u *U) Land(ctx context.Context, ids ...int64) error {
	// The landing is scored against the heard body as it stood before it: its own words
	// don't count toward their own chance. Its uses are recorded after.
	pending := map[int64][]int64{}
	for _, id := range ids {
		syms, err := u.fragments(ctx, id)
		if err != nil {
			return err
		}
		pending[id] = syms
	}
	defer func() {
		for _, id := range ids {
			u.uses(ctx, id, pending[id])
		}
	}()
	rows := map[int64]bool{}
	for _, id := range ids {
		rows[id] = true
	}
	type loop struct {
		id         int64
		kind, text string
	}
	r, err := u.S.DB.QueryContext(ctx, `SELECT id, kind, text FROM loops WHERE closed_turn IS NULL ORDER BY id`)
	if err != nil {
		return err
	}
	var ls []loop
	for r.Next() {
		var l loop
		r.Scan(&l.id, &l.kind, &l.text)
		ls = append(ls, l)
	}
	r.Close()
	var lit []string
	for _, l := range ls {
		hs := u.light(ctx, []byte(l.text), rows)
		lines := map[[2]int64]bool{}
		var bits float64
		for _, h := range hs {
			lines[[2]int64{h.row, int64(h.line)}] = true
			bits += h.bits
		}
		if len(lines) >= u.P.WakeLines && bits >= u.wakeLine(ctx) {
			lit = append(lit, fmt.Sprintf("%s %d", l.kind, l.id))
		}
	}
	if len(lit) == 0 || u.Open == nil {
		return nil
	}
	var b strings.Builder
	b.WriteString("rows:")
	for _, id := range ids {
		fmt.Fprintf(&b, " %d", id)
	}
	b.WriteString("\nlit: " + strings.Join(lit, ", "))
	return u.Open(ctx, b.String())
}

// wakeLine is the score a landing's lines must reach: where closed loops (decoys nothing
// can answer) would light about once in WakeDecoyEvery. shortcut: until the store has
// a decoy history of its own, the underlay lamp's line (LampBits per line, three lines)
// stands in (gap G001).
func (u *U) wakeLine(ctx context.Context) float64 {
	return u.P.LampBits * float64(u.P.WakeLines)
}
