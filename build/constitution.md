# Constitution — Plebian
<!-- cap: 200 -->

Read by every agent, every task. Editable, not appendable: to add a line, remove one.
Specs: `docs/agent.md`, `docs/plebian.md`, `docs/tracer-paper-memory.md` (TP-xx). A node names the sections it needs.

## Intent predicate
[BINDING] origin: specified
Plebian is done when every scenario A01–A20 in `tests/acceptance/manifest.json` passes in CI against release artifacts built from the commit under test (server for linux/arm64 and linux/amd64; the APK on an Android emulator), driving the system only through its outside surfaces — the Omi simulator behind the app's device-transport seam, the app's UI, the server's API, and fixture model, mail, calendar, texts and web servers — and Tracer Paper tests TP-T1…TP-T17 pass.
- non-vacuity: the manifest lists exactly A01–A20; every scenario asserts at least one positive existence (a sealed turn, a heard row, a rendered element, a request a fixture received) before any prohibition, and a scenario whose positive assertions also hold on an empty store fails; the server's version endpoint reports the commit under test.
- A01 install leaves nothing outside its root but its accounts, their linger flags and the system packages the box lacks for what Plebian runs (Chromium's libraries with the fonts and Xvfb Playwright's list brings, the host programs its tools use), installed through the system's package manager; uninstall removes all but those packages; a pre-existing canary service, directory and port are untouched.
- A02 pairing registers one device key; an unpaired client reads nothing.
- A03 an owner message gets a reply on Today and on the day page; every sealed turn rebuilds byte-exact.
- A04 a failed step becomes a redline; its word-for-word repeat is held once; a success of the same step closes the loop.
- A05 simulated Omi audio of two voices lands as heard rows with speaker labels; the ribbon fills live; a double-tap writes a tap and sends the 100 ms buzz.
- A06 audio recorded out of range syncs late and out of order and lands at its true wall time; a double-tap out of range writes no tap.
- A07 a voice heard in a second stretch with no name opens one question; the owner's own-words answer names it; later stretches carry the name.
- A08 overheard "send the invoices" with a pending draft sends nothing; an overheard request to the agent changes nothing; a landed mail asking for a fetch reaches nothing past the box.
- A09 a heard fact asked by text is answered with who said it and when.
- A10 mail, a calendar entry and a text land as heard rows; a question about the mail's PDF attachment is answered from its text, as outside words; a drafted reply is shown byte-exact, reaches the fixture SMTP once after an owner message approving it, and never after one declining it.
- A11 a research question is answered with the URLs it came from, using search and fetch.
- A12 a browser order's state-changing request is held and drafted while the page's GraphQL queries load unheld; approved, the fixture receives those bytes exactly once; a sign-in is done by the owner in the live view and the password appears nowhere in the store.
- A13 after the owner asks for the day's entry on two nights, the clock opens the third and opened none before; entry lines stay held until the owner retraces them; pages cut at 4 am.
- A14 a standing grant needs a device-key signature; a step with standing runs no sooner than the owner's usual time to answer after it is announced; with outside words in view it is drafted anyway; no agent tool can raise standing.
- A15 A03 and A04 pass on both wire formats; when the primary provider fails past the retry window, the fallback takes the turn; each model keeps its own budget.
- A16 after kill -9 mid-turn every sealed turn is intact and an interrupted change-making call is reported, not re-run; a feed silent past its longest quiet opens a question.
- A17 a backup restored to a fresh root replays a page byte-identical.
- A18 A05 passes with audio and originals in an S3-compatible bucket.
- A19 with the provisioned key, one real exchange with a tool call completes on the owner's provider.
- A20 an approved update builds, replays every page since the last deploy, deploys, and rolls back on a failed start.

## Source contracts

### src.owner
- grain: one owner message (typed or voice-typed) from a paired device; one admin action signed by that device's key
- keys: (device_id, client_msg_id) unique
- cardinality: one owner; one or more paired devices
- cadence: event stream, bursts separated by pauses
- late/null: messages queued offline arrive late, in order; empty messages never sent
- units/tz: device local time plus server UTC; days cut at 4 am in the owner's time zone
- access: local
- unknowns: none

### src.omi
- grain: one BLE notification (2-byte packet number, 1-byte index, codec payload); one button event (1 single, 2 double, 3 long, 4 press, 5 release); one stored offline file (index, timestamp, size)
- keys: packet number (u16, wraps) + index; file index per device; device serial
- cardinality: one Omi per owner; at most one phone connection
- cadence: stream while connected; offline files sync on reconnect
- late/null: offline audio arrives late, stamped by the device clock (set at connect); taps notify only while connected
- units/tz: codec 20 = Opus 16 kHz mono (default); 0/1 PCM16 16/8 kHz; 10/11 mu-law; little-endian; haptic write 1/2/3 = 100/300/500 ms
- access: local: the build drives the Omi simulator; the owner pairs the real Omi at first run (PL-08)
- unknowns: G003, G004

### src.models
- grain: one streamed request/response, with tool calls
- keys: provider request id
- cardinality: ordered provider list; one budget state per model
- cadence: on demand
- late/null: 429/5xx with Retry-After; streams can break mid-response
- units/tz: tokens by each provider's count; usage fields per response
- access: **blocked**: one provider API key as repo secret PLEBIAN_LIVE_KEY (+ PLEBIAN_LIVE_BASE_URL if not Anthropic) for A19; everything else runs on the fixture provider
- unknowns: none

### src.mail
- grain: one RFC 5322 message as received, per mailbox
- keys: (mailbox, UIDVALIDITY, UID) unique; Message-ID not unique
- cardinality: one or more accounts
- cadence: push via IMAP IDLE, re-issued within 29 minutes (RFC 2177)
- late/null: out-of-order arrival; deletes; missing Message-ID; Date headers can lie (INTERNALDATE wins)
- units/tz: Date with offset; INTERNALDATE
- access: local: the build uses a fixture IMAP/SMTP server; the owner connects a real account at first run
- unknowns: none

### src.calendar
- grain: one VEVENT instance (RRULE expanded) from an iCalendar feed
- keys: (UID, RECURRENCE-ID) per feed
- cardinality: one or more feeds
- cadence: HTTP conditional GET (ETag / Last-Modified)
- late/null: edits bump SEQUENCE/LAST-MODIFIED; cancellations; floating times; all-day dates
- units/tz: TZID with VTIMEZONE
- access: local: the build uses a fixture feed; the owner pastes a secret iCal URL at first run
- unknowns: G005

### src.texts
- grain: one Google Messages event (SMS, MMS or RCS) via the linked-device protocol
- keys: message id within conversation id
- cardinality: one phone; many conversations
- cadence: long-poll stream while the phone is online
- late/null: phone offline → nothing; backfill on reconnect
- units/tz: millisecond UTC timestamps
- access: local: the build uses a fixture at the texts library's interface; the owner signs in and taps the emoji at first run
- unknowns: G006, G015

### src.web
- grain: one HTTP response; one page state (accessibility snapshot)
- keys: URL + fetch time
- cardinality: unbounded
- cadence: on demand
- late/null: errors, bot walls, CAPTCHAs, sign-in walls (owner takes the wheel)
- units/tz: n/a
- access: local
- unknowns: G013

### src.host
- grain: one Linux host (Pi 5 arm64, or a cloud VM amd64/arm64) with systemd; data directory on any mounted drive; optional S3-compatible bucket
- keys: hostname
- cardinality: one Plebian per host, beside other software it must not touch
- cadence: n/a
- late/null: power loss; disk full
- units/tz: bytes; owner time zone set at install
- access: local (CI runners during the build; the owner's Pi at first run)
- unknowns: G007

### src.github
- grain: commits, CI runs, releases with artifacts, issues
- keys: commit sha; run id; tag
- cardinality: one repository
- cadence: on push
- late/null: CI queue delays
- units/tz: UTC
- access: granted (push from the build session); release signing key: G008
- unknowns: G008

## Tie-break order
[BINDING] origin: specified
Walk in order. First rule that discriminates wins. No ties.
1. The intent predicate and every [BINDING] guarantee win — never violate them.
2. Structure over behavior: the option that makes a violation impossible beats one that instructs against it or detects it.
3. Does it need to exist? Build nothing no scenario, test or guarantee needs; name what was skipped in the ledger.
4. Reuse before writing, in this order: an existing node's output, the Omi app's device code, the standard library or platform, an already-chosen dependency.
5. Prefer the reversible option.
6. Derive, never pick: every number comes from a spec, a protocol, a cited result or the store's own fitted history; a value you had to pick is a gap.
7. Explicit over inferred; never silently change grain.
8. Fewest moving parts: a new process, language or service only when it serves two or more roles.

## Escalation rule
[BINDING] origin: specified
If the tie-break order does not resolve a decision, do not ask the human.
Take the most reversible option, append a gap to `build/gaps.md`, and continue.

## Operating loop
[BINDING] origin: specified
1. Packet: constitution + own node + neighbours' assume/guarantee only + seams, prefabs and gaps naming the node + the doc sections the node names. Nothing else.
2. Predict (flagged nodes only): commit the expected output for the fixture before implementing.
3. Implement in a context that cannot read the prediction.
4. Diff prediction against output.
5. Surprise → tie-break order → else gap + reversible branch. Re-derived an existing clause? Log `rediscovered: <clause-id>`.
6. Write back a tightened clause with a kill test (`python build/scripts/ledger.py build/ledger.jsonl …`).
7. Negative-check every kill test: install the demon, watch the test go red, record it.
8. Drive outbound seams; a node isn't done until they run. Update `build/seams.jsonl` status.
9. Report only the surprise upward.
- Phase 0 is the walking skeleton: one row end to end through every node, however crude — a liveness check, never acceptance.
- `can-i-build`: a node opens only when its inbound seams are not `regressed`.
- CI is the build machine: anything the session can't run locally (arm64, Flutter, emulator, firmware) runs in CI; read results with `gh`.
- At each phase boundary run `python build/scripts/check.py build/ --phase N` and the audit subagent; fix before continuing.
- Commit at every green seam. Never commit a secret.
