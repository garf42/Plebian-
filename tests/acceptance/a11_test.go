package acceptance

import (
	"strings"
	"testing"
	"time"
)

type a11 struct {
	searched, fetched bool
	sawResults        bool
	sawFact           bool
	reply             []string
	article           string
}

func init() {
	var st *a11
	register(Scenario{
		ID: "A11",
		Setup: func(t testing.TB) *H {
			st = &a11{}
			h := fresh(t)
			w := StartFixture(t, "web")
			h.Fixtures["web"] = w
			st.article = w.URL("/research/articles/culvert-sizing")
			if code := h.Do("POST", "/v1/setup/search", map[string]string{"url": w.URL("/research/html/?q=")}, nil, true); code != 200 {
				t.Fatalf("search setting: %d", code)
			}
			h.Model("anthropic", []map[string]any{
				{"match": "culvert", "reply": map[string]any{"tool_calls": []map[string]any{{"name": "search", "input": map[string]any{"query": "driveway culvert size county"}}}}},
				{"match": "results for", "reply": map[string]any{"tool_calls": []map[string]any{{"name": "fetch", "input": map[string]any{"url": st.article}}}}},
				{"match": "18-inch", "reply": map[string]any{"text": "Marrow County wants an 18-inch culvert under a new driveway on Ridgeline Road. Source: " + st.article}},
			})
			return h
		},
		Drive: func(h *H) {
			h.Say("what size culvert does the county want under a driveway?")
			Until(90*time.Second, func() bool { return len(AgentMessages(h.Today())) > 0 })
			st.reply = AgentMessages(h.Today())
			for _, r := range Requests(h.T, h.Fixtures["web"]) {
				if strings.HasPrefix(r.URL, "/research/html/") && strings.Contains(r.URL, "q=") {
					st.searched = true
				}
				if r.URL == "/research/articles/culvert-sizing" {
					st.fetched = true
				}
			}
			for _, r := range Requests(h.T, h.Fixtures["model:anthropic"]) {
				if strings.Contains(string(r.Body), "/research/articles/culvert-sizing") && strings.Contains(string(r.Body), "results for") {
					st.sawResults = true
				}
				if strings.Contains(string(r.Body), "Marrow County requires an 18-inch culvert") {
					st.sawFact = true
				}
			}
		},
		Assert: func(h *H, c *C) {
			c.Check("searched", st.searched, "the results page was never read")
			c.Check("fetched", st.fetched, "the article was never fetched")
			c.Check("results_reached_model", st.sawResults, "search's results never reached the model")
			c.Check("page_text_reached_model", st.sawFact, "the fetched page's text never reached the model")
			c.Check("answered_with_url", len(st.reply) == 1 && strings.Contains(st.reply[0], st.article), "reply %q", st.reply)
		},
	})
}
func TestA11(t *testing.T) { Run(t, "A11") }
