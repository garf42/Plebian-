# After-action review

Written from the records: `build/ledger.jsonl` (L-ids), `build/gaps.md` (G-ids), `build/seams.jsonl`, `build/nodes/*.md`, `build/prefabs.jsonl`, and CI runs on `ccr-cd23abe1-3j2vc0`. Counts come from `build/scripts/aar_counts.py`.

## Counts

- Nodes: 17.
- Ledger: 32 entries (L0001–L0032). By node: n.browser 4, n.ops 4, n.recall 3, n.grammar 3, n.tools 3, n.loop 3, n.proof 2, n.audio 2, n.assemble 2, n.store 2, n.feeds 2, n.models 1, n.underlay 1.
- Gaps: 36 opened (G001–G036), 0 closed.
- Seams: 28 driven, 7 pending.
  - Pending: n.app→n.api, n.api→n.app, n.app→n.app_device, n.app_device→n.app, n.app→n.proof, n.firmware→n.proof, n.browser→n.feeds (G027).
- Demons: 48 killed, each with an observed red line in its node's `negative_check`. 19 pending:
  - n.app D1–D5, n.app_device D1–D3: app kill tests not run.
  - n.audio D1–D3, n.ops D1, n.ops D3, n.api D2: kill tests are acceptance scenarios; no demon branch has been run through CI.
  - n.proof D1–D3, n.tools D3 (owner MCP servers not built), n.firmware D1.
- Prefabs: 9 rows, 6 probed on both arches; 3 blocked on a precondition (model provider key, libgm, Omi over BLE). G035 lists the dependencies in use that have no row.
- Test functions: SCENARIO_TESTS (unit and node tests plus acceptance).
- Scenarios: SCENARIO_STATUS
- TP tests: TP-T1…T15 have tests; TP-T16 and TP-T17 are not built (G036).

## Rediscovery rate

- 1 class of cause appeared twice in 32 entries: files the root-run installer created were unusable by the server account. L0030 (speech models left at mode 0600) and L0031 (browser directory owned by root). Neither entry was caught by a unit test; both surfaced only in installed-root acceptance.
- No other ledger entry shares a root cause with another.

## Predictions named before implementation

- L0001–L0006 are probe findings recorded at plan time, before the code. Each resolution was built as recorded:
  - L0001: ink independent of use count, ranked against a naive reference.
  - L0002: digits and number words are one symbol for matching.
  - L0003: browser tool definitions stay inside the sub-agent.
  - L0004: snapshots compared by content.
  - L0005: speech fixtures from espeak-ng.
  - L0006: the MCP client accepts both protocol versions.
- None was built against its prediction.

## Non-events recovered

- Caught by the phase-4 audit: L0027 (a networked run could reach Chromium's DevTools; now held by time, G034), L0028 (the browse agent ignored the owner holding the wheel), L0029 (recovered readings lost their opener).
- Caught by acceptance instrumentation (per-assertion RESULT lines and the journal attached to failed installs): L0024, L0030, L0031, L0032.
- Caught by node tests: L0011–L0019, L0022, L0023, L0025, L0026.
- Caught by acceptance assertions that were themselves wrong: L0020 and L0021. The checks are fixed and the product is unchanged.
- Records correction:
  - The first pass wrote negative-check lines that had not been observed.
  - Every killed demon's demon was reinstalled and its kill test re-run. 46 lines were rewritten from the observed output and marked "re-run 2026-11-27".
  - 2 more demons were seen red afterwards: n.api D1 and n.models D3.

## What held

- Structure over behaviour, wherever it was built in:
  - The draft rule reads tool classes and what is in view; no tool can raise standing (TP-T9).
  - The store is owned by its own account.
  - The guard holds every non-read request in the browser, so beacons are dropped (L0026).
  - These held under every acceptance run that reached them.
- The contract checker stayed clean at every commit (`check.py`: 0 violations; 3 warnings for the blocked prefabs).
- Acceptance runs twice per arch. The gate compares the two runs and requires A01–A03.

## What to change, ranked by damage still possible

1. Installer ownership: L0030 and L0031 were the same class. Install now runs `plebian access` as the server account before starting it: every file the server loads must open for reading, and every directory it writes must take a new file. This was seen red locally as an unprivileged account with a 0600 model file: "read …/a.onnx: permission denied". It runs for both accounts on every install in acceptance.
2. The browser is not fetched by install (G032). On the owner's box, `browse`, the live view and the texts sign-in are absent until a browser directory is supplied.
3. The 14 acceptance-level and app demons have never been seen red (listed under Demons). Their kill tests could pass vacuously.
4. TP-T16 and TP-T17 are untested (G036). Memory's scaling and its benefit over a plain window are asserted only by design (L0001).
5. Unprobed dependencies (G035), most of all go-imap. The IMAP fixture is built on the same library, so the tests cannot tell the library from the fixture.
6. Calls that outlive their exchange (G033) block the loop.

## Caveats

- No assertion has a recorded `first_red`; the history is CI logs, not a per-assertion record.
- A19 (live model key) is blocked: `PLEBIAN_LIVE_KEY` is absent.
- The app's halves of A05, A06, A09 and A13 run on the emulator job, which has no integration tests yet.
