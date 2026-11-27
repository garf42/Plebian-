<p align="center"><img src="docs/design/wordmark-on-forest.png" width="420" alt="Plebian"></p>

# Plebian

An agent you own. One ongoing conversation with an AI that works for you, remembers like a person does, hears your day through an Omi you wear, reads your mail, texts and calendar, and acts on the internet on your behalf — on your word, or on standing you've given it. It runs on a box you own and talks to whichever AI model you choose.

- **Open source** — no telemetry; every release built by public CI.
- **Model agnostic** — any Anthropic-style or OpenAI-compatible provider, local models included.
- **Your data** — one directory on your box (or your own bucket). Nothing goes to Omi's servers or to us.
- **For working people** — a Pi and an SSD; code watches your feeds for free and the model is only called when something needs it.

## Status

| Scenario | State |
|---|---|
| A01–A20 | not built yet — see `build/constitution.md` |

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
   - It installs into `/opt/plebian` (`--root` to change), creates two system accounts, and downloads the speech models from sherpa-onnx's releases, checked against pinned hashes.
   - It prints a Tailscale sign-in link; once the box is on your tailnet it prints a pairing code.
   - `sudo /opt/plebian/bin/plebian pair` prints a new code. `sudo /opt/plebian/bin/plebian uninstall` removes everything.
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
