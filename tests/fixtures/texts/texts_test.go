package texts

import (
	"bufio"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestPairStreamSend(t *testing.T) {
	x := New()
	srv := httptest.NewServer(x.Handler())
	defer srv.Close()
	post := func(path, body string) *http.Response {
		resp, err := http.Post(srv.URL+path, "application/json", strings.NewReader(body))
		if err != nil {
			t.Fatal(err)
		}
		return resp
	}
	if r := post("/v1/send", `{"conversation_id":"c1","text":"hi"}`); r.StatusCode != 401 {
		t.Fatalf("unpaired send: %d", r.StatusCode)
	}
	var pair struct{ Emoji string }
	_ = json.NewDecoder(post("/v1/pair", `{"cookies":{"SID":"abc"}}`).Body).Decode(&pair)
	if r := []rune(pair.Emoji); len(r) != 1 || r[0] < 0x1F600 || r[0] > 0x1F64F {
		t.Fatalf("emoji %q", pair.Emoji)
	}
	if r, _ := http.Get(srv.URL + "/v1/stream"); r.StatusCode != 401 {
		t.Fatalf("stream before tap: %d", r.StatusCode)
	}
	post("/control/phone_tap", "")
	post("/control/inject", `{"conversation_id":"c1","sender":"+15550100","text":"backlog one","ts_ms":1764000000000}`)
	resp, err := http.Get(srv.URL + "/v1/stream")
	if err != nil || resp.StatusCode != 200 {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	lines := bufio.NewScanner(resp.Body)
	read := func() Message {
		got := make(chan Message, 1)
		go func() {
			var m Message
			if lines.Scan() {
				_ = json.Unmarshal(lines.Bytes(), &m)
			}
			got <- m
		}()
		select {
		case m := <-got:
			return m
		case <-time.After(5 * time.Second):
			t.Fatal("stream stalled")
		}
		return Message{}
	}
	if m := read(); m.Text != "backlog one" || m.TsMs != 1764000000000 {
		t.Fatalf("backlog %+v", m)
	}
	post("/control/inject", `{"conversation_id":"c1","sender":"+15550100","text":"live two"}`)
	if m := read(); m.Text != "live two" || m.MessageID == "" {
		t.Fatalf("live %+v", m)
	}
	var sent struct{ MessageID string `json:"message_id"` }
	_ = json.NewDecoder(post("/v1/send", `{"conversation_id":"c1","text":"on my way"}`).Body).Decode(&sent)
	if sent.MessageID == "" || len(x.sent) != 1 || x.sent[0].Text != "on my way" {
		t.Fatalf("sent %+v", x.sent)
	}
}
