package ops_test

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"plebian/server/ops"
)

func bundle(t *testing.T, files map[string]string) []byte {
	var b bytes.Buffer
	zw := gzip.NewWriter(&b)
	tw := tar.NewWriter(zw)
	for name, body := range files {
		tw.WriteHeader(&tar.Header{Name: name, Mode: 0o755, Size: int64(len(body)), Typeflag: tar.TypeReg})
		tw.Write([]byte(body))
	}
	tw.Close()
	zw.Close()
	return b.Bytes()
}

func publish(t *testing.T, tgz []byte, sum string) string {
	dir := t.TempDir()
	p := filepath.Join(dir, ops.BrowserAsset("arm64"))
	os.WriteFile(p, tgz, 0o644)
	os.WriteFile(p+".sha256", []byte(sum+"  "+filepath.Base(p)+"\n"), 0o644)
	return p
}

// G032 (n.ops): install takes the release's browser bundle only as its .sha256 names it,
// and only whole: Node and Playwright MCP's entry point must be in it.
func TestBrowserBundleChecked(t *testing.T) {
	ctx := context.Background()
	whole := bundle(t, map[string]string{"browser/node/bin/node": "node", "browser/mcp/node_modules/@playwright/mcp/cli.js": "cli", "browser/chromium/chrome-linux/chrome": "chrome"})
	h := sha256.Sum256(whole)
	d, err := ops.FetchBrowser(ctx, publish(t, whole, hex.EncodeToString(h[:])), t.TempDir())
	if err != nil {
		t.Fatalf("a checked bundle was refused: %v", err)
	}
	if b, _ := os.ReadFile(filepath.Join(d, "chromium/chrome-linux/chrome")); string(b) != "chrome" {
		t.Fatalf("the bundle unpacked to %s without Chromium", d)
	}
	if _, err := ops.FetchBrowser(ctx, publish(t, whole, strings.Repeat("0", 64)), t.TempDir()); err == nil {
		t.Fatalf("a bundle that does not match its sha256 was taken")
	}
	part := bundle(t, map[string]string{"browser/node/bin/node": "node"})
	hp := sha256.Sum256(part)
	if _, err := ops.FetchBrowser(ctx, publish(t, part, hex.EncodeToString(hp[:])), t.TempDir()); err == nil {
		t.Fatalf("a bundle without Playwright MCP was taken")
	}
}
