# Gaps

Decisions taken by default. Reviewed in batch at phase boundaries, never mid-build.

- G001 — node: n.assemble — volatility: medium — last_reviewed: phase-0
  Tracer Paper rows marked Default are conventions, not derivations: strike line τ < 0.4, deadband 5%, spotlight cap ~10% of B split a third each, strata fill half with 40/60 deep/recent, sleep pass at 10 min idle or 500 turns, budget moves +8K / ×0.75 at most once per 20 outcomes with the CUSUM tuned to a 10-point rise at one false alarm per 1,000 outcomes, cold start at 20 samples with priors (median line 5 words; usual gap one minute, standing in for the owner's time to answer too; a loop held a day), the underlay lamp's 14 bits with one word past 10, stand-ins at half weight, a third shared for what followed and to close a failure, same season ±1 month, the clock's windows (90 minutes; a day and three hours), the heard wake's three lines and once in four days.
  Assumed: implemented exactly as specified, each as a named parameter the sleep pass can refit once the store has its own history. Reversible: yes (named parameters; replay shows any effect).
  Blocks: refit of each parameter on n.assemble, n.recall, n.grammar, n.underlay, n.loop.

- G002 — node: n.recall — volatility: low — last_reviewed: phase-0
  TP-T17 (Remembers) is specified against a real year-long thread, which won't exist for a year.
  Assumed: a synthetic year (scripted restatements, re-tell probes, struck steps) stands in; the real-thread version is adjudicated by the owner after a year of use. Reversible: yes.
  Blocks: TP-T17 on n.recall; n.proof's year fixture.

- G003 — node: n.app_device — volatility: medium — last_reviewed: phase-0
  The Omi's offline storage protocol differs by firmware (a length list on the storage control characteristic; the multi-file commands 0x10 list, 0x11 read, 0x12 delete).
  Assumed: port both paths as Omi's app has them, choosing by what the device answers. Reversible: yes.
  Blocks: A06 on n.app_device.

- G004 — node: n.firmware — volatility: medium — last_reviewed: phase-0
  The owner's Omi model and firmware version are unknown to the build.
  Assumed: the current consumer release tag; the app reads the device's firmware revision and offers the update only when the tag matches. Reversible: yes.
  Blocks: n.firmware.

- G005 — node: n.feeds — volatility: medium — last_reviewed: phase-0
  How long Google takes to reflect an edit in a secret iCal feed is unknown.
  Assumed: entries land when the feed shows them; the fetch cadence follows the owner's time to answer. Reversible: yes.
  Blocks: the calendar half of A10 and A13 (live use only; fixtures unaffected).

- G006 — node: n.feeds — volatility: high — last_reviewed: phase-0
  The Google Messages linked-device protocol is unofficial and can break without notice.
  Assumed: the current libgm; the silence watch reports a dead link; the weekly sweep watches libgm releases. Reversible: yes.
  Blocks: the texts half of A10 (live use only).

- G007 — node: n.tools — volatility: medium — last_reviewed: phase-0
  Whether bubblewrap, and Chromium's own sandbox, which needs the same user namespaces, run unprivileged on Raspberry Pi OS and the cloud images is unverified.
  Assumed: yes; if a host refuses user namespaces, tools still run as the tools user without namespaces, Chromium runs without its sandbox, and the install reports the weaker sandbox in This week. Reversible: yes.
  Blocks: the sandbox half of n.tools on arm64.

- G008 — node: n.ops — volatility: medium — last_reviewed: phase-0
  Custody of the APK release signing key.
  Assumed: the build generates a keystore and stores it as repository secrets with `gh` when permitted; otherwise releases are debug-signed and each app update needs a reinstall until the owner adds the secret. Reversible: yes.
  Blocks: in-place app updates in A20.

- G009 — node: n.ops — volatility: high — last_reviewed: phase-0
  Android developer verification reaches all certified devices in 2027; unregistered APKs then need the advanced sideloading flow.
  Assumed: sideloading works as today through 2026; before enforcement the owner registers the package name and signing certificate (limited distribution). Reversible: yes.
  Blocks: app installs after enforcement.

- G010 — node: n.api — volatility: low — last_reviewed: phase-0
  License. The texts library is AGPL-3.0 and links in-process.
  Assumed: the repository is AGPL-3.0. The alternative (MIT core, texts as a separate AGPL process) is a contained change. Reversible: yes.
  Blocks: none until outside contributions arrive.

- G011 — node: n.browser — volatility: medium — last_reviewed: phase-0
  How long a page keeps a held request before the site or browser gives up is unknown.
  Assumed: a draft whose request can no longer be sent fails visibly and the agent re-asks in the thread. Reversible: yes.
  Blocks: long-waiting drafts in A12.

- G012 — node: n.loop — volatility: low — last_reviewed: phase-0
  Which model sub-agents use by default.
  Assumed: the main model, until the owner picks another in Setup. Reversible: yes.
  Blocks: none.

- G013 — node: n.tools — volatility: medium — last_reviewed: phase-0
  Which results page `search` reads when the owner has set none.
  Assumed: DuckDuckGo's plain-HTML results page (a GET needing no key, account or JavaScript); a bot wall there is a reading like any other, and the agent can search through `browse` instead. Reversible: yes (one setting).
  Blocks: live search with no results URL set (fixtures set their own).

- G014 — node: n.audio — volatility: medium — last_reviewed: phase-0
  The similarity at which a voice's embedding counts as a voice already heard or named.
  Assumed: until two voices are named, the threshold sherpa-onnx's speaker-identification example uses; after that, the point where same-voice and different-voice similarities among named stretches err equally (equal error rate). Reversible: yes (one named parameter; a relabel is a newer reading).
  Blocks: live speaker labels on n.audio (fixtures use distinct synthetic voices).

- G015 — node: n.browser — volatility: high — last_reviewed: phase-0
  Google blocks sign-in from browsers that provide automation (Google developers blog, Aug 2020), and the texts link needs a Google sign-in in Plebian's browser.
  Assumed: the owner signs in within the throwaway context with no agent attached; if Google refuses, the Texts card says so and takes the Google cookies from a private window the owner signs into elsewhere, as the mautrix-gmessages docs describe (n.feeds pairs from them the same way). Reversible: yes.
  Blocks: the texts half of A10 (live use only) on n.browser and n.feeds.

- G016 — node: n.browser — volatility: medium — last_reviewed: phase-0
  Chromium's DevTools endpoint on a localhost port takes no credentials, so anything that reaches the box's loopback, a networked `run` included, could drive the agent's signed-in browser.
  Assumed: the server holds DevTools over `--remote-debugging-pipe` and relays it to Playwright MCP and the live view on a Unix socket only the server user can open; if Playwright MCP can attach only over TCP, the port stays on loopback and networked `run` calls get a network namespace without the host's loopback. Reversible: yes.
  Blocks: n.tools' guarantee that the browser profile is out of the tools user's reach, on n.browser and n.tools.

- G017 — node: n.browser — volatility: medium — last_reviewed: phase-0
  Some sites read with POSTs whose bytes don't mark them as reads (a persisted GraphQL document sent only as an id, a search API that takes POST); such a read stalls on a held request.
  Assumed: held and drafted like any change, the body decoded in the draft so a search reads as a search; the owner's word lets it out, or the owner takes the wheel. Reversible: yes.
  Blocks: none (fixtures mark their reads).

- G018 — node: n.loop — volatility: high — last_reviewed: phase-0
  In a turn the owner opened, a page read along the way can steer a later outbound read to an address that carries words the turn holds: the Rule of Two's third leg, reached by reading.
  Assumed: accepted as the price of research, bounded by structure: browser and research sub-agents hold only their task and their own readings (never the memory bands or lookup), landing turns draft their outbound reads, and every outbound read is in the store byte for byte. Reversible: yes.
  Blocks: none.

- G019 — node: n.ops — volatility: low — last_reviewed: phase-0
  The watchdog interval (systemd WatchdogSec) has no spec, protocol or fitted source.
  Assumed: 60 s, the server beating at half of it (the sd_notify convention). Reversible: yes (one constant).
  Blocks: none.

- G020 — node: n.audio — volatility: medium — last_reviewed: phase-0
  The usual gap in speech that ends a stretch has no cold-start prior of its own in the Parameter schedule.
  Assumed: the schedule's one usual-gap prior (one minute) until the sleep pass fits the speech gap from the store's own pauses. Reversible: yes (a named function; a re-cut is a newer reading).
  Blocks: the speech-gap refit on n.audio.

- G021 — node: n.audio — volatility: low — last_reviewed: phase-0
  A live frame's wall time: the phone's arrival clock jitters per frame.
  Assumed: the first frame's arrival time plus 20 ms (or 10 ms) per frame after it, lost frames concealed so the count holds. Reversible: yes.
  Blocks: none.

- G022 — node: n.loop — volatility: medium — last_reviewed: phase-0
  TP-07 makes a question a call whose reading is the owner's reply; the provider wire formats need a tool result in the very next request.
  Assumed: `ask` returns at once ("asked"), the question stays open in Open, and the owner's reply arrives as the next owner message, which closes it. Better: a job whose result is the reply, once jobs carry results across exchanges. Reversible: yes.
  Blocks: none.

- G023 — node: n.proof — volatility: low — last_reviewed: phase-0
  Scenarios drive time and texts through outside surfaces only, but the release binary reads the host clock and Google Messages.
  Assumed: install takes `--clock-source` (a time endpoint) and `--texts-source` (the texts fixture's endpoint), empty by default; tests set them, the owner never does. Reversible: yes.
  Blocks: none.

- G024 — node: n.audio — volatility: low — last_reviewed: phase-0
  Threads per speech model on the box.
  Assumed: every core the box has (runtime.NumCPU), since the audio node runs one model at a time. Reversible: yes.
  Blocks: none.

- G025 — node: n.tools — volatility: low — last_reviewed: phase-2
  The runner keeps at most 1 MiB of each output stream; no spec bounds a reading.
  Assumed: 1 MiB per stream, cut and marked "[cut at 1 MiB]". Reversible: yes (one constant, tools.MaxOutput).
  Blocks: nothing.

- G026 — node: n.feeds — volatility: low — last_reviewed: phase-4
  No spec gives how long a feed waits after its connection fails.
  Assumed: half the owner's usual time to answer, the cadence the calendar fetch already keeps, so a broken connection retries within one stop window. Reversible: yes (feeds.retry).
  Blocks: nothing.

- G027 — node: n.feeds — volatility: high — last_reviewed: phase-4
  libgm pairs from Google cookies that only n.browser's throwaway context can supply, and n.browser is not built yet.
  Assumed: the texts feed speaks the library's interface (pair, status, stream, send) over HTTP to a link endpoint, which the fixture serves; a libgm-backed link implementing the same Link interface in-process joins when n.browser hands over cookies. Reversible: yes (one Link implementation).
  Blocks: the texts half of A10 for live use.

- G028 — node: n.tools — volatility: medium — last_reviewed: phase-4
  The agent opens a PDF "the way it reads any file", but no PDF reader ships in Plebian's root; the sandbox sees the host's /usr.
  Assumed: pdftotext from the host (poppler-utils) when installed; CI installs it. Reversible: yes (ship a reader under bin/ like bubblewrap).
  Blocks: PDF reading on a box without poppler-utils.
