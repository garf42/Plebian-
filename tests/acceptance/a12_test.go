package acceptance

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/coder/websocket"
)

// InstallBrowser installs with the box's browser (from PLEBIAN_BROWSER_FROM).
func InstallBrowser(t testing.TB) *H {
	return install(t, "none", "--browser-from", os.Getenv("PLEBIAN_BROWSER_FROM"))
}

func init() {
	type a12 struct {
		queriesLoaded, heldBefore int
		draft                     string
		released                  [][]byte
		signedIn                  bool
		frames                    int
		passwordFound             string
	}
	var st *a12
	register(Scenario{
		ID: "A12",
		Setup: func(t testing.TB) *H {
			st = &a12{}
			h := InstallBrowser(t)
			h.Fixtures["web"] = StartFixture(t, "web", "-user", "owner@example.com", "-key", "fixture-pw-7731")
			return h
		},
		Drive: func(h *H) {
			w := h.Fixtures["web"]
			store, signin := w.URL("/store/"), w.URL("/store/signin")
			tool := func(name string, in map[string]any) map[string]any {
				return map[string]any{"tool_calls": []map[string]any{{"name": name, "input": in}}}
			}
			say := func(s string) map[string]any { return map[string]any{"text": s} }
			m := h.Model("anthropic", []map[string]any{
				{"match": "order the gravel from the store", "reply": tool("browse", map[string]any{"task": "Open " + store + " and press the Place order button."})},
				{"match": "press the Place order button", "reply": tool("browser_navigate", map[string]any{"url": store})},
				{"match": "Ridgeline Supply", "reply": tool("browser_evaluate", map[string]any{"function": "() => { document.getElementById('gql-order').click(); return 'clicked' }"})},
				{"match": "clicked", "reply": say("Pressed Place order.")},
				{"match": "held for the owner's word", "reply": say("The order is held for your word.")},
			})
			said := func(s string) bool {
				return Until(120*time.Second, func() bool {
					for _, x := range AgentMessages(h.Today()) {
						if x == s {
							return true
						}
					}
					return false
				})
			}
			placeOrder := func() [][]byte {
				var out [][]byte
				for _, r := range Requests(h.T, w) {
					if r.Method == "POST" && strings.HasPrefix(r.URL, "/store/graphql") && bytes.Contains(r.Body, []byte(`"operationName":"PlaceOrder"`)) {
						out = append(out, r.Body)
					}
				}
				return out
			}
			h.Say("order the gravel from the store")
			said("The order is held for your word.")
			for _, r := range Requests(h.T, w) {
				if strings.HasPrefix(r.URL, "/store/graphql?operationName=Products") || (r.Method == "POST" && bytes.Contains(r.Body, []byte(`"operationName":"Cart"`))) {
					st.queriesLoaded++
				}
			}
			st.heldBefore = len(placeOrder())
			var open struct {
				Drafts []struct{ ID, Text string }
			}
			h.Do("GET", "/v1/open", nil, &open, true)
			if len(open.Drafts) == 0 {
				return
			}
			d := open.Drafts[len(open.Drafts)-1]
			st.draft = d.Text
			var id int64
			fmt.Sscan(d.ID, &id)
			post(h.T, m.URL("/control/script"), map[string]any{"steps": []map[string]any{
				{"match": "yes, place it", "reply": tool("browser_send", map[string]any{"held": id})},
				{"match": "released", "reply": say("Placed.")},
				{"match": "open the store sign-in page", "reply": tool("browse", map[string]any{"task": "Open " + signin + " and wait there for the owner."})},
				{"match": "wait there for the owner", "reply": tool("browser_navigate", map[string]any{"url": signin})},
				{"match": "Password", "reply": say("The sign-in page is open.")},
				{"match": "sign-in page is open", "reply": say("Take the wheel to sign in.")},
			}})
			h.Say("yes, place it")
			said("Placed.")
			time.Sleep(time.Second)
			st.released = placeOrder()
			// The owner signs in by hand in the live view.
			h.Say("open the store sign-in page")
			said("Take the wheel to sign in.")
			ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
			defer cancel()
			c, _, err := websocket.Dial(ctx, "ws://"+h.Addr+"/v1/browser/live", &websocket.DialOptions{HTTPHeader: http.Header{"Authorization": {"Bearer " + h.Token}}})
			if err != nil {
				h.T.Logf("live view: %v", err)
				return
			}
			defer c.CloseNow()
			go func() {
				for {
					typ, _, err := c.Read(ctx)
					if err != nil {
						return
					}
					if typ == websocket.MessageBinary {
						st.frames++
					}
				}
			}()
			send := func(v map[string]any) {
				b, _ := json.Marshal(v)
				c.Write(ctx, websocket.MessageText, b)
				time.Sleep(200 * time.Millisecond)
			}
			pw := fmt.Sprint(w.Info["key"])
			send(map[string]any{"type": "take"})
			send(map[string]any{"type": "key", "key": "Tab"})
			send(map[string]any{"type": "key", "text": fmt.Sprint(w.Info["user"])})
			send(map[string]any{"type": "key", "key": "Tab"})
			send(map[string]any{"type": "key", "text": pw})
			send(map[string]any{"type": "key", "key": "Enter"})
			time.Sleep(2 * time.Second)
			send(map[string]any{"type": "give"})
			for _, r := range Requests(h.T, w) {
				if r.Method == "POST" && strings.HasPrefix(r.URL, "/store/signin") && bytes.Contains(r.Body, []byte(pw)) {
					st.signedIn = true
				}
			}
			out, _ := sudo("grep", "-r", "-l", "-F", pw, filepath.Join(h.Root, "data")).Output()
			st.passwordFound = strings.TrimSpace(string(out))
		},
		Assert: func(h *H, c *C) {
			c.Check("page_queries_loaded_unheld", st.queriesLoaded >= 2, "%d of the page's GraphQL queries reached the store", st.queriesLoaded)
			c.Check("order_held_and_drafted", st.heldBefore == 0 && strings.Contains(st.draft, `"operationName":"PlaceOrder"`), "orders sent while held %d; draft %q", st.heldBefore, st.draft)
			shown := ""
			if i := strings.Index(st.draft, "\n\n"); i >= 0 {
				shown = st.draft[i+2:]
			}
			c.Check("released_once_exactly", len(st.released) == 1 && string(st.released[0]) == shown, "%d orders after approval; bytes as shown %v", len(st.released), len(st.released) == 1 && string(st.released[0]) == shown)
			c.Check("owner_signed_in_live", st.signedIn && st.frames > 0, "signed in %v, frames %d", st.signedIn, st.frames)
			c.Check("password_nowhere_in_store", st.signedIn && st.passwordFound == "", "the password is in %s", st.passwordFound)
		},
	})
}

func TestA12(t *testing.T) { Run(t, "A12") }
