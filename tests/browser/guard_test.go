package browser_test

import (
	"bytes"
	"context"
	"net/http/httptest"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"plebian/server/browser"
	"plebian/tests/fixtures/web"
)

func chrome(t *testing.T) string {
	if p := os.Getenv("PLEBIAN_CHROME"); p != "" {
		return p
	}
	for _, p := range []string{"/opt/pw-browsers/chromium-1243/chrome-linux64/chrome"} {
		if _, err := os.Stat(p); err == nil {
			return p
		}
	}
	t.Skip("no Chromium (PLEBIAN_CHROME)")
	return ""
}

type env struct {
	site   *web.Site
	url    string
	g      *browser.Guard
	page   string
	mu     sync.Mutex
	drafts [][]browser.Request
}

func open(t *testing.T) *env {
	ctx := context.Background()
	e := &env{site: web.New("owner", "pw-fixture")}
	srv := httptest.NewServer(e.site.Handler())
	t.Cleanup(srv.Close)
	e.url = srv.URL
	c, err := browser.Launch(ctx, chrome(t), t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(c.Close)
	x, err := browser.Dial(ctx, c.WS)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { x.Close() })
	e.g = &browser.Guard{C: x, Draft: func(_ context.Context, _ int64, reqs []browser.Request) (int64, error) {
		e.mu.Lock()
		defer e.mu.Unlock()
		e.drafts = append(e.drafts, reqs)
		return int64(len(e.drafts)), nil
	}}
	if err := e.g.Watch(ctx); err != nil {
		t.Fatal(err)
	}
	e.g.Step(ctx) // the agent's first action: open the store
	var tgt struct{ TargetID string }
	if err := x.Call(ctx, "", "Target.createTarget", map[string]any{"url": e.url + "/store/"}, &tgt); err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(10 * time.Second)
	for e.g.Session(tgt.TargetID) == "" && time.Now().Before(deadline) {
		time.Sleep(50 * time.Millisecond)
	}
	e.page = e.g.Session(tgt.TargetID)
	time.Sleep(1500 * time.Millisecond) // the page loads its queries
	return e
}

// click runs one agent action on the page and returns the draft it raised, if any.
func (e *env) click(t *testing.T, id string) []browser.Request {
	ctx := context.Background()
	e.g.Step(ctx)
	e.mu.Lock()
	before := len(e.drafts)
	e.mu.Unlock()
	var r any
	if err := e.g.C.Call(ctx, e.page, "Runtime.evaluate", map[string]any{"expression": "document.getElementById('" + id + "').click()"}, &r); err != nil {
		t.Fatal(err)
	}
	time.Sleep(700 * time.Millisecond)
	e.g.Settle(ctx)
	e.mu.Lock()
	defer e.mu.Unlock()
	if len(e.drafts) == before {
		return nil
	}
	return e.drafts[len(e.drafts)-1]
}

func (e *env) count(method, path string) int {
	n := 0
	for _, r := range e.site.Requests("store") {
		if r.Method == method && strings.HasPrefix(r.URL, path) {
			n++
		}
	}
	return n
}

func (e *env) bodies(method, path string) [][]byte {
	var out [][]byte
	for _, r := range e.site.Requests("store") {
		if r.Method == method && strings.HasPrefix(r.URL, path) {
			out = append(out, r.Body)
		}
	}
	return out
}

// D1, MR-2: a fetch() POST, an XHR PUT, a DELETE and a form submit are each held with
// their own bytes; none reaches the site while held.
func TestScriptRequestsHeld(t *testing.T) {
	e := open(t)
	for _, c := range []struct{ id, method, path string }{
		{"buy", "POST", "/store/api/orders"}, {"cart-put", "PUT", "/store/api/cart"}, {"cart-delete", "DELETE", "/store/api/cart"},
	} {
		d := e.click(t, c.id)
		if len(d) != 1 || d[0].Method != c.method || !strings.Contains(d[0].URL, c.path) {
			t.Fatalf("%s: held %+v", c.id, d)
		}
		if n := e.count(c.method, c.path); n != 0 {
			t.Fatalf("%s reached the site %d times while held", c.id, n)
		}
	}
	// The form submit (a navigation POST).
	e.g.Step(context.Background())
	e.g.C.Call(context.Background(), e.page, "Runtime.evaluate", map[string]any{"expression": "document.getElementById('order').submit()"}, nil)
	time.Sleep(700 * time.Millisecond)
	e.g.Settle(context.Background())
	if n := e.count("POST", "/store/order"); n != 0 {
		t.Fatalf("the form POST reached the site %d times while held", n)
	}
	e.mu.Lock()
	last := e.drafts[len(e.drafts)-1]
	e.mu.Unlock()
	if len(last) != 1 || last[0].Method != "POST" || !bytes.Contains(last[0].Body, []byte("sku=gravel-15")) {
		t.Fatalf("form submit held %+v", last)
	}
}

// D5, MR-3: the page's queries load unheld (GET, POST, a batch, a mixed document naming
// its query); a mutation named from the mixed document and a persisted document are held.
func TestGraphQLMutationHeld(t *testing.T) {
	e := open(t)
	if n := e.count("GET", "/store/graphql?operationName=Products"); n == 0 {
		t.Fatal("the GET query did not load")
	}
	reads := 0
	for _, b := range e.bodies("POST", "/store/graphql") {
		if bytes.Contains(b, []byte(`"Cart"`)) || bytes.HasPrefix(b, []byte("[")) {
			reads++
		}
	}
	if reads < 2 {
		t.Fatalf("%d of the page's POST queries loaded", reads)
	}
	d := e.click(t, "gql-order")
	if len(d) != 1 || !bytes.Contains(d[0].Body, []byte(`"operationName":"PlaceOrder"`)) {
		t.Fatalf("the mutation was not held: %+v", d)
	}
	d = e.click(t, "gql-reorder")
	if len(d) != 1 || !bytes.Contains(d[0].Body, []byte("documentId")) {
		t.Fatalf("the persisted document was not held: %+v", d)
	}
	for _, b := range e.bodies("POST", "/store/graphql") {
		if bytes.Contains(b, []byte(`"operationName":"PlaceOrder"`)) {
			t.Fatal("the mutation reached the site while held")
		}
	}
}

// D4: a beacon is dropped, never sent or drafted.
func TestBeaconDropped(t *testing.T) {
	e := open(t)
	if d := e.click(t, "beacon"); d != nil {
		t.Fatalf("a beacon raised a draft: %+v", d)
	}
	if n := e.count("POST", "/store/beacon"); n != 0 {
		t.Fatalf("a beacon was sent %d times", n)
	}
}

// D3: released, the site receives exactly the held bytes, once.
func TestReleaseBytesIdentical(t *testing.T) {
	e := open(t)
	d := e.click(t, "gql-order")
	if len(d) != 1 {
		t.Fatalf("held %+v", d)
	}
	e.mu.Lock()
	id := int64(len(e.drafts))
	e.mu.Unlock()
	if _, err := e.g.Release(context.Background(), id); err != nil {
		t.Fatal(err)
	}
	time.Sleep(500 * time.Millisecond)
	var got [][]byte
	for _, b := range e.bodies("POST", "/store/graphql") {
		if bytes.Contains(b, []byte(`"operationName":"PlaceOrder"`)) {
			got = append(got, b)
		}
	}
	if len(got) != 1 || !bytes.Equal(got[0], d[0].Body) {
		t.Fatalf("the site received %d mutation requests; first equals the held bytes: %v", len(got), len(got) > 0 && bytes.Equal(got[0], d[0].Body))
	}
	if _, err := e.g.Release(context.Background(), id); err == nil {
		t.Fatal("a draft released twice")
	}
}
