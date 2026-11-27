// Package grammar is n.grammar (TP-03A): the store compresses itself as a note-taker who
// hates writing anything twice. A pair of symbols seen twice becomes a label; labels hold
// labels; a row is spelled in labels and loose words, and expanding it returns its exact
// bytes. The package imports no model client (TP-T1).
package grammar

import (
	"bufio"
	"bytes"
	"compress/gzip"
	"context"
	_ "embed"
	"encoding/binary"
	"errors"
	"fmt"
	"math"
	"strconv"
	"strings"
	"sync"
	"unicode"
	"unicode/utf8"

	"plebian/server/store"
)

//go:embed wordfreq_en.tsv.gz
var wordfreqGz []byte // Speer, wordfreq 3.1.1 small_en (CC-BY-SA 4.0); centibels below 1

// Tag is the spelling format: 0x01, then uvarint count, then uvarint symbol ids.
const Tag = 0x01

type symbol struct {
	left, right int64 // 0 for a terminal
	exp         []byte
	key         string // the matching key (number words and digits share one)
}

// G is the grammar: one label space shared by the thread and the underlay (TP-03).
type G struct {
	S     *store.Store
	mu    sync.Mutex
	syms  map[int64]*symbol
	term  map[string]int64
	label map[[2]int64]int64
	freq  map[string]float64
	floor float64
	// formed holds the labels the latest Spell of a row formed, by the row's sha256, until
	// Index credits their first sightings.
	formed map[string][]Formed
}

// Open loads the grammar's symbols from the store.
func Open(ctx context.Context, s *store.Store) (*G, error) {
	g := &G{S: s, syms: map[int64]*symbol{}, term: map[string]int64{}, label: map[[2]int64]int64{}, formed: map[string][]Formed{}}
	if err := g.loadFreq(); err != nil {
		return nil, err
	}
	rows, err := s.DB.QueryContext(ctx, `SELECT id, expansion, COALESCE(left_sym,0), COALESCE(right_sym,0) FROM symbols ORDER BY id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var id, l, r int64
		var exp []byte
		if err := rows.Scan(&id, &exp, &l, &r); err != nil {
			return nil, err
		}
		sy := &symbol{left: l, right: r, exp: exp, key: Key(string(exp))}
		g.syms[id] = sy
		if l == 0 {
			g.term[string(exp)] = id
		} else {
			g.label[[2]int64{l, r}] = id
		}
	}
	return g, rows.Err()
}

func (g *G) loadFreq() error {
	zr, err := gzip.NewReader(bytes.NewReader(wordfreqGz))
	if err != nil {
		return err
	}
	g.freq = map[string]float64{}
	sc := bufio.NewScanner(zr)
	lowest := 0
	for sc.Scan() {
		cb, w, ok := strings.Cut(sc.Text(), "\t")
		if !ok {
			continue
		}
		n, _ := strconv.Atoi(cb)
		g.freq[w] = math.Pow(10, -float64(n)/100)
		if n > lowest {
			lowest = n
		}
	}
	// A word the list doesn't hold is at most as frequent as its rarest entry.
	g.floor = math.Pow(10, -float64(lowest)/100)
	return sc.Err()
}

// English reports a word the ordinary-English list holds (a real word).
func (g *G) English(w string) bool { _, ok := g.freq[strings.ToLower(w)]; return ok }

// EnglishBits is a word's cost written fresh: −log2 of its ordinary English frequency.
func (g *G) EnglishBits(w string) float64 {
	f, ok := g.freq[strings.ToLower(w)]
	if !ok {
		f = g.floor
	}
	return -math.Log2(f)
}

// Tokenize splits bytes into tokens whose concatenation is the input exactly: runs of
// letters, digits and inner apostrophes; runs of white space; any other rune alone.
func Tokenize(b []byte) [][]byte {
	var out [][]byte
	for i := 0; i < len(b); {
		r, n := utf8.DecodeRune(b[i:])
		j := i + n
		switch {
		case unicode.IsLetter(r) || unicode.IsDigit(r):
			for j < len(b) {
				r2, n2 := utf8.DecodeRune(b[j:])
				if unicode.IsLetter(r2) || unicode.IsDigit(r2) {
					j += n2
					continue
				}
				if (r2 == '\'' || r2 == '’') && j+n2 < len(b) {
					r3, _ := utf8.DecodeRune(b[j+n2:])
					if unicode.IsLetter(r3) {
						j += n2
						continue
					}
				}
				break
			}
		case unicode.IsSpace(r):
			for j < len(b) {
				r2, n2 := utf8.DecodeRune(b[j:])
				if !unicode.IsSpace(r2) {
					break
				}
				j += n2
			}
		}
		out = append(out, b[i:j])
		i = j
	}
	return out
}

// IsWord reports a token of letters or digits.
func IsWord(t []byte) bool {
	r, _ := utf8.DecodeRune(t)
	return unicode.IsLetter(r) || unicode.IsDigit(r)
}

func (g *G) terminal(ctx context.Context, tok []byte) (int64, error) {
	if id, ok := g.term[string(tok)]; ok {
		return id, nil
	}
	res, err := g.S.DB.ExecContext(ctx, `INSERT INTO symbols(body, expansion) VALUES('', ?)`, tok)
	if err != nil {
		return 0, err
	}
	id, _ := res.LastInsertId()
	g.syms[id] = &symbol{exp: append([]byte(nil), tok...), key: Key(string(tok))}
	g.term[string(tok)] = id
	return id, nil
}

func (g *G) newLabel(ctx context.Context, a, b int64) (int64, error) {
	exp := append(append([]byte(nil), g.syms[a].exp...), g.syms[b].exp...)
	res, err := g.S.DB.ExecContext(ctx, `INSERT INTO symbols(body, expansion, left_sym, right_sym) VALUES('', ?, ?, ?)`, exp, a, b)
	if err != nil {
		return 0, err
	}
	id, _ := res.LastInsertId()
	g.syms[id] = &symbol{left: a, right: b, exp: exp, key: Key(string(exp))}
	g.label[[2]int64{a, b}] = id
	return id, nil
}

// seen reports whether a pair occurs in an earlier row, and where first.
func (g *G) seen(ctx context.Context, a, b int64) (bool, []byte, int) {
	var row []byte
	var pos int
	err := g.S.DB.QueryRowContext(ctx, `SELECT row_sha, pos FROM pairs WHERE left_sym=? AND right_sym=? ORDER BY rowid LIMIT 1`, a, b).Scan(&row, &pos)
	return err == nil, row, pos
}

// Formed is a label formed on its second sighting; First names the row its first
// sighting is in, so that sighting is credited too.
type Formed struct {
	Label    int64
	FirstRow []byte
}

// spell reduces tokens to symbols: known labels replace their pairs, and a pair seen
// before (in an earlier row or earlier in this one) becomes a new label.
func (g *G) spell(ctx context.Context, toks [][]byte, create bool) ([]int64, []Formed, error) {
	var st []int64
	var formed []Formed
	local := map[[2]int64]int{} // pairs earlier in this row, by position
	for _, t := range toks {
		id, err := g.terminal(ctx, t)
		if err != nil {
			return nil, nil, err
		}
		st = append(st, id)
		for len(st) >= 2 {
			p := [2]int64{st[len(st)-2], st[len(st)-1]}
			if l, ok := g.label[p]; ok {
				st = append(st[:len(st)-2], l)
				continue
			}
			if !create {
				break
			}
			if pos, ok := local[p]; ok && pos < len(st)-2 {
				l, err := g.newLabel(ctx, p[0], p[1])
				if err != nil {
					return nil, nil, err
				}
				formed = append(formed, Formed{Label: l})
				st = append(st[:len(st)-2], l)
				continue
			}
			if ok, row, _ := g.seen(ctx, p[0], p[1]); ok {
				l, err := g.newLabel(ctx, p[0], p[1])
				if err != nil {
					return nil, nil, err
				}
				formed = append(formed, Formed{Label: l, FirstRow: row})
				st = append(st[:len(st)-2], l)
				continue
			}
			local[p] = len(st) - 2
			break
		}
	}
	return st, formed, nil
}

func encode(ids []int64) []byte {
	b := []byte{Tag}
	b = binary.AppendUvarint(b, uint64(len(ids)))
	for _, id := range ids {
		b = binary.AppendUvarint(b, uint64(id))
	}
	return b
}

func decode(sp []byte) ([]int64, error) {
	if len(sp) == 0 || sp[0] != Tag {
		return nil, errors.New("grammar: not a grammar spelling")
	}
	r := bytes.NewReader(sp[1:])
	n, err := binary.ReadUvarint(r)
	if err != nil || n > uint64(len(sp)) {
		return nil, errors.New("grammar: bad spelling count")
	}
	ids := make([]int64, n)
	for i := range ids {
		v, err := binary.ReadUvarint(r)
		if err != nil {
			return nil, errors.New("grammar: truncated spelling")
		}
		ids[i] = int64(v)
	}
	if r.Len() != 0 {
		return nil, errors.New("grammar: trailing bytes in spelling")
	}
	return ids, nil
}

// Spell implements store.Speller: the row's bytes as labels and loose words. Its
// adjacent pairs are recorded so a later sighting forms a label.
func (g *G) Spell(body string, b []byte) ([]byte, error) {
	g.mu.Lock()
	defer g.mu.Unlock()
	ctx := context.Background()
	ids, formed, err := g.spell(ctx, Tokenize(b), true)
	if err != nil {
		return nil, err
	}
	sha := sha(b)
	for i := 0; i+1 < len(ids); i++ {
		if _, err := g.S.DB.ExecContext(ctx, `INSERT INTO pairs(left_sym, right_sym, row_sha, pos) VALUES(?,?,?,?)`, ids[i], ids[i+1], sha, i); err != nil {
			return nil, err
		}
	}
	g.formed[string(sha)] = formed
	return encode(ids), nil
}

// Formations returns, once, the labels formed while spelling the row with these bytes.
func (g *G) Formations(b []byte) []Formed {
	g.mu.Lock()
	defer g.mu.Unlock()
	k := string(sha(b))
	f := g.formed[k]
	delete(g.formed, k)
	return f
}

// Expand implements store.Speller: rebuild a row from its spelling through the labels'
// structure alone; a loose spelling (tag 0) from before the grammar ran still rebuilds.
func (g *G) Expand(body string, sp []byte) ([]byte, error) {
	if len(sp) > 0 && sp[0] == 0 {
		return store.Loose{}.Expand(body, sp)
	}
	ids, err := decode(sp)
	if err != nil {
		return nil, err
	}
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

func (g *G) expand(id int64, w *bytes.Buffer, depth int) error {
	sy, ok := g.syms[id]
	if !ok {
		return fmt.Errorf("grammar: unknown symbol %d", id)
	}
	if depth > 1<<16 {
		return errors.New("grammar: label cycle")
	}
	if sy.left == 0 {
		w.Write(sy.exp)
		return nil
	}
	if err := g.expand(sy.left, w, depth+1); err != nil {
		return err
	}
	return g.expand(sy.right, w, depth+1)
}

// Segment spells text with the labels that exist, forming none (for counting uses).
func (g *G) Segment(ctx context.Context, text []byte) ([]int64, error) {
	g.mu.Lock()
	defer g.mu.Unlock()
	ids, _, err := g.spell(ctx, Tokenize(text), false)
	return ids, err
}

// Tree calls fn for a symbol and every label and word inside it.
func (g *G) Tree(id int64, fn func(id int64, word bool)) {
	g.mu.Lock()
	defer g.mu.Unlock()
	g.tree(id, fn)
}

func (g *G) tree(id int64, fn func(int64, bool)) {
	sy, ok := g.syms[id]
	if !ok {
		return
	}
	if sy.left == 0 {
		if IsWord(sy.exp) {
			fn(id, true)
		}
		return
	}
	fn(id, false)
	g.tree(sy.left, fn)
	g.tree(sy.right, fn)
}

// Expansion is a symbol's text.
func (g *G) Expansion(id int64) []byte {
	g.mu.Lock()
	defer g.mu.Unlock()
	if sy, ok := g.syms[id]; ok {
		return sy.exp
	}
	return nil
}

// Terminal returns the word symbol for a token, if the grammar has one.
func (g *G) Terminal(tok string) (int64, bool) {
	g.mu.Lock()
	defer g.mu.Unlock()
	id, ok := g.term[tok]
	return id, ok
}

// Node is a symbol's structure: its two parts for a label (0, 0 for a terminal), and
// whether it is a word (letters or digits).
func (g *G) Node(id int64) (left, right int64, word bool) {
	g.mu.Lock()
	defer g.mu.Unlock()
	sy, ok := g.syms[id]
	if !ok {
		return 0, 0, false
	}
	return sy.left, sy.right, sy.left == 0 && IsWord(sy.exp)
}
