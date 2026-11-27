package underlay_test

import (
	"bytes"
	"context"
	"encoding/json"
	"math/rand"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"plebian/server/core"
	"plebian/server/grammar"
	"plebian/server/loop"
	"plebian/server/recall"
	"plebian/server/store"
	"plebian/server/underlay"
)

var ctx = context.Background()

type env struct {
	s      *store.Store
	g      *grammar.G
	u      *underlay.U
	opened []string
	tick   int
}

func open(t *testing.T) *env {
	dir := t.TempDir()
	s, err := store.Open(filepath.Join(dir, "store.db"), store.DirBlobs{Dir: filepath.Join(dir, "blobs")})
	if err != nil {
		t.Fatal(err)
	}
	g, _ := grammar.Open(ctx, s)
	s.SetSpeller(g)
	r := &recall.R{S: s, G: g, P: core.Defaults}
	e := &env{s: s, g: g}
	e.u = &underlay.U{S: s, G: g, P: core.Defaults,
		Info: func(ctx context.Context, sym int64) float64 { return r.Info(ctx, sym, grammar.Thread) },
		Open: func(ctx context.Context, body string) error { e.opened = append(e.opened, body); return nil }}
	return e
}

func (e *env) thread(t *testing.T, w core.Writer, text string) int64 {
	kind := core.PartAgent
	if w == core.Owner {
		kind = core.PartOwner
	}
	sh := core.Sheet{Opener: core.ByOwner, Parts: []core.Part{{Kind: kind, Writer: w, At: "x", Text: []byte(text)}}}
	n, err := e.s.Seal(ctx, sh, store.TurnMeta{At: "x", Shown: "[]"})
	if err != nil {
		t.Fatal(err)
	}
	e.g.Index(ctx, n, sh, false, core.Defaults)
	return n
}

func (e *env) hear(t *testing.T, lines ...string) int64 {
	e.tick++
	r := underlay.Row{Source: "talk"}
	at := time.Date(2026, 11, 27, 9, e.tick, 0, 0, time.UTC).Format(time.RFC3339)
	for i, l := range lines {
		who := "voice 1"
		if i%2 == 1 {
			who = "voice 2"
		}
		r.Lines = append(r.Lines, underlay.Line{Speaker: who, At: at, Text: l})
	}
	id, err := e.s.AddHeard(ctx, store.HeardRow{Source: "talk", At: at, End: at, Bytes: r.Bytes(), Landed: at})
	if err != nil {
		t.Fatal(err)
	}
	if err := e.u.Land(ctx, id); err != nil {
		t.Fatal(err)
	}
	return id
}

// A heard body of ordinary talk, about the size of a working week, so the underlay has
// frequencies of its own and a rare word carries its bits.
func background(t *testing.T, e *env) {
	words := strings.Fields("the a to and of it was is we you they that this have got going there here what when how about just like know think yeah okay right good fine well time day week work home truck shop yard road job call back later today tomorrow morning lunch dinner kids game weather rain cold warm coffee boss crew load drive")
	rnd := rand.New(rand.NewSource(3))
	line := func() string {
		n := 4 + rnd.Intn(6)
		w := make([]string, n)
		for i := range w {
			w[i] = words[rnd.Intn(len(words))]
		}
		return strings.Join(w, " ")
	}
	for i := 0; i < 300; i++ {
		e.hear(t, line(), line(), line(), line())
	}
}

// D1, MR-3: a landing opens a turn only when it lights an open loop or kept order;
// background talk sharing common words lights nothing.
func TestLandingOpensOnlyWhenLit(t *testing.T) {
	e := open(t)
	n := e.thread(t, core.Owner, "ask Marisol whether the hydraulic coupler shipped")
	e.s.DB.Exec(`INSERT INTO loops(kind, text, opened_turn) VALUES('order', 'ask Marisol whether the hydraulic coupler shipped', ?)`, n)
	background(t, e)
	if len(e.opened) != 0 {
		t.Fatalf("background talk opened %d turns: %v", len(e.opened), e.opened)
	}
	e.hear(t, "Marisol said the hydraulic coupler shipped Tuesday", "the coupler shipped with tracking", "hydraulic parts from Marisol arrive Friday", "okay great")
	if len(e.opened) != 1 {
		t.Fatalf("a landing lighting the kept order opened %d turns", len(e.opened))
	}
	e.hear(t, "anyway the weekend was fine", "the game was good")
	if len(e.opened) != 1 {
		t.Fatalf("talk lighting nothing opened a turn: %v", e.opened)
	}
}

// D3: a heard answer closes nothing; only the owner's reply to the agent does.
func TestHeardClosesNothing(t *testing.T) {
	e := open(t)
	n := e.thread(t, core.Agent, "did the hydraulic coupler from Marisol ship")
	e.s.DB.Exec(`INSERT INTO loops(kind, text, opened_turn) VALUES('question', 'did the hydraulic coupler from Marisol ship', ?)`, n)
	background(t, e)
	e.hear(t, "Marisol says the hydraulic coupler shipped", "coupler shipped yesterday", "the hydraulic coupler from Marisol is on the truck")
	var open int
	e.s.DB.QueryRow(`SELECT COUNT(*) FROM loops WHERE closed_turn IS NULL`).Scan(&open)
	if open != 1 {
		t.Fatalf("a heard answer closed the question (%d open)", open)
	}
	var uses int
	e.s.DB.QueryRow(`SELECT COUNT(*) FROM uses WHERE body='thread' AND turn > ?`, n).Scan(&uses)
	if uses != 0 {
		t.Fatalf("heard rows wrote %d uses on the thread", uses)
	}
}

// D2: lookup returns ids; the rows' text never enters the reading (and so never the stack).
func TestLookupReturnsIds(t *testing.T) {
	e := open(t)
	e.hear(t, "the gate code is 4417", "thanks")
	var lookup interface {
		Run(context.Context, json.RawMessage) (struct{}, error)
	}
	_ = lookup
	tl := loop.CoreTools(e.s, nil)
	for _, x := range tl {
		if x.Def().Name != "lookup" {
			continue
		}
		res, err := x.Run(ctx, json.RawMessage(`{"words":"gate code"}`))
		if err != nil {
			t.Fatal(err)
		}
		if bytes.Contains(res.Reading, []byte("4417")) || bytes.Contains(res.Reading, []byte("gate")) {
			t.Fatalf("lookup's reading copies heard text: %q", res.Reading)
		}
		if ids := underlay.IDs(res.Reading); len(ids) != 1 {
			t.Fatalf("lookup returned %v", ids)
		}
		return
	}
	t.Fatal("no lookup tool")
}
