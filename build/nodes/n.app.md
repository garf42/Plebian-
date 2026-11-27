# node: n.app

spec: PL-01 … PL-09; `docs/design/tokens.json`; `docs/design/mockup.html` (look and motion reference; `docs/plebian.md` governs where they differ)

## assume
[BINDING] origin: specified
- in: n.app_device — audio frames, offline files, button events, battery, firmware revision
- in: n.api — projections, the event stream, pairing, signed-action endpoints
- in: src.owner — taps, typing, keyboard voice input, fingerprint

## guarantee
[BINDING] origin: specified
- out: screens Today, Journal (Days, Taps, search), Open, Day page, Tap, Omi, This week, Setup, Browser live view — each rendering only what the API returns; owner messages; taps and notes; signed admin actions; wheel input; the server's offline-file acknowledgements, passed to the device layer
- every control on every screen does what its label says against the real API; no screen is a placeholder
- the app's own strings are labels, names, times, counts and statuses: no hints, captions, instructions or empty-state sentences on any screen
- nothing in the conversation sends fixed words: drafts, questions, heard lines and taps are answered by the owner typing; tapping quotes the item into the composer
- heard lines show the speaker and a dashed rule until settled; settling inks the rule solid
- taps render in ore; the ribbon shows talk density by the hour and the taps on it, live
- the motions in `tokens.json` (lay, strike, live, peek, presence, settle) run on their events; with reduced motion each becomes a fade
- every control has a TalkBack label; text follows the system font size
- messages and audio queue while the server is unreachable and send in order when it returns
- Setup cards (Server, Pair, Model, Omi, Mail, Calendar, Texts, Storage, Backups, Updates), reached from This week, each show status and take one action; only Server, Pair and Model are required
- every message the agent starts raises an Android notification through the service's own server link (no push service); tapping it opens Today at that message
- the APK installs on Android 10 and later (the first to give a foreground service the connected-device type its Omi and server links run under); fonts and the wordmark are bundled

## oracle
type: relational
risk: semantic
fixtures: Flutter integration tests on an emulator against the server under test with the fixture provider; screenshots of every screen attached to the CI run for review

## metamorphic relations
[BINDING] origin: specified
- MR-1: the same store rendered twice ⇒ identical widget trees
- MR-2: system font scale raised ⇒ no text clipped; every control still reachable

## demons
- D1: a screen whose buttons do nothing (a record button that captures no audio)
  kill_test: app/test/app/test_every_control_has_effect_test.dart
  negative_check: seen red (2026-11-27, flutter test, reported by the app agent) — Tap: control "Save" changed nothing
  status: killed
- D2: a draft card with a Send button that posts fixed words
  kill_test: app/test/app/test_no_canned_replies_test.dart
  negative_check: seen red (2026-11-27, flutter test, reported by the app agent) — Today: "Send" sent a message the owner did not type
  status: killed
- D3: heard lines styled like settled lines
  kill_test: app/test/app/test_heard_styling_test.dart
  negative_check: seen red (2026-11-27, flutter test, reported by the app agent) — Expected: Color:<Color(alpha: 1.0000, red: 0.2784, green: 0.3333, blue: 0.3529, ... / Actual: Color:<Color(alpha: 1.0000, red: 0.1176, green: 0.1647, blue: 0.1765, ...
  status: killed
- D4: a message the agent starts waits silently in the thread while the app is closed
  kill_test: app/test/app/test_notification_with_app_closed_test.dart
  negative_check: seen red (2026-11-27, flutter test, reported by the app agent) — Expected: ['42'] / Actual: MappedListIterable<Map<String, dynamic>, dynamic>:[]
  status: killed
- D5: an empty screen or section shows a sentence about what will appear there, or a hint about how to use it
  kill_test: app/test/app/test_no_explanatory_strings_test.dart
  negative_check: seen red (2026-11-27, flutter test, reported by the app agent) — Actual: ['lib/ui/journal.dart: "Nothing waits on you yet."']
  status: killed

## clauses
(none yet)

## open gaps
- G009 — Android developer verification registration
