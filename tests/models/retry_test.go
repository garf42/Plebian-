package models_test

import (
	"context"
	"net/http/httptest"
	"testing"
	"time"

	"plebian/server/models"
	"plebian/tests/fixtures/model"
	"plebian/tests/kit"
)

// D2 (n.models), AG-02: a 429 whose Retry-After fits the owner's usual time to answer is
// waited out on the same provider; only a wait past it hands the turn on.
func TestRetryWindow(t *testing.T) {
	ctx := context.Background()
	for _, c := range []struct {
		retry  int
		window time.Duration
		want   bool // the same provider answers
	}{{7, time.Minute, true}, {3600, time.Minute, false}} {
		f := model.New("k")
		f.SetScript(model.Script{Steps: []*model.Step{{Error: &model.Fault{Status: 429, RetryAfterS: c.retry}}, {Reply: &model.Reply{Text: "after the wait"}}}})
		srv := httptest.NewServer(f.Handler())
		p, ms, err := models.Detect(ctx, srv.URL+"/anthropic", "k", nil)
		if err != nil {
			t.Fatal(err)
		}
		clk := kit.NewClock(time.Now())
		done := make(chan error, 1)
		var resp models.Response
		go func() {
			var err error
			resp, err = models.SendWithin(ctx, &p, models.Request{Model: ms[0].ID, Messages: []models.Message{{Role: "user", Content: []models.Block{{Type: "text", Text: "hi"}}}}, MaxTokens: 100}, c.window, clk, nil)
			done <- err
		}()
		time.Sleep(200 * time.Millisecond)
		clk.Advance(time.Duration(c.retry) * time.Second)
		select {
		case err = <-done:
		case <-time.After(5 * time.Second):
			t.Fatal("no answer")
		}
		srv.Close()
		got := err == nil && len(resp.Content) > 0 && resp.Content[0].Text == "after the wait"
		if got != c.want {
			t.Fatalf("Retry-After %ds within %v: same provider answered %v (err %v)", c.retry, c.window, got, err)
		}
	}
}
