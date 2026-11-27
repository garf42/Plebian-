package acceptance

import (
	"os"
	"sort"
	"testing"
)

// Scenarios not yet built fail with "not built" until their file registers them.
func TestA04(t *testing.T) { Run(t, "A04") }
func TestA05(t *testing.T) { Run(t, "A05") }
func TestA06(t *testing.T) { Run(t, "A06") }
func TestA07(t *testing.T) { Run(t, "A07") }
func TestA08(t *testing.T) { Run(t, "A08") }
func TestA09(t *testing.T) { Run(t, "A09") }
func TestA10(t *testing.T) { Run(t, "A10") }
func TestA11(t *testing.T) { Run(t, "A11") }
func TestA12(t *testing.T) { Run(t, "A12") }
func TestA13(t *testing.T) { Run(t, "A13") }
func TestA14(t *testing.T) { Run(t, "A14") }
func TestA15(t *testing.T) { Run(t, "A15") }
func TestA16(t *testing.T) { Run(t, "A16") }
func TestA17(t *testing.T) { Run(t, "A17") }
func TestA18(t *testing.T) { Run(t, "A18") }

// A19 needs the live key; without it it reports blocked.
func TestA19(t *testing.T) {
	if os.Getenv("PLEBIAN_LIVE_KEY") == "" {
		t.Skip("blocked: key")
	}
	Run(t, "A19")
}

func TestA20(t *testing.T) { Run(t, "A20") }

// TestNonVacuity: every built scenario's assertions fail on an empty store.
func TestNonVacuity(t *testing.T) {
	var ids []string
	for id, s := range scenarios {
		if s.Drive != nil {
			ids = append(ids, id)
		}
	}
	sort.Strings(ids)
	for _, id := range ids {
		t.Run(id, func(t *testing.T) { Undriven(t, id) })
	}
}
