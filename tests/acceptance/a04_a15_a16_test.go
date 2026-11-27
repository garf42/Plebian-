package acceptance

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func call(name string, input map[string]any) map[string]any {
	return map[string]any{"reply": map[string]any{"tool_calls": []map[string]any{{"name": name, "input": input}}}}
}

func text(s string) map[string]any { return map[string]any{"reply": map[string]any{"text": s}} }

// a04Script: a step fails, the model repeats it word for word, then issues it once more,
// then the same step succeeds with other arguments.
func a04Script(marker string) []map[string]any {
	// The step is the tool and the first word of its first argument: "run test".
	failing := map[string]any{"command": "test -f /nonexistent-plebian-marker || { echo run >> " + marker + "; exit 1; }"}
	return []map[string]any{
		{"match": "is the marker there", "reply": map[string]any{"tool_calls": []map[string]any{{"name": "run", "input": failing}}}},
		call("run", failing), // the same call, word for word: held
		call("run", failing), // issued once more: runs
		call("run", map[string]any{"command": "test -d ."}),
		text(a04Reply),
	}
}

const a04Reply = "It isn't there; the workspace is."

type a04 struct {
	runs      int
	sawHeld   bool
	redlineAt int // first request whose spotlight carries the open redline
	closedAt  int // first request after it whose spotlight no longer does
	reqs      int
	reply     []string
}

func driveA04(h *H, format string, st *a04) {
	marker := filepath.Join(h.Root, "workspace", "runs.txt")
	before := len(AgentMessages(h.Today()))
	h.Say("is the marker there")
	Until(90*time.Second, func() bool { return len(AgentMessages(h.Today())) > before })
	st.reply = AgentMessages(h.Today())[before:]
	b, _ := exec.Command("sudo", "-n", "cat", marker).Output()
	if os.Geteuid() == 0 {
		b, _ = os.ReadFile(marker)
	}
	st.runs = strings.Count(string(b), "run")
	reqs := Requests(h.T, h.Fixtures["model:"+format])
	// Requests within an exchange only append, so each round's spotlight is the new
	// part: a round whose request adds no open redline is one where the loop is closed.
	n, prev := 0, 0
	for _, r := range reqs {
		if !strings.HasSuffix(r.URL, "/messages") && !strings.HasSuffix(r.URL, "/completions") {
			continue
		}
		n++
		if bytes.Contains(r.Body, []byte("held: this is the call that just failed")) {
			st.sawHeld = true
		}
		c := bytes.Count(r.Body, []byte(`open kind=\"redline\"`))
		if c > prev && st.redlineAt == 0 {
			st.redlineAt = n
		}
		if c <= prev && st.redlineAt != 0 && st.closedAt == 0 {
			st.closedAt = n
		}
		prev = c
	}
	st.reqs = n
}

func assertA04(h *H, c *C, st *a04) {
	c.Check("reply_after_steps", len(st.reply) == 1 && st.reply[0] == a04Reply, "agent replied %q", st.reply)
	c.Check("redline_opened", st.redlineAt > 0, "no request carried an open redline (%d requests)", st.reqs)
	c.Check("repeat_held", st.sawHeld, "the word-for-word repeat was not held")
	c.Check("failing_call_ran_twice", st.runs == 2, "the failing call ran %d times (held once, then run once more)", st.runs)
	c.Check("success_closes_loop", st.closedAt > st.redlineAt && st.redlineAt > 0, "the redline stayed open after the step succeeded")
}

func init() {
	var st4 *a04
	register(Scenario{
		ID: "A04",
		Setup: func(t testing.TB) *H {
			st4 = &a04{}
			h := fresh(t)
			h.Model("anthropic", a04Script(filepath.Join(h.Root, "workspace", "runs.txt")))
			return h
		},
		Drive:  func(h *H) { driveA04(h, "anthropic", st4) },
		Assert: func(h *H, c *C) { assertA04(h, c, st4) },
	})

	// A15: A03 and A04 on the OpenAI-compatible format; the fallback takes a turn the
	// primary fails past the retry window; each model keeps its own budget.
	type a15 struct {
		a04          a04
		reply3       []string
		fallbackUsed bool
		primaryTried bool
		spendModels  map[string]bool
	}
	var st15 *a15
	register(Scenario{
		ID: "A15",
		Setup: func(t testing.TB) *H {
			st15 = &a15{a04: a04{}, spendModels: map[string]bool{}}
			h := fresh(t)
			steps := append([]map[string]any{{"match": "when is the gravel coming", "reply": map[string]any{"text": "Wednesday, by the culvert."}}},
				a04Script(filepath.Join(h.Root, "workspace", "runs.txt"))...)
			h.Model("openai", steps)
			return h
		},
		Drive: func(h *H) {
			h.Say("when is the gravel coming?")
			Until(60*time.Second, func() bool { return len(AgentMessages(h.Today())) > 0 })
			st15.reply3 = AgentMessages(h.Today())
			driveA04(h, "openai", &st15.a04)
			// Fallback: a primary that answers 529 with a wait past the owner's usual time to answer.
			primary := StartFixture(h.T, "model")
			post(h.T, primary.URL("/control/script"), map[string]any{"steps": []map[string]any{
				{"error": map[string]any{"status": 529, "retry_after_s": 3600}}}})
			post(h.T, primary.URL("/control/models"), []map[string]any{{"id": "primary-model", "context": 200000, "max_output": 8000}})
			h.UseModel(primary, "anthropic", "main")
			fb := StartFixture(h.T, "model")
			post(h.T, fb.URL("/control/script"), map[string]any{"steps": []map[string]any{{"reply": map[string]any{"text": "From the fallback."}}}})
			post(h.T, fb.URL("/control/models"), []map[string]any{{"id": "fallback-model", "context": 100000, "max_output": 4000}})
			h.UseModel(fb, "openai", "fallback")
			h.Say("anything new on the fence posts?")
			Until(90*time.Second, func() bool {
				for _, m := range AgentMessages(h.Today()) {
					if m == "From the fallback." {
						return true
					}
				}
				return false
			})
			for _, m := range AgentMessages(h.Today()) {
				if m == "From the fallback." {
					st15.fallbackUsed = true
				}
			}
			for _, r := range Requests(h.T, primary) {
				if strings.HasSuffix(r.URL, "/messages") {
					st15.primaryTried = true
				}
			}
			var w struct {
				Spend []struct{ Model string }
			}
			h.Do("GET", "/v1/week", nil, &w, true)
			for _, s := range w.Spend {
				st15.spendModels[s.Model] = true
			}
		},
		Assert: func(h *H, c *C) {
			c.Check("a03_openai_reply", len(st15.reply3) == 1 && st15.reply3[0] == "Wednesday, by the culvert.", "OpenAI-format reply %q", st15.reply3)
			assertA04(h, c, &st15.a04)
			c.Check("primary_tried", st15.primaryTried, "the primary provider was never asked")
			c.Check("fallback_took_turn", st15.fallbackUsed, "the fallback did not take the turn")
			c.Check("budget_per_model", st15.spendModels["fallback-model"] && len(st15.spendModels) >= 2, "spend kept for models %v", st15.spendModels)
		},
	})

	// A16 (crash half): kill -9 mid-call leaves every sealed turn intact, and the
	// interrupted change-making call is reported to the agent, never re-run.
	type a16 struct {
		replay    map[string]any
		ranLines  int
		reported  bool
		restarted bool
		quietAsks int
	}
	var st16 *a16
	register(Scenario{
		ID: "A16",
		Setup: func(t testing.TB) *H {
			st16 = &a16{}
			h := InstallSpeech(t)
			h.Fixtures["speech"] = StartFixture(t, "speech")
			marker := filepath.Join(h.Root, "workspace", "crash.txt")
			h.Model("anthropic", []map[string]any{
				{"match": "start the long job", "reply": map[string]any{"tool_calls": []map[string]any{{"name": "run", "input": map[string]any{"command": "echo started >> " + marker + "; sleep 20"}}}}},
				{"match": "interrupted", "reply": map[string]any{"text": "The job was interrupted; I did not run it again."}},
				{"match": "has been quiet since", "reply": map[string]any{"text": "The Omi has gone quiet."}},
			})
			return h
		},
		Drive: func(h *H) {
			marker := filepath.Join(h.Root, "workspace", "crash.txt")
			h.Say("start the long job")
			Until(30*time.Second, func() bool {
				b, _ := exec.Command("sudo", "-n", "cat", marker).Output()
				return bytes.Contains(b, []byte("started"))
			})
			out, err := sudo("sh", "-c", fmt.Sprintf("pkill -KILL -u plebian -f '%s serve'", filepath.Join(h.Root, "bin", "plebian"))).CombinedOutput()
			if err != nil {
				h.T.Logf("kill: %v %s", err, out)
			}
			// systemd restarts the server; it writes its address again.
			st16.restarted = Until(60*time.Second, func() bool {
				b, err := os.ReadFile(filepath.Join(h.Root, "run", "api.json"))
				if err != nil {
					return false
				}
				var a struct{ Addr string }
				json.Unmarshal(b, &a)
				if a.Addr == "" {
					return false
				}
				h.Addr = a.Addr
				st, err := h.Try("GET", "/v1/version", nil, nil, false)
				return err == nil && st == 200
			})
			Until(60*time.Second, func() bool {
				for _, m := range AgentMessages(h.Today()) {
					if strings.Contains(m, "interrupted") {
						return true
					}
				}
				return false
			})
			time.Sleep(25 * time.Second) // the killed call would have finished by now if anything re-ran it
			for _, m := range AgentMessages(h.Today()) {
				if strings.Contains(m, "interrupted") {
					st16.reported = true
				}
			}
			b, _ := exec.Command("sudo", "-n", "cat", marker).Output()
			st16.ranLines = strings.Count(string(b), "started")
			st16.replay = h.Replay()
			// The Omi feed: two arrivals ten minutes apart make its longest quiet; a quiet
			// past that opens one question.
			clip := Speech(h.T, h.Fixtures["speech"], talk[:1])
			h.Live(clip, 0, h.Now())
			h.Advance(10 * time.Minute)
			h.Live(clip, 1000, h.Now())
			h.Advance(11 * time.Minute)
			Until(90*time.Second, func() bool {
				for _, m := range AgentMessages(h.Today()) {
					if m == "The Omi has gone quiet." {
						return true
					}
				}
				return false
			})
			h.Advance(30 * time.Minute) // a longer quiet asks no second time
			time.Sleep(5 * time.Second)
			for _, r := range Requests(h.T, h.Fixtures["model:anthropic"]) {
				if bytes.Contains(r.Body, []byte("feed omi has been quiet since")) {
					st16.quietAsks++
				}
			}
		},
		Assert: func(h *H, c *C) {
			c.Check("call_started", st16.ranLines >= 1, "the long call never started")
			c.Check("server_restarted", st16.restarted, "the server did not come back after kill -9")
			c.Check("interruption_reported", st16.reported, "the interrupted call was not reported to the agent")
			c.Check("not_rerun", st16.ranLines == 1, "the interrupted call ran %d times", st16.ranLines)
			turns, _ := st16.replay["turns"].(float64)
			c.Check("sealed_turns_intact", st16.replay["ok"] == true && turns >= 2, "replay after the crash: %v", st16.replay)
			c.Check("silent_feed_asks_once", st16.quietAsks == 1, "the quiet Omi feed was asked about %d times", st16.quietAsks)
		},
	})
}
