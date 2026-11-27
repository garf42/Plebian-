package acceptance

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// candidate writes a release tarball and its .sha256 where the server can read them;
// broken keeps every command but makes `serve` fail, so the deploy's start fails.
func candidate(t testing.TB, broken bool) string {
	src, err := os.ReadFile(os.Getenv("PLEBIAN_ARTIFACT"))
	if err != nil {
		t.Fatal(err)
	}
	if broken {
		zr, _ := gzip.NewReader(bytes.NewReader(src))
		tr := tar.NewReader(zr)
		var out bytes.Buffer
		zw := gzip.NewWriter(&out)
		tw := tar.NewWriter(zw)
		for {
			h, err := tr.Next()
			if err == io.EOF {
				break
			}
			if err != nil {
				t.Fatal(err)
			}
			b, _ := io.ReadAll(tr)
			if h.Name == "plebian/bin/plebian" {
				h.Name = "plebian/bin/plebian.real"
				tw.WriteHeader(h)
				tw.Write(b)
				script := []byte("#!/bin/sh\ncase \"$1\" in serve) exit 1;; esac\nexec \"$(dirname \"$0\")/plebian.real\" \"$@\"\n")
				tw.WriteHeader(&tar.Header{Name: "plebian/bin/plebian", Mode: 0o755, Size: int64(len(script)), Typeflag: tar.TypeReg, ModTime: h.ModTime})
				tw.Write(script)
				continue
			}
			tw.WriteHeader(h)
			tw.Write(b)
		}
		tw.Close()
		zw.Close()
		src = out.Bytes()
	}
	p := fmt.Sprintf("/var/tmp/plebian-candidate-%d.tar.gz", time.Now().UnixNano())
	sum := sha256.Sum256(src)
	os.WriteFile(p, src, 0o644)
	os.WriteFile(p+".sha256", []byte(hex.EncodeToString(sum[:])+"  "+filepath.Base(p)+"\n"), 0o644)
	t.Cleanup(func() { os.Remove(p); os.Remove(p + ".sha256") })
	return p
}

// Reconnect follows the server to the address it wrote after a restart.
func (h *H) Reconnect(d time.Duration) bool {
	return Until(d, func() bool {
		b, err := os.ReadFile(filepath.Join(h.Root, "run", "api.json"))
		if err != nil {
			return false
		}
		var a struct{ Addr string }
		if json.Unmarshal(b, &a) != nil || a.Addr == "" {
			return false
		}
		h.Addr = a.Addr
		st, err := h.Try("GET", "/v1/version", nil, nil, false)
		return err == nil && st == 200
	})
}

func (h *H) updatesStatus() string {
	var cards struct{ Cards []struct{ ID, Status string } }
	if st, err := h.Try("GET", "/v1/setup", nil, &cards, true); err != nil || st != 200 {
		return ""
	}
	for _, c := range cards.Cards {
		if c.ID == "updates" {
			return c.Status
		}
	}
	return ""
}

func init() {
	type a20 struct {
		before, afterDeploy, afterRollback []byte
		deployed, rolledBack               string
		commitAfterDeploy, commitAfterBack string
		signed, signedBroken               int
	}
	var st *a20
	commit := func(h *H) string {
		var v struct{ Commit string }
		h.Try("GET", "/v1/version", nil, &v, false)
		return v.Commit
	}
	waitStatus := func(h *H, prefix string) string {
		var s string
		Until(4*time.Minute, func() bool {
			h.Reconnect(5 * time.Second)
			s = h.updatesStatus()
			return strings.HasPrefix(s, prefix)
		})
		return s
	}
	register(Scenario{
		ID: "A20",
		Setup: func(t testing.TB) *H {
			st = &a20{}
			h := fresh(t)
			h.Model("anthropic", []map[string]any{{"match": "gravel", "reply": map[string]any{"text": "Wednesday, by the culvert."}}})
			return h
		},
		Drive: func(h *H) {
			h.Say("when is the gravel coming?")
			Until(60*time.Second, func() bool { return len(AgentMessages(h.Today())) > 0 })
			date := PageDate(h.Now())
			h.Do("GET", "/v1/days/"+date, nil, nil, true)
			st.before = append([]byte(nil), h.LastBody...)
			st.signed = h.Signed("/v1/admin/deploy", map[string]any{"candidate": candidate(h.T, false)})
			st.deployed = waitStatus(h, "deployed")
			st.commitAfterDeploy = commit(h)
			h.Do("GET", "/v1/days/"+date, nil, nil, true)
			st.afterDeploy = append([]byte(nil), h.LastBody...)
			st.signedBroken = h.Signed("/v1/admin/deploy", map[string]any{"candidate": candidate(h.T, true)})
			st.rolledBack = waitStatus(h, "rolled back")
			h.Reconnect(60 * time.Second)
			st.commitAfterBack = commit(h)
			h.Do("GET", "/v1/days/"+date, nil, nil, true)
			st.afterRollback = append([]byte(nil), h.LastBody...)
		},
		Assert: func(h *H, c *C) {
			want := os.Getenv("PLEBIAN_COMMIT")
			c.Check("page_has_reply", bytes.Contains(st.before, []byte("Wednesday, by the culvert.")), "the page before: %s", st.before)
			c.Check("deploy_signed", st.signed == 200 && st.signedBroken == 200, "signed deploys answered %d, %d", st.signed, st.signedBroken)
			c.Check("replayed_before_deploy", strings.Contains(st.deployed, "replayed") && !strings.Contains(st.deployed, "replayed 0 "), "updates card %q", st.deployed)
			c.Check("deployed", strings.HasPrefix(st.deployed, "deployed") && st.commitAfterDeploy == want && bytes.Equal(st.before, st.afterDeploy), "status %q, commit %s, page unchanged %v", st.deployed, st.commitAfterDeploy, bytes.Equal(st.before, st.afterDeploy))
			c.Check("rolled_back_on_failed_start", strings.HasPrefix(st.rolledBack, "rolled back") && st.commitAfterBack == want && bytes.Equal(st.before, st.afterRollback), "status %q, commit %s, page unchanged %v", st.rolledBack, st.commitAfterBack, bytes.Equal(st.before, st.afterRollback))
		},
	})
}

func TestA20(t *testing.T) { Run(t, "A20") }
