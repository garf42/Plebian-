package loop_test

import (
	"strings"
	"testing"
	"time"

	"plebian/tests/fixtures/model"
	"plebian/tests/kit"
)

func clockTurns(k *kit.Kit) int {
	var n int
	k.S.DB.QueryRow(`SELECT COUNT(*) FROM turns WHERE opener='clock'`).Scan(&n)
	return n
}

// A13 (n.loop): asked for the day's entry on two nights at about the same hour, the
// clock opens the third night's turn at that hour, and opened none before.
func TestClockFromTwoAsks(t *testing.T) {
	entry := `{"lines":[{"text":"Dale brings the gravel Wednesday."}]}`
	steps := []*model.Step{}
	for i := 0; i < 2; i++ {
		steps = append(steps, &model.Step{Match: "entry", Reply: &model.Reply{ToolCalls: []model.ToolCall{{Name: "entry", Input: []byte(entry)}}}}, &model.Step{Match: "held until", Reply: &model.Reply{Text: "Written."}})
	}
	steps = append(steps, &model.Step{Match: "asked for", Reply: &model.Reply{ToolCalls: []model.ToolCall{{Name: "entry", Input: []byte(entry)}}}}, &model.Step{Match: "held until", Reply: &model.Reply{Text: "Written."}})
	k := kit.New(t, "anthropic", model.Script{Steps: steps})
	k.S.SetSetting(ctx, "tz", "America/Chicago")
	loc, _ := time.LoadLocation("America/Chicago")
	at := func(day, hour, min int) {
		target := time.Date(2026, 11, 27+day, hour, min, 0, 0, loc)
		if d := target.Sub(k.Clock.Now()); d > 0 {
			k.Clock.Advance(d)
		}
		k.Run()
	}
	at(0, 21, 0)
	k.Say("write today's entry")
	at(1, 21, 0)
	if c := clockTurns(k); c != 0 {
		t.Fatalf("the clock opened %d turns after one ask", c)
	}
	k.Say("write today's entry")
	at(2, 20, 0) // before the window's hour
	if c := clockTurns(k); c != 0 {
		t.Fatalf("the clock opened %d turns before the third night's hour", c)
	}
	at(2, 21, 3)
	if c := clockTurns(k); c != 1 {
		t.Fatalf("the clock opened %d turns on the third night", c)
	}
	var body string
	k.S.DB.QueryRow(`SELECT body FROM inbounds WHERE kind='clock'`).Scan(&body)
	if !strings.Contains(body, `"entry"`) || !strings.Contains(body, "2 of the 2 days") {
		t.Fatalf("clock inbound %q", body)
	}
	var entries int
	k.S.DB.QueryRow(`SELECT COUNT(*) FROM events WHERE type='entry'`).Scan(&entries)
	if entries != 3 {
		t.Fatalf("%d entries written", entries)
	}
	at(2, 23, 0)
	if c := clockTurns(k); c != 1 {
		t.Fatalf("the clock opened %d turns in one night", c)
	}
}
