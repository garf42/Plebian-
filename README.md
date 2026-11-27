<p align="center"><img src="docs/design/wordmark-on-forest.png" width="420" alt="Plebian"></p>

# Plebian

An agent you own. One ongoing conversation with an AI that works for you, remembers like a person does, hears your day through an Omi you wear, reads your mail, texts and calendar, and acts on the internet on your behalf — on your word, or on standing you've given it. It runs on a box you own and talks to whichever AI model you choose.

- **Open source** — no telemetry; every release built by public CI.
- **Model agnostic** — any Anthropic-style or OpenAI-compatible provider, local models included.
- **Your data** — one directory on your box (or your own bucket). Nothing goes to Omi's servers or to us.
- **For working people** — a Pi and an SSD; code watches your feeds for free and the model is only called when something needs it.

## Status

From CI run 45987993187 (commit 55c6853), the same on amd64 and arm64, each scenario run twice:

| Scenario | What it proves | State |
|---|---|---|
| A01 | install leaves nothing outside its root but its accounts and the system packages the box lacks; uninstall removes all but those packages | pass |
| A02 | pairing; an unpaired client reads nothing | pass |
| A03 | a message gets a reply; every sealed turn rebuilds byte-exact | pass |
| A04 | a failed step becomes a redline, its repeat held once | pass |
| A05 | live Omi audio lands as heard rows with speakers | pass (server half; the app half has not run on the emulator) |
| A06 | offline audio lands at its true wall time | pass (server half; the app half has not run on the emulator) |
| A07 | an unnamed voice opens one question | pass |
| A08 | overheard words never act; a mail wakes the agent | fail |
| A09 | a heard fact is answered with who and when | pass |
| A10 | mail, calendar and texts land; a PDF is read as outside words | pass |
| A11 | research answered with its URLs | pass |
| A12 | a browser order is held and drafted | pass |
| A13 | the clock opens the night's entry | fail |
| A14 | standing needs a signature and waits the owner's usual time | pass |
| A15 | both wire formats; fallback; per-model budget | pass |
| A16 | kill -9 mid-turn loses nothing; a silent feed asks once | fail |
| A17 | a backup restores byte-identical | pass |
| A18 | A05 with an S3 bucket | pass |
| A19 | one real exchange on the owner's provider | blocked (no live key) |
| A20 | an update replays, deploys and rolls back | pass |

## Install

1. **Server**: on a Raspberry Pi 5 (or any Debian/Ubuntu machine with systemd):
   ```
   v=v0.0.1                            # the newest tag on the Releases page
   arch=$(dpkg --print-architecture)   # arm64 on a Pi 5, amd64 on a PC or VM
   curl -LO https://github.com/garf42/Plebian-/releases/download/$v/plebian-linux-$arch.tar.gz
   curl -LO https://github.com/garf42/Plebian-/releases/download/$v/plebian-linux-$arch.tar.gz.sha256
   sha256sum -c plebian-linux-$arch.tar.gz.sha256 && tar xzf plebian-linux-$arch.tar.gz
   sudo ./plebian/bin/plebian install
   ```
   - It installs into `/opt/plebian` (`--root` to change) and creates two system accounts.
   - It downloads the speech models (sherpa-onnx's releases, pinned hashes) and the release's browser bundle (checked by its `.sha256`).
   - Through `apt`, and only what the box lacks: Chromium's libraries (Playwright's list, with fonts and Xvfb) for the browser, and `poppler-utils` (pdftotext) for the agent's tools.
   - It prints a Tailscale sign-in link; once the box is on your tailnet it prints a pairing code.
   - `sudo /opt/plebian/bin/plebian pair` prints a new code.
   - `sudo /opt/plebian/bin/plebian uninstall` removes the root and the two accounts and leaves the system packages.
2. **App**: on your Android phone, install the Tailscale app, then install `plebian.apk` from the same release.
3. **Pair and connect**: enter the code, then go through the Setup cards: model, Omi, mail, calendar, texts, storage, backups.

First run with the real Omi: `tests/probes/omi/first_run_checklist.md`.

## Docs

- `docs/plebian.md` — what it is, the app, the Omi, where it runs, setup
- `docs/agent.md` — how the agent works and why each decision was made
- `docs/tracer-paper-memory.md` — the memory
- `build/` — the build plan (constitution, node contracts, seams, probes)

## License

AGPL-3.0. Third-party notices in `NOTICE.md`.
