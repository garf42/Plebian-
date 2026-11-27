// Package acceptance drives the release artifact of the commit under test through its
// outside surfaces only: the installer, the server's API, the app (on the emulator, in
// its own job) and fixture servers. It imports nothing from the server.
package acceptance

import (
	"bufio"
	"bytes"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/sha256"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"time"
)

// C collects assertion results. Assertions record into it instead of failing a test
// directly, so the same Assert can run on an undriven install (non-vacuity).
type C struct {
	Scenario string
	Errors   []string
	Passed   []string
}

func (c *C) Check(id string, ok bool, format string, args ...any) bool {
	line := map[string]any{"scenario": c.Scenario, "assertion": id, "pass": ok}
	if !ok {
		msg := fmt.Sprintf(format, args...)
		c.Errors = append(c.Errors, id+": "+msg)
		line["why"] = msg
	} else {
		c.Passed = append(c.Passed, id)
	}
	b, _ := json.Marshal(line)
	fmt.Println("RESULT " + string(b))
	return ok
}

// H is one installed Plebian plus its fixtures.
type H struct {
	T        testing.TB
	Root     string
	Addr     string
	Token    string
	Key      *ecdsa.PrivateKey
	Fixtures map[string]*Fixture
	Clock    *Fixture
	PairCode string
	LastBody []byte
	http     *http.Client
}

type Fixture struct {
	Kind string
	Addr string
	Info map[string]any
	cmd  *exec.Cmd
}

func (f *Fixture) URL(path string) string { return "http://" + f.Addr + path }

func sudo(args ...string) *exec.Cmd {
	if os.Geteuid() == 0 {
		return exec.Command(args[0], args[1:]...)
	}
	return exec.Command("sudo", append([]string{"-n"}, args...)...)
}

func runOut(t testing.TB, cmd *exec.Cmd) string {
	t.Helper()
	var out bytes.Buffer
	cmd.Stdout, cmd.Stderr = &out, &out
	if err := cmd.Run(); err != nil {
		t.Fatalf("%v: %v\n%s", cmd.Args, err, out.String())
	}
	return out.String()
}

// Artifact is the release tarball of the commit under test, unpacked.
func Artifact(t testing.TB) string {
	p := os.Getenv("PLEBIAN_ARTIFACT")
	if p == "" {
		t.Fatal("PLEBIAN_ARTIFACT (the release tarball of the commit under test) is not set")
	}
	dir := t.TempDir()
	runOut(t, exec.Command("tar", "-xzf", p, "-C", dir))
	bin := filepath.Join(dir, "plebian", "bin", "plebian")
	if _, err := os.Stat(bin); err != nil {
		t.Fatalf("artifact has no plebian/bin/plebian: %v", err)
	}
	os.Chmod(dir, 0o755)
	return bin
}

// StartFixture starts one fixture server through the fixtures CLI.
func StartFixture(t testing.TB, kind string, args ...string) *Fixture {
	bin := os.Getenv("FIXTURE_BIN")
	if bin == "" {
		t.Fatal("FIXTURE_BIN (the built tests/fixtures CLI) is not set")
	}
	cmd := exec.Command(bin, append([]string{kind, "-addr", "127.0.0.1:0"}, args...)...)
	stdout, _ := cmd.StdoutPipe()
	cmd.Stderr = os.Stderr
	if err := cmd.Start(); err != nil {
		t.Fatalf("start fixture %s: %v", kind, err)
	}
	line, err := bufio.NewReader(stdout).ReadString('\n')
	if err != nil {
		cmd.Process.Kill()
		t.Fatalf("fixture %s printed no address: %v", kind, err)
	}
	f := &Fixture{Kind: kind, cmd: cmd, Info: map[string]any{}}
	json.Unmarshal([]byte(line), &f.Info)
	f.Addr, _ = f.Info["addr"].(string)
	t.Cleanup(func() { cmd.Process.Kill(); cmd.Wait() })
	return f
}

// Install installs the artifact into a fresh root with the time fixture as its clock.
func Install(t testing.TB, extra ...string) *H {
	bin := Artifact(t)
	h := &H{T: t, Fixtures: map[string]*Fixture{}, http: &http.Client{Timeout: 60 * time.Second}}
	h.Clock = StartFixture(t, "time")
	h.Root = filepath.Join("/opt", fmt.Sprintf("plebian-acc-%d", time.Now().UnixNano()))
	args := append([]string{bin, "install", "--root", h.Root, "--no-tailnet", "--tz", "America/Chicago", "--clock-source", h.Clock.URL("/now")}, extra...)
	out := runOut(t, sudo(args...))
	t.Cleanup(func() {
		if os.Getenv("KEEP_ROOT") == "" {
			cmd := sudo(bin, "uninstall", "--root", h.Root)
			if b, err := cmd.CombinedOutput(); err != nil {
				t.Logf("uninstall: %v %s", err, b)
			}
		}
	})
	code := regexp.MustCompile(`Pairing code: (\S+)`).FindStringSubmatch(out)
	if code == nil {
		t.Fatalf("install printed no pairing code:\n%s", out)
	}
	b, err := os.ReadFile(filepath.Join(h.Root, "run", "api.json"))
	if err != nil {
		t.Fatalf("no run/api.json: %v", err)
	}
	var a struct{ Addr string }
	json.Unmarshal(b, &a)
	h.Addr = a.Addr
	h.pair(code[1])
	return h
}

func (h *H) pair(code string) {
	h.Key, _ = ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	der, _ := x509.MarshalPKIXPublicKey(&h.Key.PublicKey)
	var r struct{ Token string }
	st := h.Do("POST", "/v1/pair", map[string]string{"code": code, "public_key": base64.StdEncoding.EncodeToString(der), "name": "acceptance"}, &r, false)
	if st != 200 || r.Token == "" {
		h.T.Fatalf("pairing failed: %d", st)
	}
	h.Token = r.Token
	h.PairCode = code
}

// Do sends a JSON request; auth adds the bearer token. It returns the status.
func (h *H) Do(method, path string, body any, out any, auth bool) int {
	var rd io.Reader
	if body != nil {
		b, _ := json.Marshal(body)
		rd = bytes.NewReader(b)
	}
	req, _ := http.NewRequest(method, "http://"+h.Addr+path, rd)
	if auth {
		req.Header.Set("Authorization", "Bearer "+h.Token)
	}
	resp, err := h.http.Do(req)
	if err != nil {
		h.T.Fatalf("%s %s: %v", method, path, err)
	}
	defer resp.Body.Close()
	b, _ := io.ReadAll(resp.Body)
	h.LastBody = b
	if out != nil && len(b) > 0 {
		json.Unmarshal(b, out)
	}
	return resp.StatusCode
}

// Signed sends a signed admin action.
func (h *H) Signed(path string, body map[string]any) int {
	body["nonce"] = fmt.Sprint(time.Now().UnixNano())
	body["at"] = time.Now().UTC().Format(time.RFC3339)
	b, _ := json.Marshal(body)
	sum := sha256.Sum256(b)
	sig, _ := ecdsa.SignASN1(rand.Reader, h.Key, sum[:])
	req, _ := http.NewRequest("POST", "http://"+h.Addr+path, bytes.NewReader(b))
	req.Header.Set("Authorization", "Bearer "+h.Token)
	req.Header.Set("X-Plebian-Signature", base64.StdEncoding.EncodeToString(sig))
	resp, err := h.http.Do(req)
	if err != nil {
		h.T.Fatalf("%s: %v", path, err)
	}
	resp.Body.Close()
	return resp.StatusCode
}

// Advance moves the server's clock forward.
func (h *H) Advance(d time.Duration) {
	post(h.T, h.Clock.URL("/control/advance"), map[string]any{"ms": d.Milliseconds()})
}

func post(t testing.TB, url string, body any) []byte {
	b, _ := json.Marshal(body)
	resp, err := http.Post(url, "application/json", bytes.NewReader(b))
	if err != nil {
		t.Fatalf("POST %s: %v", url, err)
	}
	defer resp.Body.Close()
	out, _ := io.ReadAll(resp.Body)
	if resp.StatusCode >= 300 {
		t.Fatalf("POST %s: %d %s", url, resp.StatusCode, out)
	}
	return out
}

func get(t testing.TB, url string, out any) {
	resp, err := http.Get(url)
	if err != nil {
		t.Fatalf("GET %s: %v", url, err)
	}
	defer resp.Body.Close()
	json.NewDecoder(resp.Body).Decode(out)
}

// Model starts the model fixture with a script and points the server's Model card at it.
func (h *H) Model(format string, steps []map[string]any) *Fixture {
	f := StartFixture(h.T, "model")
	post(h.T, f.URL("/control/script"), map[string]any{"steps": steps})
	h.UseModel(f, format, "main")
	h.Fixtures["model:"+format] = f
	return f
}

func (h *H) UseModel(f *Fixture, format, role string) {
	var r struct {
		ProviderID string `json:"provider_id"`
		Format     string
		Models     []struct{ ID string }
	}
	key, _ := f.Info["key"].(string)
	if st := h.Do("POST", "/v1/setup/model", map[string]string{"base_url": f.URL("/" + format), "api_key": key}, &r, true); st != 200 {
		h.T.Fatalf("model setup: %d %s", st, h.LastBody)
	}
	if r.Format != format || len(r.Models) == 0 {
		h.T.Fatalf("model setup detected %q with %d models", r.Format, len(r.Models))
	}
	if st := h.Do("POST", "/v1/setup/model/select", map[string]string{"provider_id": r.ProviderID, "model": r.Models[0].ID, "role": role}, nil, true); st != 200 {
		h.T.Fatalf("model select: %d %s", st, h.LastBody)
	}
}

// Req is one request a fixture received, its body exact.
type Req struct {
	Method  string              `json:"method"`
	URL     string              `json:"url"`
	Headers map[string][]string `json:"headers"`
	Body    []byte              `json:"body"`
}

// Requests reads what a fixture received.
func Requests(t testing.TB, f *Fixture) []Req {
	var r []Req
	get(t, f.URL("/control/requests"), &r)
	return r
}

// Say sends an owner message and lets the owner's pause pass, so the loop takes it.
func (h *H) Say(text string) {
	id := fmt.Sprint(time.Now().UnixNano())
	if st := h.Do("POST", "/v1/messages", map[string]string{"client_msg_id": id, "text": text, "sent_at": time.Now().UTC().Format(time.RFC3339Nano)}, nil, true); st != 200 {
		h.T.Fatalf("message: %d", st)
	}
	h.Advance(2 * time.Minute) // past the cold-start usual gap between messages (one minute)
}

// Until polls fn until it holds or the time runs out.
func Until(d time.Duration, fn func() bool) bool {
	deadline := time.Now().Add(d)
	for time.Now().Before(deadline) {
		if fn() {
			return true
		}
		time.Sleep(200 * time.Millisecond)
	}
	return fn()
}

// Today reads the current page.
func (h *H) Today() map[string]any {
	var p map[string]any
	h.Do("GET", "/v1/today", nil, &p, true)
	return p
}

// AgentMessages returns the agent's message texts on a page.
func AgentMessages(page map[string]any) []string {
	var out []string
	th, _ := page["thread"].([]any)
	for _, e := range th {
		m, _ := e.(map[string]any)
		if m["type"] == "message" && m["from"] == "agent" {
			out = append(out, fmt.Sprint(m["text"]))
		}
	}
	return out
}

// Replay runs the server's own rebuild check as the server user and returns its report.
func (h *H) Replay() map[string]any {
	out := runOut(h.T, sudo("runuser", "-u", "plebian", "--", filepath.Join(h.Root, "bin", "plebian"), "replay", "--verify"))
	var r map[string]any
	for _, ln := range strings.Split(out, "\n") {
		if strings.HasPrefix(strings.TrimSpace(ln), "{") {
			json.Unmarshal([]byte(ln), &r)
		}
	}
	return r
}

// VersionIsCommit checks the server reports the commit under test.
func (h *H) VersionIsCommit(c *C) {
	var v struct{ Commit string }
	h.Do("GET", "/v1/version", nil, &v, false)
	want := os.Getenv("PLEBIAN_COMMIT")
	c.Check("version_is_commit", want != "" && v.Commit == want, "server reports %q, commit under test is %q", v.Commit, want)
}
