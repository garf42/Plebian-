package acceptance

import (
	"os/exec"
	"os/user"
	"strings"
	"testing"
)

// Scenario is one row of the intent predicate. Drive acts through outside surfaces;
// Assert checks positive existence first, then prohibitions. Assert never drives.
type Scenario struct {
	ID     string
	Setup  func(t testing.TB) *H
	Drive  func(h *H)
	Assert func(h *H, c *C)
}

var scenarios = map[string]Scenario{}

func register(s Scenario) { scenarios[s.ID] = s }

// Run runs one scenario: setup, drive, assert; any failed assertion fails the test.
func Run(t *testing.T, id string) {
	s, ok := scenarios[id]
	if !ok || s.Drive == nil {
		t.Fatalf("%s: not built", id)
	}
	h := s.Setup(t)
	s.Drive(h)
	c := &C{Scenario: id}
	if h.Addr != "" {
		h.VersionIsCommit(c)
	}
	s.Assert(h, c)
	if len(c.Errors) > 0 {
		// The server's own account of the run: what it logged while the scenario drove it.
		if u, err := user.Lookup("plebian"); err == nil {
			if out, err := exec.Command("sudo", "-n", "journalctl", "_UID="+u.Uid, "--since", h.Started.Format("2006-01-02 15:04:05"), "--no-pager", "-o", "cat").Output(); err == nil {
				t.Logf("%s server journal since setup:\n%s", id, strings.TrimSpace(string(out)))
			}
		}
		t.Fatalf("%s failed:\n  %s", id, strings.Join(c.Errors, "\n  "))
	}
	if len(c.Passed) == 0 {
		t.Fatalf("%s asserted nothing", id)
	}
}

// Undriven runs a scenario's Assert on its setup alone (an empty store): every assertion must fail.
func Undriven(t *testing.T, id string) {
	s, ok := scenarios[id]
	if !ok || s.Drive == nil {
		t.Skipf("%s: not built", id)
	}
	h := s.Setup(t)
	c := &C{Scenario: id + "/undriven"}
	s.Assert(h, c)
	if len(c.Passed) > 0 {
		t.Fatalf("%s: %v hold on an empty store", id, c.Passed)
	}
}

// Setup used by most scenarios: one fresh install, paired.
func fresh(t testing.TB) *H { return Install(t) }
