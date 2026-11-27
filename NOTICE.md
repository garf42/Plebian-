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
- **Vollkorn, Atkinson Hyperlegible Next, Atkinson Hyperlegible Mono** — SIL Open Font License 1.1. The wordmark is set in Monarcha (Isaco Type, commercial) and ships only as an image.

Speech models (transcription, segmentation, speaker embeddings) are downloaded at install with their licenses recorded beside them.
