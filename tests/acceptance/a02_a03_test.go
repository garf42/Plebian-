package acceptance

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"encoding/base64"
	"fmt"
	"net/http"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"time"
)

// Endpoints an unpaired client tries. Each must answer 401 with nothing in the body.
var guarded = []struct{ method, path, body string }{
	{"GET", "/v1/today", ""}, {"GET", "/v1/thread", ""}, {"GET", "/v1/days", ""}, {"GET", "/v1/days/2026-11-27", ""},
	{"GET", "/v1/taps", ""}, {"GET", "/v1/open", ""}, {"GET", "/v1/search?q=a", ""}, {"GET", "/v1/week", ""},
	{"GET", "/v1/setup", ""}, {"GET", "/v1/events", ""}, {"GET", "/v1/blobs/" + strings.Repeat("0", 64), ""},
	{"POST", "/v1/messages", `{"client_msg_id":"x","text":"hi","sent_at":"2026-11-27T10:00:00Z"}`},
	{"POST", "/v1/taps", `{"at":"2026-11-27T10:00:00Z","how":"app"}`},
	{"POST", "/v1/setup/model", `{"base_url":"http://127.0.0.1:1"}`},
	{"POST", "/v1/admin/grant", `{"step":"mail_send","nonce":"1","at":"2026-11-27T10:00:00Z"}`},
	{"POST", "/v1/audio/files", "x"}, {"POST", "/v1/omi/state", `{}`},
}

type a02 struct {
	secondToken  string
	secondReads  int
	reuseStatus  int
	unpaired     map[string][2]int // path → status, body length
	wrongToken   map[string]int
	grantBearer  int
	messagesSent int
}

func pairNew(h *H) (string, *ecdsa.PrivateKey, int, string) {
	out, _ := sudo(filepath.Join(h.Root, "bin", "plebian"), "pair").CombinedOutput()
	m := regexp.MustCompile(`Pairing code: (\S+)`).FindStringSubmatch(string(out))
	if m == nil {
		h.T.Fatalf("pair printed no code: %s", out)
	}
	k, _ := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	der, _ := x509.MarshalPKIXPublicKey(&k.PublicKey)
	var r struct{ Token string }
	st := h.Do("POST", "/v1/pair", map[string]string{"code": m[1], "public_key": base64.StdEncoding.EncodeToString(der), "name": "second"}, &r, false)
	return r.Token, k, st, m[1]
}

func init() {
	var st *a02
	register(Scenario{
		ID:    "A02",
		Setup: func(t testing.TB) *H { st = &a02{}; return fresh(t) },
		Drive: func(h *H) {
			st.unpaired, st.wrongToken = map[string][2]int{}, map[string]int{}
			for _, e := range guarded {
				for _, tok := range []string{"", "not-a-token"} {
					req, _ := http.NewRequest(e.method, "http://"+h.Addr+e.path, strings.NewReader(e.body))
					if tok != "" {
						req.Header.Set("Authorization", "Bearer "+tok)
					}
					resp, err := http.DefaultClient.Do(req)
					if err != nil {
						continue
					}
					var n int64
					buf := make([]byte, 512)
					for {
						k, err := resp.Body.Read(buf)
						n += int64(k)
						if err != nil {
							break
						}
					}
					resp.Body.Close()
					if tok == "" {
						st.unpaired[e.method+" "+e.path] = [2]int{resp.StatusCode, int(n)}
					} else {
						st.wrongToken[e.method+" "+e.path] = resp.StatusCode
					}
				}
			}
			tok, _, status, code := pairNew(h)
			if status == 200 {
				st.secondToken = tok
				saved := h.Token
				h.Token = tok
				st.secondReads = h.Do("GET", "/v1/today", nil, nil, true)
				h.Token = saved
			}
			k, _ := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
			der, _ := x509.MarshalPKIXPublicKey(&k.PublicKey)
			st.reuseStatus = h.Do("POST", "/v1/pair", map[string]string{"code": code, "public_key": base64.StdEncoding.EncodeToString(der), "name": "reuse"}, nil, false)
			st.grantBearer = h.Do("POST", "/v1/admin/grant", map[string]any{"step": "mail_send", "nonce": "n1", "at": time.Now().UTC().Format(time.RFC3339)}, nil, true)
			var p map[string]any
			h.Do("GET", "/v1/thread", nil, &p, true)
		},
		Assert: func(h *H, c *C) {
			c.Check("second_device_paired", st.secondToken != "" && st.secondReads == 200, "a fresh code did not pair a second device that can read (status %d)", st.secondReads)
			c.Check("probed_unpaired", len(st.unpaired) == len(guarded), "probed %d of %d endpoints unpaired", len(st.unpaired), len(guarded))
			c.Check("code_works_once", st.reuseStatus == 401, "a used pairing code answered %d", st.reuseStatus)
			for p, v := range st.unpaired {
				c.Check("unpaired_reads_nothing", v[0] == 401 && v[1] == 0, "%s answered %d with %d bytes", p, v[0], v[1])
			}
			for p, v := range st.wrongToken {
				c.Check("wrong_token_reads_nothing", v == 401, "%s answered %d", p, v)
			}
			c.Check("grant_needs_signature", st.grantBearer == 403, "a grant with a bearer token alone answered %d", st.grantBearer)
		},
	})

	register(Scenario{
		ID: "A03",
		Setup: func(t testing.TB) *H {
			h := fresh(t)
			h.Model("anthropic", []map[string]any{{"match": "gravel", "reply": map[string]any{"text": "The gravel comes Wednesday, by the culvert."}}})
			return h
		},
		Drive: func(h *H) {
			h.Say("when is the gravel coming?")
			Until(60*time.Second, func() bool { return len(AgentMessages(h.Today())) > 0 })
		},
		Assert: func(h *H, c *C) {
			reqs := Requests(h.T, h.Fixtures["model:anthropic"])
			saw := false
			for _, r := range reqs {
				if strings.Contains(string(r.Body), "when is the gravel coming?") {
					saw = true
				}
			}
			c.Check("model_received_owner_words", saw, "the model fixture never received the owner's message (%d requests)", len(reqs))
			today := h.Today()
			msgs := AgentMessages(today)
			c.Check("reply_on_today", len(msgs) == 1 && msgs[0] == "The gravel comes Wednesday, by the culvert.", "Today shows agent messages %q", msgs)
			var page map[string]any
			h.Do("GET", "/v1/days/"+fmt.Sprint(today["date"]), nil, &page, true)
			dm := AgentMessages(page)
			c.Check("reply_on_day_page", len(dm) == 1 && dm[0] == "The gravel comes Wednesday, by the culvert.", "the day page shows %q", dm)
			rep := h.Replay()
			turns, _ := rep["turns"].(float64)
			c.Check("turns_sealed", turns >= 1, "replay found %v sealed turns", rep["turns"])
			c.Check("turns_rebuild_byte_exact", rep["ok"] == true, "replay: %v", rep)
		},
	})
}

func TestA02(t *testing.T) { Run(t, "A02") }
func TestA03(t *testing.T) { Run(t, "A03") }

var _ = exec.Command
