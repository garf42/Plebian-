# Plebian

An agent you own. Plebian is one ongoing conversation with an AI agent that works for you, remembers like a person does, hears your day through an Omi you wear, reads your mail, texts and calendar, and acts on the internet on your behalf — on your word, or on standing you've given a step. It runs on a box you own (a Raspberry Pi at home by default, or a cloud machine you rent) and talks to whichever AI model you choose.

The name is the brief: built for working people, not for the companies that sell to them.

## Principles

Each is a property the built system must show.

- **Open source.** All code public under the repository's license. No telemetry, no analytics, no crash reporting to anyone. Every release is built by public CI from a tagged commit.
- **Model agnostic.** Any provider speaking the Anthropic Messages API or the OpenAI-compatible Chat Completions API works, local runtimes included (llama.cpp, Ollama, vLLM). Switching models is a setting, not a rebuild. Memory never depends on a model (Tracer Paper Memory, `docs/tracer-paper-memory.md`).
- **Data ownership.** Everything Plebian keeps lives in one directory on the owner's box (or the owner's own bucket). Export is copying that directory. The only outbound traffic is: the model provider the owner configured, the feeds the owner connected, the web pages the agent visits on the owner's behalf, backups to the owner's destinations, the owner's tailnet (its control server and encrypted relays), and the repository (release checks; issues and the mechanic's branches when Updates is set). Nothing goes to Omi's servers or to Plebian's authors.
- **For working people.** Runs on about the cost of a Pi and an SSD. Watching feeds costs nothing; the model is called only when something needs it. Plain words everywhere (no jargon in the app). Keeps working through dead zones (the Omi records offline; the phone buffers). Maintains itself through a weekly brief the owner approves item by item.
- **Acts only on the owner's word or standing.** Reads, drafts and prepares freely. Anything that would change something past its own machine — send, post, pay, sign, delete, submit — is shown exactly as it would run and waits for the owner's word, until the owner gives that step standing. Anything shaped by someone else's words (overheard talk, a web page, an email) is always drafted, and the agent goes out onto the web only on the owner's word, so a stranger's mail can't send it anywhere.

## The three parts

- **Server** — one program on the owner's box: listens to the Omi through the phone, transcribes, watches mail/texts/calendar, runs the agent and its memory, drives a browser, keeps backups.
- **App** — Plebian on the owner's Android phone: pairs with the Omi the way Omi's own app does, streams its audio to the server, and is where the owner talks to the agent and reads their days back.
- **Omi** — the wearable recorder. Unmodified it works fully; one optional firmware change adds a long buzz when a double-tap can't reach the phone.

---

## PL-01 · The app in your hand

Tabs: **Today · Journal · Open**. Header: the Plebian wordmark on the logo gradient, an **Omi chip** (live / syncing / out of range) and a **gear** that opens **This week** (the brief, health, spend and the Setup cards).

- **Today** — one ongoing thread with the agent. Each night's page tops it: what the day held, the marks, and what needs the owner's word.
- **Journal** — every day on its own page, newest first. **Marks** lists every moment the owner marked, across all days. **Search** covers everything said and heard.
- **Open** — what waits on the owner: the agent's questions, its drafts, and kept orders. Only the owner's own words close one; a draft runs exactly as shown.
- A draft shows exactly what would go out (a message as it would be sent; a site's fields as they would be submitted, decoded for reading, nothing left out) with its step's record: run as drafted, changed, turned down. It runs as shown or not at all.
- Every message the agent starts arrives as a phone notification; tapping it opens Today at that message.
- The Omi pairs with the phone as it does with Omi's app; Plebian sends what it hears to the server as it comes in.
- Heard lines carry the speaker's name and sit lighter, on a dashed rule, until the owner says something about them. Any reply in the owner's own words settles the line; tapping a line only quotes it into the thread.
- No canned replies anywhere in the conversation: no Yes/No/Send buttons on drafts, questions or heard lines. Tapping quotes; the owner answers in their own words; the agent interprets.

| | Plebian | Claude Duet | Omi app |
|---|---|---|---|
| Main surface | One thread, cut into day pages | One ongoing thread | A list of conversations |
| Waiting on you | Open; only your words close one | Tasks; only you mark one done | Action items |
| Each day | The night's entry, verbatim day behind every line; a morning look once you've asked for one twice | A morning brief | A summary per conversation |
| Capture | The Omi through your phone, plus mail, texts, calendar — all to your box | Your connected accounts | The pendant, through your phone to Omi's servers |
| Checking | Code watches every feed at no model cost; the agent is called only when what lands touches something open | Every few hours, counted against your usage | — |
| Before acting | Drafts and waits for your word; a step with standing runs after a short stop window, unless someone else's words were in view | Shows the plan and waits for your OK | — |
| Its browser | On your box; you can watch it and take the wheel | In its cloud; can't be viewed | — |
| Grows with use | The clock takes over what you ask for twice; steps earn standing on their record | You choose what to hear about and how often | — |

---

## PL-02 · How a day fills

1. **Omi** records all day and streams to Plebian on the phone over Bluetooth.
2. **Omi** double-tap marks the moment. In the phone's range the app writes the **mark** at once and gives the Omi a short buzz (100 ms); out of range the Omi gives itself a long one (500 ms, firmware change). A single tap does nothing (so a bump can't mark); a three-second hold powers the Omi off.
3. **Phone** sends the audio on to the server as it arrives (Opus frames, never decoded on the phone). Everything Omi's app would send to Omi's servers goes to the owner's server.
4. **Phone** — out of range, the Omi records to its own storage and syncs when back, stamped with its own clock, as it does with Omi's app.
5. **Server** transcribes as audio arrives and labels each speaker. A voice it hears in a second conversation with no name (the owner's own included) becomes a question in Open; once the owner names it in their own words it is known from then on. Each conversation lands as it ends, as underlay rows; each mark becomes a pointer into one.
6. **Server** fetches mail, texts and calendar by code as they arrive; they land the same way. The agent is called only when what lands (a calendar entry coming due included) lights an open loop or a kept order.
7. **Memory** — the night's read gets the day with what is open, the kept orders and the marks. The agent writes the entry. A heard answer comes back as a question; nothing heard closes anything.
8. **App** — the page is up: the entry on top, the marks on the ribbon. Whatever the owner says back about a heard line settles it.

- Conversations and messages land through the day. The night's read runs once, at the hour the owner has come to ask for it, over all of them (the clock learns it from two asks; until then the owner asks in their own words).
- Plebian's background service holds the Omi's Bluetooth link and the line to the server, so audio flows and notifications arrive without any push service.

---

## PL-03 · The page

- A page is a day, cut at 4 am, so a late night stays with its day.
- Layers stack top to bottom like the tracing paper beneath: **Entry**, **With Plebian** (the owner's talk with the agent), **Marks** in the margin, and the **Heard** day beneath.
- The **ribbon** across the top is the day by the hour: how much talk there was and where the marks fall.
- The heard day is only ever peeked at (lift the page). It never mixes into the thread.

| In the app | In the store | Rule |
|---|---|---|
| Page | Everything stamped that day, in both bodies | Cut at 4 am, owner's time zone |
| Entry | The agent's lines from the night's read | Held at ½, covering nothing, until the owner settles them |
| With Plebian | Sheets stamped that day | As the thread holds them |
| Heard | Underlay rows: talk, mail, texts, calendar | Never written into the thread |
| Mark | A pointer row: the time, the stretch around it, the owner's note | The night's read reports every mark |
| Open | Open loops, kept orders, drafts waiting on the owner's word | Only the owner's words close one; a draft runs exactly as shown |
| Search | The lamp, over both bodies | Counts shown per body: "never said here, heard once" |
| The owner's reply | Anything the owner says about the line, in their own words | Retraces and settles it; a corrected heard word refits speech's edit costs |

---

## PL-04 · Marks

- Two ways to mark, both writing one row: double-tap the Omi, or long-press any line in the app. The row holds a time, the stretch of talk around it, and an optional note.
- A double-tap reaches the phone at once, so the mark is on the day's ribbon right away. Short buzz: saved. Long buzz: the phone was out of range — the audio still syncs, and long-pressing the line later marks it. The two buzzes are the Omi's own shortest and longest (100 and 500 ms).
- The night's read reports every mark; that line becomes the mark's title, so search finds a mark by what was said.
- Three ways back to a mark: Journal → Marks, newest first; the pick on each day's ribbon; or search.
- A mark says the moment mattered, not that the words were heard right. Heard words still wait until the owner has said something about them.
- **Mark screen:** the pick, day and time, "tapped on the Omi" or "pressed in the app", the line, a player for the stretch with waveform and playhead, the verbatim talk around the mark (the marked line highlighted). Tapping the mark's line quotes it into the thread; the owner asks in their own words. A note is typed by the owner and rides on the mark.

---

## PL-05 · Look and motion

Taken from the wordmark (`docs/design/wordmark.png`: *Plebian* in a working italic serif whose *n* ends in a pick). Tokens: `docs/design/tokens.json`. Visual reference with a working phone mockup: `docs/design/mockup.html`.

- The ground is the logo's gradient, five stops from dawn slate to forest. **Ore** is the one warm color and belongs to marks, so a mark stands out on any page.
- Pages are tracing paper laid on that ground: soft white cards, 14 px radius, a light double shadow.
- Heard text sits a shade lighter on a dashed rule until settled; settling inks the rule solid.
- Type: **Alegreya** italic for names and dates (close to the wordmark's hand); **Alegreya Sans** for reading; **IBM Plex Mono** for times and counts. All three under the SIL Open Font License, bundled with the app.
- The agent's messages are plain text on the page; the owner's sit in steel bubbles. The agent's stamp reads "Plebian · time".
- Every animation stands for something that happened:
  - **Lay** — a new page is laid over the last, the way trace goes down on a drawing (screen transitions).
  - **Strike** — a mark lands: the pick taps the ribbon at its minute, chips fly.
  - **Live** — the ribbon's bars grow as the day is heard.
  - **Peek** — lift the page to see the heard day beneath.
  - **Presence** — while the agent works, the gradient drifts along the composer's top edge.
  - **Settle** — once the owner has answered a heard line, its dashed rule inks solid.
- Touch feedback runs 180–320 ms. The night's read moves at the pace of the real work. With reduced motion on, each animation becomes a short fade.
- Accessibility: every control reachable by TalkBack with a spoken label; text scales with the system font size; color is never the only carrier of meaning (heard lines also carry "heard · speaker").

---

## PL-06 · This week (admin)

Research → brief → the owner approves item by item → a mechanic builds on a branch → replay → deploy.

- A sweep (weekly by default; the clock can learn another cadence) covers: the agent's own breakage, security notices for everything Plebian runs, releases of its dependencies and of Omi's firmware and app, new model releases from the owner's providers, and ideas from other agents (Claude Duet among them). It runs as an ordinary agent task with web access.
- The brief has four lanes: **Breakage**, **Security**, **Ideas**, **Standing**. Each item says what changes and why, and what replay found.
- **Standing is granted here and nowhere else.** The brief ranks steps by record; once the owner has given one step of a tool standing, that tool's other steps are offered as their record reaches that line. Grants require the owner's fingerprint on the phone. The agent can hand standing back from the thread, never take it.
- An approved code change goes to the **mechanic**: the coding agent the owner configured (Setup → Updates), working headless on a branch of the owner's copy of the repository; with none configured, the item becomes an issue on the repository for the owner to hand to any agent.
- **Replay gates every update:** the candidate rebuilds every page and request since the last deploy from the store; if any differs from what the owner lived, the update waits with the difference shown.
- **Deploy** is one tap; the previous version is kept and comes back on a failed start.
- Each sweep restores last night's backup to scratch space and replays a page from it, so the backup is proven, not assumed.
- Omi firmware updates install from Plebian over Bluetooth, the way Omi's app installs them, carrying Plebian's one change.
- Hands-on work: a link to the owner's coding agent's remote session, where it has one.

---

## PL-07 · Where it runs

- **Default: a Raspberry Pi 5 at home** (8 GB or more, an SSD). Everything — transcription, memory, the agent loop, the browser, backups — runs on it. The model runs wherever the owner points it.
- **Cloud option:** the same program on any Linux machine the owner rents; audio and originals can live in any S3-compatible bucket the owner owns. Pi + bucket, or cloud machine + bucket, or cloud machine + its own disk: the same install, one setting each.
- **Isolation:** Plebian installs under one root directory with its own system users and their own services; it reads, writes and binds nothing belonging to other software on the box, and picks free ports. Uninstall removes all of it.
- **Storage:** the owner points the data directory at any drive mounted on the box (an SSD); the Setup screen shows free space and how many days that lasts at the current rate.
- **Reaching it:** the server joins the owner's Tailscale network as its own device through Tailscale's embedded library, so nothing is installed on the box outside Plebian's root; the phone runs the Tailscale app (WireGuard underneath; free for personal use; its relays only ever carry encrypted packets, so it works behind carrier-grade NAT). The phone reaches the server the same way at home and away. Headscale works instead for owners who want the control plane too. The server listens only on its tailnet address and localhost.
- **Pairing:** the server shows a code once; the app scans it, makes a device key in the phone's secure hardware, and registers it. Admin actions (grants, deploy) are signed with that key behind the owner's fingerprint.
- **Mail:** any IMAP/SMTP account. Gmail: turn on 2-Step Verification and make an app password (no Google Cloud project). Mail lands as it arrives (IMAP IDLE). A reply is drafted in the thread, complete down to its Date and Message-ID, and goes out over SMTP exactly as shown, only on the owner's word.
- **Calendar:** any iCalendar feed URL (Google: the calendar's secret address in iCal format), read as a feed. The agent adds or changes entries through its browser, held and drafted like any site action.
- **Texts:** Plebian links to Google Messages as a linked device, the way Messages for web does. Connecting: the owner signs into Google inside Plebian's own browser (from the app's live view), then taps the matching emoji in Google Messages on the phone. The phone must stay online (Messages relays through it). The pairing is unofficial; if Google breaks it the feed goes quiet and the silence watch says so.
- **Silence watch:** every feed is watched (for the Omi, the audio arriving); a quiet longer than any it has had becomes the agent's question in Open, so a dead sign-in can't pass for a slow week.
- **Backups:** each night the store, the audio and every message as received are copied to a second disk and to an offsite destination (any restic backend: another drive, S3-compatible bucket, SFTP), encrypted with a key only the owner holds.
- **Speech:** the phone's keyboard voice input fills the composer.

| Quantity | Size | Note |
|---|---|---|
| Audio | About 14 MB an hour | Opus at 16 kHz, the Omi's format; assumes about 32 kbps (estimate) |
| A 12-hour day | About 170 MB | Streamed as heard; out-of-range stretches sync on reconnect |
| A year of days | About 62 GB | On the data drive, outside the store |
| Omi storage | 512 MB, about 35 hours | Holds what it records out of range until it reconnects (estimate) |
| Omi battery | 10 to 14 hours recording | Omi's figure; a 16-hour day needs a top-up |
| Transcribing | About 5× faster than live on a Pi-class board | Parakeet-TDT 0.6B int8 on sherpa-onnx: 3.5% WER at 0.21 real-time factor (8 GB board); a better model can redo a day overnight and its transcript covers the first |
| Mail, texts, calendar | Small beside the audio | Kept as received, attachments included |

---

## PL-08 · Setup

From nothing to working, in the order the owner meets it. Each step is one card on the app's Setup screen with a status and one action.

1. **Server** — on the Pi (or cloud machine): one command installs Plebian under its own root, creates its users and services, downloads its speech models and browser, joins the owner's Tailscale network (it prints a sign-in link to open on the phone) and prints a pairing code. The same command updates and uninstalls.
2. **App** — on the phone, install the Tailscale app (same account) and the APK from the repository's Releases.
3. **Pair** — scan the pairing code; the app reaches the server over the tailnet, at home or away, and registers its device key (fingerprint).
4. **Model** — paste an API key, with the provider's endpoint URL unless it is Anthropic's (a local runtime needs only its URL); the wire format is whichever model listing the endpoint answers; pick the model from that list. Optional: a second provider to fall back on, a cheaper model for sub-agents.
5. **Omi** — pair over Bluetooth; the card shows live, battery, and anything waiting to sync. Voices, the owner's included, are learned by naming them when asked (PL-02).
6. **Mail · Calendar · Texts** — each its own card (PL-07).
7. **Storage · Backups** — data drive, second disk, offsite destination and the backup key (shown once; the owner keeps it).
8. **Updates** — a GitHub token for the owner's copy of the repository and, if the owner has one, the command that starts their coding agent headless (PL-06).

Every card can be redone any time from This week. Nothing is required except Server, App, Pair and Model; each other card adds one connection.

---

## PL-09 · The browser you can watch

- The agent's browser runs on the owner's box with its own profile, so sign-ins persist there and nowhere else.
- **Watch / take the wheel:** from any browser step in the thread, the owner opens a live view of the agent's page, can tap and type into it, and hands it back. While the owner holds the wheel the agent waits.
- **Signing in:** when a site needs the owner, the agent asks in the thread; the owner takes the wheel and signs in. What the owner types goes straight to the page and is never recorded or shown to the model.
- **Acting:** reading, searching, scrolling and filling fields run freely; an action that would send something to a site (a form submit, an order, a post) is held and drafted with exactly what it would send. Approved, that exact request goes out once.

---

## Sources

- Omi app–device protocol (BLE services, codecs, packet header): docs.omi.me/doc/developer/Protocol.
- Omi firmware `button.c` (tap under 300 ms; second tap within 600 ms is a double; 3 s hold powers off; taps notify only while a phone is connected) and `haptic.c` (1/2/3 → 100/300/500 ms): github.com/BasedHardware/omi.
- Omi app device code (MIT): `app/lib/services/devices/` — BLE UUIDs, storage list/read/delete commands, time sync, haptic write.
- Omi hardware: nRF5340, 512 MB on-board storage; offline recording 10–14 h per charge (omi.me).
- Omi firmware v3.0.17: near-accurate timestamps for offline recordings.
- Omi docs, custom STT: with your own transcriber, Omi's app still forwards transcripts to Omi's backend.
- Claude Duet (claude.com/blog/claude-duet): one thread; morning brief; checks every few hours; approval before sending, paying, signing, deleting; cloud browser not yet viewable.
- Parakeet-TDT 0.6B int8 on sherpa-onnx, Pi-class board: 3.51% WER, 0.21 RTF.
- mautrix-gmessages docs: Google-account pairing via the browser session's cookies and an emoji confirmation; the phone must stay online. Google removed QR pairing (May 2026).
- Google: app passwords for IMAP with 2-Step Verification; secret iCal address.
- Tailscale pricing (Personal plan free, unlimited devices); tsnet (tailscale.com/docs/features/tsnet): a tailnet device embedded in a Go program, signed in by link or auth key, any control server.
- Android developer verification: sideloading unregistered apps moves to an advanced flow — four countries from 30 Sep 2026, globally in 2027.
