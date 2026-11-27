# node: n.api

spec: PL-01 … PL-09 (what each screen reads and writes); AG-05 (owner channel, egress); AG-09 (pairing, listener)

## assume
[BINDING] origin: specified
- in: n.loop — thread events, drafts, questions, owner-facing messages, notifications
- in: n.store — read access for projections: day pages, journal, taps, open items, search with per-body counts
- in: n.app — owner messages, audio frames and offline files, taps and notes, signed admin actions, wheel input
- in: n.audio — speech seconds per minute as audio arrives; each offline file's sha256 once it is durable in the blob store

## guarantee
[BINDING] origin: specified
- out: an authenticated API and event stream for the app; owner messages delivered to n.loop as the only owner channel; audio and taps delivered to n.audio; admin actions verified against the device key before they reach anything
- listens only on its own tailnet address (Tailscale's embedded library, state under the root) and a free localhost port; until the tailnet sign-in completes (and in CI) it serves on localhost alone
- an unpaired client receives nothing and changes nothing
- admin actions (standing grants, deploys) require a signature from a paired device key; a bearer token alone is refused
- projections are pure functions of the store: replaying them on a copy gives identical bytes; pages cut at 4 am in the owner's time zone
- a version endpoint reports the commit the server was built from
- the event stream carries notifications for every message the agent starts, and the ribbon's live speech minutes
- each offline file is acknowledged to the app with its sha256 only once it is durable in the blob store

## oracle
type: relational
risk: semantic
fixtures: `tests/api/`; scenarios A02, A03, A14

## metamorphic relations
[BINDING] origin: specified
- MR-1: project the same store twice, in separate processes ⇒ byte-identical pages
- MR-2: change the owner's time zone ⇒ only page cut points move

## demons
- D1: accepts a standing grant with a bearer token alone
  kill_test: tests/api/test_grant_requires_signature
  negative_check: pending
  status: live
- D2: listens on every interface
  kill_test: tests/acceptance/a01_install_isolation
  negative_check: pending
  status: live
- D3: a projection reads the wall clock, so replay differs
  kill_test: tests/api/test_projection_replay_identical
  negative_check: pending
  status: live
- D4: acknowledges an offline file on receipt, before it is durable, so the Omi deletes audio the server can still lose
  kill_test: tests/audio/ack_test.go TestFileAckOnlyWhenDurable
  negative_check: seen red — TestFileAckOnlyWhenDurable: a file the store could not keep was answered 200 with its sha (acknowledged before durable)
  status: killed

## clauses
(none yet)

## open gaps
- G010 — license choice
