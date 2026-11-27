# node: n.models

spec: AG-02; TP-04 (cache lifetimes); TP-06 (one budget state per model)

## assume
[BINDING] origin: specified
- in: src.models — providers speaking the Anthropic Messages API or the OpenAI-compatible Chat Completions API, each with a model listing endpoint

## guarantee
[BINDING] origin: specified
- out: for a request — streamed text and tool calls, usage (input, output, cached tokens), an error class; the token count of any text before sending; per model: context limit, cache mode, tool-calling support
- both wire formats carry the same request content in the same order; only breakpoint markup and field names differ
- a failed call is retried as the provider's Retry-After directs while the wait stays within the owner's usual time to answer; past that, the next provider in the owner's list takes the turn
- no model name is built in; models come from each provider's own listing, and a provider's wire format is the one whose listing call it answers
- token counts come from the provider's own counter where it has one, else from that model's highest tokens per byte in its usage reports (its first request measures it), so a count never falls short; the context limit comes from the listing where it gives one, else from the provider's overflow error or a usage report counting fewer input tokens than were sent
- thinking blocks are passed back within a tool round where the provider requires it and are never returned for storage
- a scripted fixture provider speaking both wire formats records every request byte for byte for tests

## oracle
type: differential
risk: reproducibility
reference: the fixture provider's request log; MR-1, MR-2

## metamorphic relations
[BINDING] origin: specified
- MR-1: the same request through each wire format ⇒ identical message content and order in the fixture log
- MR-2: the same request in two separate processes ⇒ byte-identical request bodies

## demons
- D1: puts cache breakpoints anywhere that fits, not on band boundaries
  kill_test: tests/assemble/assemble_test.go TestBreakpointsOnBands
  negative_check: seen red (re-run 2026-11-27) — assemble_test.go:228: request 0: a breakpoint on the newest inbound
  status: killed
- D2: switches provider on the first 429 regardless of Retry-After
  kill_test: tests/models/retry_test.go TestRetryWindow
  negative_check: seen red (re-run 2026-11-27) — retry_test.go:48: Retry-After 7s within 1m0s: same provider answered false (err model retry (429): {"error":{"message":"fixture: scripted error","type":"rate_limit_error"},"request_id":"req_fixture_1"
  status: killed
- D3: counts every model's tokens with one generic tokenizer, so a request that fits B on paper overflows another model or is silently cut by a runtime that truncates
  kill_test: tests/models/count_test.go TestCountsNeverShort
  negative_check: seen red (2026-11-27) — count_test.go:24: before any report a 4528-byte request counted 1132 tokens: a token covers at least one byte
  status: killed

## clauses
(none yet)

## open gaps
- G012 — sub-agent model default
