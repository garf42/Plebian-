package tools_test

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"plebian/server/tools"
)

var ctx = context.Background()

func runner(t *testing.T) *tools.Runner {
	bw, err := exec.LookPath("bwrap")
	if err != nil {
		t.Skip("bubblewrap is not installed")
	}
	root := t.TempDir()
	ws := filepath.Join(root, "workspace")
	os.MkdirAll(filepath.Join(ws, "outside"), 0o700)
	r := &tools.Runner{Workspace: ws, Outside: filepath.Join(ws, "outside"), Bwrap: bw}
	if d := r.Run(ctx, tools.Job{Command: "true"}); d.Exit != 0 {
		if bytes.Contains(d.Stderr, []byte("namespace")) || bytes.Contains(d.Stderr, []byte("Operation not permitted")) {
			t.Skipf("bubblewrap can't make a sandbox here (user namespaces refused, gap G007): %s", d.Stderr)
		}
		t.Fatalf("sandbox failed: %s %s", d.Stderr, d.Error)
	}
	return r
}

type secrets map[string]string

func (s secrets) Get(n string) string { return s[n] }
func (s secrets) Values() []string {
	var v []string
	for _, x := range s {
		v = append(v, x)
	}
	return v
}

type direct struct{ r *tools.Runner }

// D2: `read` writes nowhere: not the workspace, not /tmp, not the root.
func TestReadCannotWrite(t *testing.T) {
	r := runner(t)
	for _, c := range []string{"echo x > /tmp/p", "echo x > " + r.Workspace + "/p", "echo x > /var/tmp/p", "touch /p", "mkdir " + r.Workspace + "/d"} {
		d := r.Run(ctx, tools.Job{Command: c})
		if d.Exit == 0 {
			t.Fatalf("read wrote: %q", c)
		}
	}
	if _, err := os.Stat(filepath.Join(r.Workspace, "p")); err == nil {
		t.Fatal("a file appeared in the workspace")
	}
	if d := r.Run(ctx, tools.Job{Command: "cat /etc/hostname >/dev/null && echo ok"}); d.Exit != 0 || !bytes.Contains(d.Stdout, []byte("ok")) {
		t.Fatalf("read cannot read: %s", d.Stderr)
	}
	if d := r.Run(ctx, tools.Job{Command: "getent hosts example.com || (exec 3<>/dev/tcp/1.1.1.1/53) 2>&1"}); d.Exit == 0 && !bytes.Contains(d.Stdout, []byte("No such")) {
		t.Fatalf("read reached the network: %s", d.Stdout)
	}
}

// D1: a secret's value never appears in a reading, from stdout or stderr; MR-1: renaming
// the variable changes nothing.
func TestSecretScrubAllStreams(t *testing.T) {
	r := runner(t)
	sock := filepath.Join(t.TempDir(), "r.sock")
	ln, err := netListen(sock)
	if err != nil {
		t.Fatal(err)
	}
	go r.Serve(ctx, ln)
	for _, name := range []string{"TOKEN", "OTHER_NAME"} {
		sec := secrets{name: "s3cr3t-value-123456"}
		sh := tools.Shell{Write: true, Runner: tools.Client{Socket: sock}, Secrets: sec}
		in, _ := json.Marshal(map[string]any{"command": "echo out $" + name + "; echo err $" + name + " >&2; printf %s $" + name + " | base64", "secrets": []string{name}})
		res, err := sh.Run(ctx, in)
		if err != nil {
			t.Fatal(err)
		}
		if bytes.Contains(res.Reading, []byte("s3cr3t-value-123456")) {
			t.Fatalf("secret in the reading: %q", res.Reading)
		}
		if !bytes.Contains(res.Reading, []byte("out [secret]")) || !bytes.Contains(res.Reading, []byte("err [secret]")) {
			t.Fatalf("reading %q", res.Reading)
		}
	}
}

// D5: a networked run can't save a page into the workspace outside the outside folder;
// what it writes lands in the outside folder, and a later call sees that folder only
// when it asks, with its reading marked outside.
func TestOutsideBytesKeepMark(t *testing.T) {
	r := runner(t)
	d := r.Run(ctx, tools.Job{Command: "echo page > " + r.Workspace + "/saved.html", Write: true, Network: true})
	if d.Exit == 0 {
		t.Fatal("a networked run wrote into the workspace")
	}
	d = r.Run(ctx, tools.Job{Command: "echo page > " + r.Outside + "/saved.html", Write: true, Network: true})
	if d.Exit != 0 {
		t.Fatalf("a networked run could not write into the outside folder: %s", d.Stderr)
	}
	d = r.Run(ctx, tools.Job{Command: "cat " + r.Outside + "/saved.html"})
	if d.Exit == 0 {
		t.Fatal("a call that didn't ask saw the outside folder")
	}
	d = r.Run(ctx, tools.Job{Command: "cat " + r.Outside + "/saved.html", Outside: true})
	if d.Exit != 0 || strings.TrimSpace(string(d.Stdout)) != "page" {
		t.Fatalf("a call that asked could not read the outside folder: %s", d.Stderr)
	}
	sh := tools.Shell{Write: false}
	in, _ := json.Marshal(map[string]any{"command": "cat x", "outside": true})
	if sh.Class(in) != tools.Read {
		t.Fatal("read with the outside folder is not a read")
	}
	run := tools.Shell{Write: true}
	in2, _ := json.Marshal(map[string]any{"command": "curl x", "network": true})
	if run.Class(in2) != tools.Outside || !run.ReachesOut(in2) {
		t.Fatal("a networked run is not an outside change")
	}
}

// G016 (n.tools): the sandbox's network is the host's and DevTools listens on its
// loopback, so a networked run never runs while the box's browser is up.
func TestNetworkedRunWaitsForBrowser(t *testing.T) {
	up := true
	sh := tools.Shell{Write: true, Runner: tools.Client{Socket: "/nonexistent.sock"}, Secrets: none{}, BrowserUp: func() bool { return up }}
	res, err := sh.Run(context.Background(), json.RawMessage(`{"command":"curl http://127.0.0.1:9222/json","network":true}`))
	if err != nil || res.Exit == 0 || !strings.Contains(string(res.Reading), "browser is open") {
		t.Fatalf("a networked run with the browser up: %v %q", err, res.Reading)
	}
	up = false
	if _, err := sh.Run(context.Background(), json.RawMessage(`{"command":"true","network":true}`)); err == nil {
		t.Fatal("with the browser closed the run should reach the runner (here: no socket)")
	}
}

type none struct{}

func (none) Get(string) string { return "" }
func (none) Values() []string  { return nil }
