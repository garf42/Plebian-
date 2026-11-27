# node: n.grammar

spec: TP-02 steps 3–5, 9–10; TP-03A; TP-T1, TP-T3, TP-T10, TP-T12, TP-T14

## assume
[BINDING] origin: specified
- in: n.store — TP-03 tables writable by the server user
- in: n.loop — sheet bytes in turn order, each fragment tagged with its writer (owner, agent, tool, or a heard source with speaker/sender) and whether heard or outside words opened its turn

## guarantee
[BINDING] origin: specified
- out: per sheet — its spelling in labels, loose words and changes; new symbols, pairs, uses (with d fixed at the moment of use), frames; near-match records; stand-in requests for words the thread has never used (written as plain rows for an outside writer)
- expanding any spelling yields the sheet's exact bytes, before and after any respelling
- after each sleep pass no pair of symbols appears twice and no label has fewer than two uses
- the round trip runs only on owner text, heard speech, mail and texts, each on its own edit costs; tool output and agent words are indexed exactly as written
- a real word new to the thread is never recorded as a near-match of a familiar word
- a near-match becomes its own label only when the other writer uses it word for word
- rendered text is never altered by near-matches, slot classes or stand-ins
- the grammar package imports no model client

## oracle
type: derivable
risk: semantic,reproducibility
fixtures: `tests/grammar/` (TP-03A worked examples: A = "copy the prod config", B = A + "to staging"; `confg`; 8080 → 8081; fifty vs fifteen); MR-1…MR-4

## metamorphic relations
[BINDING] origin: specified
- MR-1: index the same sheets in two separate processes ⇒ byte-identical tables
- MR-2: index sheets 1..n in one run vs 1..k, restart, k+1..n ⇒ identical grammar after a sleep pass
- MR-3: respell then expand ⇒ original bytes for every sheet
- MR-4: shift every wall-clock time by a constant ⇒ identical labels, uses and spellings

## demons
- D1: forms labels on the second sighting but never credits the first, so tracing a line forward never darkens the original
  kill_test: tests/grammar/test_first_sighting_counts
  negative_check: pending
  status: live
- D2: the round trip "corrects" a real word into a familiar one (fifteen recorded as a use of fifty)
  kill_test: tests/grammar/test_real_word_stays
  negative_check: pending
  status: live
- D3: number words and digits never match, so typed "fifteen" and heard "15" are strangers
  kill_test: tests/grammar/test_number_forms
  negative_check: pending
  status: live
- D4: stand-ins fetched by importing a model client inside the grammar package
  kill_test: tests/grammar/test_no_model_imports
  negative_check: pending
  status: live

- D5: keeps each sheet's raw bytes beside its spelling and "rebuilds" from the raw copy, so the grammar is never proven to round-trip
  kill_test: tests/grammar/test_rebuild_from_spelling_only
  negative_check: pending
  status: live

## clauses
- [BINDING] origin: derived — a number written in words and the same number in digits are one symbol for matching; what is rendered stays as written (ledger: L0002) (kill_test: tests/grammar/test_number_forms)

## open gaps
- G001 — default parameters are conventions awaiting refit
