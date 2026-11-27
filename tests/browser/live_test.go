package browser_test

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"log"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/coder/websocket"

	"plebian/server/browser"
)

// D2: while the owner holds the wheel, what the owner types reaches the page and nothing
// else: no draft, no log, in no encoding; the guard holds nothing the owner sends.
func TestWheelNotRecorded(t *testing.T) {
	e := open(t)
	ctx := context.Background()
	var logs bytes.Buffer
	lv := &browser.Live{G: e.g, Log: log.New(&logs, "", 0)}
	e.g.C.Call(ctx, e.page, "Page.navigate", map[string]any{"url": e.url + "/store/signin"}, nil)
	time.Sleep(800 * time.Millisecond)
	srv := httptest.NewServer(http.HandlerFunc(lv.Handler))
	defer srv.Close()
	c, _, err := websocket.Dial(ctx, "ws"+strings.TrimPrefix(srv.URL, "http"), nil)
	if err != nil {
		t.Fatal(err)
	}
	defer c.CloseNow()
	frames := 0
	go func() {
		for {
			typ, _, err := c.Read(ctx)
			if err != nil {
				return
			}
			if typ == websocket.MessageBinary {
				frames++
			}
		}
	}()
	send := func(v map[string]any) {
		b, _ := json.Marshal(v)
		c.Write(ctx, websocket.MessageText, b)
		time.Sleep(150 * time.Millisecond)
	}
	const password = "pw-fixture"
	send(map[string]any{"type": "take"})
	send(map[string]any{"type": "key", "key": "Tab"})
	send(map[string]any{"type": "key", "text": "owner"})
	send(map[string]any{"type": "key", "key": "Tab"})
	send(map[string]any{"type": "key", "text": password})
	send(map[string]any{"type": "key", "key": "Enter"})
	time.Sleep(1200 * time.Millisecond)
	send(map[string]any{"type": "give"})
	signedIn := false
	for _, r := range e.site.Requests("store") {
		if r.Method == "POST" && strings.HasPrefix(r.URL, "/store/signin") && bytes.Contains(r.Body, []byte(password)) {
			signedIn = true
		}
	}
	if !signedIn {
		t.Fatal("the owner's sign-in did not reach the site")
	}
	if frames == 0 {
		t.Fatal("the live view sent no frames")
	}
	forms := []string{password, base64.StdEncoding.EncodeToString([]byte(password)), hex.EncodeToString([]byte(password)), url.QueryEscape(password)}
	e.mu.Lock()
	defer e.mu.Unlock()
	for _, d := range e.drafts {
		for _, r := range d {
			for _, f := range forms {
				if strings.Contains(r.Shown(), f) {
					t.Fatalf("a draft holds the owner's typing (%q)", f)
				}
			}
		}
	}
	for _, f := range forms {
		if strings.Contains(logs.String(), f) {
			t.Fatalf("the log holds the owner's typing (%q)", f)
		}
	}
}
