// Package model is a scripted model provider speaking two wire formats:
// Anthropic Messages under /anthropic and OpenAI Chat Completions under /openai.
//
// Token rule (both formats, deterministic): input tokens are
// ceil(bytes/4) over the raw JSON of the prompt-bearing request fields exactly
// as sent (Anthropic: system, messages, tools; OpenAI: messages, tools), so
// count_tokens and the message call agree on the same prompt. Output tokens are
// ceil(bytes/4) over the reply's thinking, text and tool-input JSON. Four bytes
// per token is OpenAI's published rule of thumb for English text.
package model

import (
	"bytes"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"

	"plebian/tests/fixtures"
)

// ToolCall is one tool call a scripted reply makes.
type ToolCall struct {
	Name  string          `json:"name"`
	Input json.RawMessage `json:"input"`
}

// Reply is what the scripted model answers.
type Reply struct {
	Text      string     `json:"text,omitempty"`
	ToolCalls []ToolCall `json:"tool_calls,omitempty"`
	Thinking  string     `json:"thinking,omitempty"`
}

// Fault makes a step fail: an HTTP error, or a stream cut after n bytes.
type Fault struct {
	Status                int `json:"status,omitempty"`
	RetryAfterS           int `json:"retry_after_s,omitempty"`
	BreakStreamAfterBytes int `json:"break_stream_after_bytes,omitempty"`
}

// Step is one scripted answer; Match is a regexp over the request's last user content.
type Step struct {
	Match string `json:"match,omitempty"`
	Reply *Reply `json:"reply,omitempty"`
	Error *Fault `json:"error,omitempty"`

	re   *regexp.Regexp
	used bool
}

// Model is one model the provider lists.
type Model struct {
	ID        string `json:"id"`
	Context   int    `json:"context"`
	MaxOutput int    `json:"max_output"`
}

// Script is the body of POST /control/script.
type Script struct {
	Steps   []*Step `json:"steps"`
	Default *Reply  `json:"default,omitempty"`
}

// Provider is the fixture's state.
type Provider struct {
	Key string

	mu     sync.Mutex
	script Script
	models []Model
	seq    int
	log    *fixtures.Log
}

// DefaultModels is the list served until POST /control/models replaces it:
// one model with the context and output limits of the current Claude Opus
// (1M input, 128K output).
var DefaultModels = []Model{{ID: "fixture-model", Context: 1000000, MaxOutput: 128000}}

// New returns a provider accepting key on both formats.
func New(key string) *Provider {
	return &Provider{Key: key, models: DefaultModels, log: fixtures.NewLog("Authorization", "X-Api-Key")}
}

// SetScript replaces the script; every match must compile.
func (p *Provider) SetScript(s Script) error {
	for i, st := range s.Steps {
		if st.Reply == nil && (st.Error == nil || st.Error.Status == 0) && s.Default == nil {
			return fmt.Errorf("step %d: needs a reply (or a default) to send", i)
		}
		if st.Match != "" {
			re, err := regexp.Compile(st.Match)
			if err != nil {
				return fmt.Errorf("step %d: %w", i, err)
			}
			st.re = re
		}
	}
	p.mu.Lock()
	p.script = s
	p.mu.Unlock()
	return nil
}

// Requests returns every provider request received.
func (p *Provider) Requests() []fixtures.Request { return p.log.List() }

// take consumes the first unused step fitting last; nil step means the default.
func (p *Provider) take(last string) (*Reply, *Fault, bool) {
	p.mu.Lock()
	defer p.mu.Unlock()
	for _, st := range p.script.Steps {
		if st.used || (st.re != nil && !st.re.MatchString(last)) {
			continue
		}
		st.used = true
		r := st.Reply
		if r == nil {
			r = p.script.Default
		}
		return r, st.Error, true
	}
	if p.script.Default != nil {
		return p.script.Default, nil, true
	}
	return nil, nil, false
}

func (p *Provider) next(prefix string) string {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.seq++
	return fmt.Sprintf("%s%d", prefix, p.seq)
}

func (p *Provider) model(id string) (Model, bool) {
	p.mu.Lock()
	defer p.mu.Unlock()
	for _, m := range p.models {
		if m.ID == id {
			return m, true
		}
	}
	return Model{}, false
}

func tokens(n int) int { return (n + 3) / 4 }

func outTokens(r *Reply) int {
	n := len(r.Thinking) + len(r.Text)
	for _, c := range r.ToolCalls {
		n += len(inputJSON(c.Input))
	}
	return tokens(n)
}

func inputJSON(in json.RawMessage) []byte {
	if len(bytes.TrimSpace(in)) == 0 {
		return []byte("{}")
	}
	var b bytes.Buffer
	if json.Compact(&b, in) != nil {
		return in
	}
	return b.Bytes()
}

// pieces splits s after every sep, so streamed deltas follow the content's own boundaries.
func pieces(s, sep string) []string {
	if s == "" {
		return nil
	}
	out := strings.SplitAfter(s, sep)
	if out[len(out)-1] == "" {
		out = out[:len(out)-1]
	}
	return out
}

// Handler serves the provider and its control endpoints.
func (p *Provider) Handler() http.Handler {
	mux := http.NewServeMux()
	rec := func(h http.HandlerFunc) http.Handler { return p.log.Recording(h) }
	mux.Handle("GET /anthropic/v1/models", rec(p.antModels))
	mux.Handle("GET /anthropic/v1/models/{id}", rec(p.antModel))
	mux.Handle("POST /anthropic/v1/messages", rec(p.antMessages))
	mux.Handle("POST /anthropic/v1/messages/count_tokens", rec(p.antCount))
	mux.Handle("GET /openai/v1/models", rec(p.oaiModels))
	mux.Handle("POST /openai/v1/chat/completions", rec(p.oaiChat))
	mux.HandleFunc("POST /control/script", func(w http.ResponseWriter, r *http.Request) {
		var s Script
		if !fixtures.ReadJSON(w, r, &s) {
			return
		}
		if err := p.SetScript(s); err != nil {
			fixtures.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
			return
		}
		fixtures.WriteJSON(w, http.StatusOK, map[string]int{"steps": len(s.Steps)})
	})
	mux.HandleFunc("POST /control/models", func(w http.ResponseWriter, r *http.Request) {
		var ms []Model
		if !fixtures.ReadJSON(w, r, &ms) {
			return
		}
		p.mu.Lock()
		p.models = ms
		p.mu.Unlock()
		fixtures.WriteJSON(w, http.StatusOK, ms)
	})
	mux.HandleFunc("GET /control/requests", func(w http.ResponseWriter, r *http.Request) {
		fixtures.WriteJSON(w, http.StatusOK, p.log.List())
	})
	mux.HandleFunc("POST /control/reset", func(w http.ResponseWriter, r *http.Request) {
		p.mu.Lock()
		p.script = Script{}
		p.models = DefaultModels
		p.seq = 0
		p.mu.Unlock()
		p.log.Reset()
		fixtures.WriteJSON(w, http.StatusOK, map[string]bool{"reset": true})
	})
	return mux
}

// ---- Anthropic Messages ----

var antErrType = map[int]string{400: "invalid_request_error", 401: "authentication_error", 403: "permission_error",
	404: "not_found_error", 413: "request_too_large", 429: "rate_limit_error", 500: "api_error", 529: "overloaded_error"}

func (p *Provider) antError(w http.ResponseWriter, status int, msg string, retry int) {
	t := antErrType[status]
	if t == "" {
		t = "api_error"
	}
	if retry > 0 {
		w.Header().Set("Retry-After", strconv.Itoa(retry))
	}
	fixtures.WriteJSON(w, status, map[string]any{"type": "error",
		"error": map[string]string{"type": t, "message": msg}, "request_id": p.next("req_fixture_")})
}

func (p *Provider) antAuth(w http.ResponseWriter, r *http.Request) bool {
	if r.Header.Get("X-Api-Key") != p.Key {
		p.antError(w, 401, "invalid x-api-key", 0)
		return false
	}
	if r.Header.Get("Anthropic-Version") == "" {
		p.antError(w, 400, "anthropic-version: header is required", 0)
		return false
	}
	return true
}

func (p *Provider) antModelJSON(m Model) map[string]any {
	return map[string]any{"type": "model", "id": m.ID, "display_name": m.ID,
		"created_at": "2026-01-01T00:00:00Z", "max_input_tokens": m.Context, "max_tokens": m.MaxOutput}
}

func (p *Provider) antModels(w http.ResponseWriter, r *http.Request) {
	if !p.antAuth(w, r) {
		return
	}
	p.mu.Lock()
	ms := append([]Model{}, p.models...)
	p.mu.Unlock()
	data := []map[string]any{}
	for _, m := range ms {
		data = append(data, p.antModelJSON(m))
	}
	page := map[string]any{"data": data, "has_more": false, "first_id": nil, "last_id": nil}
	if len(ms) > 0 {
		page["first_id"], page["last_id"] = ms[0].ID, ms[len(ms)-1].ID
	}
	fixtures.WriteJSON(w, 200, page)
}

func (p *Provider) antModel(w http.ResponseWriter, r *http.Request) {
	if !p.antAuth(w, r) {
		return
	}
	m, ok := p.model(r.PathValue("id"))
	if !ok {
		p.antError(w, 404, "model: "+r.PathValue("id"), 0)
		return
	}
	fixtures.WriteJSON(w, 200, p.antModelJSON(m))
}

type antRequest struct {
	Model     string          `json:"model"`
	MaxTokens int             `json:"max_tokens"`
	Stream    bool            `json:"stream"`
	System    json.RawMessage `json:"system"`
	Messages  json.RawMessage `json:"messages"`
	Tools     json.RawMessage `json:"tools"`
}

type antMessage struct {
	Role    string          `json:"role"`
	Content json.RawMessage `json:"content"`
}

type antBlock struct {
	Type    string          `json:"type"`
	Text    string          `json:"text"`
	Content json.RawMessage `json:"content"`
}

// antText is the text of a content value: a string, or text and tool_result blocks.
func antText(c json.RawMessage) string {
	var s string
	if json.Unmarshal(c, &s) == nil {
		return s
	}
	var blocks []antBlock
	_ = json.Unmarshal(c, &blocks)
	var parts []string
	for _, b := range blocks {
		switch b.Type {
		case "text":
			parts = append(parts, b.Text)
		case "tool_result":
			parts = append(parts, antText(b.Content))
		}
	}
	return strings.Join(parts, "\n")
}

func (p *Provider) antParse(w http.ResponseWriter, r *http.Request, needMax bool) (*antRequest, []antMessage, bool) {
	if !p.antAuth(w, r) {
		return nil, nil, false
	}
	var req antRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		p.antError(w, 400, "invalid JSON: "+err.Error(), 0)
		return nil, nil, false
	}
	var msgs []antMessage
	if err := json.Unmarshal(req.Messages, &msgs); err != nil || len(msgs) == 0 {
		p.antError(w, 400, "messages: at least one message is required", 0)
		return nil, nil, false
	}
	if _, ok := p.model(req.Model); !ok {
		p.antError(w, 404, "model: "+req.Model, 0)
		return nil, nil, false
	}
	if needMax && req.MaxTokens <= 0 {
		p.antError(w, 400, "max_tokens: field required", 0)
		return nil, nil, false
	}
	return &req, msgs, true
}

func antInputTokens(req *antRequest) int {
	return tokens(len(req.System) + len(req.Messages) + len(req.Tools))
}

func (p *Provider) antCount(w http.ResponseWriter, r *http.Request) {
	req, _, ok := p.antParse(w, r, false)
	if !ok {
		return
	}
	fixtures.WriteJSON(w, 200, map[string]int{"input_tokens": antInputTokens(req)})
}

func (p *Provider) antMessages(w http.ResponseWriter, r *http.Request) {
	req, msgs, ok := p.antParse(w, r, true)
	if !ok {
		return
	}
	last := ""
	for i := len(msgs) - 1; i >= 0; i-- {
		if msgs[i].Role == "user" {
			last = antText(msgs[i].Content)
			break
		}
	}
	reply, fault, ok := p.take(last)
	if !ok {
		p.antError(w, 500, "fixture: no scripted step fits and no default is set", 0)
		return
	}
	if fault != nil && fault.Status != 0 {
		p.antError(w, fault.Status, "fixture: scripted error", fault.RetryAfterS)
		return
	}
	cut := -1
	if fault != nil && fault.BreakStreamAfterBytes > 0 {
		cut = fault.BreakStreamAfterBytes
	}
	id := p.next("msg_fixture_")
	w.Header().Set("Request-Id", p.next("req_fixture_"))
	in, out := antInputTokens(req), outTokens(reply)
	stop := "end_turn"
	if len(reply.ToolCalls) > 0 {
		stop = "tool_use"
	}
	type block = map[string]any
	var content []block
	if reply.Thinking != "" {
		content = append(content, block{"type": "thinking", "thinking": reply.Thinking, "signature": signature(reply.Thinking)})
	}
	if reply.Text != "" {
		content = append(content, block{"type": "text", "text": reply.Text})
	}
	toolIDs := make([]string, len(reply.ToolCalls))
	for i, c := range reply.ToolCalls {
		toolIDs[i] = p.next("toolu_fixture_")
		content = append(content, block{"type": "tool_use", "id": toolIDs[i], "name": c.Name, "input": json.RawMessage(inputJSON(c.Input))})
	}
	usage := map[string]int{"input_tokens": in, "output_tokens": out, "cache_creation_input_tokens": 0, "cache_read_input_tokens": 0}
	if !req.Stream {
		msg := map[string]any{"id": id, "type": "message", "role": "assistant", "model": req.Model, "content": content,
			"stop_reason": stop, "stop_sequence": nil, "usage": usage}
		body, _ := json.Marshal(msg)
		writeCut(w, "application/json", [][]byte{body}, cut)
		return
	}
	ev := func(name string, v any) []byte {
		b, _ := json.Marshal(v)
		return []byte("event: " + name + "\ndata: " + string(b) + "\n\n")
	}
	startUsage := map[string]int{"input_tokens": in, "output_tokens": 0, "cache_creation_input_tokens": 0, "cache_read_input_tokens": 0}
	events := [][]byte{
		ev("message_start", block{"type": "message_start", "message": block{"id": id, "type": "message", "role": "assistant",
			"model": req.Model, "content": []any{}, "stop_reason": nil, "stop_sequence": nil, "usage": startUsage}}),
		ev("ping", block{"type": "ping"}),
	}
	idx := 0
	delta := func(d block) []byte {
		return ev("content_block_delta", block{"type": "content_block_delta", "index": idx, "delta": d})
	}
	open := func(cb block) { events = append(events, ev("content_block_start", block{"type": "content_block_start", "index": idx, "content_block": cb})) }
	shut := func() {
		events = append(events, ev("content_block_stop", block{"type": "content_block_stop", "index": idx}))
		idx++
	}
	if reply.Thinking != "" {
		open(block{"type": "thinking", "thinking": "", "signature": ""})
		for _, s := range pieces(reply.Thinking, " ") {
			events = append(events, delta(block{"type": "thinking_delta", "thinking": s}))
		}
		events = append(events, delta(block{"type": "signature_delta", "signature": signature(reply.Thinking)}))
		shut()
	}
	if reply.Text != "" {
		open(block{"type": "text", "text": ""})
		for _, s := range pieces(reply.Text, " ") {
			events = append(events, delta(block{"type": "text_delta", "text": s}))
		}
		shut()
	}
	for i, c := range reply.ToolCalls {
		open(block{"type": "tool_use", "id": toolIDs[i], "name": c.Name, "input": block{}})
		for _, s := range pieces(string(inputJSON(c.Input)), ",") {
			events = append(events, delta(block{"type": "input_json_delta", "partial_json": s}))
		}
		shut()
	}
	events = append(events,
		ev("message_delta", block{"type": "message_delta", "delta": block{"stop_reason": stop, "stop_sequence": nil}, "usage": block{"output_tokens": out}}),
		ev("message_stop", block{"type": "message_stop"}))
	writeCut(w, "text/event-stream", events, cut)
}

// signature stands in for the provider's opaque thinking signature: it is
// derived from the thinking text, so a replayed block can be checked.
func signature(thinking string) string {
	h := sha256.Sum256([]byte(thinking))
	return base64.StdEncoding.EncodeToString(h[:])
}

// writeCut writes chunks, flushing each; with cut >= 0 the connection is
// dropped once cut bytes of body have gone out.
func writeCut(w http.ResponseWriter, ctype string, chunks [][]byte, cut int) {
	w.Header().Set("Content-Type", ctype)
	if ctype == "text/event-stream" {
		w.Header().Set("Cache-Control", "no-cache")
	} else {
		w.Header().Set("Content-Length", strconv.Itoa(len(bytes.Join(chunks, nil))))
	}
	w.WriteHeader(200)
	f, _ := w.(http.Flusher)
	for _, c := range chunks {
		if cut >= 0 && len(c) >= cut {
			_, _ = w.Write(c[:cut])
			if f != nil {
				f.Flush()
			}
			panic(http.ErrAbortHandler) // closes the connection mid-body
		}
		_, _ = w.Write(c)
		if f != nil {
			f.Flush()
		}
		if cut >= 0 {
			cut -= len(c)
		}
	}
}

// ---- OpenAI Chat Completions ----

func (p *Provider) oaiError(w http.ResponseWriter, status int, typ, msg string, retry int) {
	if retry > 0 {
		w.Header().Set("Retry-After", strconv.Itoa(retry))
	}
	fixtures.WriteJSON(w, status, map[string]any{"error": map[string]any{"message": msg, "type": typ, "param": nil, "code": nil}})
}

func (p *Provider) oaiAuth(w http.ResponseWriter, r *http.Request) bool {
	if r.Header.Get("Authorization") != "Bearer "+p.Key {
		p.oaiError(w, 401, "invalid_request_error", "Incorrect API key provided", 0)
		return false
	}
	return true
}

func (p *Provider) oaiModels(w http.ResponseWriter, r *http.Request) {
	if !p.oaiAuth(w, r) {
		return
	}
	p.mu.Lock()
	ms := append([]Model{}, p.models...)
	p.mu.Unlock()
	data := []map[string]any{}
	for _, m := range ms {
		data = append(data, map[string]any{"id": m.ID, "object": "model", "created": int64(1767225600), "owned_by": "fixture",
			"context_length": m.Context})
	}
	fixtures.WriteJSON(w, 200, map[string]any{"object": "list", "data": data})
}

type oaiRequest struct {
	Model         string          `json:"model"`
	Stream        bool            `json:"stream"`
	Messages      json.RawMessage `json:"messages"`
	Tools         json.RawMessage `json:"tools"`
	StreamOptions *struct {
		IncludeUsage bool `json:"include_usage"`
	} `json:"stream_options"`
}

type oaiMessage struct {
	Role    string          `json:"role"`
	Content json.RawMessage `json:"content"`
}

func oaiText(c json.RawMessage) string {
	var s string
	if json.Unmarshal(c, &s) == nil {
		return s
	}
	var parts []struct {
		Type, Text string
	}
	_ = json.Unmarshal(c, &parts)
	var out []string
	for _, p := range parts {
		if p.Type == "text" {
			out = append(out, p.Text)
		}
	}
	return strings.Join(out, "\n")
}

func (p *Provider) oaiChat(w http.ResponseWriter, r *http.Request) {
	if !p.oaiAuth(w, r) {
		return
	}
	var req oaiRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		p.oaiError(w, 400, "invalid_request_error", "invalid JSON: "+err.Error(), 0)
		return
	}
	var msgs []oaiMessage
	if err := json.Unmarshal(req.Messages, &msgs); err != nil || len(msgs) == 0 {
		p.oaiError(w, 400, "invalid_request_error", "messages: at least one message is required", 0)
		return
	}
	if _, ok := p.model(req.Model); !ok {
		p.oaiError(w, 404, "invalid_request_error", "The model `"+req.Model+"` does not exist", 0)
		return
	}
	last := ""
	for i := len(msgs) - 1; i >= 0; i-- {
		// A tool message is the newest input after a tool call, as a user tool_result is on the other format.
		if msgs[i].Role == "user" || msgs[i].Role == "tool" {
			last = oaiText(msgs[i].Content)
			break
		}
	}
	reply, fault, ok := p.take(last)
	if !ok {
		p.oaiError(w, 500, "server_error", "fixture: no scripted step fits and no default is set", 0)
		return
	}
	if fault != nil && fault.Status != 0 {
		typ := "server_error"
		if fault.Status == 429 {
			typ = "rate_limit_exceeded"
		}
		p.oaiError(w, fault.Status, typ, "fixture: scripted error", fault.RetryAfterS)
		return
	}
	cut := -1
	if fault != nil && fault.BreakStreamAfterBytes > 0 {
		cut = fault.BreakStreamAfterBytes
	}
	id := p.next("chatcmpl-fixture-")
	w.Header().Set("X-Request-Id", p.next("req_fixture_"))
	in, out := tokens(len(req.Messages)+len(req.Tools)), outTokens(reply)
	usage := map[string]int{"prompt_tokens": in, "completion_tokens": out, "total_tokens": in + out}
	created := time.Now().Unix()
	finish := "stop"
	if len(reply.ToolCalls) > 0 {
		finish = "tool_calls"
	}
	type obj = map[string]any
	callIDs := make([]string, len(reply.ToolCalls))
	for i := range reply.ToolCalls {
		callIDs[i] = p.next("call_fixture_")
	}
	if !req.Stream {
		msg := obj{"role": "assistant", "content": nil}
		if reply.Text != "" {
			msg["content"] = reply.Text
		}
		if reply.Thinking != "" {
			msg["reasoning_content"] = reply.Thinking
		}
		if len(reply.ToolCalls) > 0 {
			var calls []obj
			for i, c := range reply.ToolCalls {
				calls = append(calls, obj{"id": callIDs[i], "type": "function",
					"function": obj{"name": c.Name, "arguments": string(inputJSON(c.Input))}})
			}
			msg["tool_calls"] = calls
		}
		body, _ := json.Marshal(obj{"id": id, "object": "chat.completion", "created": created, "model": req.Model,
			"choices": []obj{{"index": 0, "message": msg, "finish_reason": finish}}, "usage": usage})
		writeCut(w, "application/json", [][]byte{body}, cut)
		return
	}
	chunk := func(delta obj, fin any) []byte {
		b, _ := json.Marshal(obj{"id": id, "object": "chat.completion.chunk", "created": created, "model": req.Model,
			"choices": []obj{{"index": 0, "delta": delta, "finish_reason": fin}}})
		return []byte("data: " + string(b) + "\n\n")
	}
	events := [][]byte{chunk(obj{"role": "assistant", "content": ""}, nil)}
	for _, s := range pieces(reply.Thinking, " ") {
		events = append(events, chunk(obj{"reasoning_content": s}, nil))
	}
	for _, s := range pieces(reply.Text, " ") {
		events = append(events, chunk(obj{"content": s}, nil))
	}
	for i, c := range reply.ToolCalls {
		events = append(events, chunk(obj{"tool_calls": []obj{{"index": i, "id": callIDs[i], "type": "function",
			"function": obj{"name": c.Name, "arguments": ""}}}}, nil))
		for _, s := range pieces(string(inputJSON(c.Input)), ",") {
			events = append(events, chunk(obj{"tool_calls": []obj{{"index": i, "function": obj{"arguments": s}}}}, nil))
		}
	}
	events = append(events, chunk(obj{}, finish))
	if req.StreamOptions != nil && req.StreamOptions.IncludeUsage {
		b, _ := json.Marshal(obj{"id": id, "object": "chat.completion.chunk", "created": created, "model": req.Model,
			"choices": []obj{}, "usage": usage})
		events = append(events, []byte("data: "+string(b)+"\n\n"))
	}
	events = append(events, []byte("data: [DONE]\n\n"))
	writeCut(w, "text/event-stream", events, cut)
}
