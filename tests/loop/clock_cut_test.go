package loop_test

import (
	"context"
	"testing"
	"time"

	"plebian/tests/fixtures/model"
	"plebian/tests/kit"
)

// A13 at any hour (n.loop): asked just after the page cut on two nights, the clock opens
// the third night's turn at that hour too.
func TestClockFromTwoAsksAtTheCut(t *testing.T) {
	entry := `{"lines":[{"text":"Dale brings the gravel Wednesday."}]}`
	for _, start := range []struct{ hour, min int }{{4, 9}, {3, 50}, {21, 0}} {
		steps := []*model.Step{}
		for i := 0; i < 2; i++ {
			steps = append(steps, &model.Step{Match: "entry", Reply: &model.Reply{ToolCalls: []model.ToolCall{{Name: "entry", Input: []byte(entry)}}}}, &model.Step{Match: "held until", Reply: &model.Reply{Text: "Written."}})
		}
		steps = append(steps, &model.Step{Match: "asked for", Reply: &model.Reply{ToolCalls: []model.ToolCall{{Name: "entry", Input: []byte(entry)}}}}, &model.Step{Match: "held until", Reply: &model.Reply{Text: "Written."}})
		k := kit.New(t, "anthropic", model.Script{Steps: steps})
		k.S.SetSetting(ctx, "tz", "America/Chicago")
		loc, _ := time.LoadLocation("America/Chicago")
		t0 := time.Date(2026, 11, 28, start.hour, start.min, 0, 0, loc) // after the kit's clock starts
		at := func(d time.Duration) {
			if x := t0.Add(d).Sub(k.Clock.Now()); x > 0 {
				k.Clock.Advance(x)
			}
			k.Run()
		}
		at(0)
		k.Say("write today's entry")
		at(24 * time.Hour)
		k.Say("write today's entry")
		at(47 * time.Hour)
		if c := clockTurns(k); c != 0 {
			t.Fatalf("from %02d:%02d: the clock opened %d turns an hour early", start.hour, start.min, c)
		}
		at(48*time.Hour + 5*time.Minute)
		if c := clockTurns(k); c != 1 {
			t.Fatalf("from %02d:%02d: the clock opened %d turns on the third night", start.hour, start.min, c)
		}
	}
}

// A13 as the server runs it (n.loop): the loop's own cycle, not a test stepping it, wakes
// for the third night's window after a page day on which nothing was due.
func TestClockWakesTheRunningLoop(t *testing.T) {
	entry := `{"lines":[{"text":"Dale brings the gravel Wednesday."}]}`
	steps := []*model.Step{}
	for i := 0; i < 2; i++ {
		steps = append(steps, &model.Step{Match: "entry", Reply: &model.Reply{ToolCalls: []model.ToolCall{{Name: "entry", Input: []byte(entry)}}}}, &model.Step{Match: "held until", Reply: &model.Reply{Text: "Written."}})
	}
	steps = append(steps, &model.Step{Match: "asked for", Reply: &model.Reply{ToolCalls: []model.ToolCall{{Name: "entry", Input: []byte(entry)}}}}, &model.Step{Match: "held until", Reply: &model.Reply{Text: "Written."}})
	k := kit.New(t, "anthropic", model.Script{Steps: steps})
	k.S.SetSetting(ctx, "tz", "America/Chicago")
	loc, _ := time.LoadLocation("America/Chicago")
	t0 := time.Date(2026, 11, 28, 4, 9, 0, 0, loc)
	k.Clock.Advance(t0.Sub(k.Clock.Now()))
	k.Say("write today's entry")
	k.Clock.Advance(t0.Add(24 * time.Hour).Sub(k.Clock.Now()))
	k.Say("write today's entry")
	k.Clock.Advance(t0.Add(47 * time.Hour).Sub(k.Clock.Now())) // the next page day hasn't begun
	run, stop := context.WithCancel(ctx)
	done := make(chan struct{})
	go func() { k.L.Run(run); close(done) }()
	defer func() { stop(); <-done }()
	time.Sleep(200 * time.Millisecond) // the loop settles on its timer
	k.Clock.Advance(t0.Add(48*time.Hour + 5*time.Minute).Sub(k.Clock.Now()))
	deadline := time.Now().Add(5 * time.Second)
	for clockTurns(k) == 0 && time.Now().Before(deadline) {
		time.Sleep(20 * time.Millisecond)
	}
	if c := clockTurns(k); c != 1 {
		t.Fatalf("the running loop opened %d clock turns on the third night", c)
	}
}
