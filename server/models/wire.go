package models

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"
)

const maxBody = 64 << 20

func readLimited(r io.Reader) ([]byte, error) {
	b, err := io.ReadAll(io.LimitReader(r, maxBody+1))
	if err != nil {
		return nil, err
	}
	if len(b) > maxBody {
		return nil, errors.New("models: response too large")
	}
	return b, nil
}

// --- Anthropic Messages ---------------------------------------------------------

type aCache struct {
	Type string `json:"type"`
	TTL  string `json:"ttl,omitempty"`
}

type aBlock struct {
	Type      string          `json:"type"`
	Text      *string         `json:"text,omitempty"`
	ID        string          `json:"id,omitempty"`
	Name      string          `json:"name,omitempty"`
	Input     json.RawMessage `json:"input,omitempty"`
	ToolUseID string          `json:"tool_use_id,omitempty"`
	Content   *string         `json:"content,omitempty"`
	IsError   bool            `json:"is_error,omitempty"`
	Thinking  *string         `json:"thinking,omitempty"`
	Signature string          `json:"signature,omitempty"`
	Cache     *aCache         `json:"cache_control,omitempty"`
}

type aTool struct {
	Name        string          `json:"name"`
	Description string          `json:"description"`
	Schema      json.RawMessage `json:"input_schema"`
	Cache       *aCache         `json:"cache_control,omitempty"`
}

type aMessage struct {
	Role    string   `json:"role"`
	Content []aBlock `json:"content"`
}

type aBody struct {
	Model     string     `json:"model"`
	MaxTokens int        `json:"max_tokens"`
	Stream    bool       `json:"stream,omitempty"`
	System    []aBlock   `json:"system,omitempty"`
	Tools     []aTool    `json:"tools,omitempty"`
	Messages  []aMessage `json:"messages"`
}

func cacheOf(ttl string) *aCache {
	if ttl == "" {
		return nil
	}
	return &aCache{Type: "ephemeral", TTL: ttl}
}

func toA(b Block) aBlock {
	s := func(v string) *string { return &v }
	switch b.Type {
	case "text":
		return aBlock{Type: "text", Text: s(b.Text), Cache: cacheOf(b.Cache)}
	case "tool_use":
		in := b.Input
		if len(in) == 0 {
			in = json.RawMessage("{}")
		}
		return aBlock{Type: "tool_use", ID: b.ID, Name: b.Name, Input: in, Cache: cacheOf(b.Cache)}
	case "tool_result":
		return aBlock{Type: "tool_result", ToolUseID: b.ToolUseID, Content: s(b.Text), IsError: b.IsError, Cache: cacheOf(b.Cache)}
	case "thinking":
		return aBlock{Type: "thinking", Thinking: s(b.Text), Signature: b.Signature}
	}
	return aBlock{Type: "text", Text: s(b.Text)}
}

func anthropicBody(r Request, stream bool) ([]byte, error) {
	body := aBody{Model: r.Model, MaxTokens: r.MaxTokens, Stream: stream}
	for _, b := range r.System {
		body.System = append(body.System, toA(b))
	}
	for i, t := range r.Tools {
		at := aTool{Name: t.Name, Description: t.Description, Schema: t.Schema}
		if i == len(r.Tools)-1 {
			at.Cache = cacheOf(r.ToolsCache)
		}
		body.Tools = append(body.Tools, at)
	}
	for _, m := range r.Messages {
		am := aMessage{Role: m.Role}
		for _, b := range m.Content {
			am.Content = append(am.Content, toA(b))
		}
		body.Messages = append(body.Messages, am)
	}
	return json.Marshal(body)
}

func (p *Provider) post(ctx context.Context, path string, body []byte) (*http.Response, error) {
	req, err := http.NewRequestWithContext(ctx, "POST", p.url(path), bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	p.auth(req)
	req.Header.Set("content-type", "application/json")
	req.Header.Set("accept", "text/event-stream")
	resp, err := p.client().Do(req)
	if err != nil {
		return nil, &Error{Class: "network", Msg: err.Error()}
	}
	if resp.StatusCode != 200 {
		defer resp.Body.Close()
		b, _ := readLimited(resp.Body)
		return nil, classify(resp.StatusCode, resp.Header, b)
	}
	return resp, nil
}

// sse yields (event, data) pairs.
func sse(r io.Reader, fn func(event string, data []byte) error) error {
	sc := bufio.NewScanner(r)
	sc.Buffer(make([]byte, 1<<20), maxBody)
	var event string
	var data bytes.Buffer
	flush := func() error {
		if data.Len() == 0 {
			event = ""
			return nil
		}
		err := fn(event, bytes.TrimSuffix(data.Bytes(), []byte("\n")))
		event = ""
		data.Reset()
		return err
	}
	for sc.Scan() {
		line := sc.Text()
		switch {
		case line == "":
			if err := flush(); err != nil {
				return err
			}
		case strings.HasPrefix(line, "event:"):
			event = strings.TrimSpace(line[6:])
		case strings.HasPrefix(line, "data:"):
			data.WriteString(strings.TrimPrefix(line[5:], " "))
			data.WriteByte('\n')
		}
	}
	if err := sc.Err(); err != nil {
		return err
	}
	return flush()
}

func (p *Provider) sendAnthropic(ctx context.Context, r Request, onText func(string)) (Response, error) {
	body, err := anthropicBody(r, true)
	if err != nil {
		return Response{}, err
	}
	resp, err := p.post(ctx, "/v1/messages", body)
	if err != nil {
		return Response{}, err
	}
	defer resp.Body.Close()
	var out Response
	out.RequestID = resp.Header.Get("request-id")
	type partial struct {
		b    Block
		json strings.Builder
	}
	var blocks []*partial
	done := false
	err = sse(resp.Body, func(event string, data []byte) error {
		var ev struct {
			Type    string `json:"type"`
			Index   int    `json:"index"`
			Message struct {
				ID    string `json:"id"`
				Usage struct {
					In    int `json:"input_tokens"`
					Read  int `json:"cache_read_input_tokens"`
					Write int `json:"cache_creation_input_tokens"`
				} `json:"usage"`
			} `json:"message"`
			Block struct {
				Type      string          `json:"type"`
				Text      string          `json:"text"`
				ID        string          `json:"id"`
				Name      string          `json:"name"`
				Input     json.RawMessage `json:"input"`
				Thinking  string          `json:"thinking"`
				Signature string          `json:"signature"`
			} `json:"content_block"`
			Delta struct {
				Type        string `json:"type"`
				Text        string `json:"text"`
				PartialJSON string `json:"partial_json"`
				Thinking    string `json:"thinking"`
				Signature   string `json:"signature"`
				StopReason  string `json:"stop_reason"`
			} `json:"delta"`
			Usage struct {
				Out int `json:"output_tokens"`
			} `json:"usage"`
			Error struct {
				Type    string `json:"type"`
				Message string `json:"message"`
			} `json:"error"`
		}
		if err := json.Unmarshal(data, &ev); err != nil {
			return err
		}
		switch ev.Type {
		case "message_start":
			if out.RequestID == "" {
				out.RequestID = ev.Message.ID
			}
			out.Usage.In, out.Usage.CacheRead, out.Usage.CacheWrite = ev.Message.Usage.In, ev.Message.Usage.Read, ev.Message.Usage.Write
		case "content_block_start":
			for len(blocks) <= ev.Index {
				blocks = append(blocks, &partial{})
			}
			b := Block{Type: ev.Block.Type, Text: ev.Block.Text, ID: ev.Block.ID, Name: ev.Block.Name, Signature: ev.Block.Signature}
			if ev.Block.Type == "thinking" {
				b.Text = ev.Block.Thinking
			}
			blocks[ev.Index].b = b
		case "content_block_delta":
			if ev.Index >= len(blocks) {
				return errors.New("delta before block start")
			}
			pb := blocks[ev.Index]
			switch ev.Delta.Type {
			case "text_delta":
				pb.b.Text += ev.Delta.Text
				if onText != nil {
					onText(ev.Delta.Text)
				}
			case "input_json_delta":
				pb.json.WriteString(ev.Delta.PartialJSON)
			case "thinking_delta":
				pb.b.Text += ev.Delta.Thinking
			case "signature_delta":
				pb.b.Signature += ev.Delta.Signature
			}
		case "message_delta":
			out.Stop = ev.Delta.StopReason
			out.Usage.Out = ev.Usage.Out
		case "message_stop":
			done = true
		case "error":
			return &Error{Class: "retry", Status: 529, Msg: ev.Error.Type + ": " + ev.Error.Message}
		}
		return nil
	})
	if err != nil {
		var me *Error
		if errors.As(err, &me) {
			return Response{}, me
		}
		return Response{}, &Error{Class: "broken", Msg: err.Error()}
	}
	if !done {
		return Response{}, &Error{Class: "broken", Msg: "stream ended before message_stop"}
	}
	for _, pb := range blocks {
		if pb.b.Type == "tool_use" {
			in := pb.json.String()
			if in == "" {
				in = "{}"
			}
			if !json.Valid([]byte(in)) {
				return Response{}, &Error{Class: "broken", Msg: "tool input is not valid JSON"}
			}
			pb.b.Input = json.RawMessage(in)
		}
		out.Content = append(out.Content, pb.b)
	}
	return out, nil
}

// --- OpenAI-compatible Chat Completions ----------------------------------------

type oCall struct {
	ID       string `json:"id"`
	Type     string `json:"type"`
	Function struct {
		Name      string `json:"name"`
		Arguments string `json:"arguments"`
	} `json:"function"`
}

type oMessage struct {
	Role       string  `json:"role"`
	Content    *string `json:"content"`
	ToolCalls  []oCall `json:"tool_calls,omitempty"`
	ToolCallID string  `json:"tool_call_id,omitempty"`
}

type oTool struct {
	Type     string `json:"type"`
	Function struct {
		Name        string          `json:"name"`
		Description string          `json:"description"`
		Parameters  json.RawMessage `json:"parameters"`
	} `json:"function"`
}

func joinText(bs []Block) string {
	var parts []string
	for _, b := range bs {
		if b.Type == "text" {
			parts = append(parts, b.Text)
		}
	}
	return strings.Join(parts, "\n\n")
}

// openAIBody keeps the Anthropic order: system, then each message's blocks in order.
// Tool results become role "tool" messages ahead of the user text they arrived with.
func openAIBody(r Request, stream bool, maxField string) ([]byte, error) {
	var msgs []oMessage
	s := func(v string) *string { return &v }
	if sys := joinText(r.System); sys != "" {
		msgs = append(msgs, oMessage{Role: "system", Content: s(sys)})
	}
	for _, m := range r.Messages {
		switch m.Role {
		case "assistant":
			om := oMessage{Role: "assistant"}
			if t := joinText(m.Content); t != "" {
				om.Content = s(t)
			}
			for _, b := range m.Content {
				if b.Type == "tool_use" {
					c := oCall{ID: b.ID, Type: "function"}
					c.Function.Name = b.Name
					in := string(b.Input)
					if in == "" {
						in = "{}"
					}
					c.Function.Arguments = in
					om.ToolCalls = append(om.ToolCalls, c)
				}
			}
			msgs = append(msgs, om)
		default:
			for _, b := range m.Content {
				if b.Type == "tool_result" {
					msgs = append(msgs, oMessage{Role: "tool", ToolCallID: b.ToolUseID, Content: s(b.Text)})
				}
			}
			if t := joinText(m.Content); t != "" {
				msgs = append(msgs, oMessage{Role: "user", Content: s(t)})
			}
		}
	}
	var tools []oTool
	for _, t := range r.Tools {
		var ot oTool
		ot.Type = "function"
		ot.Function.Name, ot.Function.Description, ot.Function.Parameters = t.Name, t.Description, t.Schema
		tools = append(tools, ot)
	}
	// Field order is fixed by building an ordered document.
	var buf bytes.Buffer
	buf.WriteString(`{"model":`)
	mj, _ := json.Marshal(r.Model)
	buf.Write(mj)
	buf.WriteString(`,"` + maxField + `":`)
	nj, _ := json.Marshal(r.MaxTokens)
	buf.Write(nj)
	if stream {
		buf.WriteString(`,"stream":true,"stream_options":{"include_usage":true}`)
	}
	buf.WriteString(`,"messages":`)
	msj, err := json.Marshal(msgs)
	if err != nil {
		return nil, err
	}
	buf.Write(msj)
	if len(tools) > 0 {
		buf.WriteString(`,"tools":`)
		tj, err := json.Marshal(tools)
		if err != nil {
			return nil, err
		}
		buf.Write(tj)
	}
	buf.WriteString("}")
	return buf.Bytes(), nil
}

func (p *Provider) sendOpenAI(ctx context.Context, r Request, onText func(string)) (Response, error) {
	field := "max_tokens"
	body, err := openAIBody(r, true, field)
	if err != nil {
		return Response{}, err
	}
	resp, err := p.post(ctx, "/v1/chat/completions", body)
	var me *Error
	if errors.As(err, &me) && me.Class == "request" && strings.Contains(me.Msg, "max_completion_tokens") {
		// shortcut: newer OpenAI models name the field differently; the retry is per request,
		// remember it per model if it shows up in spend logs often.
		body, _ = openAIBody(r, true, "max_completion_tokens")
		resp, err = p.post(ctx, "/v1/chat/completions", body)
	}
	if err != nil {
		return Response{}, err
	}
	defer resp.Body.Close()
	var out Response
	var text strings.Builder
	type call struct {
		id, name string
		args     strings.Builder
	}
	var calls []*call
	done := false
	err = sse(resp.Body, func(_ string, data []byte) error {
		if string(data) == "[DONE]" {
			done = true
			return nil
		}
		var ch struct {
			ID      string `json:"id"`
			Choices []struct {
				Delta struct {
					Content   string `json:"content"`
					ToolCalls []struct {
						Index    int    `json:"index"`
						ID       string `json:"id"`
						Function struct {
							Name      string `json:"name"`
							Arguments string `json:"arguments"`
						} `json:"function"`
					} `json:"tool_calls"`
				} `json:"delta"`
				FinishReason *string `json:"finish_reason"`
			} `json:"choices"`
			Usage *struct {
				Prompt     int `json:"prompt_tokens"`
				Completion int `json:"completion_tokens"`
				Details    *struct {
					Cached int `json:"cached_tokens"`
				} `json:"prompt_tokens_details"`
			} `json:"usage"`
			Error *struct {
				Message string `json:"message"`
			} `json:"error"`
		}
		if err := json.Unmarshal(data, &ch); err != nil {
			return err
		}
		if ch.Error != nil {
			return &Error{Class: "retry", Status: 500, Msg: ch.Error.Message}
		}
		if out.RequestID == "" {
			out.RequestID = ch.ID
		}
		for _, c := range ch.Choices {
			if c.Delta.Content != "" {
				text.WriteString(c.Delta.Content)
				if onText != nil {
					onText(c.Delta.Content)
				}
			}
			for _, tc := range c.Delta.ToolCalls {
				for len(calls) <= tc.Index {
					calls = append(calls, &call{})
				}
				k := calls[tc.Index]
				if tc.ID != "" {
					k.id = tc.ID
				}
				if tc.Function.Name != "" {
					k.name = tc.Function.Name
				}
				k.args.WriteString(tc.Function.Arguments)
			}
			if c.FinishReason != nil {
				out.Stop = *c.FinishReason
			}
		}
		if ch.Usage != nil {
			out.Usage.In, out.Usage.Out = ch.Usage.Prompt, ch.Usage.Completion
			if ch.Usage.Details != nil {
				out.Usage.CacheRead = ch.Usage.Details.Cached
			}
		}
		return nil
	})
	if err != nil {
		if errors.As(err, &me) {
			return Response{}, me
		}
		return Response{}, &Error{Class: "broken", Msg: err.Error()}
	}
	if !done {
		return Response{}, &Error{Class: "broken", Msg: "stream ended before [DONE]"}
	}
	if text.Len() > 0 {
		out.Content = append(out.Content, Block{Type: "text", Text: text.String()})
	}
	for _, c := range calls {
		args := c.args.String()
		if args == "" {
			args = "{}"
		}
		if !json.Valid([]byte(args)) {
			return Response{}, &Error{Class: "broken", Msg: "tool arguments are not valid JSON"}
		}
		out.Content = append(out.Content, Block{Type: "tool_use", ID: c.id, Name: c.name, Input: json.RawMessage(args)})
	}
	switch out.Stop {
	case "tool_calls":
		out.Stop = "tool_use"
	case "stop":
		out.Stop = "end_turn"
	case "length":
		out.Stop = "max_tokens"
	}
	return out, nil
}
