# After-action review

Written from the records: `build/ledger.jsonl` (L-ids), `build/gaps.md` (G-ids), `build/seams.jsonl`, `build/nodes/*.md`, `build/prefabs.jsonl`, and CI runs on `ccr-cd23abe1-3j2vc0`. Counts come from `build/scripts/aar_counts.py`.

## Counts

- **Nodes:** 17.
- **Ledger:** 49 entries (L0001–L0049).
  - By node: n.browser 5, n.proof 5, n.app 5, n.app_device 5, n.ops 4, n.loop 4, n.recall 3, n.grammar 3, n.tools 3, n.audio 2, n.assemble 2, n.store 2, n.feeds 2, n.models 1, n.underlay 2, n.api 1.
- **Gaps:** 49 opened (G001–G049), 0 closed. G045–G049 record unbuilt clauses the final audit found.
- **Seams:** 26 driven, 9 pending.
  - The final audit set four back to pending because their acceptance driver is red: n.underlay→n.assemble (A09), n.loop→n.audio (A07), n.underlay→n.loop (A08), n.browser→n.tools (A12).
  - Also pending: n.app↔n.api (G049), n.app→n.proof, n.firmware→n.proof, n.browser→n.feeds (G027).
- **Demons:** 65 killed, each with the red line seen in its node's `negative_check`.
  - Where each was seen:
    - 46: node tests, re-run 2026-11-27.
    - 3: node tests in this session (n.api D1, n.models D3, n.proof D3).
    - 8: the app agent's `flutter test` (n.app D1–D5, n.app_device D1–D3).
    - 8: CI demons run 45982484950, each a patch in `tests/demons/` built into a release artifact and run through its scenario.
  - 2 pending: n.tools D3 (owner MCP servers not built) and n.firmware D1 (firmware not built).
- **Prefabs:** 13 rows, 7 probed.
  - Blocked on a precondition: model provider key, libgm, Omi over BLE, universal_ble, biometric_signature, and the Flutter packages' platform halves.
  - G035 lists the server dependencies in use without a row.
- **Tests:** 109 Go test functions, 80 Flutter tests, and 20 acceptance scenarios run twice per arch.
- **Scenarios** (server run 45987993187, commit 55c6853, identical on amd64 and arm64, both runs agreeing):
  - Pass: A01, A02, A03, A04, A05, A06, A07, A09, A10, A11, A12, A14, A15, A17, A18, A20, Manifest, NonVacuity (under the every-assertion-red rule), VersionIsCommitUnderTest.
  - A05 and A06 count their server halves only; the app halves have not run on the emulator (G049).
  - Fail:
    - A08: no woken turn.
    - A13: the clock-written entry is not shown held.
    - A16: the quiet feed's question was counted 4 times.
  - Blocked: A19.
  - The previous run (45983583848, commit 835583c) had A07, A09 and A12 red. A09 went green with L0049, A12 with the Chromium environment change (correcting L0048's flags-only fix), and A07 with counting distinct question blocks.
- **TP tests:** TP-T1…T15 have tests; TP-T16 and TP-T17 are not built (G036).

## Rediscovery rate

- 0 confirmed rediscoveries in 48 entries.
- L0031 claimed the same cause class as L0030 (installer-owned files the server couldn't use). L0048 shows that diagnosis was wrong: the browser directory was already the server's, and the real cause was Chromium's crash reporter.

## Predictions named before implementation

- L0001–L0006 are probe findings recorded at plan time, before the code. Each resolution was built as recorded; none was built against its prediction.
- L0001: ink is independent of a label's use count. The sqlite probe confirms the shape on arm64: an indexed lookup grows 8× from 1K to 1M turns.

## Non-events recovered

- **Caught by the phase-4 audit:** L0027, L0028, L0029.
- **Caught by acceptance instrumentation** (per-assertion RESULT lines, the install-failure journal, Chromium's stderr in Launch's error): L0024, L0030, L0032, L0048.
- **Caught by node tests:** L0011–L0019, L0022, L0023, L0025, L0026.
- **Caught by the app agent's tests:** L0038–L0047.
- **Acceptance checks that were themselves wrong:**
  - L0020 and L0021.
  - A07 and A16 counted every request carrying a question in its history, not the exchanges a question opened.
  - A10 and A13 read one page while the run's real start (04:09 in the test's time zone) put rows across the 4 am cut.
  - Six assertions held on an empty store. Each now requires evidence the drive happened, and the undriven run requires every assertion red: n.proof D2.
- **Caught only in CI because the unit test stepped the loop by hand:** L0037 (the clock slept through the next day's window).
- **Records correction:**
  - The first pass wrote negative-check lines that had not been observed.
  - Every killed demon was reinstalled and its kill test re-run. The 46 lines were rewritten from the observed output and marked "re-run 2026-11-27".
  - L0031's cause was wrong and is corrected by L0048.

## Final audit

- 40 findings: 15 violations, 22 suspect, 3 clean.
- Acted on: docstrings in main.go and tailnet.go, the NOTICE bubblewrap claim, A08's prohibitions conditioned on the woken turn, five seams set back to pending (four red-driven, plus the device seam's emulator citation), gaps G045–G049, and Flutter pinned to 3.50.1.
- Open:
  - first_red per assertion is not recorded (n.proof guarantee).
  - Seven assertions are evaluated only inside branches no demon or undriven run has reached.
  - A14's drafted_with_outside_words can be satisfied by an earlier draft.
  - A20 repackages the same artifact, so a swap that changes nothing would also pass.
  - A15 reads spend per model, not B per model.
  - The prefab rows for the app packages, biometric_signature and the Go speech binding are unprobed.
  - 7 of the 8 app demon lines rest on the app agent's report (n.app_device D2 was reproduced).

## What held

- Structure over behaviour, wherever it was built in:
  - The draft rule reads tool classes and what is in view; no tool can raise standing.
  - The store is owned by its own account.
  - The browser guard holds every non-read request, and beacons are dropped.
  - Signed admin actions require the device key (n.api D1).
  - No scenario reached by a demon run let its demon through: 8 of 8 went red on the assertion the demon targets.
- The contract checker stayed clean at every commit (`check.py`: 0 violations; warnings only for blocked prefabs).
- The gate (A01–A03 in both runs on both arches) was green from run 36 onward.

## What to change, ranked by damage still possible

1. **A08, A13, A16.**
   - A08: the woken turn is still undiagnosed; the node-level wake passes with the same lines (`tests/underlay/wake_test.go`).
   - A13: the clock now opens the turn, but its entry reads as not held; held entries project correctly at the node level.
   - A16: the question is still counted more than once.
   - The journal printed for each failing scenario came back empty (`journalctl _UID` with `--since`), so these three have no server-side record yet.
2. **The browser is not fetched by install (G032).** On the owner's box, `browse`, the live view and the texts sign-in are absent until a browser directory is supplied.
3. **Tailnet reach is untested in CI** (CI runs `--no-tailnet`).
   - L0036 fixed a paired address that changed on every restart; no unit or acceptance test would have caught it.
   - First-run checklist line 8 is the only check.
4. **TP-T16 and TP-T17 are untested (G036).** Memory's scaling and its benefit over a plain window are asserted only by design (L0001).
5. **Unprobed dependencies (G035).** go-imap's tests cannot separate the library from a fixture built on it.
6. **Calls that outlive their exchange (G033)** block the loop.
7. **Install now checks** that both accounts can use what it laid down (`plebian access`), but nothing checks the box's own libraries for Chromium on Raspberry Pi OS Lite.

## Caveats

- No assertion has a recorded `first_red`; the history is CI logs, not a per-assertion record.
- A19 (live model key) is blocked.
- The app's emulator integration tests and the Android build run only in CI. Its first green build is the compile check for the Kotlin side (G044).
- The test clock starts at real wall time, so a scenario's span can cross the 4 am cut depending on when CI runs. Page reads now span every page a scenario's rows can fall on; anything still reading a single page is exposed.
