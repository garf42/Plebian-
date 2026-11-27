package model

import (
	"bufio"
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func post(t *testing.T, url string, hdr map[string]string, body string) *http.Response {
	t.Helper()
	req, _ := http.NewRequest("POST", url, strings.NewReader(body))
	for k, v := range hdr {
		req.Header.Set(k, v)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	return resp
}

func script(t *testing.T, base, s string) {
	t.Helper()
	resp := post(t, base+"/control/script", nil, s)
	if resp.StatusCode != 200 {
		b, _ := io.ReadAll(resp.Body)
		t.Fatalf("script: %d %s", resp.StatusCode, b)
	}
}

// sse parses an SSE body into (event, data) pairs.
func sse(t *testing.T, r io.Reader) (names []string, datas []string) {
	sc := bufio.NewScanner(r)
	name := ""
	for sc.Scan() {
		line := sc.Text()
		switch {
		case strings.HasPrefix(line, "event: "):
			name = strings.TrimPrefix(line, "event: ")
		case strings.HasPrefix(line, "data: "):
			names = append(names, name)
			datas = append(datas, strings.TrimPrefix(line, "data: "))
			name = ""
		}
	}
	return
}

const antScript = `{"steps":[
 {"match":"weather","reply":{"thinking":"look it up first","text":"Checking the weather.","tool_calls":[{"name":"get_weather","input":{"city":"Paris","units":"c"}}]}},
 {"match":"busy","error":{"status":429,"retry_after_s":7}},
 {"match":"cut","reply":{"text":"this stream is cut short"},"error":{"break_stream_after_bytes":40}}
],"default":{"text":"plain answer"}}`

func TestAnthropicStreamToolUseAndTokens(t *testing.T) {
	p := New("k1")
	srv := httptest.NewServer(p.Handler())
	defer srv.Close()
	script(t, srv.URL, antScript)
	hdr := map[string]string{"x-api-key": "k1", "anthropic-version": "2026-10-21", "content-type": "application/json"}
	body := `{"model":"fixture-model","max_tokens":1000,"stream":true,"system":"s","messages":[{"role":"user","content":"what is the weather"}]}`
	resp := post(t, srv.URL+"/anthropic/v1/messages", hdr, body)
	if resp.StatusCode != 200 || resp.Header.Get("Content-Type") != "text/event-stream" {
		t.Fatalf("status %d %s", resp.StatusCode, resp.Header.Get("Content-Type"))
	}
	names, datas := sse(t, resp.Body)
	if names[0] != "message_start" || names[len(names)-1] != "message_stop" {
		t.Fatalf("events %v", names)
	}
	var thinking, text, partial string
	var toolName, stop string
	var inTok, outTok int
	for _, d := range datas {
		var ev struct {
			Type    string
			Message struct {
				Usage struct {
					InputTokens int `json:"input_tokens"`
				}
			}
			ContentBlock struct{ Type, Name string } `json:"content_block"`
			Delta        struct {
				Type, Text, Thinking string
				PartialJSON          string `json:"partial_json"`
				StopReason           string `json:"stop_reason"`
			}
			Usage struct {
				OutputTokens int `json:"output_tokens"`
			}
		}
		if err := json.Unmarshal([]byte(d), &ev); err != nil {
			t.Fatal(err)
		}
		switch ev.Type {
		case "message_start":
			inTok = ev.Message.Usage.InputTokens
		case "content_block_start":
			if ev.ContentBlock.Type == "tool_use" {
				toolName = ev.ContentBlock.Name
			}
		case "content_block_delta":
			thinking += ev.Delta.Thinking
			text += ev.Delta.Text
			partial += ev.Delta.PartialJSON
		case "message_delta":
			stop, outTok = ev.Delta.StopReason, ev.Usage.OutputTokens
		}
	}
	if thinking != "look it up first" || text != "Checking the weather." || toolName != "get_weather" || stop != "tool_use" {
		t.Fatalf("got %q %q %q %q", thinking, text, toolName, stop)
	}
	var in map[string]string
	if err := json.Unmarshal([]byte(partial), &in); err != nil || in["city"] != "Paris" {
		t.Fatalf("partial_json %q: %v", partial, err)
	}
	if outTok != tokens(len("look it up first")+len("Checking the weather.")+len(`{"city":"Paris","units":"c"}`)) {
		t.Fatalf("output tokens %d", outTok)
	}
	// count_tokens agrees with the message call on the same prompt.
	cresp := post(t, srv.URL+"/anthropic/v1/messages/count_tokens", hdr,
		`{"model":"fixture-model","system":"s","messages":[{"role":"user","content":"what is the weather"}]}`)
	var c struct {
		InputTokens int `json:"input_tokens"`
	}
	_ = json.NewDecoder(cresp.Body).Decode(&c)
	if c.InputTokens != inTok || inTok == 0 {
		t.Fatalf("count %d vs message %d", c.InputTokens, inTok)
	}
	// Steps are consumed: the same request now gets the default.
	resp = post(t, srv.URL+"/anthropic/v1/messages", hdr, strings.Replace(body, `"stream":true`, `"stream":false`, 1))
	var msg struct {
		Content    []struct{ Type, Text string }
		StopReason string `json:"stop_reason"`
	}
	_ = json.NewDecoder(resp.Body).Decode(&msg)
	if len(msg.Content) != 1 || msg.Content[0].Text != "plain answer" || msg.StopReason != "end_turn" {
		t.Fatalf("default reply %+v", msg)
	}
	// The log has every provider request with the key redacted and the body exact.
	reqs := p.Requests()
	if len(reqs) != 3 || reqs[0].Headers.Get("X-Api-Key") != "[redacted]" || string(reqs[0].Body) != body {
		t.Fatalf("log %+v", reqs)
	}
}

func TestAnthropicErrorsAndAuth(t *testing.T) {
	p := New("k1")
	srv := httptest.NewServer(p.Handler())
	defer srv.Close()
	script(t, srv.URL, antScript)
	hdr := map[string]string{"x-api-key": "k1", "anthropic-version": "2026-10-21"}
	resp := post(t, srv.URL+"/anthropic/v1/messages", hdr, `{"model":"fixture-model","max_tokens":10,"messages":[{"role":"user","content":"too busy"}]}`)
	var e struct {
		Type  string
		Error struct{ Type string }
	}
	_ = json.NewDecoder(resp.Body).Decode(&e)
	if resp.StatusCode != 429 || resp.Header.Get("Retry-After") != "7" || e.Error.Type != "rate_limit_error" || e.Type != "error" {
		t.Fatalf("429: %d %q %+v", resp.StatusCode, resp.Header.Get("Retry-After"), e)
	}
	resp = post(t, srv.URL+"/anthropic/v1/messages", hdr, `{"model":"fixture-model","max_tokens":10,"stream":true,"messages":[{"role":"user","content":"cut please"}]}`)
	got, err := io.ReadAll(resp.Body)
	if err == nil || len(got) != 40 {
		t.Fatalf("cut stream: %d bytes, err %v", len(got), err)
	}
	resp = post(t, srv.URL+"/anthropic/v1/messages", map[string]string{"x-api-key": "wrong", "anthropic-version": "x"}, `{}`)
	if resp.StatusCode != 401 {
		t.Fatalf("bad key: %d", resp.StatusCode)
	}
	req, _ := http.NewRequest("GET", srv.URL+"/anthropic/v1/models", nil)
	req.Header.Set("x-api-key", "k1")
	req.Header.Set("anthropic-version", "2026-10-21")
	mresp, _ := http.DefaultClient.Do(req)
	var list struct {
		Data []struct {
			Type           string
			ID             string
			MaxInputTokens int `json:"max_input_tokens"`
		}
	}
	_ = json.NewDecoder(mresp.Body).Decode(&list)
	if len(list.Data) != 1 || list.Data[0].Type != "model" || list.Data[0].MaxInputTokens != 1000000 {
		t.Fatalf("models %+v", list)
	}
}

func TestOpenAIStreamToolCalls(t *testing.T) {
	p := New("k2")
	srv := httptest.NewServer(p.Handler())
	defer srv.Close()
	script(t, srv.URL, `{"steps":[{"match":"result: 21","reply":{"text":"It is 21 degrees."}},
		{"match":"weather","reply":{"text":"Checking.","tool_calls":[{"name":"get_weather","input":{"city":"Paris"}}]}}]}`)
	hdr := map[string]string{"Authorization": "Bearer k2"}
	resp := post(t, srv.URL+"/openai/v1/chat/completions", hdr,
		`{"model":"fixture-model","stream":true,"stream_options":{"include_usage":true},"messages":[{"role":"system","content":"s"},{"role":"user","content":"weather?"}]}`)
	_, datas := sse(t, resp.Body)
	if datas[len(datas)-1] != "[DONE]" {
		t.Fatalf("no [DONE]: %v", datas)
	}
	var text, args, name, finish string
	var usage int
	for _, d := range datas[:len(datas)-1] {
		var c struct {
			Choices []struct {
				Delta struct {
					Content   string
					ToolCalls []struct {
						Function struct{ Name, Arguments string }
					} `json:"tool_calls"`
				}
				FinishReason *string `json:"finish_reason"`
			}
			Usage *struct {
				TotalTokens int `json:"total_tokens"`
			}
		}
		if err := json.Unmarshal([]byte(d), &c); err != nil {
			t.Fatal(err)
		}
		if c.Usage != nil {
			usage = c.Usage.TotalTokens
		}
		for _, ch := range c.Choices {
			text += ch.Delta.Content
			for _, tc := range ch.Delta.ToolCalls {
				name += tc.Function.Name
				args += tc.Function.Arguments
			}
			if ch.FinishReason != nil {
				finish = *ch.FinishReason
			}
		}
	}
	if text != "Checking." || name != "get_weather" || args != `{"city":"Paris"}` || finish != "tool_calls" || usage == 0 {
		t.Fatalf("got %q %q %q %q %d", text, name, args, finish, usage)
	}
	// The tool message is the newest input on this format.
	resp = post(t, srv.URL+"/openai/v1/chat/completions", hdr,
		`{"model":"fixture-model","messages":[{"role":"user","content":"weather?"},{"role":"assistant","content":null},{"role":"tool","tool_call_id":"c","content":"result: 21"}]}`)
	var out bytes.Buffer
	_, _ = io.Copy(&out, resp.Body)
	if !strings.Contains(out.String(), "It is 21 degrees.") {
		t.Fatalf("tool turn: %s", out.String())
	}
	if resp := post(t, srv.URL+"/openai/v1/chat/completions", map[string]string{"Authorization": "Bearer no"}, `{}`); resp.StatusCode != 401 {
		t.Fatalf("bad bearer: %d", resp.StatusCode)
	}
}
