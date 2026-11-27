// Package tools is n.tools: the tool band (one line per tool), each step's class, and
// the runs. The draft rule (n.loop) reads Class and ReachesOut; nothing here decides
// whether a call runs.
package tools

import (
	"context"
	"encoding/json"
	"sort"

	"plebian/server/models"
)

type Class string

const (
	Read    Class = "read"
	Local   Class = "local change"
	Outside Class = "outside change"
)

// Result is what a run returns: the reading's bytes, its exit code, and whether its
// bytes came from beyond the machine (outside words).
type Result struct {
	Reading []byte
	Exit    int
	Outside bool
	Job     string // set when the run outlasts its turn
}

// Tool is one line of the band.
type Tool interface {
	Def() models.Tool
	// Class is the step's class for this input; anything that can't be shown is Outside.
	Class(input json.RawMessage) Class
	// ReachesOut reports a read that leaves the machine (fetch, search, browse, open-world MCP).
	ReachesOut(input json.RawMessage) bool
	Run(ctx context.Context, input json.RawMessage) (Result, error)
}

// Registry holds the band. The band never changes with what is in view.
type Registry struct{ tools map[string]Tool }

func NewRegistry(ts ...Tool) *Registry {
	r := &Registry{tools: map[string]Tool{}}
	for _, t := range ts {
		r.tools[t.Def().Name] = t
	}
	return r
}

func (r *Registry) Add(t Tool) { r.tools[t.Def().Name] = t }

func (r *Registry) Get(name string) (Tool, bool) { t, ok := r.tools[name]; return t, ok }

// Band is the tool list in name order (stable across requests, so it caches).
func (r *Registry) Band() []models.Tool {
	names := make([]string, 0, len(r.tools))
	for n := range r.tools {
		names = append(names, n)
	}
	sort.Strings(names)
	out := make([]models.Tool, 0, len(names))
	for _, n := range names {
		out = append(out, r.tools[n].Def())
	}
	return out
}

// ClassOf is the class of a call; an unknown tool is an outside change.
func (r *Registry) ClassOf(name string, input json.RawMessage) Class {
	if t, ok := r.tools[name]; ok {
		return t.Class(input)
	}
	return Outside
}

func (r *Registry) ReachesOut(name string, input json.RawMessage) bool {
	if t, ok := r.tools[name]; ok {
		return t.ReachesOut(input)
	}
	return true
}

// Schema builds a JSON schema object from property name → (type, description) pairs.
func Schema(required []string, props map[string][2]string) json.RawMessage {
	p := map[string]map[string]string{}
	for k, v := range props {
		p[k] = map[string]string{"type": v[0], "description": v[1]}
	}
	if required == nil {
		required = []string{}
	}
	b, _ := json.Marshal(map[string]any{"type": "object", "properties": p, "required": required, "additionalProperties": false})
	return b
}

// Func adapts plain functions into a Tool.
type Func struct {
	D       models.Tool
	C       Class
	Out     bool
	RunFunc func(ctx context.Context, input json.RawMessage) (Result, error)
}

func (f Func) Def() models.Tool                                            { return f.D }
func (f Func) Class(json.RawMessage) Class                                 { return f.C }
func (f Func) ReachesOut(json.RawMessage) bool                             { return f.Out }
func (f Func) Run(ctx context.Context, in json.RawMessage) (Result, error) { return f.RunFunc(ctx, in) }
