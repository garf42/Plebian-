# node: n.browser

spec: AG-04; PL-09; Plebian bindings (browser actions); RFC 9110 §9.2.1; RFC 10008; GraphQL spec §2.3 and GraphQL over HTTP

## assume
[BINDING] origin: specified
- in: src.web — public sites, sites that need the owner's sign-in, and fixture sites with request logs in tests

## guarantee
[BINDING] origin: specified
- out: browse sub-agent summaries; page readings (accessibility snapshots) flagged as outside words; held requests (method, URL, headers, body) raised as drafts; downloads, put in the tools' outside folder; a live-view stream of the page and an input relay for the owner; a cookie set from a throwaway context for the texts link
- one browser per box with a persistent profile under Plebian's root
- after an agent action, every request that is not a read is held, and what one action set off before the sub-agent's next step is one draft; a read is a safe method (GET, HEAD, OPTIONS, TRACE, QUERY) or a GraphQL request whose document is in its bytes and whose selected operations (every entry of a batch) are all queries; a beacon or ping (no page waits on its answer) is dropped, never sent or drafted; reads are never held
- an approved held request is released with exactly the bytes shown, once; if the page can no longer send it, the draft fails and says so
- while the owner holds the wheel the agent waits; nothing the owner types is recorded, and no reading is taken until the owner hands back
- the throwaway context for the texts sign-in is closed after its cookies are handed over

## oracle
type: derivable
risk: semantic,reproducibility
fixtures: `tests/browser/` fixture sites (form POST, fetch POST, XHR PUT, DELETE, a GET search form, a sign-in form, a sendBeacon, a GraphQL endpoint taking queries, mutations, mixed documents, batches and persisted documents, a file download); probe `tests/probes/browser`; MR-1…MR-3

## metamorphic relations
[BINDING] origin: specified
- MR-1: the same task in two separate processes ⇒ the same held requests in the same order
- MR-2: the same order placed by form submit, fetch() POST and XHR PUT ⇒ each is held with its own exact bytes
- MR-3: the same GraphQL query sent by GET and by POST ⇒ neither is held; the same document with operationName pointed at its mutation ⇒ held

## demons
- D1: holds form submissions but lets fetch() and XHR state changes through
  kill_test: tests/browser/guard_test.go TestScriptRequestsHeld
  negative_check: seen red — TestScriptRequestsHeld: buy: held [] (fetch() and XHR let through)
  status: killed
- D2: records the owner's keystrokes while the owner holds the wheel, in plain text or any encoding (base64, hex, URL-encoded)
  kill_test: tests/browser/live_test.go TestWheelNotRecorded
  negative_check: seen red — TestWheelNotRecorded: the log holds the owner's typing (base64 of the password)
  status: killed
- D3: on approval, re-composes the request from a fresh snapshot instead of releasing the held bytes
  kill_test: tests/browser/guard_test.go TestReleaseBytesIdentical
  negative_check: seen red — TestReleaseBytesIdentical: first equals the held bytes: false (release re-composed the body)
  status: killed
- D4: holds analytics beacons as drafts, so every agent click raises one
  kill_test: tests/browser/guard_test.go TestBeaconDropped
  negative_check: seen red — TestBeaconDropped: a beacon raised a draft (beacons held)
  status: killed
- D5: passes a GraphQL mutation as a read: one selected by operationName from a document that also holds a query, one inside a batch, or a persisted document whose bytes show no operation
  kill_test: tests/browser/guard_test.go TestGraphQLMutationHeld
  negative_check: seen red — TestGraphQLMutationHeld: the mutation was not held (a mixed document read by its first operation)
  status: killed

## clauses
- [ADVISORY] origin: derived — probe: a paused POST reached the fixture zero times while held and exactly once, byte-identical, after release; owner input through CDP Input events on the same page worked; cookies read back (prefab: Chromium via CDP)
- [ADVISORY] origin: derived — Playwright MCP snapshot text varied by a few bytes between identical runs; compare snapshots by content, never byte for byte (ledger: L0004)

## open gaps
- G011 — how long a held request survives
- G015 — Google sign-in in the agent's browser
- G016 — DevTools endpoint reachable from loopback
- G017 — reads a site sends as unmarked POSTs
