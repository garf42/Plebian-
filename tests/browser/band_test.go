package browser_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"plebian/server/browser"
	"plebian/tests/fixtures/model"
	"plebian/tests/kit"
)

// mcpCli is Playwright MCP's entry point (PLEBIAN_BROWSER_FROM/mcp, or a local install).
func mcpCli(t *testing.T) (node, cli string) {
	for _, base := range []string{os.Getenv("PLEBIAN_BROWSER_FROM"), "/tmp/claude-0"} {
		c := filepath.Join(base, "mcp", "node_modules", "@playwright", "mcp", "cli.js")
		if _, err := os.Stat(c); err == nil && base != "" {
			n := filepath.Join(base, "node", "bin", "node")
			if _, err := os.Stat(n); err != nil {
				n = "node"
				if p, err := execLook("node"); err == nil {
					n = p
				}
			}
			return n, c
		}
	}
	t.Skip("no Playwright MCP (PLEBIAN_BROWSER_FROM)")
	return "", ""
}

// D4 (n.tools): Playwright MCP's own tool definitions live only in the browse
// sub-agent's requests, never in the main tool band.
func TestMainBandExcludesBrowserDefs(t *testing.T) {
	node, cli := mcpCli(t)
	box := &browser.Box{Chrome: chrome(t), Node: node, MCPCli: cli, Profile: t.TempDir()}
	var k *kit.Kit
	br := browser.Browse{B: box}
	k = kit.New(t, "anthropic", model.Script{Steps: []*model.Step{
		call("browse", `{"task":"say hello from the browser"}`),
		{Match: "say hello from the browser", Reply: &model.Reply{Text: "Hello from the browser."}},
		{Match: "Hello from the browser", Reply: &model.Reply{Text: "Done."}},
	}}, &subBrowse{b: &br, k: &k})
	k.Say("browse something")
	var main, sub int
	for _, r := range k.Requests() {
		s := string(r)
		switch {
		case strings.Contains(s, `"name":"browse"`):
			main++
			if strings.Contains(s, "browser_navigate") {
				t.Fatal("a main request carries Playwright MCP's tool definitions")
			}
		case strings.Contains(s, "browser_navigate"):
			sub++
		}
	}
	if main == 0 || sub == 0 {
		t.Fatalf("main requests %d, sub-agent requests %d", main, sub)
	}
}
