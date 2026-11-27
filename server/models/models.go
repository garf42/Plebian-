// Package models is n.models: two wire formats (Anthropic Messages, OpenAI-compatible
// Chat Completions), no gateway. Both carry the same content in the same order; only
// breakpoint markup and field names differ.
package models

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"
)

const (
	Anthropic = "anthropic"
	OpenAI    = "openai"
)

// Block is one content block. Cache, when set, is a breakpoint after this block with
// that lifetime ("1h" or "5m"); only the Anthropic format renders it.
type Block struct {
	Type      string          `json:"type"` // text | tool_use | tool_result | thinking
	Text      string          `json:"text,omitempty"`
	ID        string          `json:"id,omitempty"`
	Name      string          `json:"name,omitempty"`
	Input     json.RawMessage `json:"input,omitempty"`
	ToolUseID string          `json:"tool_use_id,omitempty"`
	IsError   bool            `json:"is_error,omitempty"`
	Signature string          `json:"signature,omitempty"`
	Cache     string          `json:"-"`
}

type Message struct {
	Role    string  `json:"role"` // user | assistant
	Content []Block `json:"content"`
}

type Tool struct {
	Name        string          `json:"name"`
	Description string          `json:"description"`
	Schema      json.RawMessage `json:"input_schema"`
}

// Request is provider-neutral. ToolsCache is the breakpoint lifetime after the tool band.
type Request struct {
	Model      string
	System     []Block
	Tools      []Tool
	ToolsCache string
	Messages   []Message
	MaxTokens  int
}

type Usage struct {
	In, Out, CacheRead, CacheWrite int
}

type Response struct {
	Content   []Block
	Stop      string
	Usage     Usage
	RequestID string
}

// ModelInfo is one entry of a provider's own model listing.
type ModelInfo struct {
	ID      string `json:"id"`
	Context int    `json:"context"` // 0 when the listing gives none
	MaxOut  int    `json:"max_out"`
}

// Provider is one endpoint the owner configured.
type Provider struct {
	ID      string       `json:"id"`
	Format  string       `json:"format"`
	BaseURL string       `json:"base_url"`
	Key     string       `json:"-"`
	HTTP    *http.Client `json:"-"`
}

// Error classes the loop and the router act on.
type Error struct {
	Class      string // retry | overflow | auth | request | broken | network
	Status     int
	RetryAfter time.Duration
	Msg        string
}

func (e *Error) Error() string { return fmt.Sprintf("model %s (%d): %s", e.Class, e.Status, e.Msg) }

func classify(status int, h http.Header, body []byte) *Error {
	e := &Error{Status: status, Msg: strings.TrimSpace(string(body))}
	if len(e.Msg) > 2000 {
		e.Msg = e.Msg[:2000]
	}
	switch {
	case status == 429 || status == 529 || status >= 500:
		e.Class = "retry"
		if v := h.Get("Retry-After"); v != "" {
			if s, err := strconv.ParseFloat(v, 64); err == nil {
				e.RetryAfter = time.Duration(s * float64(time.Second))
			} else if t, err := http.ParseTime(v); err == nil {
				e.RetryAfter = time.Until(t)
			}
		}
	case status == 401 || status == 403:
		e.Class = "auth"
	case status == 400 || status == 413:
		low := strings.ToLower(e.Msg)
		if strings.Contains(low, "context") && (strings.Contains(low, "length") || strings.Contains(low, "window") || strings.Contains(low, "too long") || strings.Contains(low, "exceed")) ||
			strings.Contains(low, "prompt is too long") || strings.Contains(low, "maximum context") {
			e.Class = "overflow"
		} else {
			e.Class = "request"
		}
	default:
		e.Class = "request"
	}
	return e
}

func (p *Provider) client() *http.Client {
	if p.HTTP != nil {
		return p.HTTP
	}
	return http.DefaultClient
}

func (p *Provider) url(path string) string { return strings.TrimRight(p.BaseURL, "/") + path }

// Detect finds an endpoint's wire format by which listing call it answers, and returns
// its models. No model name is built in.
func Detect(ctx context.Context, baseURL, key string, hc *http.Client) (Provider, []ModelInfo, error) {
	if baseURL == "" {
		baseURL = "https://api.anthropic.com"
	}
	var errs []string
	for _, f := range []string{Anthropic, OpenAI} {
		p := Provider{Format: f, BaseURL: baseURL, Key: key, HTTP: hc}
		ms, err := p.List(ctx)
		if err == nil && len(ms) > 0 {
			p.ID = f + ":" + baseURL
			return p, ms, nil
		}
		if err != nil {
			errs = append(errs, err.Error())
		}
	}
	return Provider{}, nil, fmt.Errorf("no model listing answered at %s: %s", baseURL, strings.Join(errs, "; "))
}

// List reads the provider's model listing in its own format; an answer in the other
// format's shape is not an answer.
func (p *Provider) List(ctx context.Context) ([]ModelInfo, error) {
	req, err := http.NewRequestWithContext(ctx, "GET", p.url("/v1/models"), nil)
	if err != nil {
		return nil, err
	}
	p.auth(req)
	resp, err := p.client().Do(req)
	if err != nil {
		return nil, &Error{Class: "network", Msg: err.Error()}
	}
	defer resp.Body.Close()
	var body struct {
		Data []map[string]any `json:"data"`
	}
	b, err := readLimited(resp.Body)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode != 200 {
		return nil, classify(resp.StatusCode, resp.Header, b)
	}
	if err := json.Unmarshal(b, &body); err != nil {
		return nil, fmt.Errorf("listing: %w", err)
	}
	var out []ModelInfo
	for _, m := range body.Data {
		kind, _ := m["type"].(string)
		obj, _ := m["object"].(string)
		if (p.Format == Anthropic && kind != "model") || (p.Format == OpenAI && obj != "model") {
			continue
		}
		id, _ := m["id"].(string)
		if id == "" {
			continue
		}
		out = append(out, ModelInfo{ID: id, Context: firstInt(m, "max_input_tokens", "context_length", "context_window", "max_model_len", "max_context_length"), MaxOut: firstInt(m, "max_tokens", "max_output_tokens", "max_completion_tokens")})
	}
	return out, nil
}

func firstInt(m map[string]any, keys ...string) int {
	for _, k := range keys {
		if v, ok := m[k].(float64); ok && v > 0 {
			return int(v)
		}
	}
	return 0
}

func (p *Provider) auth(r *http.Request) {
	switch p.Format {
	case Anthropic:
		if p.Key != "" {
			r.Header.Set("x-api-key", p.Key)
		}
		r.Header.Set("anthropic-version", "2023-06-01")
	case OpenAI:
		if p.Key != "" {
			r.Header.Set("Authorization", "Bearer "+p.Key)
		}
	}
}

// Send streams one request and returns the whole response. onText receives text as it
// arrives (the app's presence and streamed words).
func (p *Provider) Send(ctx context.Context, r Request, onText func(string)) (Response, error) {
	switch p.Format {
	case Anthropic:
		return p.sendAnthropic(ctx, r, onText)
	case OpenAI:
		return p.sendOpenAI(ctx, r, onText)
	}
	return Response{}, errors.New("models: unknown format " + p.Format)
}

// Body returns the exact request body Send would post (tests compare these).
func (p *Provider) Body(r Request) ([]byte, error) {
	if p.Format == Anthropic {
		return anthropicBody(r, true)
	}
	return openAIBody(r, true, "max_tokens")
}

// Count returns the provider's own token count for r, when the provider has a counter.
func (p *Provider) Count(ctx context.Context, r Request) (int, bool, error) {
	if p.Format != Anthropic {
		return 0, false, nil
	}
	b, err := anthropicBody(r, false)
	if err != nil {
		return 0, false, err
	}
	var m map[string]json.RawMessage
	json.Unmarshal(b, &m)
	delete(m, "max_tokens")
	delete(m, "stream")
	b, _ = json.Marshal(m)
	req, err := http.NewRequestWithContext(ctx, "POST", p.url("/v1/messages/count_tokens"), strings.NewReader(string(b)))
	if err != nil {
		return 0, false, err
	}
	p.auth(req)
	req.Header.Set("content-type", "application/json")
	resp, err := p.client().Do(req)
	if err != nil {
		return 0, false, &Error{Class: "network", Msg: err.Error()}
	}
	defer resp.Body.Close()
	body, _ := readLimited(resp.Body)
	if resp.StatusCode == 404 {
		return 0, false, nil
	}
	if resp.StatusCode != 200 {
		return 0, false, classify(resp.StatusCode, resp.Header, body)
	}
	var v struct {
		InputTokens int `json:"input_tokens"`
	}
	if err := json.Unmarshal(body, &v); err != nil {
		return 0, false, err
	}
	return v.InputTokens, true, nil
}
