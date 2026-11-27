// Package kit runs the server's loop in-process against the fixture model over HTTP, on
// a clock the test moves. It is for node tests (n.loop, n.assemble); acceptance drives
// the release build instead.
package kit

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http/httptest"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"plebian/server/assemble"
	"plebian/server/config"
	"plebian/server/core"
	"plebian/server/grammar"
	"plebian/server/loop"
	"plebian/server/models"
	"plebian/server/recall"
	"plebian/server/store"
	"plebian/server/tools"
	"plebian/tests/fixtures/model"
)

// Clock is a manual clock.
type Clock struct {
	mu  sync.Mutex
	now time.Time
}

func (c *Clock) Now() time.Time { c.mu.Lock(); defer c.mu.Unlock(); return c.now }
func (c *Clock) After(t time.Time) <-chan time.Time {
	ch := make(chan time.Time, 1)
	if !c.Now().Before(t) {
		ch <- c.Now()
	}
	return ch
}
func (c *Clock) Advance(d time.Duration) { c.mu.Lock(); c.now = c.now.Add(d); c.mu.Unlock() }

type Kit struct {
	T      testing.TB
	S      *store.Store
	G      *grammar.G
	R      *recall.R
	L      *loop.Loop
	Clock  *Clock
	Model  *model.Provider
	server *httptest.Server
	Route  models.Route
	n      int
}

// New builds a store and a loop whose one route is the fixture model in the given format.
func New(t testing.TB, format string, script model.Script, extra ...tools.Tool) *Kit {
	dir := t.TempDir()
	s, err := store.Open(filepath.Join(dir, "store.db"), store.DirBlobs{Dir: filepath.Join(dir, "blobs")})
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	g, err := grammar.Open(ctx, s)
	if err != nil {
		t.Fatal(err)
	}
	s.SetSpeller(g)
	k := &Kit{T: t, S: s, G: g, Clock: &Clock{now: time.Date(2026, 11, 27, 15, 0, 0, 0, time.UTC)}}
	k.Model = model.New("kit-key")
	if err := k.Model.SetScript(script); err != nil {
		t.Fatal(err)
	}
	k.server = httptest.NewServer(k.Model.Handler())
	t.Cleanup(k.server.Close)
	p, ms, err := models.Detect(ctx, k.server.URL+"/"+format, "kit-key", nil)
	if err != nil {
		t.Fatal(err)
	}
	k.Route = models.Route{Provider: p, Model: ms[0]}
	rs, _ := json.Marshal([]models.Route{k.Route})
	s.SetSetting(ctx, "routes", string(rs))
	sec := &config.Secrets{Path: filepath.Join(dir, "secrets.json")}
	sec.Set("provider:"+p.ID, "kit-key")
	k.R = &recall.R{S: s, G: g, P: core.Defaults}
	counter := models.NewCounter()
	reg := tools.NewRegistry(append(loop.CoreTools(s, nil), extra...)...)
	k.L = &loop.Loop{S: s, Clock: k.Clock, P: core.Defaults, Tools: reg, Counter: counter, Secrets: sec,
		Index: func(ctx context.Context, n int64, sh core.Sheet, heard bool) error {
			ix, err := g.Index(ctx, n, sh, heard, core.Defaults)
			if err != nil {
				return err
			}
			return k.R.Cover(ctx, ix.Fragments, n)
		},
		A: &assemble.Assembler{S: s, G: g, R: k.R, P: core.Defaults, Counter: counter}, R: k.R}
	if err := k.L.Init(ctx); err != nil {
		t.Fatal(err)
	}
	return k
}

// Say sends an owner message and runs the loop until it is idle.
func (k *Kit) Say(text string) {
	k.n++
	ctx := context.Background()
	now := k.Clock.Now().UTC().Format(time.RFC3339Nano)
	if _, err := k.S.DB.ExecContext(ctx, `INSERT INTO owner_messages(device, client_msg_id, text, sent_at, received_at) VALUES('kit', ?, ?, ?, ?)`, fmt.Sprint(k.n), text, now, now); err != nil {
		k.T.Fatal(err)
	}
	k.Clock.Advance(2 * time.Minute)
	k.Run()
}

// Run steps the loop until nothing is ready.
func (k *Kit) Run() {
	for i := 0; i < 50; i++ {
		before := k.Turns()
		if err := k.L.Step(context.Background()); err != nil {
			k.T.Fatalf("loop: %v", err)
		}
		if k.Turns() == before {
			return
		}
	}
}

func (k *Kit) Turns() int {
	var n int
	k.S.DB.QueryRow(`SELECT COUNT(*) FROM turns`).Scan(&n)
	return n
}

// Requests are the fixture's logged message calls.
func (k *Kit) Requests() [][]byte {
	var out [][]byte
	for _, r := range k.Model.Requests() {
		if r.Method == "POST" && (r.URL == "/anthropic/v1/messages" || r.URL == "/openai/v1/chat/completions") {
			out = append(out, r.Body)
		}
	}
	return out
}
