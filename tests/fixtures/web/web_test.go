package web

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"plebian/tests/fixtures"
)

func reader(t *testing.T) func(*http.Response, error) string {
	return func(r *http.Response, err error) string {
		t.Helper()
		if err != nil {
			t.Fatal(err)
		}
		b, _ := io.ReadAll(r.Body)
		return string(b)
	}
}

func TestStoreRequestsAreLoggedExactly(t *testing.T) {
	body := reader(t)
	s := New("owner@plebian.example", "pw-store")
	srv := httptest.NewServer(s.Handler())
	defer srv.Close()
	page := body(http.Get(srv.URL + "/store/"))
	for _, want := range []string{"sendBeacon", "XMLHttpRequest", "method: 'DELETE'", "documentId", `type="password"`} {
		if !strings.Contains(page+body(http.Get(srv.URL+"/store/signin")), want) {
			t.Fatalf("store pages lack %q", want)
		}
	}
	form := "sku=gravel-15&qty=2"
	if got := body(http.Post(srv.URL+"/store/order", "application/x-www-form-urlencoded", strings.NewReader(form))); !strings.Contains(got, "ORD-1") {
		t.Fatalf("order: %s", got)
	}
	gql := func(b string) map[string]any {
		var out map[string]any
		_ = json.Unmarshal([]byte(body(http.Post(srv.URL+"/store/graphql", "application/json", strings.NewReader(b)))), &out)
		return out
	}
	mixed, _ := json.Marshal(map[string]any{"operationName": "Cart", "query": documents[1]})
	if out := gql(string(mixed)); out["data"].(map[string]any)["cart"] == nil {
		t.Fatalf("mixed query: %v", out)
	}
	if out := gql(`{"documentId":"sha256:` + docID(documents[2]) + `"}`); out["data"].(map[string]any)["stock"] == nil {
		t.Fatalf("persisted id: %v", out)
	}
	if out := gql(`{"extensions":{"persistedQuery":{"version":1,"sha256Hash":"` + docID(documents[3]) + `"}}}`); out["data"].(map[string]any)["reorder"] == nil {
		t.Fatalf("persisted hash mutation: %v", out)
	}
	var batch []map[string]any
	_ = json.Unmarshal([]byte(body(http.Post(srv.URL+"/store/graphql", "application/json",
		strings.NewReader(`[{"query":"{ products { sku } }"},{"query":"query Stock { stock { sku } }"}]`)))), &batch)
	if len(batch) != 2 {
		t.Fatalf("batch %v", batch)
	}
	if r, _ := http.Get(srv.URL + "/store/graphql?operationName=PlaceOrder&query=" + url.QueryEscape(documents[1])); r.StatusCode != 405 {
		t.Fatalf("mutation over GET: %d", r.StatusCode)
	}
	beacon := []byte("feedback=fast delivery")
	if r, _ := http.Post(srv.URL+"/store/beacon", "text/plain", bytes.NewReader(beacon)); r.StatusCode != 204 {
		t.Fatal("beacon")
	}
	reqs := s.Requests("store")
	last := reqs[len(reqs)-1]
	if last.Method != "POST" || last.URL != "/store/beacon" || !bytes.Equal(last.Body, beacon) {
		t.Fatalf("log %+v", last)
	}
	var formReq fixtures.Request
	for _, r := range reqs {
		if r.URL == "/store/order" {
			formReq = r
		}
	}
	if string(formReq.Body) != form || formReq.Headers.Get("Content-Type") != "application/x-www-form-urlencoded" {
		t.Fatalf("form request %+v", formReq)
	}

	// Sign-in sets a session cookie that opens the account page.
	jar, _ := cookiejar.New(nil)
	c := &http.Client{Jar: jar}
	if got := body(c.PostForm(srv.URL+"/store/signin", url.Values{"username": {"owner@plebian.example"}, "password": {"wrong"}})); !strings.Contains(got, "Wrong") {
		t.Fatal("wrong password accepted")
	}
	if got := body(c.PostForm(srv.URL+"/store/signin", url.Values{"username": {"owner@plebian.example"}, "password": {"pw-store"}})); !strings.Contains(got, "<h1>Account</h1>") {
		t.Fatalf("signin: %s", got)
	}
	pdf := body(http.Get(srv.URL + "/store/invoice.pdf"))
	if !strings.HasPrefix(pdf, "%PDF-1.4") || !strings.Contains(pdf, "(Total due: USD 412.50) Tj") || !strings.HasSuffix(pdf, "%%EOF\n") {
		t.Fatalf("pdf %q", pdf)
	}
	if !bytes.Equal([]byte(pdf), fixtures.PDF(fixtures.InvoiceLines)) {
		t.Fatal("pdf not deterministic")
	}
}

func TestResearchAndInject(t *testing.T) {
	body := reader(t)
	s := New("u", "p")
	srv := httptest.NewServer(s.Handler())
	defer srv.Close()
	res := body(http.PostForm(srv.URL+"/research/html/", url.Values{"q": {"driveway culvert county"}}))
	if strings.Count(res, `class="links_main links_deep result__body"`) < 1 || !strings.Contains(res, "/research/articles/culvert-sizing") {
		t.Fatalf("results %s", res)
	}
	if first := Search("driveway culvert county")[0]; first.Slug != "culvert-sizing" {
		t.Fatalf("ranking: %s", first.Slug)
	}
	art := body(http.Get(srv.URL + "/research/articles/culvert-sizing"))
	if !strings.Contains(art, "18-inch culvert") {
		t.Fatalf("article %s", art)
	}
	inj := body(http.Get(srv.URL + "/inject/mail.txt"))
	if !strings.Contains(inj, "http://"+strings.TrimPrefix(srv.URL, "http://")+"/inject/collect?data=") {
		t.Fatalf("inject %s", inj)
	}
	if len(s.Requests("inject")) != 1 || len(s.Requests("research")) != 2 {
		t.Fatalf("site filter: %d %d", len(s.Requests("inject")), len(s.Requests("research")))
	}
}
