package clock

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestSetAdvanceRun(t *testing.T) {
	c := New()
	srv := httptest.NewServer(c.Handler())
	defer srv.Close()
	read := func(resp *http.Response, err error) int64 {
		if err != nil {
			t.Fatal(err)
		}
		var v struct {
			UnixMs int64 `json:"unix_ms"`
		}
		_ = json.NewDecoder(resp.Body).Decode(&v)
		return v.UnixMs
	}
	post := func(path, body string) (*http.Response, error) {
		return http.Post(srv.URL+path, "application/json", strings.NewReader(body))
	}
	if got := read(post("/control/set", `{"unix_ms":1764216000000,"running":false}`)); got != 1764216000000 {
		t.Fatalf("set %d", got)
	}
	time.Sleep(20 * time.Millisecond)
	if got := read(http.Get(srv.URL + "/now")); got != 1764216000000 {
		t.Fatalf("stopped clock moved: %d", got)
	}
	if got := read(post("/control/advance", `{"ms":86400000}`)); got != 1764216000000+86400000 {
		t.Fatalf("advance %d", got)
	}
	read(post("/control/set", `{"unix_ms":1000,"running":true}`))
	time.Sleep(30 * time.Millisecond)
	if got := read(http.Get(srv.URL + "/now")); got < 1030 || got > 2000 {
		t.Fatalf("running clock %d", got)
	}
}
