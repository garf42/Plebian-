# node: n.recall

spec: TP-01 (S, L, τ, λ), TP-07, TP-09; TP-T5, TP-T6, TP-T7, TP-T10, TP-T11, TP-T15, TP-T16, TP-T17; Plebian bindings (outside readings)

## assume
[BINDING] origin: specified
- in: n.grammar — uses with d fixed, labels per fragment, retraced shares, parent candidates
- in: n.loop — outcomes (exit codes, readings, owner messages, draft answers, redos) each naming the call it settles, and the ids in view for each action

## guarantee
[BINDING] origin: specified
- out: for turn n — S, L, τ and v per reachable fragment; λ and the lit fragments with their latest exchange per beam; strikes; open loops and kept orders; held-call decisions; signature-check recalls; what-followed tallies; per-body use counts for the inbound's rarest words
- rendering a fragment adds no ink; a tool repeating a reading adds none; no reading is ranked for the strata
- heard rows write no use, cover or outcome on a sheet
- only outcome records write trust; readings carry none; a line written in a turn heard words opened holds ½ until an owner message retraces it
- slot classes are never read when charging outcomes or closing loops
- a struck call repeated word for word in answer to its own failure is held once; issued again it runs; a non-identical call is never held
- elapsed wall-clock time changes no value; ten minutes idle and ten days idle leave identical stores after a sleep pass
- computing v for a turn's reach takes no longer on a million-turn store than on a thousand-turn store, within noise
- on a synthetic year-long thread the agent draws fewer re-tells and fewer repeats of a struck step than under a plain recent window of the same B

## oracle
type: differential
risk: semantic
reference: a naive implementation (every sum taken over every use, every cover recomputed) used only on small fixtures; `tests/recall/`; TP-03 worked example (port 8080's record 0.33 after its one call failed); MR-1…MR-4

## metamorphic relations
[BINDING] origin: specified
- MR-1: insert any wall-clock gap between turns ⇒ identical S, L, τ, v
- MR-2: render any set of fragments any number of times ⇒ identical S
- MR-3: a tool repeats a reading ⇒ the older reading's L drops; no S rises
- MR-4: run in two separate processes ⇒ identical values to the last bit

## demons
- D1: ink summed over every use of a label — values correct, per-turn cost grows with history (the probe saw a 9× slower label lookup at 1M turns)
  kill_test: tests/recall/test_flat_per_turn
  negative_check: pending
  status: live
- D2: trust bumped whenever a line is repeated
  kill_test: tests/recall/test_repetition_never_moves_trust
  negative_check: pending
  status: live
- D3: heard lines counted as uses on the sheets
  kill_test: tests/recall/test_heard_writes_no_use
  negative_check: pending
  status: live
- D4: decay driven by the wall clock
  kill_test: tests/recall/test_idle_is_free
  negative_check: pending
  status: live
- D5: holds near-repeats of a struck call, not only exact ones
  kill_test: tests/recall/test_held_exact_only
  negative_check: pending
  status: live

## clauses
- [BINDING] origin: derived — ink is computed in time independent of a label's use count (recent uses exact, older uses folded into an aggregate as in Petrov 2006), and on every fixture the resulting ranking equals the differential reference's except where two values lie within the deadband (ledger: L0001) (kill_test: tests/recall/test_ink_rank_matches_reference)

## open gaps
- G001 — default parameters are conventions awaiting refit
- G002 — Remembers on a real year-long thread
