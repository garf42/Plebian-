package core

import "time"

// Params are the Parameter schedule's values (docs/tracer-paper-memory.md). Rows marked
// Default there are conventions awaiting a fit from the store's own history (gap G001);
// each is a named field so the sleep pass can refit it and a replay can show its effect.
type Params struct {
	ColdStart int // samples before the store's own fits replace the priors

	// Priors until ColdStart samples exist.
	MedianLineWords int
	UsualGap        time.Duration // owner's usual gap between messages; stands in for time to answer
	LoopHeld        time.Duration // a loop held a day

	DecayK, DecayAlpha float64 // Pavlik & Anderson

	StrikeLine float64 // τ below it renders struck
	Deadband   float64

	BudgetFloor, BudgetStart, BudgetCeiling int
	BudgetStep                              int     // +8K on a re-tell the next step would have shown
	BudgetShrink                            float64 // ×0.75 when the CUSUM crosses
	BudgetGrowEvery                         int     // at most once per 20 outcomes

	SpotlightShare float64 // about 10% of B
	StrataShare    float64 // half of what B leaves after tools, system and spotlight cap
	DeepShare      float64 // 40% deep / 60% recent
	PromotionEvery int     // LSM size ratio 4

	LampBits, LampWordBits float64 // underlay: 14 bits with one word past 10
	StandInWeight          float64
	FollowedShare          float64 // what followed and closing a failure: ≥ ⅓ shared
	Season                 time.Duration

	ClockWindow     time.Duration // same weekday within 90 minutes
	ClockDateWindow time.Duration // same date within a day and three hours

	WakeLines      int           // heard wake: three lines
	WakeDecoyEvery time.Duration // decoys light about once in four days

	SleepIdle  time.Duration // 10 minutes idle
	SleepTurns int           // or every 500 turns

	CacheLong, CacheShort string
}

// Defaults are the schedule as written.
var Defaults = Params{
	ColdStart:       20,
	MedianLineWords: 5,
	UsualGap:        time.Minute,
	LoopHeld:        24 * time.Hour,
	DecayK:          0.232,
	DecayAlpha:      0.167,
	StrikeLine:      0.4,
	Deadband:        0.05,
	BudgetFloor:     16_000,
	BudgetStart:     32_000,
	BudgetCeiling:   200_000,
	BudgetStep:      8_000,
	BudgetShrink:    0.75,
	BudgetGrowEvery: 20,
	SpotlightShare:  0.10,
	StrataShare:     0.5,
	DeepShare:       0.4,
	PromotionEvery:  4,
	LampBits:        14,
	LampWordBits:    10,
	StandInWeight:   0.5,
	FollowedShare:   1.0 / 3,
	Season:          31 * 24 * time.Hour,
	ClockWindow:     90 * time.Minute,
	ClockDateWindow: 27 * time.Hour,
	WakeLines:       3,
	WakeDecoyEvery:  4 * 24 * time.Hour,
	SleepIdle:       10 * time.Minute,
	SleepTurns:      500,
	CacheLong:       "1h",
	CacheShort:      "5m",
}
