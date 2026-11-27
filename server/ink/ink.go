// Package ink is S, the trace a label's uses leave (TP-01): each use fades as
// (turns since)^−d with d = k × (the label's ink at that moment) + α (Pavlik & Anderson's
// spacing model). Ink is computed in time independent of how many uses a label has: the
// most recent use exact, the older ones folded into one aggregate (Petrov 2006), so a
// lookup costs the same at a thousand turns and at a million (ledger L0001).
package ink

import "math"

// State is one label's use history, folded.
type State struct {
	N      int     `json:"n"`      // uses so far
	First  float64 `json:"first"`  // clock of the first use
	Last   float64 `json:"last"`   // clock of the most recent use
	LastD  float64 `json:"last_d"` // d of the most recent use
	SumD   float64 `json:"sum_d"`  // Σ d over the older uses (all but the last)
	Second float64 `json:"second"` // clock of the second most recent use
}

// Ink is Σ_j (now − t_j)^−d_j: exact for the latest use; for the n−1 older uses,
// Petrov's approximation with their mean d, spread evenly from the first use to the
// second most recent (integral of t^−d over that span, times their density).
func (s State) Ink(now float64) float64 {
	if s.N == 0 {
		return 0
	}
	age := func(t float64) float64 {
		a := now - t
		if a < 1 {
			a = 1 // a use in the current turn counts as one turn old
		}
		return a
	}
	v := math.Pow(age(s.Last), -s.LastD)
	k := s.N - 1
	if k == 0 {
		return v
	}
	d := s.SumD / float64(k)
	if k == 1 || s.Second <= s.First {
		return v + float64(k)*math.Pow(age(s.Second), -d)
	}
	tn, t1 := age(s.First), age(s.Second) // tn: oldest age, t1: youngest of the older uses
	if math.Abs(1-d) < 1e-9 {
		return v + float64(k)*(math.Log(tn)-math.Log(t1))/(tn-t1)
	}
	return v + float64(k)*(math.Pow(tn, 1-d)-math.Pow(t1, 1-d))/((1-d)*(tn-t1))
}

// Use records a use at now and returns the d fixed for it.
func (s *State) Use(now, k, alpha float64) float64 {
	d := k*s.Ink(now) + alpha
	if s.N > 0 {
		s.SumD += s.LastD
		s.Second = s.Last
	} else {
		s.First = now
	}
	s.N++
	s.Last, s.LastD = now, d
	if s.N == 1 {
		s.First = now
	}
	return d
}

// Exact is the reference: every use summed (used by tests on small histories).
func Exact(now float64, times, ds []float64) float64 {
	var v float64
	for i, t := range times {
		a := now - t
		if a < 1 {
			a = 1
		}
		v += math.Pow(a, -ds[i])
	}
	return v
}
