package feeds_test

import (
	"encoding/json"
	"testing"

	"plebian/server/feeds"
	fmail "plebian/tests/fixtures/mail"
	"plebian/tests/fixtures/model"
	"plebian/tests/kit"
)

func sendCall(in string) *model.Step {
	return &model.Step{Reply: &model.Reply{ToolCalls: []model.ToolCall{{Name: "mail_send", Input: json.RawMessage(in)}}}}
}

func say(s string) *model.Step { return &model.Step{Reply: &model.Reply{Text: s}} }

// D2: SMTP sends only an approved draft: drafted on the agent's call, sent once, its
// shown bytes exactly, after an owner message the agent answers by repeating the call.
func TestSMTPOnlyOnApprovedDraft(t *testing.T) {
	e := newEnv(t)
	m := fmail.New("owner@example.com", "pw")
	srv, err := m.Start("127.0.0.1:0", "127.0.0.1:0", "127.0.0.1:0", false)
	if err != nil {
		t.Fatal(err)
	}
	defer srv.Close()
	mf := &feeds.Mail{L: e.l, Secrets: e.sec, Usual: hour}
	if err := mf.Connect(ctx, feeds.Account{IMAP: srv.Info["imap"].(string), SMTP: srv.Info["smtp"].(string), User: "owner@example.com", Password: "pw"}); err != nil {
		t.Fatal(err)
	}
	in := `{"to":"Dale <dale@quarry.example>","body":"Wednesday works."}`
	k := kit.New(t, "anthropic", model.Script{Steps: []*model.Step{sendCall(in), say("Drafted.")}}, feeds.SendTool{M: mf, Clock: e.clock})
	k.Say("tell Dale Wednesday works")
	if n := len(m.SentMessages()); n != 0 {
		t.Fatalf("SMTP received %d messages before any approval", n)
	}
	var shown string
	k.S.DB.QueryRow(`SELECT shown FROM drafts WHERE state='waiting'`).Scan(&shown)
	k.Model.SetScript(model.Script{Steps: []*model.Step{sendCall(in), say("Sent.")}})
	k.Say("yes, send it")
	sent := m.SentMessages()
	if len(sent) != 1 || string(sent[0].Data) != shown || shown == "" {
		t.Fatalf("after approval SMTP received %d messages; shown bytes sent: %v", len(sent), len(sent) == 1 && string(sent[0].Data) == shown)
	}
}
