package acceptance

import (
	"strings"
	"testing"
	"time"
)

type a14 struct {
	bearerOnly, signed int
	postsBeforeWindow  int
	postsAfterWindow   int
	postsAfterOutside  int
	announced          bool
	drafts             int
	toolNames          []string
}

func webPosts(h *H, w *Fixture) int {
	var n int
	for _, r := range Requests(h.T, w) {
		if r.Method == "POST" && strings.Contains(r.URL, "/store/order") {
			n++
		}
	}
	return n
}

func init() {
	var st *a14
	register(Scenario{
		ID: "A14",
		Setup: func(t testing.TB) *H {
			st = &a14{}
			h := fresh(t)
			w := StartFixture(t, "web")
			h.Fixtures["web"] = w
			post := map[string]any{"command": "curl -s -X POST " + w.URL("/store/order") + " -d qty=2", "network": true}
			h.Model("anthropic", []map[string]any{
				{"match": "post the weekly order", "reply": map[string]any{"tool_calls": []map[string]any{{"name": "run", "input": post}}}},
				{"match": "drafted", "reply": map[string]any{"text": "Drafted; it waits for your word."}},
				{"match": "post the weekly order", "reply": map[string]any{"tool_calls": []map[string]any{{"name": "run", "input": post}}}},
				{"match": "qty|order", "reply": map[string]any{"text": "Posted the weekly order."}},
				{"match": "read the order page", "reply": map[string]any{"tool_calls": []map[string]any{{"name": "fetch", "input": map[string]any{"url": w.URL("/inject/")}}}}},
				{"reply": map[string]any{"tool_calls": []map[string]any{{"name": "run", "input": post}}}},
				{"reply": map[string]any{"text": "Drafted, since the page was in view."}},
			})
			return h
		},
		Drive: func(h *H) {
			w := h.Fixtures["web"]
			h.Say("post the weekly order")
			Until(60*time.Second, func() bool { return len(AgentMessages(h.Today())) >= 1 })
			st.bearerOnly = h.Do("POST", "/v1/admin/grant", map[string]any{"step": "run curl", "nonce": "x", "at": time.Now().UTC().Format(time.RFC3339)}, nil, true)
			st.signed = h.Signed("/v1/admin/grant", map[string]any{"step": "run curl"})
			h.Say("post the weekly order")
			Until(30*time.Second, func() bool {
				var p map[string]any
				h.Do("GET", "/v1/thread", nil, &p, true)
				evs, _ := p["events"].([]any)
				for _, e := range evs {
					if m, _ := e.(map[string]any); m["type"] == "step" && m["state"] == "announced" {
						st.announced = true
					}
				}
				return st.announced
			})
			time.Sleep(2 * time.Second)
			st.postsBeforeWindow = webPosts(h, w)
			h.Advance(61 * time.Second) // past the owner's usual time to answer (cold start: one minute)
			Until(60*time.Second, func() bool { return webPosts(h, w) > 0 })
			st.postsAfterWindow = webPosts(h, w)
			Until(30*time.Second, func() bool { return len(AgentMessages(h.Today())) >= 2 })
			h.Say("read the order page then post the order")
			Until(60*time.Second, func() bool { return len(AgentMessages(h.Today())) >= 3 })
			st.postsAfterOutside = webPosts(h, w)
			var open struct{ Drafts []any }
			h.Do("GET", "/v1/open", nil, &open, true)
			st.drafts = len(open.Drafts)
			for _, r := range Requests(h.T, h.Fixtures["model:anthropic"]) {
				if strings.HasSuffix(r.URL, "/messages") {
					for _, name := range []string{"grant", "standing"} {
						if strings.Contains(string(r.Body), `"name":"`+name) {
							st.toolNames = append(st.toolNames, name)
						}
					}
				}
			}
		},
		Assert: func(h *H, c *C) {
			c.Check("signed_grant_accepted", st.signed == 200, "a signed grant answered %d", st.signed)
			c.Check("announced", st.announced, "the step with standing was not announced")
			c.Check("ran_after_window", st.postsAfterWindow == 1, "the order was posted %d times after the window", st.postsAfterWindow)
			c.Check("bearer_grant_refused", st.bearerOnly == 403, "a grant with a bearer token alone answered %d", st.bearerOnly)
			c.Check("not_before_window", st.postsBeforeWindow == 0, "posted %d times inside the stop window", st.postsBeforeWindow)
			c.Check("drafted_with_outside_words", st.postsAfterOutside == 1 && st.drafts >= 1, "with a page in view: %d posts, %d drafts", st.postsAfterOutside, st.drafts)
			c.Check("no_tool_raises_standing", len(st.toolNames) == 0, "the tool band holds %v", st.toolNames)
		},
	})
}

func TestA14(t *testing.T) { Run(t, "A14") }
