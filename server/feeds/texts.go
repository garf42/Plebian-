package feeds

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net/http"
	"strings"
	"sync"
	"time"

	"plebian/server/config"
	"plebian/server/models"
	"plebian/server/tools"
	"plebian/server/underlay"
	"plebian/server/watch"
)

// Text is one inbound text as the link delivers it.
type Text struct {
	ConversationID string `json:"conversation_id"`
	MessageID      string `json:"message_id"`
	Sender         string `json:"sender"`
	Text           string `json:"text"`
	TsMs           int64  `json:"ts_ms"`
}

// Link is the texts library's interface: pair from Google cookies plus an emoji the owner
// taps on the phone, a stream of inbound texts (backlog, then live), and send.
type Link interface {
	Pair(ctx context.Context, cookies map[string]string) (emoji string, err error)
	Paired(ctx context.Context) (bool, error)
	Stream(ctx context.Context, fn func(raw []byte, t Text) error) error
	Send(ctx context.Context, conversation, text string) (id string, err error)
}

// Bridge is a Link served over HTTP at the library's interface (gap G027).
type Bridge struct {
	URL  string
	HTTP *http.Client
}

func (b Bridge) do(ctx context.Context, method, path string, in, out any) error {
	var body *bytes.Reader
	if in != nil {
		j, _ := json.Marshal(in)
		body = bytes.NewReader(j)
	} else {
		body = bytes.NewReader(nil)
	}
	req, err := http.NewRequestWithContext(ctx, method, strings.TrimRight(b.URL, "/")+path, body)
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := b.HTTP.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		return fmt.Errorf("texts link %s answered %d", path, resp.StatusCode)
	}
	return json.NewDecoder(resp.Body).Decode(out)
}

func (b Bridge) Pair(ctx context.Context, cookies map[string]string) (string, error) {
	var r struct{ Emoji string }
	err := b.do(ctx, "POST", "/v1/pair", map[string]any{"cookies": cookies}, &r)
	return r.Emoji, err
}

func (b Bridge) Paired(ctx context.Context) (bool, error) {
	var r struct{ Paired bool }
	err := b.do(ctx, "GET", "/v1/status", nil, &r)
	return r.Paired, err
}

func (b Bridge) Send(ctx context.Context, conv, text string) (string, error) {
	var r struct {
		MessageID string `json:"message_id"`
	}
	err := b.do(ctx, "POST", "/v1/send", map[string]string{"conversation_id": conv, "text": text}, &r)
	return r.MessageID, err
}

func (b Bridge) Stream(ctx context.Context, fn func([]byte, Text) error) error {
	req, err := http.NewRequestWithContext(ctx, "GET", strings.TrimRight(b.URL, "/")+"/v1/stream", nil)
	if err != nil {
		return err
	}
	resp, err := (&http.Client{Transport: b.HTTP.Transport}).Do(req) // a long poll: no client timeout
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		return fmt.Errorf("texts stream answered %d", resp.StatusCode)
	}
	sc := bufio.NewScanner(resp.Body)
	sc.Buffer(make([]byte, 64<<10), 16<<20)
	for sc.Scan() {
		raw := append([]byte(nil), sc.Bytes()...)
		var t Text
		if err := json.Unmarshal(raw, &t); err != nil {
			continue
		}
		if err := fn(raw, t); err != nil {
			return err
		}
	}
	if err := sc.Err(); err != nil {
		return err
	}
	return errors.New("texts stream ended")
}

const textsSecret = "texts" // the link's endpoint

// Texts is the texts feed.
type Texts struct {
	L       *Lander
	Watch   *watch.Watch
	Secrets *config.Secrets
	HTTP    *http.Client
	Usual   func(ctx context.Context) time.Duration
	Log     *log.Logger

	once sync.Once
	kick chan struct{}
}

func (t *Texts) init() { t.once.Do(func() { t.kick = make(chan struct{}, 1) }) }

func (t *Texts) link() (Link, bool) {
	u := t.Secrets.Get(textsSecret)
	if u == "" {
		return nil, false
	}
	return Bridge{URL: u, HTTP: t.HTTP}, true
}

// Connect pairs the link from the cookies and returns the emoji for the owner to tap.
func (t *Texts) Connect(ctx context.Context, bridge string, cookies map[string]string) (string, error) {
	t.init()
	if bridge != "" {
		if err := t.Secrets.Set(textsSecret, bridge); err != nil {
			return "", err
		}
	}
	l, ok := t.link()
	if !ok {
		return "", errors.New("no texts link is set")
	}
	emoji, err := l.Pair(ctx, cookies)
	if err != nil {
		return "", err
	}
	select {
	case t.kick <- struct{}{}:
	default:
	}
	return emoji, nil
}

// Run lands texts while the link is paired.
func (t *Texts) Run(ctx context.Context) {
	t.init()
	for ctx.Err() == nil {
		l, ok := t.link()
		if !ok {
			select {
			case <-ctx.Done():
				return
			case <-t.kick:
			}
			continue
		}
		if p, err := l.Paired(ctx); err == nil && p {
			err := l.Stream(ctx, func(raw []byte, x Text) error {
				at := time.UnixMilli(x.TsMs).UTC()
				row := underlay.Row{Source: "text", Lines: Lines(x.Sender, at, x.Text)}
				_, _, err := t.L.Put(ctx, t.Watch, "text", x.ConversationID+"/"+x.MessageID, at, at, row, raw)
				return err
			})
			if err != nil && ctx.Err() == nil && t.Log != nil {
				t.Log.Printf("texts: %v", err)
			}
		}
		select {
		case <-ctx.Done():
			return
		case <-t.kick:
		case <-t.L.Clock.After(t.L.Clock.Now().Add(retry(t.Usual(ctx)))):
		}
	}
}

// TextTool is `text_send`: an outside change, sent only on an approved draft.
type TextTool struct{ T *Texts }

func (TextTool) Def() models.Tool {
	return models.Tool{Name: "text_send", Description: "Send a text in a conversation through the owner's phone; sent only on the owner's word.",
		Schema: tools.Schema([]string{"conversation_id", "text"}, map[string][2]string{
			"conversation_id": {"string", "the conversation, from a text's heard row"},
			"text":            {"string", "the text to send"},
		})}
}
func (TextTool) Class(json.RawMessage) tools.Class { return tools.Outside }
func (TextTool) ReachesOut(json.RawMessage) bool   { return true }
func (x TextTool) Run(ctx context.Context, in json.RawMessage) (tools.Result, error) {
	var v struct {
		ConversationID string `json:"conversation_id"`
		Text           string `json:"text"`
	}
	if json.Unmarshal(in, &v) != nil || v.ConversationID == "" || v.Text == "" {
		return tools.Result{Reading: []byte("text_send needs a conversation_id and text"), Exit: 2}, nil
	}
	l, ok := x.T.link()
	if !ok {
		return tools.Result{Reading: []byte("no texts link is connected"), Exit: 1}, nil
	}
	id, err := l.Send(ctx, v.ConversationID, v.Text)
	if err != nil {
		return tools.Result{Reading: []byte("not sent: " + err.Error()), Exit: 1}, nil
	}
	return tools.Result{Reading: []byte("sent " + id)}, nil
}
