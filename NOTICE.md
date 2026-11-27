# Notices

Plebian is licensed under the GNU Affero General Public License v3.0 (`LICENSE`).

It builds on the following work, each under its own license:

- **Omi** (Based Hardware Contributors) — MIT. Plebian's Omi device layer ports the protocol handling from Omi's app (`app/lib/services/devices/`), and its optional firmware change patches Omi's firmware (`omi/firmware/`). https://github.com/BasedHardware/omi
- **mautrix-gmessages / libgm** (Tulir Asokan and contributors) — AGPL-3.0. Google Messages linked-device protocol. https://github.com/mautrix/gmessages
- **Ponytail** (Dietrich Gebert) — MIT. The build's decision ladder is adapted from it. https://github.com/DietrichGebert/ponytail
- **Playwright MCP** (Microsoft) — Apache-2.0. https://github.com/microsoft/playwright-mcp
- **MCP Go SDK** (Model Context Protocol contributors) — Apache-2.0 / MIT. https://github.com/modelcontextprotocol/go-sdk
- **sherpa-onnx** (k2-fsa) — Apache-2.0. https://github.com/k2-fsa/sherpa-onnx
- **restic** — BSD-2-Clause. https://github.com/restic/restic
- **Tailscale** (Tailscale Inc. and contributors) — BSD-3-Clause. The server joins the owner's tailnet through its embedded library, tsnet. https://github.com/tailscale/tailscale
- **bubblewrap** (its contributors) — LGPL-2.0-or-later. Built by the release CI and run unmodified as the tools sandbox. https://github.com/containers/bubblewrap
- **go-imap, go-sasl, go-message, go-smtp, go-ical** (Simon Ser and contributors) — MIT. The mail feed's IMAP client and the calendar feed's iCalendar decoder; go-smtp serves the build's mail fixture. https://github.com/emersion
- **rrule-go** (Teambition) — MIT. Calendar recurrence expansion. https://github.com/teambition/rrule-go
- **modernc.org/sqlite** (the modernc authors) — BSD-3-Clause. The store. https://gitlab.com/cznic/sqlite
- **coder/websocket** (Coder) — ISC. https://github.com/coder/websocket
- **go-qrcode** (Tom Harwood) — MIT. The pairing code. https://github.com/skip2/go-qrcode
- **wordfreq** (Robyn Speer) — data CC-BY-SA 4.0, code Apache-2.0. The English word frequencies the grammar prices words by (`server/grammar/wordfreq_en.tsv.gz`). https://github.com/rspeer/wordfreq
- **poppler** (pdftotext) — GPL-2.0-or-later. Used from the host when installed; not shipped.
- **Vollkorn, Atkinson Hyperlegible Next, Atkinson Hyperlegible Mono** — SIL Open Font License 1.1. The wordmark is set in Monarcha (Isaco Type, commercial) and ships only as an image.

Speech models (transcription, segmentation, speaker embeddings) are downloaded at install with their licenses recorded beside them.
