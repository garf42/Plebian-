# Building Plebian

Instructions for the coding agent building this repository. The owner is not watching; do not ask them anything (see the escalation rule).

## What you're building

Plebian: a self-hosted, model-agnostic personal agent. A Go server on the owner's box (Raspberry Pi 5 by default, any Linux VM as an option), a Flutter Android app paired with the Omi wearable, and Tracer Paper Memory as its memory. The specs:

- `docs/plebian.md` — the product, the app, the Omi flow, where it runs, setup (PL-xx)
- `docs/agent.md` — loop, models, tools, browser, safety, reliability, install, decisions (AG-xx, Dxx)
- `docs/tracer-paper-memory.md` — memory and context (TP-xx, tests TP-T1…T17)
- `docs/design/` — tokens, wordmark, and a working HTML mockup of the app (`mockup.html`; open it in a browser to see look and motion)

## How the build is governed

The build is a contract graph in `build/`:

- `build/constitution.md` — read every task. The intent predicate (scenarios A01–A20) is what "done" means.
- `build/nodes/<id>.md` — one contract per node: assume, guarantee, oracle, metamorphic relations, demons with kill tests.
- `build/seams.jsonl` — one entry per edge; `build/prefabs.jsonl` — external dependencies with probes; `build/gaps.md` — decisions taken by default; `build/ledger.jsonl` — append-only, one entry per resolved surprise.
- `build/scripts/check.py build/ [--phase N]` validates the plan; `build/scripts/ledger.py` appends to the ledger atomically.

**Packet rule.** For a node, load only: the constitution; that node's file; the `assume` and `guarantee` sections of its neighbours; seams, prefabs and gaps naming it; the doc sections its `spec:` line names. Never another node's clauses. Sub-agents get packets, not the whole repo.

Follow the operating loop in the constitution. Every demon needs its kill test seen red with the demon installed (record it in the node's `negative_check`). Update seam status as seams are driven.

## Phases

- **Phase 0 — walking skeleton.** n.proof's fixtures and CI first, then one row end to end through every node, however crude: Omi simulator → app device layer → app → server API → transcription → underlay → assembly → loop on the fixture model → API → app shows the reply and the heard row. CI running on amd64 and arm64 runners and an Android emulator. Re-run every probe in `tests/probes/` on arm64 and update `build/prefabs.jsonl` if results differ.
- **Phase 1 — memory:** n.store, n.grammar, n.recall, n.assemble, n.underlay until TP-T1…T17 pass; A01 on the installer the skeleton already uses.
- **Phase 2 — the agent:** n.api, n.loop, n.models, n.tools, n.browser (A02, A03, A04, A08, A11, A12, A14, A15, A16).
- **Phase 3 — Omi and app:** n.app_device, n.audio, n.app (A05, A06, A07, A09, A13).
- **Phase 4 — feeds and ops:** n.feeds, n.ops (A10, A17, A18, A20).
- **Phase 5 — firmware and finish:** n.firmware, A19 if the live key exists, a final audit, `build/aar.md`.

**Audit subagent** (at every phase boundary and before the final acceptance run): a fresh subagent reads the constitution, all node files, seams, prefabs, gaps and the test-result history (not the ledger) and returns only JSON findings `{"check","target","node","verdict","evidence","action"}`, verdict `clean | suspect | violation`, evidence citing a count, file or run id. Checks: two BINDING guarantees on one node that can't both hold; seams still `pending` whose upstream is done; assertions with no `first_red` in their history; acceptance clauses an empty store would satisfy; docstrings naming stages the code doesn't call; unprobed prefabs in use; demons with `negative_check: pending` on a finished node. No prose findings during the build.

**After-action review** (`build/aar.md`, the build's only free-text artifact, written as bullets from the records, not from memory; every claim cites a ledger id, gap id, seam or count): counts (nodes, ledger entries, gaps opened/closed, seams driven/pending, tests, scenario status); rediscovery rate; predictions named before implementation that were built anyway; non-events recovered and whether instrumentation or audit caught them; what held; what to change, ranked by damage still possible; caveats.

At the end of any phase that leaves A01–A03 green, tag a pre-release `v0.0.N` with the server artifacts and the APK, so the owner always has the newest working build. Tag `v0.1.0` when the intent predicate holds. Update the Status table in `README.md` from CI results at each tag.

## How to write code here

Lazy senior developer mode (after Ponytail, MIT, github.com/DietrichGebert/ponytail): solve the whole problem with the least new code. Take the first option that fully works: does it need to exist → already in this codebase → standard library or platform → an already-chosen dependency → one clear line → the minimum code that works. A shortcut with a known limit gets a comment: `shortcut: <the limit>, <when to upgrade>`.

Never cut: validation at trust boundaries, error handling that prevents data loss, security, accessibility, anything a scenario or guarantee requires.

Standing instructions from the owner:

- **Nothing arbitrary.** Every number comes from a spec, a protocol, a cited result or the store's own fitted history. If you had to pick it, it's a gap.
- **Structure over behavior.** Make violations impossible rather than instructing against them.
- **Features fall out of the structure.** Prefer a component that serves two or more roles to one more component.
- **No canned replies** in anything conversational: the owner answers in their own words and the system interprets them. No Yes/No/Send buttons on drafts, questions or heard lines.
- **Docs stand alone.** When you edit `docs/`, describe what is, never what changed or what came before. Write docs as bullets of information, not prose.
- **Verify before you decide.** Read the actual code, the current upstream docs and the probe results before choosing; never build on a remembered API.
- **Settled stays settled.** If you see a better approach than a spec or a settled decision, say so in one sentence in a gap, then build what is specified.
- **Never commit a secret.** The live model key exists only as the repository secret `PLEBIAN_LIVE_KEY`.

Where the code lives is yours to decide; the suggested shape is `server/` (one Go module), `app/` (Flutter; assets already in `app/assets/brand/`), `firmware/` (the Omi patch and its CI), `tests/` (acceptance, fixtures, seams, probes).

Third-party code you port or vendor keeps its notice; list it in `NOTICE.md`.
