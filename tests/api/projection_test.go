package api_test

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"plebian/server/api"
	"plebian/tests/fixtures/model"
	"plebian/tests/kit"
)

// D3 (n.api): a page is a projection of the store alone: rendered at two different
// times, it is byte-identical, so a replay of it can match.
func TestPageReadsNoClock(t *testing.T) {
	k := kit.New(t, "anthropic", model.Script{Default: &model.Reply{Text: "Wednesday, by the culvert."}})
	k.S.SetSetting(context.Background(), "tz", "America/Chicago")
	k.Say("when is the gravel coming?")
	a := &api.Server{S: k.S, Clock: k.Clock}
	date := api.DayOf(k.Clock.Now(), api.Location(context.Background(), k.S))
	p1, err := a.Page(context.Background(), date)
	if err != nil {
		t.Fatal(err)
	}
	k.Clock.Advance(26 * time.Hour)
	p2, _ := a.Page(context.Background(), date)
	b1, _ := json.Marshal(p1)
	b2, _ := json.Marshal(p2)
	if string(b1) != string(b2) || len(p1["thread"].([]map[string]any)) == 0 {
		t.Fatalf("the page changed with the clock:\n%s\n%s", b1, b2)
	}
}
