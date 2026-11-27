package tools

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"time"
)

// Job is one command handed to the runner over its socket.
type Job struct {
	Command string            `json:"command"`
	Write   bool              `json:"write"`   // run: the workspace is writable
	Network bool              `json:"network"` // the call asked for the network
	Outside bool              `json:"outside"` // the call asked for the outside folder
	Env     map[string]string `json:"env"`     // secrets by name, values filled by the harness
	Timeout time.Duration     `json:"timeout"`
}

// Done is what the runner returns.
type Done struct {
	Stdout  []byte `json:"stdout"`
	Stderr  []byte `json:"stderr"`
	Exit    int    `json:"exit"`
	Sandbox string `json:"sandbox"` // bubblewrap | none (the host refused user namespaces; gap G007)
	Error   string `json:"error,omitempty"`
}

// Runner executes jobs as the tools user, inside bubblewrap. A call sees the workspace
// and, only when it asks, the network or the outside folder; a call that reaches the
// network or opens the outside folder can write only into the outside folder, so outside
// bytes keep their mark on disk (AG-03, D21).
type Runner struct {
	Workspace string // <root>/workspace
	Outside   string // <root>/workspace/outside
	Bwrap     string // path to bwrap
}

// Args builds the bubblewrap command line for a job.
func (r *Runner) Args(j Job) []string {
	a := []string{"--die-with-parent", "--new-session", "--unshare-all", "--ro-bind", "/", "/", "--dev", "/dev", "--proc", "/proc"}
	// Hide the host's writable places that sit outside the root; they are made read-only
	// after the binds below, which may need mount points inside them.
	var hidden []string
	for _, p := range []string{"/tmp", "/var/tmp", "/run", "/dev/shm", "/home", "/root"} {
		if _, err := os.Stat(p); err == nil {
			a = append(a, "--tmpfs", p)
			hidden = append(hidden, p)
		}
	}
	marked := j.Network || j.Outside
	switch {
	case j.Write && !marked:
		a = append(a, "--bind", r.Workspace, r.Workspace, "--tmpfs", r.Outside, "--remount-ro", r.Outside)
	case j.Write && marked:
		a = append(a, "--ro-bind", r.Workspace, r.Workspace, "--bind", r.Outside, r.Outside)
	case marked:
		a = append(a, "--ro-bind", r.Workspace, r.Workspace, "--ro-bind", r.Outside, r.Outside)
	default:
		a = append(a, "--ro-bind", r.Workspace, r.Workspace, "--tmpfs", r.Outside, "--remount-ro", r.Outside)
	}
	for _, p := range hidden {
		a = append(a, "--remount-ro", p)
	}
	if j.Network {
		a = append(a, "--share-net")
	}
	a = append(a, "--chdir", r.Workspace, "--clearenv", "--setenv", "PATH", "/usr/local/bin:/usr/bin:/bin", "--setenv", "HOME", r.Workspace)
	for k, v := range j.Env {
		a = append(a, "--setenv", k, v)
	}
	return append(a, "--", "/bin/sh", "-c", j.Command)
}

// Run executes one job.
func (r *Runner) Run(ctx context.Context, j Job) Done {
	if j.Timeout <= 0 {
		j.Timeout = 10 * time.Minute
	}
	ctx, cancel := context.WithTimeout(ctx, j.Timeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, r.Bwrap, r.Args(j)...)
	var out, errb bytes.Buffer
	cmd.Stdout, cmd.Stderr = &limited{b: &out}, &limited{b: &errb}
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	err := cmd.Run()
	d := Done{Stdout: out.Bytes(), Stderr: errb.Bytes(), Sandbox: "bubblewrap"}
	var ee *exec.ExitError
	switch {
	case err == nil:
	case errors.As(err, &ee):
		d.Exit = ee.ExitCode()
		if d.Exit < 0 {
			d.Exit = 128 + int(ee.Sys().(syscall.WaitStatus).Signal())
		}
	default:
		d.Exit, d.Error = 126, err.Error()
	}
	return d
}

// MaxOutput bounds a reading kept from one stream (the rest is cut and marked).
const MaxOutput = 1 << 20

type limited struct {
	b   *bytes.Buffer
	cut bool
}

func (l *limited) Write(p []byte) (int, error) {
	if room := MaxOutput - l.b.Len(); room > 0 {
		if len(p) > room {
			l.b.Write(p[:room])
			l.b.WriteString("\n[cut at 1 MiB]")
			l.cut = true
		} else {
			l.b.Write(p)
		}
	}
	return len(p), nil
}

// Serve accepts jobs on the runner's socket, one JSON request and one JSON answer per
// connection. Only the server (through the tools group) can reach the socket.
func (r *Runner) Serve(ctx context.Context, ln net.Listener) error {
	go func() { <-ctx.Done(); ln.Close() }()
	for {
		c, err := ln.Accept()
		if err != nil {
			return nil
		}
		go func() {
			defer c.Close()
			var j Job
			if err := json.NewDecoder(bufio.NewReader(c)).Decode(&j); err != nil {
				json.NewEncoder(c).Encode(Done{Exit: 126, Error: "bad job: " + err.Error()})
				return
			}
			json.NewEncoder(c).Encode(r.Run(ctx, j))
		}()
	}
}

// Client hands jobs to the runner.
type Client struct{ Socket string }

func (c Client) Do(ctx context.Context, j Job) (Done, error) {
	var d net.Dialer
	conn, err := d.DialContext(ctx, "unix", c.Socket)
	if err != nil {
		return Done{}, fmt.Errorf("tools runner: %w", err)
	}
	defer conn.Close()
	if dl, ok := ctx.Deadline(); ok {
		conn.SetDeadline(dl)
	}
	if err := json.NewEncoder(conn).Encode(j); err != nil {
		return Done{}, err
	}
	var done Done
	if err := json.NewDecoder(conn).Decode(&done); err != nil {
		return Done{}, fmt.Errorf("tools runner: %w", err)
	}
	return done, nil
}

// Scrub removes every secret value from bytes, whatever stream they came from.
func Scrub(b []byte, secrets []string) []byte {
	for _, s := range secrets {
		if len(s) < 4 {
			continue // too short to tell from ordinary text; such a secret is refused when set
		}
		b = bytes.ReplaceAll(b, []byte(s), []byte("[secret]"))
	}
	return b
}

// Within reports whether path p lies inside dir.
func Within(dir, p string) bool {
	rel, err := filepath.Rel(dir, p)
	return err == nil && rel != ".." && !strings.HasPrefix(rel, "../")
}
