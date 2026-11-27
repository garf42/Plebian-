package acceptance

import (
	"encoding/json"
	"fmt"
	"os"
	"testing"
)

// TestManifest: the manifest lists exactly A01–A20, each with a runner.
func TestManifest(t *testing.T) {
	b, err := os.ReadFile("manifest.json")
	if err != nil {
		t.Fatal(err)
	}
	var m struct {
		Scenarios []struct{ ID, Runner string }
	}
	if err := json.Unmarshal(b, &m); err != nil {
		t.Fatal(err)
	}
	if len(m.Scenarios) != 20 {
		t.Fatalf("manifest lists %d scenarios", len(m.Scenarios))
	}
	for i, s := range m.Scenarios {
		if want := fmt.Sprintf("A%02d", i+1); s.ID != want || s.Runner == "" {
			t.Fatalf("entry %d is %q (runner %q), want %s", i, s.ID, s.Runner, want)
		}
	}
}

// TestVersionIsCommitUnderTest: the installed server reports the commit under test,
// so no scenario passes against a mock or a dev build.
func TestVersionIsCommitUnderTest(t *testing.T) {
	h := Install(t)
	c := &C{Scenario: "version"}
	h.VersionIsCommit(c)
	if len(c.Errors) > 0 {
		t.Fatal(c.Errors)
	}
}
