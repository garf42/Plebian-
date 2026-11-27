// Probe: official Go MCP SDK as client of @playwright/mcp attached over CDP to one
// already-running Chrome (the same browser the owner's live view uses).
package main

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

func main() {
	go http.ListenAndServe("127.0.0.1:8765", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, `<!doctype html><title>Feed store</title><h1>Feed store</h1><button>Place order</button>`)
	}))
	chrome := exec.Command("google-chrome", "--headless=new", "--remote-debugging-port=9333", "--user-data-dir=/tmp/pl-probe-profile", "--no-first-run", "about:blank")
	if err := chrome.Start(); err != nil {
		panic(err)
	}
	defer chrome.Process.Kill()
	time.Sleep(2 * time.Second)
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	client := mcp.NewClient(&mcp.Implementation{Name: "plebian-probe", Version: "0"}, nil)
	tr := &mcp.CommandTransport{Command: exec.Command("node", "node_modules/@playwright/mcp/cli.js", "--cdp-endpoint", "http://127.0.0.1:9333")}
	s, err := client.Connect(ctx, tr, nil)
	if err != nil {
		panic(err)
	}
	defer s.Close()
	tools, err := s.ListTools(ctx, nil)
	if err != nil {
		panic(err)
	}
	b, _ := json.Marshal(tools.Tools)
	_, err = s.CallTool(ctx, &mcp.CallToolParams{Name: "browser_navigate", Arguments: map[string]any{"url": "http://127.0.0.1:8765/"}})
	if err != nil {
		panic(err)
	}
	res, err := s.CallTool(ctx, &mcp.CallToolParams{Name: "browser_snapshot", Arguments: map[string]any{}})
	if err != nil {
		panic(err)
	}
	text := ""
	for _, c := range res.Content {
		if t, ok := c.(*mcp.TextContent); ok {
			text += t.Text
		}
	}
	fmt.Printf("protocol=%s tools=%d tool_defs_bytes=%d snapshot_bytes=%d sees_button=%v\n",
		s.InitializeResult().ProtocolVersion, len(tools.Tools), len(b), len(text), contains(text, "Place order"))
	os.Exit(0)
}

func contains(s, sub string) bool {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}
