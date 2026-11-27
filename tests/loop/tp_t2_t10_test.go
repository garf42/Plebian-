package loop_test

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"plebian/server/browser"
	"plebian/server/config"
	"plebian/server/feeds"
	"plebian/server/loop"
	"plebian/server/store"
	"plebian/server/tools"
	"plebian/tests/fixtures/model"
	"plebian/tests/kit"
)

// The memory tables (TP-03).
var memory = []string{"turns", "fragments", "symbols", "pairs", "uses", "frames", "classes", "stand_ins", "outcomes", "trust", "bands", "heard", "standing"}

func memoryHash(t *testing.T, s *store.Store) string {
	h := sha256.New()
	for _, tb := range memory {
		rows, err := s.DB.Query(`SELECT * FROM ` + tb + ` ORDER BY rowid`)
		if err != nil {
			t.Fatal(err)
		}
		cols, _ := rows.Columns()
		for rows.Next() {
			v := make([]any, len(cols))
			p := make([]any, len(cols))
			for i := range v {
				p[i] = &v[i]
			}
			rows.Scan(p...)
			fmt.Fprintf(h, "%s %v\n", tb, v)
		}
		rows.Close()
	}
	return fmt.Sprintf("%x", h.Sum(nil))
}

// TP-T2: no tool the agent holds writes a memory table; each runs here on ordinary
// input and the memory tables are byte-identical after.
func TestNoMemoryTools(t *testing.T) {
	page := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte("<html><title>t</title><p>text</p></html>"))
	}))
	defer page.Close()
	k := kit.New(t, "anthropic", model.Script{Steps: []*model.Step{say("Noted.")}})
	k.Say("the gravel comes Wednesday") // a store with memory in it
	sec := t.TempDir()
	secrets := &config.Secrets{Path: filepath.Join(sec, "secrets.json")}
	mail := &feeds.Mail{L: &feeds.Lander{S: k.S, Clock: k.Clock}, Secrets: secrets}
	box := &browser.Box{Chrome: "/nonexistent", Node: "/nonexistent", MCPCli: "/nonexistent", Profile: sec}
	all := append(loop.CoreTools(k.S, nil, nil),
		tools.Fetch{HTTP: page.Client()}, tools.Search{HTTP: page.Client(), URL: func() string { return page.URL + "/?q=" }},
		feeds.SendTool{M: mail, Clock: k.Clock}, feeds.TextTool{T: &feeds.Texts{L: mail.L, Secrets: secrets}},
		browser.Browse{B: box}, browser.Send{B: box})
	inputs := map[string]string{
		"ask": `{"question":"When does the gravel come?"}`, "keep": `{"words":"the gravel comes Wednesday"}`,
		"lookup": `{"words":"gravel"}`, "entry": `{"lines":[{"text":"Gravel Wednesday."}]}`,
		"fetch": `{"url":"` + page.URL + `"}`, "search": `{"query":"gravel"}`,
		"mail_send": `{"to":"a@example.com","body":"x"}`, "text_send": `{"conversation_id":"c","text":"x"}`,
		"browse": `{"task":"x"}`, "browser_send": `{"held":1}`,
	}
	before := memoryHash(t, k.S)
	for _, tl := range all {
		name := tl.Def().Name
		in, ok := inputs[name]
		if !ok {
			t.Fatalf("no input for tool %s: every tool must be checked", name)
		}
		tl.Run(context.Background(), json.RawMessage(in))
		if after := memoryHash(t, k.S); after != before {
			t.Fatalf("tool %s wrote a memory table", name)
		}
	}
}

// TP-T10: no slot class is read when charging outcomes or closing loops: the code that
// charges and closes names neither classes nor frames, and filling them changes nothing.
func TestClassesNeverTouchTrust(t *testing.T) {
	re := regexp.MustCompile(`(?i)\b(FROM|JOIN|INTO|UPDATE)\s+(classes|frames)\b`)
	for _, dir := range []string{"../../server/recall", "../../server/loop"} {
		fs, _ := filepath.Glob(filepath.Join(dir, "*.go"))
		for _, f := range fs {
			b, _ := os.ReadFile(f)
			if m := re.Find(b); m != nil {
				t.Fatalf("%s reads slot classes: %q", f, m)
			}
		}
	}
	run := func(fill bool) string {
		sent := &sends{}
		in := `{"to":"x@example.com","body":"y"}`
		k := kit.New(t, "anthropic", model.Script{Steps: []*model.Step{call("mail_send", in), say("Drafted.")}}, sent)
		if fill {
			for i := 0; i < 50; i++ {
				k.S.DB.Exec(`INSERT INTO classes(members) VALUES(?)`, strings.Repeat(fmt.Sprint(i), 3))
				k.S.DB.Exec(`INSERT INTO frames(left_sym, right_sym, filler, n) VALUES(?,?,?,?)`, i, i+1, i+2, i)
			}
		}
		k.Say("draft a note to x")
		k.Model.SetScript(model.Script{Steps: []*model.Step{call("mail_send", in), say("Sent.")}})
		k.Say("yes, send it")
		var trust, outcomes string
		k.S.DB.QueryRow(`SELECT COALESCE(group_concat(fragment||':'||a||':'||b, ','),'') FROM (SELECT * FROM trust ORDER BY fragment)`).Scan(&trust)
		k.S.DB.QueryRow(`SELECT COALESCE(group_concat(kind||':'||success, ','),'') FROM (SELECT * FROM outcomes ORDER BY rowid)`).Scan(&outcomes)
		return trust + "|" + outcomes
	}
	if a, b := run(false), run(true); a != b {
		t.Fatalf("filling slot classes changed trust or outcomes:\n%s\n%s", a, b)
	}
}
