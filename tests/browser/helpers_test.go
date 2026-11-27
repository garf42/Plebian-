package browser_test

import (
	"context"
	"encoding/json"
	"os/exec"

	"plebian/server/browser"
	"plebian/server/models"
	"plebian/server/tools"
	"plebian/tests/fixtures/model"
	"plebian/tests/kit"
)

func execLook(name string) (string, error) { return exec.LookPath(name) }

func call(name, input string) *model.Step {
	return &model.Step{Reply: &model.Reply{ToolCalls: []model.ToolCall{{Name: name, Input: json.RawMessage(input)}}}}
}

// subBrowse is browse with its sub-agent calls on the kit's loop.
type subBrowse struct {
	b *browser.Browse
	k **kit.Kit
}

func (s *subBrowse) Def() models.Tool                     { return s.b.Def() }
func (s *subBrowse) Class(in json.RawMessage) tools.Class { return s.b.Class(in) }
func (s *subBrowse) ReachesOut(in json.RawMessage) bool   { return s.b.ReachesOut(in) }
func (s *subBrowse) Run(ctx context.Context, in json.RawMessage) (tools.Result, error) {
	b := *s.b
	b.Send = func(ctx context.Context, r models.Request) (models.Response, error) { return (*s.k).L.SubCall(ctx, r) }
	return b.Run(ctx, in)
}
