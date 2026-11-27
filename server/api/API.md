# Server API v1

The app's only surface on the server. JSON over HTTP; one WebSocket for events, one for live audio, one for the browser live view.

## Listener

- Tailnet address (tsnet) and `127.0.0.1:<free port>`; nothing else.
- The port in use: `<root>/run/api.json` → `{"addr": "127.0.0.1:PORT", "tailnet": "host.tailnet.ts.net:PORT" | ""}`.

## Auth

- `GET /v1/version` — `{"commit": "<sha>", "version": "<tag or dev>"}`; no auth; reads nothing from the store.
- `POST /v1/pair` — `{"code": "<pairing code>", "public_key": "<base64 SPKI DER, ECDSA P-256>", "name": "<device name>"}` → `{"device_id": "...", "token": "..."}`. A code works once.
- Every other endpoint: `Authorization: Bearer <token>`. Missing or unknown token → `401`, empty body, nothing read or written.
- Signed admin actions: header `X-Plebian-Signature: <base64 ASN.1 ECDSA-SHA256 over the exact request body>`, body carries `"nonce"` (unique per device) and `"at"` (RFC 3339). A bearer token alone → `403`.

## Owner

- `POST /v1/messages` — `{"client_msg_id": "...", "text": "...", "sent_at": "<RFC 3339>", "quote": {"kind": "heard|draft|question|tap|line", "id": "..."} | null}` → `{"id": "..."}`. Idempotent on `(device_id, client_msg_id)`.
- `GET /v1/thread?before=<event id>&limit=<n>` — `{"events": [Event...]}`, oldest first.
- `GET /v1/days?before=<YYYY-MM-DD>&limit=<n>` — `{"days": [{"date", "heard", "taps", "messages", "open"}]}`, newest first.
- `GET /v1/days/{YYYY-MM-DD}` — `Page`.
- `GET /v1/today` — the current `Page` (day cut at 4 am in the owner's time zone).
- `GET /v1/taps?before=<id>&limit=<n>` — `{"taps": [Tap...]}`, newest first. `GET /v1/taps/{id}` — `Tap` with `"stretch": HeardRow`.
- `POST /v1/taps` — `{"at": "<RFC 3339>", "how": "omi|app", "line": {"kind": "heard|event", "id": "..."} | null}` → `Tap`.
- `PATCH /v1/taps/{id}` — `{"note": "..."}` → `Tap`.
- `GET /v1/open` — `{"questions": [Item], "drafts": [Item], "orders": [Item]}`.
- `GET /v1/search?q=<text>` — `{"thread": {"count", "first", "last", "hits": [Hit]}, "heard": {"count", "first", "last", "hits": [Hit]}}`.
- `GET /v1/blobs/{sha256}` — the original bytes (audio as Ogg Opus; messages as received); supports `Range`.

## Events

- `GET /v1/events?after=<event id>` — WebSocket. Server → app text frames, one `Event` each, in order. Backlog after `after` first, then live.
- `Event`: `{"id", "at", "type", ...}`; `type` is one of:
  - `message` — `{"from": "owner|agent", "text", "notify": bool}`; `notify` true for every message the agent starts (not a reply).
  - `draft` — `{"draft_id", "step", "shown": "<exact bytes as UTF-8, decoded for reading>", "record": {"as_drafted", "changed", "turned_down"}, "state": "waiting|ran|failed|declined"}`.
  - `question` — `{"question_id", "text", "state": "open|closed"}`.
  - `step` — `{"tool", "summary", "state": "running|done|failed|held|interrupted", "browser": bool}`.
  - `heard` — `HeardRow` (a stretch, message or entry landed).
  - `tap` — `Tap`.
  - `speech` — `{"minute": "<RFC 3339 minute>", "seconds": n}` (live ribbon fill).
  - `omi` — `{"state": "live|syncing|out_of_range", "battery", "firmware", "waiting_files"}`.
  - `presence` — `{"working": bool}`.

## Shapes

- `Page`: `{"date", "cut": "<RFC 3339 start>", "entry": [Line], "thread": [Event], "taps": [Tap], "heard": [HeardRow], "ribbon": {"hours": [{"hour": 0-23, "seconds": n}], "taps": ["<RFC 3339>"]}}`.
- `Line`: `{"id", "text", "held": bool, "heard_by": "speaker"|null}` — `held` true until the owner settles it.
- `HeardRow`: `{"id", "source": "talk|mail|text|calendar", "at", "end", "lines": [{"speaker", "text", "at"}], "settled": bool, "blob": "<sha256>"}`.
- `Tap`: `{"id", "at", "how": "omi|app", "title": "...", "note": "...", "stretch_id": "..."|null}`.
- `Item`: `{"id", "kind": "question|draft|order", "text", "at", "shown"?}`.
- `Hit`: `{"id", "kind", "text", "at", "who"}`.

## Omi audio

- `GET /v1/audio/live?codec=<20|21>&offset_ms=<device clock offset>` — WebSocket. App → server binary frames, one Opus frame each: `u16 LE packet number`, `i64 LE arrival unix ms`, payload. Server → app text frames `{"speech": {"minute", "seconds"}}`.
- `POST /v1/audio/files` — one stored offline file, as the Opus frames the app's port of Omi's storage sync read out of it, in order: body = repeated `u16 LE length` + frame. Headers `X-Omi-File-Index`, `X-Omi-File-Timestamp` (device unix milliseconds of the first frame), `X-Omi-Clock-Offset-Ms` (wall = device + offset), `X-Omi-Codec`. → `{"sha256": "<sha256 of the body>"}` only once the body is durable in the blob store; the app deletes the file from the Omi only when this answer equals the sha256 it computed over the body it sent.
- `POST /v1/omi/state` — `{"connected": bool, "battery": n, "firmware": "...", "clock_offset_ms": n, "waiting_files": n}`.

## This week and Setup

- `GET /v1/week` — `{"brief": [{"id", "lane": "breakage|security|ideas|standing", "text", "replay": "...", "state"}], "health": {...}, "spend": [{"model", "day", "in", "out", "cached"}]}`.
- `GET /v1/setup` — `{"cards": [{"id": "server|pair|model|omi|mail|calendar|texts|storage|backups|updates", "status": "...", "required": bool}]}`.
- `POST /v1/setup/model` — `{"base_url": "..."|"", "api_key": "..."|""}` → `{"provider_id", "format": "anthropic|openai", "models": [{"id", "context"}]}`. `POST /v1/setup/model/select` — `{"provider_id", "model", "role": "main|sub|stand_in|fallback"}`.
- `POST /v1/setup/mail` — `{"imap": "host:port", "smtp": "host:port", "user", "password"}`.
- `POST /v1/setup/calendar` — `{"url"}`.
- `POST /v1/setup/texts` — `{}` → `{"emoji": "..."}` once paired cookies are in hand.
- `POST /v1/setup/storage` — `{"bucket": {"endpoint", "bucket", "key", "secret", "region"} | null}`.
- `POST /v1/setup/backups` — `{"second_disk": "/path"|"", "offsite": "<restic repository>"|""}` → `{"key": "..."}` shown once.
- `POST /v1/setup/updates` — `{"github_token", "mechanic": "<command>"|""}`.
- Signed: `POST /v1/admin/grant` `{"step", "nonce", "at"}`; `POST /v1/admin/approve` `{"item", "nonce", "at"}`; `POST /v1/admin/deploy` `{"candidate", "nonce", "at"}`.

## Browser live view

- `GET /v1/browser/live` — WebSocket. Server → app binary JPEG frames. App → server text frames: `{"type": "take"|"give"}`, `{"type": "mouse", "x", "y", "event": "down|up|move"}`, `{"type": "key", "text"}`. Nothing typed is recorded.
