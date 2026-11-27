# node: n.assemble

spec: TP-04, TP-05, TP-06, TP-08 (merges, sleep pass, no turn blows the budget); TP-T3, TP-T4, TP-T13; AG-02 (caching, thinking)

## assume
[BINDING] origin: specified
- in: n.recall — v, λ, strikes, loops, kept orders, what-followed tallies and lit exchanges for the current inbound
- in: n.underlay — heard lines lit past chance, each with the talk or thread around it, speaker or sender, and time
- in: n.models — token count of any text for the target model, the model's context limit, and its cache mode (explicit breakpoints or automatic prefix)

## guarantee
[BINDING] origin: specified
- out: one request per turn — tool band (a line per tool), fixed system rules, deep strata, recent strata, tail, one user turn holding the inbound with the spotlight quoted beside it, and a cache plan (breakpoints with lifetimes where the provider uses them)
- every request is counted before it is sent; one that opens an exchange never exceeds B; none exceeds the ceiling
- every rendered line is a byte-exact span of a sealed turn or heard stretch, a gap marker, or a computed count marked as the harness's own
- consecutive requests diverge only at band boundaries
- heard rows appear only in the user turn, never in a band or the tail
- the tail folds only as an exchange opens and never folds the newest sheet; within an exchange the request only grows
- the model's thinking is never stored and leaves when the exchange closes
- B per model: starts 32K, floor 16K, ceiling 200K, each capped at the model's context limit; moves per TP-06; restarts at 32K on a model change
- the sleep pass never runs on the reply path

## oracle
type: relational
risk: semantic,reproducibility
fixtures: `tests/assemble/` (TP-04 drawing at turn 48: 6K / 4.4K / 6.7K / 4.8K / 2.6K at B = 32K); MR-1…MR-3

## metamorphic relations
[BINDING] origin: specified
- MR-1: assemble the same turn in two separate processes ⇒ byte-identical request
- MR-2: swap the target provider's wire format ⇒ identical content in identical order; only breakpoint markup differs
- MR-3: assemble on a 1K-turn and a 1M-turn store with the same recent history ⇒ the same time within noise

## demons
- D1: checks the budget only after the spotlight is added mid-exchange, so an opening request passes B
  kill_test: tests/assemble/test_budget_on_opening
  negative_check: seen red (re-run 2026-11-27) — assemble_test.go:60: opening request 25 counted 16016 tokens, B is 16000
  status: killed
- D2: places a lit heard line into the tail so it rides later requests
  kill_test: tests/assemble/test_heard_user_turn_only
  negative_check: seen red (re-run 2026-11-27) — assemble_test.go:183: a heard line rode into a later exchange's request
  status: killed
- D3: rewrites a band mid-exchange when a rank changes
  kill_test: tests/assemble/test_cache_breaks_on_boundaries
  negative_check: seen red (re-run 2026-11-27) — assemble_test.go:156: request 2 changed message 0 inside the tail
  status: killed
- D4: stores the model's thinking with the sheet
  kill_test: tests/assemble/test_no_thinking_stored
  negative_check: seen red (re-run 2026-11-27) — assemble_test.go:121: turn 1 holds the model's thinking
  status: killed

## clauses
(none yet)

## open gaps
- G001 — default parameters are conventions awaiting refit
