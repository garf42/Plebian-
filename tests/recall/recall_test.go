package recall_test

import (
	"context"
	"math"
	"math/rand"
	"path/filepath"
	"sort"
	"testing"

	"plebian/server/core"
	"plebian/server/grammar"
	"plebian/server/ink"
	"plebian/server/recall"
	"plebian/server/store"
)

var ctx = context.Background()

func open(t *testing.T) (*store.Store, *grammar.G, *recall.R) {
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
	return s, g, &recall.R{S: s, G: g, P: core.Defaults}
}

func seal(t *testing.T, s *store.Store, g *grammar.G, r *recall.R, w core.Writer, kind, text string) int64 {
	t.Helper()
	sh := core.Sheet{Opener: core.ByOwner, Parts: []core.Part{{Kind: kind, Writer: w, At: "x", Meta: "c1 run", Text: []byte(text)}}}
	n, err := s.Seal(ctx, sh, store.TurnMeta{At: "x", Shown: "[]"})
	if err != nil {
		t.Fatal(err)
	}
	ix, err := g.Index(ctx, n, sh, false, core.Defaults)
	if err != nil {
		t.Fatal(err)
	}
	if err := r.Cover(ctx, ix.Fragments, n); err != nil {
		t.Fatal(err)
	}
	return n
}

// TP-03 worked example: port 8080 has been in one call, which failed: its record is 0.33,
// under the strike line.
func TestWorkedExamplePort8080(t *testing.T) {
	s, g, r := open(t)
	seal(t, s, g, r, core.Agent, core.PartAgent, "the dev server is on port 8080")
	seal(t, s, g, r, core.Agent, core.PartCall, `{"command":"curl localhost port 8080"}`)
	n := seal(t, s, g, r, core.Agent, core.PartAgent, "port 8080 is the one")
	if err := r.Charge(ctx, n, "c1", []byte("port 8080"), false, "exit"); err != nil {
		t.Fatal(err)
	}
	tau := r.Trust(ctx, []byte("port 8080"))
	if math.Abs(tau-1.0/3) > 0.02 {
		t.Fatalf("port 8080's record is %.3f, want 0.33", tau)
	}
	if !r.Struck(ctx, []byte("port 8080")) {
		t.Fatal("port 8080 is not struck")
	}
	if got := r.Trust(ctx, []byte("brand new wording")); got != 0.5 {
		t.Fatalf("untested wording at %.3f, want ½", got)
	}
}

// TP-T5: rendering a fragment adds no ink; MR-2: render any number of times ⇒ identical S.
func TestShownIsNotUsed(t *testing.T) {
	s, g, r := open(t)
	seal(t, s, g, r, core.Owner, core.PartOwner, "the gravel goes by the culvert")
	n := seal(t, s, g, r, core.Agent, core.PartAgent, "the gravel goes by the culvert, noted")
	before := r.Ink(ctx, []byte("the gravel goes by the culvert"), grammar.Thread, float64(n+1))
	for i := 0; i < 50; i++ {
		r.Lamp(ctx, []byte("gravel culvert"), float64(n+1), nil)
		r.Ink(ctx, []byte("the gravel goes by the culvert"), grammar.Thread, float64(n+1))
	}
	after := r.Ink(ctx, []byte("the gravel goes by the culvert"), grammar.Thread, float64(n+1))
	if before != after || before == 0 {
		t.Fatalf("ink moved from %v to %v by being shown", before, after)
	}
}

// TP-T6: repetition never moves trust; only outcomes write trust.
func TestRepetitionNeverMovesTrust(t *testing.T) {
	s, g, r := open(t)
	n := seal(t, s, g, r, core.Agent, core.PartAgent, "the backup runs at midnight")
	r.Charge(ctx, n, "c1", []byte("backup runs"), true, "exit")
	before := r.Trust(ctx, []byte("the backup runs at midnight"))
	for i := 0; i < 20; i++ {
		seal(t, s, g, r, core.Agent, core.PartAgent, "the backup runs at midnight")
		seal(t, s, g, r, core.Owner, core.PartOwner, "the backup runs at midnight")
	}
	if after := r.Trust(ctx, []byte("the backup runs at midnight")); after != before {
		t.Fatalf("trust moved %.4f → %.4f by repetition", before, after)
	}
}

// TP-T15 and MR-1: wall-clock gaps between turns change no value; only turns count.
func TestIdleIsFree(t *testing.T) {
	values := func(at string) float64 {
		s, g, r := open(t)
		for i := 0; i < 5; i++ {
			sh := core.Sheet{Opener: core.ByOwner, Parts: []core.Part{{Kind: core.PartOwner, Writer: core.Owner, At: at, Text: []byte("order more fence posts")}}}
			n, _ := s.Seal(ctx, sh, store.TurnMeta{At: at, Shown: "[]"})
			g.Index(ctx, n, sh, false, core.Defaults)
		}
		return r.Ink(ctx, []byte("order more fence posts"), grammar.Thread, 2000)
	}
	a, b := values("2026-11-27T10:00:00Z"), values("2036-01-01T00:00:00Z")
	if a != b {
		t.Fatalf("ink depends on wall time: %v vs %v", a, b)
	}
}

// Ledger L0001: folded ink ranks labels as the exact sum over every use does, except
// where two values lie within the deadband.
func TestInkRankMatchesReference(t *testing.T) {
	rnd := rand.New(rand.NewSource(7))
	const labels = 40
	type rec struct {
		st    ink.State
		times []float64
		ds    []float64
	}
	recs := make([]rec, labels)
	for turn := 1.0; turn <= 2000; turn++ {
		for i := range recs {
			// Each label is used with its own spacing, so histories differ in shape.
			if rnd.Float64() < 0.002*float64(i+1) {
				d := recs[i].st.Use(turn, core.Defaults.DecayK, core.Defaults.DecayAlpha)
				recs[i].times = append(recs[i].times, turn)
				recs[i].ds = append(recs[i].ds, d)
			}
		}
	}
	now := 2001.0
	type pair struct {
		i           int
		fold, exact float64
	}
	var ps []pair
	for i, r := range recs {
		if r.st.N == 0 {
			continue
		}
		ps = append(ps, pair{i, r.st.Ink(now), ink.Exact(now, r.times, r.ds)})
	}
	byFold := append([]pair(nil), ps...)
	sort.Slice(byFold, func(a, b int) bool { return byFold[a].fold > byFold[b].fold })
	for a := 0; a < len(byFold); a++ {
		for b := a + 1; b < len(byFold); b++ {
			x, y := byFold[a], byFold[b]
			if x.exact < y.exact {
				gap := math.Abs(x.exact-y.exact) / math.Max(x.exact, y.exact)
				if gap > core.Defaults.Deadband {
					t.Fatalf("labels %d and %d ranked against the reference by %.1f%% (fold %.4f %.4f, exact %.4f %.4f)", x.i, y.i, gap*100, x.fold, y.fold, x.exact, y.exact)
				}
			}
		}
	}
}

// Recall's per-lookup work does not grow with a label's uses (a sum over every use would
// be twenty times slower at 200K than at 10K).
func TestFlatPerTurn(t *testing.T) {
	var small, big ink.State
	for i := 1; i <= 10000; i++ {
		small.Use(float64(i), core.Defaults.DecayK, core.Defaults.DecayAlpha)
	}
	for i := 1; i <= 200000; i++ {
		big.Use(float64(i), core.Defaults.DecayK, core.Defaults.DecayAlpha)
	}
	r1 := testing.Benchmark(func(b *testing.B) {
		for i := 0; i < b.N; i++ {
			small.Ink(10001)
		}
	})
	r2 := testing.Benchmark(func(b *testing.B) {
		for i := 0; i < b.N; i++ {
			big.Ink(200001)
		}
	})
	t.Logf("ink: %d buckets at 10K uses (%d ns), %d at 200K (%d ns)", len(small.B), r1.NsPerOp(), len(big.B), r2.NsPerOp())
	if r2.NsPerOp() > 2*r1.NsPerOp() {
		t.Fatalf("ink at 200K uses %d ns vs %d ns at 10K", r2.NsPerOp(), r1.NsPerOp())
	}
}
