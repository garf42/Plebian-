package acceptance

import (
	"os"
	"strings"
	"testing"
	"time"
)

// A19: with the provisioned key, one real exchange with a tool call completes on the
// owner's provider (PLEBIAN_LIVE_BASE_URL, or Anthropic's API when unset).
func init() {
	type a19 struct {
		format, model string
		reply         []string
		ran           bool
	}
	var st *a19
	register(Scenario{
		ID: "A19",
		Setup: func(t testing.TB) *H {
			st = &a19{}
			return fresh(t)
		},
		Drive: func(h *H) {
			base := os.Getenv("PLEBIAN_LIVE_BASE_URL")
			var r struct {
				ProviderID string `json:"provider_id"`
				Format     string
				Models     []struct{ ID string }
			}
			if c := h.Do("POST", "/v1/setup/model", map[string]string{"base_url": base, "api_key": os.Getenv("PLEBIAN_LIVE_KEY")}, &r, true); c != 200 || len(r.Models) == 0 {
				h.T.Logf("live model setup: %d %s", c, h.LastBody)
				return
			}
			st.format, st.model = r.Format, r.Models[0].ID
			h.Do("POST", "/v1/setup/model/select", map[string]string{"provider_id": r.ProviderID, "model": st.model, "role": "main"}, nil, true)
			h.Say("Use the read tool to run the command `echo plebian-live-check` and tell me exactly what it printed.")
			Until(5*time.Minute, func() bool {
				for _, m := range AgentMessages(h.Today()) {
					if strings.Contains(m, "plebian-live-check") {
						return true
					}
				}
				return false
			})
			st.reply = AgentMessages(h.Today())
			var p struct{ Thread []map[string]any }
			h.Do("GET", "/v1/today", nil, &p, true)
			for _, e := range p.Thread {
				if e["type"] == "step" && e["tool"] == "read" && e["state"] == "done" {
					st.ran = true
				}
			}
		},
		Assert: func(h *H, c *C) {
			c.Check("provider_detected", st.model != "", "no model from the provider's listing")
			c.Check("tool_call_ran", st.ran, "no read call ran (model %s, %s format)", st.model, st.format)
			ok := false
			for _, m := range st.reply {
				ok = ok || strings.Contains(m, "plebian-live-check")
			}
			c.Check("exchange_completed", ok, "replies %q", st.reply)
		},
	})
}
