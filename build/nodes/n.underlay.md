# node: n.underlay

spec: TP-01 (underlay lamp), TP-02 step 8 (lookup), TP-03 (`heard`), TP-07 (heard wake, kept orders, calendar entries), TP-08; Plebian bindings (taps, sources, questions code raises); TP-T5, TP-T7; PL-02 steps 5–7

## assume
[BINDING] origin: specified
- in: n.grammar — the round trip per source, on each source's own edit costs
- in: n.audio — stretches: lines per speaker (named or an unknown-voice id), wall times, a pointer to the audio original; newer readings of the same audio; taps (wall time, made on the Omi or in the app, the owner's note); questions it raises (an unnamed voice heard twice, the Omi gone silent)
- in: n.feeds — messages and calendar entries as received, with sender, wall time and a pointer to the original; questions from its silence watch

## guarantee
[BINDING] origin: specified
- out: heard rows (one per stretch, message or entry; exact; hashed; pointing to the original); landings (batches) naming the loops and kept orders each lit; lookup results as row ids; tap pointer rows; per-source counts, medians and frequencies
- a landing opens at most one turn however much it holds, and none when it lights nothing
- a heard line lights a loop only past the decoy line of TP-07; background talk sharing common words lights nothing
- nothing heard settles a line, closes a loop, gives an order or counts as a re-tell
- lookup returns ids; the rows it found are quoted for that request only and never copied into the stack; given a message's row id, lookup puts that message's attachments, as files, in the tools' outside folder, and its reading lists what it put there; the body stays a heard row and is never put out
- a row that arrives late or out of order lands at its wall time; a later transcript of the same audio, or an edit to a message or entry, covers the earlier row
- a calendar entry lands when written and again as it comes due, early by the owner's usual time to answer the agent
- a tap keeps how it was made and resolves to the stretch around its time once that stretch lands
- questions raised upstream go to the loop as inbounds of their own, never inside a landing, so each wakes the agent once and lights nothing

## oracle
type: relational
risk: semantic
fixtures: `tests/underlay/` scripted moments (two spoken answers, an overheard go-ahead, someone else addressing the agent, near misses, background talk, a misheard number against a typed one, an oblique answer); MR-1…MR-3

## metamorphic relations
[BINDING] origin: specified
- MR-1: permute the arrival order of a day's landings ⇒ identical heard rows, pages and lit loops
- MR-2: split one landing into two ⇒ the same loops lit, at most one turn each
- MR-3: add background talk with none of a loop's rare words ⇒ no new turn

## demons
- D1: every landing opens a turn whether or not it lit anything (model cost grows with everything heard)
  kill_test: tests/underlay/test_landing_opens_only_when_lit
  negative_check: seen red (re-run 2026-11-27) — underlay_test.go:108: background talk opened 300 turns: [rows: 1
  status: killed
- D2: lookup returns row text into the sheet, so heard words enter the stack
  kill_test: tests/underlay/test_lookup_returns_ids
  negative_check: seen red (re-run 2026-11-27) — underlay_test.go:157: lookup's reading copies heard text: "rows: 1\n1\n<heard id=\"1\" source=\"talk\">..."
  status: killed
- D3: a heard answer closes the question it answers
  kill_test: tests/underlay/test_heard_closes_nothing
  negative_check: seen red (re-run 2026-11-27) — underlay_test.go:130: a heard answer closed the question (0 open)
  status: killed
- D4: lookup puts an attachment where a call that didn't ask for the outside folder can read it
  kill_test: tests/underlay/putout_test.go TestAttachmentsLandOutsideOnly
  negative_check: seen red (re-run 2026-11-27) — putout_test.go:65: an attachment landed outside the outside folder: /tmp/TestAttachmentsLandOutsideOnly3630181143/002/workspace/1/invoice.pdf
  status: killed
- D5: lookup puts out the whole message, so a `read` copies its heard body into the stack
  kill_test: tests/underlay/putout_test.go TestAttachmentsLandOutsideOnly
  negative_check: seen red (re-run 2026-11-27) — putout_test.go:54: put out [outside/1/invoice.pdf outside/1/message.txt]
  status: killed

## clauses
(none yet)

## open gaps
- G005 — calendar feed refresh delay
