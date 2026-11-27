# node: n.proof

spec: constitution intent predicate (A01–A20); TP tests; every node's demons

## assume
[BINDING] origin: specified
- in: n.ops — release artifacts and the installer for linux/arm64 and linux/amd64
- in: n.app — the APK and its integration tests
- in: n.firmware — the DFU package, when built

## guarantee
[BINDING] origin: specified
- out: `tests/acceptance/manifest.json` listing A01–A20 with a runner each; fixtures — a model provider for both wire formats, IMAP/SMTP, an iCal feed, the texts-library interface, web sites with request logs, an S3-compatible bucket, the Omi simulator, a synthetic speech generator, a synthetic year-long thread; CI workflows on arm64 and amd64 runners plus an Android emulator; results recording `first_red` per assertion
- every scenario drives only outside surfaces and asserts positive existence before any prohibition
- every scenario checks the server's version endpoint reports the commit under test
- each scenario runs twice in separate processes and both runs must pass
- fixtures use only content the repository may redistribute (generated, or public domain)
- A19 runs only when the live-key secret exists and reports "blocked: key" otherwise

## oracle
type: derivable
risk: reproducibility
fixtures: the manifest itself; CI history of `first_red`

## metamorphic relations
[BINDING] origin: specified
- MR-1: run the suite twice ⇒ identical pass/fail per assertion

## demons
- D1: scenarios pass against a mocked server instead of the release build
  kill_test: tests/acceptance/test_version_is_commit_under_test
  negative_check: pending
  status: live
- D2: a scenario's assertions also pass on an empty store
  kill_test: tests/acceptance/test_non_vacuity
  negative_check: pending
  status: live
- D3: a flaky scenario passes on retry
  kill_test: tests/acceptance/test_two_runs_both_pass
  negative_check: pending
  status: live

## clauses
- [BINDING] origin: derived — synthetic speech fixtures are generated with espeak-ng at test time (two distinct voices); the sherpa-onnx 2.0.3 pip wheel's Piper voices failed with no espeak frontend (ledger: L0005) (kill_test: tests/fixtures/test_speech_fixture_deterministic)

## open gaps
- G002 — Remembers on a real year-long thread
