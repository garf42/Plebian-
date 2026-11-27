package grammar

import (
	"bytes"
	"context"
	"encoding/json"
	"math"
	"strconv"
	"strings"
	"time"

	"plebian/server/core"
	"plebian/server/ink"
)

// Bodies: the thread (sheets) and the underlay keep their own uses and clocks (TP-03).
const (
	Thread = "thread"
	Heard  = "heard"
)

// Fragment is one indexed span: a sentence, a line or a code block.
type Fragment struct {
	ID          int64
	Part        int
	Start, Stop int // offsets into the row's exact bytes
	Writer      core.Writer
	Symbols     []int64 // every label and word in it
	Text        []byte
}

// NearMatch is a word recorded as a use of the closest label plus a small change.
type NearMatch struct {
	Word, Target, Change string
	Symbol               int64
}

type Indexed struct {
	Fragments   []Fragment
	NearMatches []NearMatch
	StandIns    []string
}

// Endings are what a known word may carry and still be the same word (restarted cues
// restart; the owner's possessive is the owner).
var Endings = []string{"s", "es", "ed", "d", "ing", "er", "ers", "'s", "’s", "ly"}

func (g *G) inkState(ctx context.Context, sym int64, body string) ink.State {
	var s ink.State
	var b string
	if g.S.DB.QueryRowContext(ctx, `SELECT state FROM inks WHERE symbol=? AND body=?`, sym, body).Scan(&b) == nil {
		json.Unmarshal([]byte(b), &s)
	}
	return s
}

// use records a use of sym at clock now with d fixed at that moment.
func (g *G) use(ctx context.Context, sym int64, body string, now float64, p core.Params, change string) error {
	st := g.inkState(ctx, sym, body)
	d := st.Use(now, p.DecayK, p.DecayAlpha)
	b, _ := json.Marshal(st)
	if _, err := g.S.DB.ExecContext(ctx, `INSERT INTO inks(symbol, body, state) VALUES(?,?,?) ON CONFLICT(symbol, body) DO UPDATE SET state=excluded.state`, sym, body, string(b)); err != nil {
		return err
	}
	var turn, tick any
	if body == Thread {
		turn = int64(now)
	} else {
		tick = int64(now)
	}
	var ch any
	if change != "" {
		ch = change
	}
	_, err := g.S.DB.ExecContext(ctx, `INSERT INTO uses(symbol, turn, tick, body, d, change) VALUES(?,?,?,?,?,?)`, sym, turn, tick, body, d, ch)
	return err
}

// Ink is a symbol's ink in a body at clock now.
func (g *G) Ink(ctx context.Context, sym int64, body string, now float64) float64 {
	return g.inkState(ctx, sym, body).Ink(now)
}

// Uses is how many uses a symbol has in a body.
func (g *G) Uses(ctx context.Context, sym int64, body string) int {
	return g.inkState(ctx, sym, body).N
}

// fragments splits a part's text into lines and sentences; a fenced code block is one.
func fragments(text []byte) [][2]int {
	var out [][2]int
	start := 0
	inCode := false
	i := 0
	emit := func(end int) {
		if len(bytes.TrimSpace(text[start:end])) > 0 {
			out = append(out, [2]int{start, end})
		}
		start = end
	}
	for i < len(text) {
		if bytes.HasPrefix(text[i:], []byte("```")) {
			if !inCode {
				emit(i)
			}
			inCode = !inCode
			i += 3
			if !inCode {
				for i < len(text) && text[i] != '\n' {
					i++
				}
				emit(i)
			}
			continue
		}
		if !inCode {
			if text[i] == '\n' {
				emit(i + 1)
			} else if (text[i] == '.' || text[i] == '?' || text[i] == '!') && i+1 < len(text) && text[i+1] == ' ' {
				emit(i + 1)
			}
		}
		i++
	}
	emit(len(text))
	return out
}

// Index indexes sealed turn n: fragments with their symbols, uses (owner and agent words
// add ink; a tool's reading counts once, when it is new), the round trip on the owner's
// words, frames, and stand-in requests. Shown is not used: nothing here reads what a
// request rendered.
func (g *G) Index(ctx context.Context, turn int64, sh core.Sheet, heardOpened bool, p core.Params) (Indexed, error) {
	var out Indexed
	b := sh.Bytes()
	now := float64(turn)
	// A first sighting counts once the second arrives.
	for _, f := range g.Formations(b) {
		if f.FirstRow != nil {
			var first int64
			if g.S.DB.QueryRowContext(ctx, `SELECT id FROM turns WHERE sha=?`, f.FirstRow).Scan(&first) == nil && first < turn {
				if err := g.use(ctx, f.Label, Thread, float64(first), p, ""); err != nil {
					return out, err
				}
			}
		}
	}
	offs := sh.TextOffsets()
	for pi, part := range sh.Parts {
		writes := part.Writer == core.Owner || part.Writer == core.Agent
		if part.Kind == core.PartCall {
			writes = true // a call is the agent's wording; its record rides on it (TP-07)
		}
		reading := part.Kind == core.PartReading
		newReading := reading && !g.seenReading(ctx, turn, part.Text)
		for _, sp := range fragments(part.Text) {
			text := part.Text[sp[0]:sp[1]]
			ids, err := g.Segment(ctx, text)
			if err != nil {
				return out, err
			}
			set := map[int64]bool{}
			var syms []int64
			for _, id := range ids {
				g.Tree(id, func(s int64, _ bool) {
					if !set[s] {
						set[s] = true
						syms = append(syms, s)
					}
				})
			}
			res, err := g.S.DB.ExecContext(ctx, `INSERT INTO fragments(turn, part, start, stop, tokens, writer, heard_opened, kind) VALUES(?,?,?,?,?,?,?,?)`,
				turn, pi, offs[pi]+sp[0], offs[pi]+sp[1], len(ids), string(part.Writer), heardOpened, part.Kind)
			if err != nil {
				return out, err
			}
			fid, _ := res.LastInsertId()
			for _, s := range syms {
				g.S.DB.ExecContext(ctx, `INSERT OR IGNORE INTO fragment_symbols(fragment, symbol) VALUES(?,?)`, fid, s)
			}
			// The round trip runs against the words the thread used before this turn; a word
			// recorded as a near-match is a use of the word it misspells, not of itself.
			matched := map[int64]bool{}
			if part.Writer == core.Owner {
				nm, si, err := g.roundTrip(ctx, text, Thread, now, p)
				if err != nil {
					return out, err
				}
				for _, m := range nm {
					for _, w := range strings.Fields(m.Word) {
						if id, ok := g.Terminal(w); ok {
							matched[id] = true
						}
					}
				}
				out.NearMatches = append(out.NearMatches, nm...)
				out.StandIns = append(out.StandIns, si...)
			}
			if writes || newReading {
				for _, s := range syms {
					if matched[s] {
						continue
					}
					if err := g.use(ctx, s, Thread, now, p, ""); err != nil {
						return out, err
					}
				}
			}
			if writes {
				g.frames(ctx, text)
			}
			out.Fragments = append(out.Fragments, Fragment{ID: fid, Part: pi, Start: offs[pi] + sp[0], Stop: offs[pi] + sp[1], Writer: part.Writer, Symbols: syms, Text: text})
		}
	}
	return out, nil
}

// seenReading reports an identical reading in an earlier turn (a repeat covers, adds no ink).
func (g *G) seenReading(ctx context.Context, turn int64, text []byte) bool {
	ids, err := g.Segment(ctx, text)
	if err != nil || len(ids) == 0 {
		return false
	}
	var n int
	g.S.DB.QueryRowContext(ctx, `SELECT COUNT(*) FROM fragments f WHERE f.kind=? AND f.turn < ? AND f.tokens=? AND f.stop-f.start=?`, core.PartReading, turn, len(ids), len(text)).Scan(&n)
	return n > 0
}

func (g *G) frames(ctx context.Context, text []byte) {
	var words []int64
	for _, t := range Tokenize(text) {
		if IsWord(t) {
			if id, ok := g.Terminal(string(t)); ok {
				words = append(words, id)
			}
		}
	}
	for i := 0; i+2 < len(words); i++ {
		g.S.DB.ExecContext(ctx, `INSERT INTO frames(left_sym, right_sym, filler, n) VALUES(?,?,?,1)`, words[i], words[i+2], words[i+1])
	}
}

// vocabulary is the words a body's writers have used, by matching key.
func (g *G) vocabulary(ctx context.Context, body string) map[string]int64 {
	out := map[string]int64{}
	rows, err := g.S.DB.QueryContext(ctx, `SELECT symbol FROM inks WHERE body=?`, body)
	if err != nil {
		return out
	}
	defer rows.Close()
	g.mu.Lock()
	defer g.mu.Unlock()
	for rows.Next() {
		var id int64
		rows.Scan(&id)
		if sy, ok := g.syms[id]; ok && sy.left == 0 && IsWord(sy.exp) {
			out[sy.key] = id
		}
	}
	return out
}

func (g *G) totalUses(ctx context.Context, body string) int {
	var n int
	g.S.DB.QueryRowContext(ctx, `SELECT COUNT(*) FROM uses WHERE body=?`, body).Scan(&n)
	return n
}

// editBits is one edit's description length, even across edit kinds until refit: its
// kind (insert, delete, substitute, transpose), its position, and its character.
func editBits(word string) float64 {
	return math.Log2(4) + math.Log2(float64(len(word)+1)) + math.Log2(27)
}

// Distance is the Damerau-Levenshtein distance (optimal string alignment).
func Distance(a, b string) int {
	ra, rb := []rune(a), []rune(b)
	d := make([][]int, len(ra)+1)
	for i := range d {
		d[i] = make([]int, len(rb)+1)
		d[i][0] = i
	}
	for j := range d[0] {
		d[0][j] = j
	}
	for i := 1; i <= len(ra); i++ {
		for j := 1; j <= len(rb); j++ {
			c := 1
			if ra[i-1] == rb[j-1] {
				c = 0
			}
			d[i][j] = min(d[i-1][j]+1, d[i][j-1]+1, d[i-1][j-1]+c)
			if i > 1 && j > 1 && ra[i-1] == rb[j-2] && ra[i-2] == rb[j-1] {
				d[i][j] = min(d[i][j], d[i-2][j-2]+1)
			}
		}
	}
	return d[len(ra)][len(rb)]
}

// roundTrip matches each new word to the closest word the body knows. Label + change is
// kept only when it is shorter to write down than the word fresh at its ordinary
// English frequency (noisy-channel rule; minimum description length).
func (g *G) roundTrip(ctx context.Context, text []byte, body string, now float64, p core.Params) ([]NearMatch, []string, error) {
	vocab := g.vocabulary(ctx, body)
	total := g.totalUses(ctx, body)
	var nm []NearMatch
	var si []string
	toks := Tokenize(text)
	var run []string
	flushNumber := func() {
		if len(run) > 1 {
			if v, ok := NumberValue(run); ok {
				key := Key(itoa(v))
				if id, ok := vocab[key]; ok {
					nm = append(nm, NearMatch{Word: strings.Join(run, " "), Target: string(g.Expansion(id)), Change: "number words", Symbol: id})
					g.use(ctx, id, body, now, p, "number words")
				}
			}
		}
		run = nil
	}
	for _, t := range toks {
		w := string(t)
		if !IsWord(t) {
			if strings.TrimSpace(w) != "" {
				flushNumber()
			}
			continue
		}
		if _, isNum := NumberValue([]string{w}); isNum {
			run = append(run, w)
		} else {
			flushNumber()
		}
		key := Key(w)
		if _, ok := vocab[key]; ok {
			continue // a word the body already uses (or the same number in digits)
		}
		fresh := g.EnglishBits(w)
		bestBits, bestID, bestChange := math.Inf(1), int64(0), ""
		pointer := func(id int64) float64 {
			u := g.Uses(ctx, id, body)
			return -math.Log2(float64(u+1) / float64(total+len(vocab)+1))
		}
		lw := strings.ToLower(w)
		for _, e := range Endings {
			if base := strings.TrimSuffix(lw, e); base != lw && base != "" {
				if id, ok := vocab[base]; ok {
					if b := pointer(id) + math.Log2(float64(len(Endings))); b < bestBits {
						bestBits, bestID, bestChange = b, id, "+"+e
					}
				}
			}
		}
		// A real word (one the English list holds) is never recorded as another word: its
		// fresh price can undercut pointer + change in a small store ("card" 13.2 bits
		// against "cart" + one edit, 13.1), so the rule is structural, not priced.
		// Endings above are the same word and stay allowed.
		maxEdits := -1
		if !g.English(lw) {
			maxEdits = int(fresh / editBits(lw)) // an edit past this can't be shorter than fresh
		}
		for k, id := range vocab {
			if dk := len(k) - len(lw); dk > maxEdits || -dk > maxEdits {
				continue
			}
			dist := Distance(lw, k)
			if dist == 0 || dist > maxEdits {
				continue
			}
			if b := pointer(id) + float64(dist)*editBits(lw); b < bestBits {
				bestBits, bestID, bestChange = b, id, k+"→"+lw
			}
		}
		if bestID != 0 && bestBits < fresh {
			nm = append(nm, NearMatch{Word: w, Target: string(g.Expansion(bestID)), Change: bestChange, Symbol: bestID})
			g.S.DB.ExecContext(ctx, `INSERT INTO near_matches(body, writer, word, symbol, change, turn) VALUES(?,?,?,?,?,?)`, body, "owner", w, bestID, bestChange, int64(now))
			if err := g.use(ctx, bestID, body, now, p, bestChange); err != nil {
				return nil, nil, err
			}
			continue
		}
		// A word the thread has never used: its stand-in row is written once, from outside.
		res, _ := g.S.DB.ExecContext(ctx, `INSERT OR IGNORE INTO stand_ins(word, proposals, written_at) VALUES(?, '', ?)`, lw, time.Now().UTC().Format(time.RFC3339))
		if n, _ := res.RowsAffected(); n == 1 {
			si = append(si, lw)
		}
	}
	flushNumber()
	return nm, si, nil
}

func itoa(v int) string { return strconv.Itoa(v) }
