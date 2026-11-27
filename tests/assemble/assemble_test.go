package assemble_test

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"regexp"
	"strings"
	"testing"

	"plebian/tests/fixtures/model"
	"plebian/tests/kit"
)

var ctx = context.Background()

func count(t *testing.T, k *kit.Kit, body []byte) int {
	var m map[string]json.RawMessage
	json.Unmarshal(body, &m)
	delete(m, "max_tokens")
	delete(m, "stream")
	b, _ := json.Marshal(m)
	req, _ := http.NewRequest("POST", k.Route.Provider.BaseURL+"/v1/messages/count_tokens", bytes.NewReader(b))
	req.Header.Set("x-api-key", "kit-key")
	req.Header.Set("anthropic-version", "2023-06-01")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	var v struct {
		InputTokens int `json:"input_tokens"`
	}
	json.NewDecoder(resp.Body).Decode(&v)
	return v.InputTokens
}

func long(i int) string {
	return strings.Repeat("the culvert pipe and the gravel delivery need sorting before the frost ", 30) + " number " + string(rune('a'+i%26))
}

// TP-T4: a request that opens an exchange never exceeds B; the tail folds into recent
// strata to keep it there (D1: the budget is checked only after the spotlight is added).
func TestBudgetOnOpening(t *testing.T) {
	k := kit.New(t, "anthropic", model.Script{Default: &model.Reply{Text: "noted"}})
	B := 16000
	k.S.DB.Exec(`INSERT INTO budget(model, b) VALUES(?, ?)`, k.Route.Model.ID, B)
	for i := 0; i < 40; i++ {
		k.Say(long(i))
	}
	reqs := k.Requests()
	if len(reqs) != 40 {
		t.Fatalf("%d requests for 40 exchanges", len(reqs))
	}
	folded := false
	for i, r := range reqs {
		n := count(t, k, r)
		if n > B {
			t.Fatalf("opening request %d counted %d tokens, B is %d", i, n, B)
		}
		if bytes.Contains(r, []byte(`strata layer=\"recent\"`)) {
			folded = true
		}
	}
	if !folded {
		t.Fatal("the tail never folded into recent strata")
	}
}

// TP-T3: every rendered strata line is a byte-exact span of a sealed turn.
func TestStrataExtractive(t *testing.T) {
	k := kit.New(t, "anthropic", model.Script{Default: &model.Reply{Text: "noted, the culvert first"}})
	k.S.DB.Exec(`INSERT INTO budget(model, b) VALUES(?, 16000)`, k.Route.Model.ID)
	for i := 0; i < 40; i++ {
		k.Say(long(i))
	}
	reqs := k.Requests()
	last := reqs[len(reqs)-1]
	var m struct {
		Messages []struct {
			Content []struct{ Text string } `json:"content"`
		} `json:"messages"`
	}
	json.Unmarshal(last, &m)
	line := regexp.MustCompile(`<line turn="(\d+)" who="\w+">([\s\S]*?)</line>`)
	seen := 0
	for _, msg := range m.Messages {
		for _, c := range msg.Content {
			for _, mm := range line.FindAllStringSubmatch(c.Text, -1) {
				var tn int64
				json.Unmarshal([]byte(mm[1]), &tn)
				b, err := k.S.TurnBytes(ctx, tn)
				if err != nil {
					t.Fatal(err)
				}
				if !bytes.Contains(b, []byte(mm[2])) {
					t.Fatalf("strata line %q is not a span of turn %d", mm[2], tn)
				}
				seen++
			}
		}
	}
	if seen == 0 {
		t.Fatal("no strata lines rendered")
	}
}

// D4: the model's thinking is never stored with the sheet.
func TestNoThinkingStored(t *testing.T) {
	k := kit.New(t, "anthropic", model.Script{Default: &model.Reply{Text: "done", Thinking: "SECRET-CHAIN-OF-THOUGHT"}})
	k.Say("think about the gravel")
	var n int
	k.S.DB.QueryRow(`SELECT COUNT(*) FROM turns`).Scan(&n)
	for i := int64(1); i <= int64(n); i++ {
		b, err := k.S.TurnBytes(ctx, i)
		if err != nil {
			t.Fatal(err)
		}
		if bytes.Contains(b, []byte("SECRET-CHAIN-OF-THOUGHT")) {
			t.Fatalf("turn %d holds the model's thinking", i)
		}
	}
	var hits int
	k.S.DB.QueryRow(`SELECT COUNT(*) FROM symbols WHERE instr(expansion, 'SECRET') > 0`).Scan(&hits)
	if hits > 0 {
		t.Fatal("the thinking reached the grammar")
	}
}

// TP-T13 and D3: consecutive requests diverge only at band boundaries. Within an
// exchange the request only grows; across exchanges the bands above the tail are
// unchanged until a merge rebuilds them.
func TestCacheBreaksOnBoundaries(t *testing.T) {
	k := kit.New(t, "anthropic", model.Script{Default: &model.Reply{Text: "noted"}})
	for i := 0; i < 6; i++ {
		k.Say("tell me about the culvert, part " + string(rune('a'+i)))
	}
	reqs := k.Requests()
	type body struct {
		System   json.RawMessage   `json:"system"`
		Tools    json.RawMessage   `json:"tools"`
		Messages []json.RawMessage `json:"messages"`
	}
	for i := 1; i < len(reqs); i++ {
		var a, b body
		json.Unmarshal(reqs[i-1], &a)
		json.Unmarshal(reqs[i], &b)
		if !bytes.Equal(a.System, b.System) || !bytes.Equal(a.Tools, b.Tools) {
			t.Fatalf("request %d rewrote the tools or system band", i)
		}
		// Every message before the previous request's last two (its closing user turn,
		// whose spotlight leaves, and the tail's breakpoint) is byte-identical.
		for j := 0; j+2 < len(a.Messages) && j < len(b.Messages); j++ {
			if !bytes.Equal(stripCache(a.Messages[j]), stripCache(b.Messages[j])) {
				t.Fatalf("request %d changed message %d inside the tail", i, j)
			}
		}
	}
}

var cacheRe = regexp.MustCompile(`,"cache_control":\{[^}]*\}`)

func stripCache(b []byte) []byte { return cacheRe.ReplaceAll(b, nil) }

// TP-T5/T7 and D2: heard lines lit past chance ride in the user turn of the requests of
// one exchange only; they never enter a band or the tail of a later exchange.
func TestHeardUserTurnOnly(t *testing.T) {
	k := kit.New(t, "anthropic", model.Script{Default: &model.Reply{Text: "noted"}})
	k.L.Heard = func(ctx context.Context, cue []byte) []byte {
		if bytes.Contains(cue, []byte("culvert")) {
			return []byte("<heard id=\"1\">Dana · 09:14: OVERHEARD-CULVERT-LINE</heard>\n")
		}
		return nil
	}
	k.Say("what about the culvert")
	k.Say("and the fence posts")
	reqs := k.Requests()
	if !bytes.Contains(reqs[0], []byte("OVERHEARD-CULVERT-LINE")) {
		t.Fatal("the lit heard line never reached the request it was lit for")
	}
	if bytes.Contains(reqs[1], []byte("OVERHEARD-CULVERT-LINE")) {
		t.Fatal("a heard line rode into a later exchange's request")
	}
	var n int
	k.S.DB.QueryRow(`SELECT COUNT(*) FROM turns`).Scan(&n)
	for i := int64(1); i <= int64(n); i++ {
		b, _ := k.S.TurnBytes(ctx, i)
		if bytes.Contains(b, []byte("OVERHEARD-CULVERT-LINE")) {
			t.Fatalf("turn %d holds heard text", i)
		}
	}
}
