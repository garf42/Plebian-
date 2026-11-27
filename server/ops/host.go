package ops

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
)

// The box's browser is one bundle per release, built by CI from server/browser/bundle:
// Node.js, Playwright MCP and the Chromium that Playwright pins, checked by its .sha256.

// BrowserAsset is the release asset holding the browser bundle for an architecture.
func BrowserAsset(goarch string) string { return "plebian-browser-linux-" + goarch + ".tar.gz" }

// FetchBrowser downloads the bundle at url (an https URL or an absolute path, its .sha256
// beside it), checks it, unpacks it under dir and returns the bundle's browser directory.
func FetchBrowser(ctx context.Context, url, dir string) (string, error) {
	tgz, err := fetchCandidate(ctx, url)
	if err != nil {
		return "", err
	}
	if err := unpack(tgz, dir); err != nil {
		return "", err
	}
	b := filepath.Join(dir, "browser")
	for _, p := range []string{"node/bin/node", "mcp/node_modules/@playwright/mcp/cli.js"} {
		if _, err := os.Stat(filepath.Join(b, p)); err != nil {
			return "", fmt.Errorf("the browser bundle has no %s", p)
		}
	}
	return b, nil
}

// hostPrograms are programs the box's own system provides to the tools sandbox (it
// binds the host read-only), by the Debian package that ships each.
var hostPrograms = []struct{ program, pkg string }{
	{"pdftotext", "poppler-utils"}, // the agent reads a mailed PDF with it (A10)
}

// missingLibs lists the shared libraries the dynamic loader cannot find for bin.
func missingLibs(bin string) ([]string, error) {
	out, err := exec.Command("ldd", bin).CombinedOutput()
	if err != nil && len(out) == 0 {
		return nil, fmt.Errorf("ldd %s: %v", bin, err)
	}
	var miss []string
	for _, l := range strings.Split(string(out), "\n") {
		if strings.Contains(l, "not found") {
			miss = append(miss, strings.TrimSpace(strings.SplitN(l, "=>", 2)[0]))
		}
	}
	return miss, nil
}

func aptInstall(out io.Writer, pkgs ...string) error {
	if _, err := exec.LookPath("apt-get"); err != nil {
		return fmt.Errorf("no apt-get on this box; install %s with its package manager", strings.Join(pkgs, " "))
	}
	env := append(os.Environ(), "DEBIAN_FRONTEND=noninteractive")
	for _, args := range [][]string{{"update", "-qq"}, append([]string{"install", "-y", "-qq", "--no-install-recommends"}, pkgs...)} {
		cmd := exec.Command("apt-get", args...)
		cmd.Env = env
		var b bytes.Buffer
		cmd.Stdout, cmd.Stderr = &b, &b
		if err := cmd.Run(); err != nil {
			return fmt.Errorf("apt-get %s: %v: %s", strings.Join(args, " "), err, strings.TrimSpace(b.String()))
		}
	}
	fmt.Fprintln(out, "installed from the system's packages:", strings.Join(pkgs, " "))
	return nil
}

// EnsureHost puts on the box what its programs need from the system: the host programs
// above, and the libraries Chromium links against (Playwright's own list for this
// distribution, run only when the loader finds some missing). It changes nothing when
// everything is present. The packages stay when Plebian is uninstalled: other software
// may use them.
func EnsureHost(r Root, out io.Writer) error {
	var pkgs []string
	for _, h := range hostPrograms {
		if _, err := exec.LookPath(h.program); err != nil {
			pkgs = append(pkgs, h.pkg)
		}
	}
	if len(pkgs) > 0 {
		if err := aptInstall(out, pkgs...); err != nil {
			return err
		}
	}
	chrome, _ := filepath.Glob(r.P("browser", "chromium", "*", "chrome"))
	if len(chrome) == 0 {
		return nil
	}
	miss, err := missingLibs(chrome[0])
	if err != nil || len(miss) == 0 {
		return err
	}
	cmd := exec.Command(r.P("browser", "node", "bin", "node"), r.P("browser", "mcp", "node_modules", "playwright", "cli.js"), "install-deps", "chromium")
	cmd.Env = append(os.Environ(), "DEBIAN_FRONTEND=noninteractive")
	var b bytes.Buffer
	cmd.Stdout, cmd.Stderr = &b, &b
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("Chromium's libraries (%s): playwright install-deps: %v: %s", strings.Join(miss, " "), err, strings.TrimSpace(b.String()))
	}
	if still, _ := missingLibs(chrome[0]); len(still) > 0 {
		sort.Strings(still)
		return fmt.Errorf("Chromium still lacks %s after installing its libraries", strings.Join(still, " "))
	}
	fmt.Fprintln(out, "installed Chromium's libraries:", strings.Join(miss, " "))
	return nil
}
