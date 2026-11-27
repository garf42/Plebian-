package ops

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"plebian/server/store"
)

// DeployState is what the deploy helper leaves for the server to read at its next start.
type DeployState struct {
	State   string `json:"state"` // deployed | rolled back | blocked
	Version string `json:"version"`
	Detail  string `json:"detail"`
	At      string `json:"at"`
}

func (r Root) deployFile() string { return r.P("run", "deploy.json") }

// ReadDeploy is the helper's last word: the newest deploy's outcome.
func ReadDeploy(r Root) (DeployState, bool) {
	b, err := os.ReadFile(r.deployFile())
	if err != nil {
		return DeployState{}, false
	}
	var d DeployState
	return d, json.Unmarshal(b, &d) == nil
}

// LastDeployed is when a deploy last succeeded (zero before the first).
func LastDeployed(r Root) time.Time {
	b, _ := os.ReadFile(r.P("run", "deployed-at"))
	t, _ := time.Parse(time.RFC3339, strings.TrimSpace(string(b)))
	return t
}

// WriteDeploy records a deploy's outcome (the server writes "blocked" itself).
func WriteDeploy(r Root, d DeployState) { writeDeploy(r, d) }

func writeDeploy(r Root, d DeployState) {
	d.At = time.Now().UTC().Format(time.RFC3339)
	b, _ := json.Marshal(d)
	os.WriteFile(r.deployFile(), b, 0o600)
}

// fetchCandidate reads a release tarball from a path or an https URL and checks it
// against the sha256 published beside it (<candidate>.sha256).
func fetchCandidate(ctx context.Context, src string) ([]byte, error) {
	read := func(p string) ([]byte, error) {
		if strings.HasPrefix(p, "https://") {
			req, err := http.NewRequestWithContext(ctx, "GET", p, nil)
			if err != nil {
				return nil, err
			}
			resp, err := http.DefaultClient.Do(req)
			if err != nil {
				return nil, err
			}
			defer resp.Body.Close()
			if resp.StatusCode != 200 {
				return nil, fmt.Errorf("%s answered %d", p, resp.StatusCode)
			}
			return store.ReadAllLimit(resp.Body, 1<<30)
		}
		if !filepath.IsAbs(p) {
			return nil, errors.New("a candidate is an absolute path or an https URL")
		}
		return os.ReadFile(p)
	}
	tgz, err := read(src)
	if err != nil {
		return nil, err
	}
	sum, err := read(src + ".sha256")
	if err != nil {
		return nil, fmt.Errorf("the candidate's sha256: %w", err)
	}
	want := strings.Fields(string(sum))
	h := sha256.Sum256(tgz)
	if len(want) == 0 || want[0] != hex.EncodeToString(h[:]) {
		return nil, errors.New("the candidate does not match its sha256")
	}
	return tgz, nil
}

// unpack extracts a release tarball (plebian/bin, plebian/lib) under dir.
func unpack(tgz []byte, dir string) error {
	zr, err := gzip.NewReader(bytes.NewReader(tgz))
	if err != nil {
		return err
	}
	tr := tar.NewReader(zr)
	for {
		h, err := tr.Next()
		if err == io.EOF {
			return nil
		}
		if err != nil {
			return err
		}
		name := filepath.Clean(h.Name)
		if filepath.IsAbs(name) || strings.HasPrefix(name, "..") {
			return fmt.Errorf("refused path %q in the candidate", h.Name)
		}
		p := filepath.Join(dir, name)
		switch h.Typeflag {
		case tar.TypeDir:
			if err := os.MkdirAll(p, 0o755); err != nil {
				return err
			}
		case tar.TypeReg:
			if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
				return err
			}
			f, err := os.OpenFile(p, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, os.FileMode(h.Mode)&0o755)
			if err != nil {
				return err
			}
			if _, err := io.Copy(f, io.LimitReader(tr, h.Size)); err != nil {
				f.Close()
				return err
			}
			if err := f.Close(); err != nil {
				return err
			}
		}
	}
}

func pages(bin, lib, storeFile, since string) (string, error) {
	cmd := exec.Command(bin, "replay", "--pages", since, "--store", storeFile)
	cmd.Env = append(os.Environ(), "LD_LIBRARY_PATH="+lib)
	var out, errb bytes.Buffer
	cmd.Stdout, cmd.Stderr = &out, &errb
	if err := cmd.Run(); err != nil {
		return "", fmt.Errorf("%s replay: %v: %s", bin, err, strings.TrimSpace(errb.String()))
	}
	return out.String(), nil
}

// Prepare fetches and unpacks a candidate, then replays every page since the last deploy
// with the running program and with the candidate on one snapshot of the store. It
// returns the candidate's directory, its commit and how many pages matched; any page
// that differs blocks the deploy.
func Prepare(ctx context.Context, r Root, s *store.Store, candidate, since string) (string, string, int, error) {
	tgz, err := fetchCandidate(ctx, candidate)
	if err != nil {
		return "", "", 0, err
	}
	dir := r.P("run", "candidate")
	os.RemoveAll(dir)
	if err := unpack(tgz, dir); err != nil {
		return "", "", 0, err
	}
	cbin, clib := filepath.Join(dir, "plebian", "bin", "plebian"), filepath.Join(dir, "plebian", "lib")
	vc := exec.Command(cbin, "version")
	vc.Env = append(os.Environ(), "LD_LIBRARY_PATH="+clib)
	vo, err := vc.Output()
	if err != nil {
		return "", "", 0, fmt.Errorf("the candidate's version: %w", err)
	}
	commit := ""
	if f := strings.Fields(string(vo)); len(f) > 1 {
		commit = f[1]
	}
	snap := filepath.Join(dir, "store.db")
	if _, err := s.DB.ExecContext(ctx, `VACUUM INTO ?`, snap); err != nil {
		return "", "", 0, err
	}
	defer os.Remove(snap)
	now, err := pages(r.Bin(), r.P("lib"), snap, since)
	if err != nil {
		return "", "", 0, err
	}
	next, err := pages(cbin, clib, snap, since)
	if err != nil {
		return "", "", 0, err
	}
	a, b := strings.Split(strings.TrimSpace(now), "\n"), strings.Split(strings.TrimSpace(next), "\n")
	var differ []string
	for i := range a {
		if i >= len(b) || a[i] != b[i] {
			differ = append(differ, strings.Fields(a[i])[0])
		}
	}
	if len(b) != len(a) || len(differ) > 0 {
		return dir, commit, 0, fmt.Errorf("pages differ under the candidate: %s", strings.Join(differ, " "))
	}
	return dir, commit, len(a), nil
}

// Deploy hands the swap to a transient unit of the server's own user manager, so the
// swap outlives the server it restarts.
func Deploy(r Root, dir, expect string, replayed int) error {
	bin := filepath.Join(dir, "plebian", "bin", "plebian")
	cmd := exec.Command("systemd-run", "--user", "--collect", "--unit", fmt.Sprintf("plebian-deploy-%d", time.Now().Unix()),
		"--setenv=LD_LIBRARY_PATH="+filepath.Join(dir, "plebian", "lib"),
		bin, "deploy-helper", "--root", string(r), "--from", filepath.Join(dir, "plebian"), "--expect", expect, "--replayed", fmt.Sprint(replayed))
	var out bytes.Buffer
	cmd.Stdout, cmd.Stderr = &out, &out
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("systemd-run: %v: %s", err, strings.TrimSpace(out.String()))
	}
	return nil
}

func swapIn(from, root string) error {
	for _, sub := range []string{"bin", "lib"} {
		ents, err := os.ReadDir(filepath.Join(from, sub))
		if err != nil {
			if os.IsNotExist(err) {
				continue
			}
			return err
		}
		for _, e := range ents {
			if e.IsDir() {
				continue
			}
			src, dst := filepath.Join(from, sub, e.Name()), filepath.Join(root, sub, e.Name())
			fi, _ := os.Stat(src)
			tmp := dst + ".new"
			if err := copyFile(src, tmp, fi.Mode()&0o755); err != nil {
				return err
			}
			if err := os.Rename(tmp, dst); err != nil {
				return err
			}
		}
	}
	return nil
}

func started(r Root, expect string, d time.Duration) bool {
	deadline := time.Now().Add(d)
	for time.Now().Before(deadline) {
		if b, err := os.ReadFile(r.APIFile()); err == nil {
			var a struct{ Addr string }
			if json.Unmarshal(b, &a) == nil && a.Addr != "" {
				if resp, err := http.Get("http://" + a.Addr + "/v1/version"); err == nil {
					var v struct{ Commit string }
					json.NewDecoder(resp.Body).Decode(&v)
					resp.Body.Close()
					if expect == "" || v.Commit == expect {
						return true
					}
				}
			}
		}
		time.Sleep(500 * time.Millisecond)
	}
	return false
}

// DeployHelper keeps the running program, puts the candidate's in its place, restarts
// the server and waits for it; a failed start puts the kept program back.
func DeployHelper(r Root, from, expect string, replayed int) error {
	prev := r.P("run", "previous")
	os.RemoveAll(prev)
	for _, sub := range []string{"bin", "lib"} {
		os.MkdirAll(filepath.Join(prev, sub), 0o755)
		ents, _ := os.ReadDir(r.P(sub))
		for _, e := range ents {
			if !e.IsDir() && !strings.HasSuffix(e.Name(), ".new") {
				fi, _ := e.Info()
				if err := copyFile(r.P(sub, e.Name()), filepath.Join(prev, sub, e.Name()), fi.Mode()&0o755); err != nil {
					return err
				}
			}
		}
	}
	restart := func() error {
		os.Remove(r.APIFile())
		// A program that keeps failing hits systemd's start limit; clear it first.
		exec.Command("systemctl", "--user", "reset-failed", "plebian.service").Run()
		return exec.Command("systemctl", "--user", "restart", "plebian.service").Run()
	}
	version := strings.TrimSpace(expect)
	if err := swapIn(from, string(r)); err == nil {
		restart()
		if started(r, expect, 60*time.Second) {
			writeDeploy(r, DeployState{State: "deployed", Version: version, Detail: fmt.Sprintf("replayed %d pages identical", replayed)})
			os.WriteFile(r.P("run", "deployed-at"), []byte(time.Now().UTC().Format(time.RFC3339)), 0o600)
			os.RemoveAll(prev)
			return nil
		}
	}
	if err := swapIn(prev, string(r)); err != nil {
		return err
	}
	writeDeploy(r, DeployState{State: "rolled back", Version: version, Detail: "the candidate did not start; the previous program is back"})
	return restart()
}
