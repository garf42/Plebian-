package tools

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"golang.org/x/net/html"

	"plebian/server/models"
)

// Secrets is the harness's secret store as tools see it: values by name, and all values
// for scrubbing.
type Secrets interface {
	Get(name string) string
	Values() []string
}

type shellIn struct {
	Command string   `json:"command"`
	Network bool     `json:"network"`
	Outside bool     `json:"outside"`
	Secrets []string `json:"secrets"`
}

// Shell is `read` (Write false) or `run` (Write true): shell commands handed to the tools
// user's runner. A secret is named in the call; its value goes into the command's
// environment and is scrubbed from every stream before the output becomes a reading.
type Shell struct {
	Write   bool
	Runner  Client
	Secrets Secrets
}

func (s Shell) Def() models.Tool {
	if s.Write {
		return models.Tool{Name: "run", Description: "Run shell commands in the sandbox with a writable workspace; network or the outside folder only when asked.",
			Schema: Schema([]string{"command"}, map[string][2]string{
				"command": {"string", "the shell command"},
				"network": {"boolean", "the command needs the network"},
				"outside": {"boolean", "the command reads the outside folder"},
				"secrets": {"array", "names of secrets the command needs in its environment"},
			})}
	}
	return models.Tool{Name: "read", Description: "Run shell commands in a read-only sandbox with no network; the outside folder only when asked.",
		Schema: Schema([]string{"command"}, map[string][2]string{
			"command": {"string", "the shell command"},
			"outside": {"boolean", "the command reads the outside folder"},
		})}
}

func (s Shell) parse(in json.RawMessage) (shellIn, error) {
	var x shellIn
	if err := json.Unmarshal(in, &x); err != nil {
		return x, err
	}
	if !s.Write {
		x.Network = false
	}
	return x, nil
}

func (s Shell) Class(in json.RawMessage) Class {
	x, err := s.parse(in)
	switch {
	case err != nil:
		return Outside
	case !s.Write:
		return Read
	case x.Network:
		return Outside
	}
	return Local
}

func (s Shell) ReachesOut(in json.RawMessage) bool {
	x, err := s.parse(in)
	return err != nil || x.Network
}

func (s Shell) Run(ctx context.Context, in json.RawMessage) (Result, error) {
	x, err := s.parse(in)
	if err != nil || strings.TrimSpace(x.Command) == "" {
		return Result{Reading: []byte("needs a command"), Exit: 2}, nil
	}
	env := map[string]string{}
	for _, name := range x.Secrets {
		v := s.Secrets.Get(name)
		if v == "" {
			return Result{Reading: []byte("no secret named " + name), Exit: 2}, nil
		}
		env[name] = v
	}
	d, err := s.Runner.Do(ctx, Job{Command: x.Command, Write: s.Write, Network: x.Network, Outside: x.Outside, Env: env})
	if err != nil {
		return Result{}, err
	}
	var b bytes.Buffer
	b.Write(d.Stdout)
	if len(d.Stderr) > 0 {
		if b.Len() > 0 && !bytes.HasSuffix(b.Bytes(), []byte("\n")) {
			b.WriteByte('\n')
		}
		b.Write(d.Stderr)
	}
	if d.Error != "" {
		b.WriteString(d.Error)
	}
	return Result{Reading: Scrub(b.Bytes(), s.Secrets.Values()), Exit: d.Exit, Outside: x.Network || x.Outside}, nil
}

// MaxPage bounds one fetched page (a page larger is cut and says so).
const MaxPage = 8 << 20

func get(ctx context.Context, hc *http.Client, u string) ([]byte, string, int, error) {
	pu, err := url.Parse(u)
	if err != nil || (pu.Scheme != "http" && pu.Scheme != "https") {
		return nil, "", 0, fmt.Errorf("not an http(s) URL: %q", u)
	}
	req, err := http.NewRequestWithContext(ctx, "GET", u, nil)
	if err != nil {
		return nil, "", 0, err
	}
	req.Header.Set("User-Agent", "plebian")
	resp, err := hc.Do(req)
	if err != nil {
		return nil, "", 0, err
	}
	defer resp.Body.Close()
	b, err := io.ReadAll(io.LimitReader(resp.Body, MaxPage))
	return b, resp.Header.Get("Content-Type"), resp.StatusCode, err
}

// Readable is a page's text: its title and the text of its body, scripts and styles
// left out, links kept as their URLs.
func Readable(page []byte, base string) string {
	doc, err := html.Parse(bytes.NewReader(page))
	if err != nil {
		return string(page)
	}
	var b strings.Builder
	var walk func(n *html.Node)
	walk = func(n *html.Node) {
		if n.Type == html.ElementNode {
			switch n.Data {
			case "script", "style", "noscript", "svg", "head":
				if n.Data == "head" {
					for c := n.FirstChild; c != nil; c = c.NextSibling {
						if c.Type == html.ElementNode && c.Data == "title" && c.FirstChild != nil {
							b.WriteString("# " + strings.TrimSpace(c.FirstChild.Data) + "\n")
						}
					}
				}
				return
			case "br", "p", "div", "li", "tr", "h1", "h2", "h3", "h4", "h5", "h6", "section", "article":
				b.WriteString("\n")
			}
		}
		if n.Type == html.TextNode {
			if t := strings.Join(strings.Fields(n.Data), " "); t != "" {
				b.WriteString(t + " ")
			}
		}
		for c := n.FirstChild; c != nil; c = c.NextSibling {
			walk(c)
		}
		if n.Type == html.ElementNode && n.Data == "a" {
			for _, a := range n.Attr {
				if a.Key == "href" {
					if u, err := url.Parse(base); err == nil {
						if r, err := u.Parse(a.Val); err == nil {
							b.WriteString("<" + r.String() + "> ")
						}
					}
				}
			}
		}
	}
	walk(doc)
	var out []string
	for _, l := range strings.Split(b.String(), "\n") {
		if l = strings.TrimSpace(l); l != "" {
			out = append(out, l)
		}
	}
	return strings.Join(out, "\n")
}

// Fetch is `fetch`: HTTP GET a URL and return its readable text, as outside words.
type Fetch struct{ HTTP *http.Client }

func (f Fetch) Def() models.Tool {
	return models.Tool{Name: "fetch", Description: "HTTP GET a URL and return its readable text.",
		Schema: Schema([]string{"url"}, map[string][2]string{"url": {"string", "the URL"}})}
}
func (Fetch) Class(json.RawMessage) Class     { return Read }
func (Fetch) ReachesOut(json.RawMessage) bool { return true }
func (f Fetch) Run(ctx context.Context, in json.RawMessage) (Result, error) {
	var x struct{ URL string }
	if json.Unmarshal(in, &x) != nil {
		return Result{Reading: []byte("needs a url"), Exit: 2}, nil
	}
	b, ct, status, err := get(ctx, f.HTTP, x.URL)
	if err != nil {
		return Result{Reading: []byte(err.Error()), Exit: 1, Outside: true}, nil
	}
	text := string(b)
	if strings.Contains(ct, "html") || bytes.Contains(b[:min(len(b), 512)], []byte("<html")) {
		text = Readable(b, x.URL)
	}
	exit := 0
	if status >= 400 {
		exit = 1
	}
	return Result{Reading: []byte(fmt.Sprintf("%s (%d)\n%s", x.URL, status, text)), Exit: exit, Outside: true}, nil
}

// Search is `search`: it reads a results page whose URL is a setting (with none set, a
// keyless engine's plain-HTML page, gap G013) and returns titles, URLs and snippets.
type Search struct {
	HTTP *http.Client
	URL  func() string // the results URL; the query is appended
}

// DefaultSearch is DuckDuckGo's plain-HTML results page (gap G013).
const DefaultSearch = "https://html.duckduckgo.com/html/?q="

func (s Search) Def() models.Tool {
	return models.Tool{Name: "search", Description: "Web search: titles, URLs and snippets from a results page.",
		Schema: Schema([]string{"query"}, map[string][2]string{"query": {"string", "what to search for"}})}
}
func (Search) Class(json.RawMessage) Class     { return Read }
func (Search) ReachesOut(json.RawMessage) bool { return true }
func (s Search) Run(ctx context.Context, in json.RawMessage) (Result, error) {
	var x struct{ Query string }
	if json.Unmarshal(in, &x) != nil || strings.TrimSpace(x.Query) == "" {
		return Result{Reading: []byte("needs a query"), Exit: 2}, nil
	}
	base := DefaultSearch
	if s.URL != nil && s.URL() != "" {
		base = s.URL()
	}
	u := base + url.QueryEscape(x.Query)
	b, _, status, err := get(ctx, s.HTTP, u)
	if err != nil {
		return Result{Reading: []byte(err.Error()), Exit: 1, Outside: true}, nil
	}
	results := Results(b, u)
	var out strings.Builder
	fmt.Fprintf(&out, "results for %q (%d):\n", x.Query, status)
	for _, r := range results {
		fmt.Fprintf(&out, "- %s\n  %s\n  %s\n", r.Title, r.URL, r.Snippet)
	}
	if len(results) == 0 {
		out.WriteString(Readable(b, u))
	}
	return Result{Reading: []byte(out.String()), Exit: 0, Outside: true}, nil
}

// Result is one search result.
type SearchResult struct{ Title, URL, Snippet string }

// Results reads a results page shaped like DuckDuckGo's HTML one: links with class
// result__a, snippets with class result__snippet; the target URL is unwrapped from a
// redirect's uddg parameter.
func Results(page []byte, base string) []SearchResult {
	doc, err := html.Parse(bytes.NewReader(page))
	if err != nil {
		return nil
	}
	cls := func(n *html.Node, c string) bool {
		for _, a := range n.Attr {
			if a.Key == "class" && strings.Contains(" "+a.Val+" ", " "+c+" ") {
				return true
			}
		}
		return false
	}
	text := func(n *html.Node) string {
		var b strings.Builder
		var w func(*html.Node)
		w = func(n *html.Node) {
			if n.Type == html.TextNode {
				b.WriteString(n.Data)
			}
			for c := n.FirstChild; c != nil; c = c.NextSibling {
				w(c)
			}
		}
		w(n)
		return strings.Join(strings.Fields(b.String()), " ")
	}
	var out []SearchResult
	var walk func(*html.Node)
	walk = func(n *html.Node) {
		if n.Type == html.ElementNode && n.Data == "a" && cls(n, "result__a") {
			r := SearchResult{Title: text(n)}
			for _, a := range n.Attr {
				if a.Key == "href" {
					if u, err := url.Parse(base); err == nil {
						if h, err := u.Parse(a.Val); err == nil {
							if t := h.Query().Get("uddg"); t != "" {
								r.URL = t
							} else {
								r.URL = h.String()
							}
						}
					}
				}
			}
			out = append(out, r)
		}
		if n.Type == html.ElementNode && cls(n, "result__snippet") && len(out) > 0 {
			out[len(out)-1].Snippet = text(n)
		}
		for c := n.FirstChild; c != nil; c = c.NextSibling {
			walk(c)
		}
	}
	walk(doc)
	return out
}

// HTTPClient is the client fetch and search use.
func HTTPClient() *http.Client { return &http.Client{Timeout: 60 * time.Second} }
