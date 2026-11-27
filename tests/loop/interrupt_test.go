package loop_test

import (
	"context"
	"encoding/json"
	"strings"
	"sync"
	"testing"
	"time"

	"plebian/server/models"
	"plebian/server/tools"
	"plebian/tests/fixtures/model"
	"plebian/tests/kit"
)

// stuck is a change-making call that never returns: the server dies while it runs.
type stuck struct {
	mu   sync.Mutex
	runs int
}

func (s *stuck) Def() models.Tool {
	return models.Tool{Name: "post_order", Description: "post", Schema: tools.Schema(nil, map[string][2]string{"qty": {"integer", "qty"}})}
}
func (s *stuck) Class(json.RawMessage) tools.Class { return tools.Local }
func (s *stuck) ReachesOut(json.RawMessage) bool   { return false }
func (s *stuck) Run(ctx context.Context, _ json.RawMessage) (tools.Result, error) {
	s.mu.Lock()
	s.runs++
	s.mu.Unlock()
	<-ctx.Done()
	select {} // the process is gone; nothing returns
}

// D4 (n.loop), AG-06: a change-making call interrupted mid-run is reported to the agent
// as interrupted after the restart and never re-run on its own.
func TestInterruptedNotRerun(t *testing.T) {
	st := &stuck{}
	k := kit.New(t, "anthropic", model.Script{Steps: []*model.Step{
		call("post_order", `{"qty":2}`),
		{Match: "interrupted", Reply: &model.Reply{Text: "It was interrupted; I did not post it again."}},
	}}, st)
	go k.Say("post the weekly order")
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		st.mu.Lock()
		n := st.runs
		st.mu.Unlock()
		if n == 1 {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	k.Restart() // the old loop is abandoned mid-call, as kill -9 leaves it
	k.Run()
	st.mu.Lock()
	runs := st.runs
	st.mu.Unlock()
	if runs != 1 {
		t.Fatalf("the interrupted call ran %d times", runs)
	}
	var reported bool
	for _, r := range k.Requests() {
		if strings.Contains(string(r), "interrupted: the server stopped while this call was running") {
			reported = true
		}
	}
	if !reported {
		t.Fatal("the interruption was not reported to the agent")
	}
}
