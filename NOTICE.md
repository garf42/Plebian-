# Notices

Plebian is licensed under the GNU Affero General Public License v3.0 (`LICENSE`).

It builds on the following work, each under its own license:

- **Omi** (Based Hardware Contributors) — MIT. Plebian's Omi device layer ports the protocol handling from Omi's app at commit f2f8bbc4 (`app/lib/services/devices/`: UUIDs, frame reassembly, button codes, codec ids, the three storage flows; into `app/lib/device/`, attribution in each file's header), its optional firmware change patches Omi's firmware (`omi/firmware/`), and the Omi simulator (`tests/fixtures/omi/`) ports firmware logic from `omi/firmware/omi/src/lib/core/` (transport.c fragmentation, custody.c record packing and advance rules, storage.c ring protocol, button.c, haptic.c) and the storage wire shapes Omi's app parses (`app/lib/services/devices/`, `app/lib/services/wals/`). https://github.com/BasedHardware/omi
- **Flutter packages** (each under its own license, used unmodified; none vendored): universal_ble (BSD-3-Clause), biometric_signature (MIT), material_ui, flutter_local_notifications, flutter_secure_storage, mobile_scanner, just_audio, http, web_socket_channel, path_provider, crypto (BSD-3-Clause / MIT / Apache-2.0).
- **mautrix-gmessages / libgm** (Tulir Asokan and contributors) — AGPL-3.0. Google Messages linked-device protocol. https://github.com/mautrix/gmessages
- **Ponytail** (Dietrich Gebert) — MIT. The build's decision ladder is adapted from it. https://github.com/DietrichGebert/ponytail
- **Playwright MCP** and **Playwright** (Microsoft) — Apache-2.0. Shipped unmodified in each release's browser bundle. https://github.com/microsoft/playwright-mcp
- **Node.js** (OpenJS Foundation and contributors) — MIT. The official build, checked against nodejs.org's checksum, shipped in the browser bundle. https://nodejs.org
- **Chromium** (The Chromium Authors) — BSD-3-Clause and the licenses of its components (listed in its `chrome://credits`). Chrome for Testing as Playwright pins it, shipped unmodified in the browser bundle. https://www.chromium.org
- **MCP Go SDK** (Model Context Protocol contributors) — Apache-2.0 / MIT. https://github.com/modelcontextprotocol/go-sdk
- **sherpa-onnx** (k2-fsa) — Apache-2.0. https://github.com/k2-fsa/sherpa-onnx
- **libopus** (Xiph.Org Foundation and contributors) — BSD-3-Clause. Linked statically into the server to decode the Omi's audio. https://opus-codec.org
- **restic** — BSD-2-Clause. https://github.com/restic/restic
- **Tailscale** (Tailscale Inc. and contributors) — BSD-3-Clause. The server joins the owner's tailnet through its embedded library, tsnet. https://github.com/tailscale/tailscale
- **bubblewrap** (its contributors) — LGPL-2.0-or-later. Copied unmodified from the build runner's Ubuntu 22.04 package into the release and run as the tools sandbox. https://github.com/containers/bubblewrap
- **go-imap, go-sasl, go-message, go-smtp, go-ical** (Simon Ser and contributors) — MIT. The mail feed's IMAP client and the calendar feed's iCalendar decoder; go-smtp serves the build's mail fixture. https://github.com/emersion
- **rrule-go** (Teambition) — MIT. Calendar recurrence expansion. https://github.com/teambition/rrule-go
- **modernc.org/sqlite** (the modernc authors) — BSD-3-Clause. The store. https://gitlab.com/cznic/sqlite
- **coder/websocket** (Coder) — ISC. https://github.com/coder/websocket
- **go-qrcode** (Tom Harwood) — MIT. The pairing code. https://github.com/skip2/go-qrcode
- **wordfreq** (Robyn Speer) — data CC-BY-SA 4.0, code Apache-2.0. The English word frequencies the grammar prices words by (`server/grammar/wordfreq_en.tsv.gz`). https://github.com/rspeer/wordfreq
- **poppler** (pdftotext) — GPL-2.0-or-later. Used from the host when installed; not shipped.
- **Vollkorn, Atkinson Hyperlegible Next, Atkinson Hyperlegible Mono** — SIL Open Font License 1.1. The wordmark is set in Monarcha (Isaco Type, commercial) and ships only as an image.

Speech models (transcription, segmentation, speaker embeddings) are downloaded at install with their licenses recorded beside them.
