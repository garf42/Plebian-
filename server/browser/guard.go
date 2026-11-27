package browser

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"
)

// Chrome is the box's one browser.
type Chrome struct {
	cmd  *exec.Cmd
	Port string // the DevTools port on 127.0.0.1
	WS   string // the browser target's WebSocket URL
}

// Launch starts Chromium on a persistent profile, DevTools on a free loopback port.
func Launch(ctx context.Context, bin, profile string) (*Chrome, error) {
	os.MkdirAll(profile, 0o700)
	os.Remove(filepath.Join(profile, "DevToolsActivePort"))
	args := []string{"--headless=new", "--remote-debugging-address=127.0.0.1", "--remote-debugging-port=0",
		"--user-data-dir=" + profile, "--no-first-run", "--no-default-browser-check", "--disable-background-networking"}
	if os.Geteuid() == 0 {
		args = append(args, "--no-sandbox") // Chromium refuses root with its sandbox; the server's own account keeps it
	}
	cmd := exec.CommandContext(ctx, bin, append(args, "about:blank")...)
	if err := cmd.Start(); err != nil {
		return nil, err
	}
	exited := make(chan struct{})
	go func() { cmd.Wait(); close(exited) }()
	for i := 0; i < 300; i++ {
		select {
		case <-exited:
			return nil, errors.New("chromium exited before opening its DevTools port")
		default:
		}
		b, err := os.ReadFile(filepath.Join(profile, "DevToolsActivePort"))
		if err == nil {
			lines := strings.Split(strings.TrimSpace(string(b)), "\n")
			if len(lines) == 2 {
				return &Chrome{cmd: cmd, Port: lines[0], WS: "ws://127.0.0.1:" + lines[0] + lines[1]}, nil
			}
		}
		time.Sleep(100 * time.Millisecond)
	}
	cmd.Process.Kill()
	return nil, errors.New("chromium did not open its DevTools port")
}

func (c *Chrome) Endpoint() string { return "http://127.0.0.1:" + c.Port }

func (c *Chrome) Close() {
	if c.cmd != nil && c.cmd.Process != nil {
		c.cmd.Process.Kill()
	}
}

// Request is a held request exactly as the page made it.
type Request struct {
	Method  string            `json:"method"`
	URL     string            `json:"url"`
	Headers map[string]string `json:"headers"`
	Body    []byte            `json:"body,omitempty"`
}

// Shown is the request written out for the owner, nothing left out.
func (r Request) Shown() string {
	var b strings.Builder
	fmt.Fprintf(&b, "%s %s\n", r.Method, r.URL)
	ks := make([]string, 0, len(r.Headers))
	for k := range r.Headers {
		ks = append(ks, k)
	}
	sort.Strings(ks)
	for _, k := range ks {
		fmt.Fprintf(&b, "%s: %s\n", k, r.Headers[k])
	}
	if len(r.Body) > 0 {
		b.WriteString("\n" + string(r.Body))
	}
	return b.String()
}

type held struct {
	session, id string
	req         Request
}

// Guard watches every page target: reads go, beacons drop, the rest is held while an
// agent drives. What one agent action set off is one group, raised as one draft.
type Guard struct {
	C *CDP
	// Draft raises a group of held requests as a draft and returns its id.
	Draft func(ctx context.Context, group int64, reqs []Request) (int64, error)

	mu      sync.Mutex
	owner   bool  // the owner holds the wheel: nothing is held, nothing is recorded
	action  int64 // the agent's current action
	groups  map[int64][]held
	drafted map[int64]int64   // group → draft
	byDraft map[int64]int64   // draft → group
	pages   []string          // page sessions, newest last
	targets map[string]string // target id → session
	raised  []int64           // drafts raised since the last TakeRaised
}

// TakeRaised returns the drafts raised since it was last called.
func (g *Guard) TakeRaised() []int64 {
	g.mu.Lock()
	defer g.mu.Unlock()
	r := g.raised
	g.raised = nil
	return r
}

// Watch attaches to every target the browser opens and starts guarding.
func (g *Guard) Watch(ctx context.Context) error {
	g.groups, g.drafted, g.byDraft, g.targets = map[int64][]held{}, map[int64]int64{}, map[int64]int64{}, map[string]string{}
	g.C.On("Target.attachedToTarget", func(_ string, p json.RawMessage) {
		var a struct {
			SessionID  string `json:"sessionId"`
			TargetInfo struct {
				Type     string `json:"type"`
				TargetID string `json:"targetId"`
				URL      string `json:"url"`
			} `json:"targetInfo"`
			Waiting bool `json:"waitingForDebugger"`
		}
		json.Unmarshal(p, &a)
		c := context.Background()
		g.C.Call(c, a.SessionID, "Target.setAutoAttach", map[string]any{"autoAttach": true, "waitForDebuggerOnStart": true, "flatten": true}, nil)
		if a.TargetInfo.Type == "page" || a.TargetInfo.Type == "iframe" || strings.HasSuffix(a.TargetInfo.Type, "worker") {
			g.C.Call(c, a.SessionID, "Fetch.enable", map[string]any{"patterns": []map[string]any{{"urlPattern": "*", "requestStage": "Request"}}}, nil)
		}
		if a.TargetInfo.Type == "page" {
			g.mu.Lock()
			g.targets[a.TargetInfo.TargetID] = a.SessionID
			g.pages = append(g.pages, a.SessionID)
			g.mu.Unlock()
		}
		g.C.Call(c, a.SessionID, "Runtime.runIfWaitingForDebugger", nil, nil)
	})
	g.C.On("Target.detachedFromTarget", func(_ string, p json.RawMessage) {
		var d struct {
			SessionID string `json:"sessionId"`
		}
		json.Unmarshal(p, &d)
		g.mu.Lock()
		for i, s := range g.pages {
			if s == d.SessionID {
				g.pages = append(g.pages[:i], g.pages[i+1:]...)
				break
			}
		}
		g.mu.Unlock()
	})
	g.C.On("Fetch.requestPaused", g.paused)
	// Beacons outlive their page (keepalive) and leave through the browser itself; only
	// those are caught at the browser, so nothing else is paused twice.
	if err := g.C.Call(ctx, "", "Fetch.enable", map[string]any{"patterns": []map[string]any{{"urlPattern": "*", "resourceType": "Ping", "requestStage": "Request"}}}, nil); err != nil {
		return err
	}
	return g.C.Call(ctx, "", "Target.setAutoAttach", map[string]any{"autoAttach": true, "waitForDebuggerOnStart": true, "flatten": true}, nil)
}

// Page is the newest page's session (the one the live view shows).
func (g *Guard) Page() string {
	g.mu.Lock()
	defer g.mu.Unlock()
	if len(g.pages) == 0 {
		return ""
	}
	return g.pages[len(g.pages)-1]
}

// Session is the session attached to a target, once attached.
func (g *Guard) Session(target string) string {
	g.mu.Lock()
	defer g.mu.Unlock()
	return g.targets[target]
}

// Owner sets who holds the wheel.
func (g *Guard) Owner(on bool) { g.mu.Lock(); g.owner = on; g.mu.Unlock() }

// Holding reports the owner holding the wheel.
func (g *Guard) Holding() bool { g.mu.Lock(); defer g.mu.Unlock(); return g.owner }

// Step marks the start of an agent action; it raises the last action's holds as one draft.
func (g *Guard) Step(ctx context.Context) {
	g.mu.Lock()
	last := g.action
	g.action++
	g.mu.Unlock()
	g.raise(ctx, last)
}

// Settle raises whatever the current action has held.
func (g *Guard) Settle(ctx context.Context) {
	g.mu.Lock()
	a := g.action
	g.mu.Unlock()
	g.raise(ctx, a)
}

func (g *Guard) raise(ctx context.Context, group int64) {
	g.mu.Lock()
	hs := g.groups[group]
	_, done := g.drafted[group]
	g.mu.Unlock()
	if len(hs) == 0 || done || g.Draft == nil {
		return
	}
	reqs := make([]Request, len(hs))
	for i, h := range hs {
		reqs[i] = h.req
	}
	id, err := g.Draft(ctx, group, reqs)
	if err != nil {
		return
	}
	g.mu.Lock()
	g.drafted[group], g.byDraft[id] = id, group
	g.raised = append(g.raised, id)
	g.mu.Unlock()
}

func header(h map[string]string, k string) string {
	for hk, v := range h {
		if strings.EqualFold(hk, k) {
			return v
		}
	}
	return ""
}

func (g *Guard) paused(session string, p json.RawMessage) {
	var ev struct {
		RequestID string `json:"requestId"`
		Request   struct {
			URL             string            `json:"url"`
			Method          string            `json:"method"`
			Headers         map[string]string `json:"headers"`
			PostData        string            `json:"postData"`
			PostDataEntries []struct {
				Bytes string `json:"bytes"`
			} `json:"postDataEntries"`
		} `json:"request"`
		ResourceType string `json:"resourceType"`
	}
	if json.Unmarshal(p, &ev) != nil {
		return
	}
	ctx := context.Background()
	cont := func() {
		g.C.Call(ctx, session, "Fetch.continueRequest", map[string]any{"requestId": ev.RequestID}, nil)
	}
	var body []byte
	if len(ev.Request.PostDataEntries) > 0 {
		for _, e := range ev.Request.PostDataEntries {
			b, _ := base64.StdEncoding.DecodeString(e.Bytes)
			body = append(body, b...)
		}
	} else {
		body = []byte(ev.Request.PostData)
	}
	g.mu.Lock()
	owner, action := g.owner, g.action
	g.mu.Unlock()
	switch {
	case owner:
		cont() // the owner's own hand; nothing is recorded
	case safe(ev.Request.Method):
		cont()
	case graphqlRead(body, header(ev.Request.Headers, "Content-Type")):
		cont()
	case ev.ResourceType == "Ping" || ev.ResourceType == "CSPViolationReport":
		// A beacon or ping: no page waits on its answer, so it is neither sent nor drafted.
		g.C.Call(ctx, session, "Fetch.failRequest", map[string]any{"requestId": ev.RequestID, "errorReason": "BlockedByClient"}, nil)
	default:
		g.mu.Lock()
		g.groups[action] = append(g.groups[action], held{session: session, id: ev.RequestID,
			req: Request{Method: ev.Request.Method, URL: ev.Request.URL, Headers: ev.Request.Headers, Body: body}})
		g.mu.Unlock()
	}
}

// Release sends a draft's held requests exactly as they were held, once; a request the
// page can no longer send fails the draft and says so.
func (g *Guard) Release(ctx context.Context, draft int64) (string, error) {
	g.mu.Lock()
	group, ok := g.byDraft[draft]
	hs := g.groups[group]
	delete(g.groups, group)
	delete(g.byDraft, draft)
	g.mu.Unlock()
	if !ok || len(hs) == 0 {
		return "", fmt.Errorf("draft %d holds nothing this browser still has: the page can no longer send it", draft)
	}
	var out bytes.Buffer
	for _, h := range hs {
		if err := g.C.Call(ctx, h.session, "Fetch.continueRequest", map[string]any{"requestId": h.id}, nil); err != nil {
			return out.String(), fmt.Errorf("%s %s: the page can no longer send it (%v)", h.req.Method, h.req.URL, err)
		}
		u, _ := url.Parse(h.req.URL)
		fmt.Fprintf(&out, "released %s %s\n", h.req.Method, u.Redacted())
	}
	return out.String(), nil
}
