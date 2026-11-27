package grammar_test

import (
	"bytes"
	"context"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"plebian/server/core"
	"plebian/server/grammar"
	"plebian/server/store"
)

var ctx = context.Background()

func open(t *testing.T) (*store.Store, *grammar.G) {
	t.Helper()
	dir := t.TempDir()
	s, err := store.Open(filepath.Join(dir, "store.db"), store.DirBlobs{Dir: filepath.Join(dir, "blobs")})
	if err != nil {
		t.Fatal(err)
	}
	g, err := grammar.Open(ctx, s)
	if err != nil {
		t.Fatal(err)
	}
	s.SetSpeller(g)
	return s, g
}

func sheet(w core.Writer, text string) core.Sheet {
	kind := core.PartAgent
	if w == core.Owner {
		kind = core.PartOwner
	}
	return core.Sheet{Opener: core.ByOwner, Parts: []core.Part{{Kind: kind, Writer: w, At: "2026-11-27T10:00:00Z", Text: []byte(text)}}}
}

func seal(t *testing.T, s *store.Store, g *grammar.G, sh core.Sheet) int64 {
	t.Helper()
	n, err := s.Seal(ctx, sh, store.TurnMeta{At: "2026-11-27T10:00:00Z", Shown: "[]"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := g.Index(ctx, n, sh, false, core.Defaults); err != nil {
		t.Fatal(err)
	}
	return n
}

// MR-3 and TP-T14: every sheet rebuilds byte for byte from its spelling alone.
func TestRebuildFromSpellingOnly(t *testing.T) {
	s, g := open(t)
	texts := []string{"copy the prod config", "copy the prod config to staging", "Staging: copy the prod config, port 8080", "café ünïcode ``` code\nblock ```\nend", "", "  spaces  and\ttabs\n\n"}
	var ids []int64
	for i, x := range texts {
		w := core.Agent
		if i%2 == 0 {
			w = core.Owner
		}
		ids = append(ids, seal(t, s, g, sheet(w, x)))
	}
	for i, id := range ids {
		var sp []byte
		s.DB.QueryRow(`SELECT spelling FROM turns WHERE id=?`, id).Scan(&sp)
		if sp[0] != grammar.Tag {
			t.Fatalf("sheet %d is not spelled by the grammar", id)
		}
		if bytes.Contains(sp, []byte(texts[i])) && len(texts[i]) > 8 {
			t.Fatalf("sheet %d keeps its raw text beside its spelling", id)
		}
		b, err := s.TurnBytes(ctx, id)
		if err != nil {
			t.Fatal(err)
		}
		sh, _ := core.ParseSheet(b)
		if string(sh.Parts[0].Text) != texts[i] {
			t.Fatalf("sheet %d rebuilt %q", id, sh.Parts[0].Text)
		}
	}
	// A fresh grammar over the same store (another process) rebuilds them too.
	g2, err := grammar.Open(ctx, s)
	if err != nil {
		t.Fatal(err)
	}
	s.SetSpeller(g2)
	if n, _, err := s.Verify(ctx); err != nil || n != len(texts) {
		t.Fatalf("verify: %d %v", n, err)
	}
}

// The second sighting forms a label, and the first sighting is credited with a use.
func TestFirstSightingCounts(t *testing.T) {
	s, g := open(t)
	seal(t, s, g, sheet(core.Agent, "the culvert pipe"))
	seal(t, s, g, sheet(core.Owner, "about the culvert pipe"))
	id, ok := g.Terminal("culvert")
	if !ok {
		t.Fatal("no culvert symbol")
	}
	var label int64
	var turns []int64
	rows, _ := s.DB.Query(`SELECT id FROM symbols WHERE left_sym IS NOT NULL AND instr(expansion, 'culvert') > 0 ORDER BY length(expansion) DESC LIMIT 1`)
	for rows.Next() {
		rows.Scan(&label)
	}
	rows.Close()
	if label == 0 {
		t.Fatalf("no label formed holding culvert (%d)", id)
	}
	r2, _ := s.DB.Query(`SELECT turn FROM uses WHERE symbol=? AND body='thread' ORDER BY turn`, label)
	for r2.Next() {
		var x int64
		r2.Scan(&x)
		turns = append(turns, x)
	}
	r2.Close()
	if len(turns) < 2 || turns[0] != 1 {
		t.Fatalf("label %q uses at turns %v: the first sighting was not credited", g.Expansion(label), turns)
	}
}

// A real word new to the thread is never "corrected" into a familiar one.
func TestRealWordStays(t *testing.T) {
	s, g := open(t)
	seal(t, s, g, sheet(core.Agent, "we need fifty yards"))
	sh := sheet(core.Owner, "fifteen yards should do it")
	n, _ := s.Seal(ctx, sh, store.TurnMeta{At: "x", Shown: "[]"})
	ix, err := g.Index(ctx, n, sh, false, core.Defaults)
	if err != nil {
		t.Fatal(err)
	}
	for _, m := range ix.NearMatches {
		if strings.EqualFold(m.Word, "fifteen") {
			t.Fatalf("fifteen recorded as a use of %q (%s)", m.Target, m.Change)
		}
	}
	// One edit from a known word, and cheaper in bits than fresh in a small store: still itself.
	for i := 0; i < 6; i++ {
		seal(t, s, g, sheet(core.Agent, "cart cart cart cart cart cart cart cart"))
	}
	shc := sheet(core.Owner, "bring the card")
	nc, _ := s.Seal(ctx, shc, store.TurnMeta{At: "x", Shown: "[]"})
	ixc, _ := g.Index(ctx, nc, shc, false, core.Defaults)
	for _, m := range ixc.NearMatches {
		if strings.EqualFold(m.Word, "card") {
			t.Fatalf("card recorded as a use of %q (%s)", m.Target, m.Change)
		}
	}
	// A typo, though, counts as the word it misspells.
	sh2 := sheet(core.Owner, "send the confg over")
	seal(t, s, g, sheet(core.Agent, "the config is ready"))
	n2, _ := s.Seal(ctx, sh2, store.TurnMeta{At: "x", Shown: "[]"})
	ix2, _ := g.Index(ctx, n2, sh2, false, core.Defaults)
	found := false
	for _, m := range ix2.NearMatches {
		if m.Word == "confg" && m.Target == "config" {
			found = true
		}
	}
	if !found {
		t.Fatalf("confg not matched to config: %+v", ix2.NearMatches)
	}
}

// Ledger L0002: a number in words and in digits are one symbol for matching.
func TestNumberForms(t *testing.T) {
	if grammar.Key("fifteen") != grammar.Key("15") {
		t.Fatal("fifteen and 15 are strangers")
	}
	if v, ok := grammar.NumberValue([]string{"three", "forty"}); !ok || v != 340 {
		t.Fatalf("three forty = %d %v", v, ok)
	}
	if v, ok := grammar.NumberValue([]string{"twenty", "five"}); !ok || v != 25 {
		t.Fatalf("twenty five = %d %v", v, ok)
	}
	s, g := open(t)
	seal(t, s, g, sheet(core.Agent, "the pipe is 340"))
	sh := sheet(core.Owner, "is it three forty")
	n, _ := s.Seal(ctx, sh, store.TurnMeta{At: "x", Shown: "[]"})
	ix, _ := g.Index(ctx, n, sh, false, core.Defaults)
	ok := false
	for _, m := range ix.NearMatches {
		if m.Target == "340" {
			ok = true
		}
	}
	if !ok {
		t.Fatalf("three forty did not match 340: %+v", ix.NearMatches)
	}
	// What is rendered stays as written.
	b, _ := s.TurnBytes(ctx, n)
	if !bytes.Contains(b, []byte("three forty")) {
		t.Fatal("rendered text changed")
	}
}

// TP-T1: the grammar package imports no model client.
func TestNoModelImports(t *testing.T) {
	for _, pkg := range []string{"grammar", "ink", "recall", "underlay", "store"} {
		dir := filepath.Join("..", "..", "server", pkg)
		files, _ := filepath.Glob(filepath.Join(dir, "*.go"))
		for _, f := range files {
			af, err := parser.ParseFile(token.NewFileSet(), f, nil, parser.ImportsOnly)
			if err != nil {
				t.Fatal(err)
			}
			for _, im := range af.Imports {
				p := strings.Trim(im.Path.Value, `"`)
				if strings.Contains(p, "server/models") || strings.Contains(p, "server/loop") || strings.Contains(p, "anthropic") || strings.Contains(p, "openai") {
					t.Fatalf("%s imports %s", f, p)
				}
			}
		}
	}
	if _, err := os.Stat(filepath.Join("..", "..", "server", "grammar")); err != nil {
		t.Fatal(err)
	}
}

// MR-1: the same sheets in two separate stores give identical spellings.
func TestSameSheetsSameSpellings(t *testing.T) {
	texts := []string{"copy the prod config", "copy the prod config to staging", "the config", "port 8080 port 8080"}
	spell := func() [][]byte {
		s, g := open(t)
		var out [][]byte
		for _, x := range texts {
			n := seal(t, s, g, sheet(core.Agent, x))
			var sp []byte
			s.DB.QueryRow(`SELECT spelling FROM turns WHERE id=?`, n).Scan(&sp)
			out = append(out, sp)
		}
		return out
	}
	a, b := spell(), spell()
	for i := range a {
		if !bytes.Equal(a[i], b[i]) {
			t.Fatalf("sheet %d spelled differently", i)
		}
	}
}
