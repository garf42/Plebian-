// Package ink is S, the trace a label's uses leave (TP-01): each use fades as
// (turns since)^−d with d = k × (the label's ink at that moment) + α (Pavlik & Anderson's
// spacing model). Ink is computed without a sum over every use (ledger L0001): uses are
// folded into time buckets, and two neighbouring buckets merge only while the merged
// bucket's width stays within the deadband of its age, so the fold's error stays inside
// the tolerance ranking already allows. A bucket is integrated as uses spread evenly
// across it (Petrov 2006). The bucket count grows with the logarithm of the history.
package ink

import "math"

// Bucket is a run of uses folded together.
type Bucket struct {
	T0, T1     float64 // clock of its first and last use
	N          int
	SumD       float64
	DMin, DMax float64
}

// State is one label's use history, folded.
type State struct {
	N int      `json:"n"`
	B []Bucket `json:"b"`
}

func age(now, t float64) float64 {
	a := now - t
	if a < 1 {
		a = 1 // a use in the current turn counts as one turn old
	}
	return a
}

// term is Σ over a bucket's uses of age^−d, its uses taken as evenly spaced from its
// first to its last with their mean d: the integral over the span at that spacing plus
// half of each end (Euler–Maclaurin), exact for one use or two.
func term(b Bucket, now float64) float64 {
	d := b.SumD / float64(b.N)
	hi, lo := age(now, b.T0), age(now, b.T1)
	if b.N == 1 || b.T1 <= b.T0 {
		return float64(b.N) * math.Pow(lo, -d)
	}
	ends := (math.Pow(lo, -d) + math.Pow(hi, -d)) / 2
	if b.N == 2 {
		return 2 * ends
	}
	h := (hi - lo) / float64(b.N-1)
	var integral float64
	if math.Abs(1-d) < 1e-9 {
		integral = math.Log(hi) - math.Log(lo)
	} else {
		integral = (math.Pow(hi, 1-d) - math.Pow(lo, 1-d)) / (1 - d)
	}
	return integral/h + ends
}

// Ink is Σ_j (now − t_j)^−d_j over the folded history.
func (s State) Ink(now float64) float64 {
	var v float64
	for _, b := range s.B {
		v += term(b, now)
	}
	return v
}

// Deadband is the fold's tolerance: the Parameter schedule's deadband (5%, gap G001).
var Deadband = 0.05

// Use records a use at now and returns the d fixed for it.
func (s *State) Use(now, k, alpha float64) float64 {
	d := k*s.Ink(now) + alpha
	s.N++
	s.B = append(s.B, Bucket{T0: now, T1: now, N: 1, SumD: d, DMin: d, DMax: d})
	// Merge neighbours, oldest first, while the merged bucket's worst-case error stays
	// within the deadband: spreading its uses evenly misplaces at most its width, and
	// a^−d moves by about d × (width / age), so width ≤ (deadband / d) × age; one mean d
	// stands for its uses' own, and a^−d moves by (spread of d) × ln a, so that product
	// stays within the deadband too.
	out := s.B[:0]
	for _, b := range s.B {
		if n := len(out); n > 0 {
			p := out[n-1]
			md := (p.SumD + b.SumD) / float64(p.N+b.N)
			if md <= 0 {
				md = 1
			}
			lo, hi := math.Min(p.DMin, b.DMin), math.Max(p.DMax, b.DMax)
			young := age(now, b.T1)
			if b.T1-p.T0 <= Deadband/md*young && (hi-lo)*math.Log(age(now, p.T0)) <= Deadband {
				out[n-1] = Bucket{T0: p.T0, T1: b.T1, N: p.N + b.N, SumD: p.SumD + b.SumD, DMin: lo, DMax: hi}
				continue
			}
		}
		out = append(out, b)
	}
	s.B = out
	return d
}

// Exact is the reference: every use summed (used by tests on small histories).
func Exact(now float64, times, ds []float64) float64 {
	var v float64
	for i, t := range times {
		v += math.Pow(age(now, t), -ds[i])
	}
	return v
}
