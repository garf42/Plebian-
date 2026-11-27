# Probes

Each probe exercises one external dependency at Plebian's own dimensions, twice, and prints results that must match across runs. Results are recorded in `build/prefabs.jsonl`; re-run on arm64 in CI during phase 0.

| Probe | Run |
|---|---|
| SQLite at a million-turn store | `cd sqlite && go run . /tmp/probe.db` |
| Speech: two voices, diarization, transcription | `python3 -I speech/probe_speech.py <models-dir>` (models from sherpa-onnx releases: Parakeet-TDT 0.6B v2 int8, pyannote segmentation 3.0, TitaNet-small; voices from `espeak-ng`) |
| Browser: held unsafe requests, owner input, cookies | `python3 -I browser/probe_browser.py` (Playwright for Python, Chrome) |
| MCP client ↔ Playwright MCP over CDP | `cd mcp && npm i @playwright/mcp && go run .` |
| Backups | `RESTIC=restic backup/probe_restic.sh` |
