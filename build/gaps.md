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

- G029 — node: n.store — volatility: low — last_reviewed: phase-4
  No spec says whether a bucket, once set, can be unset, or whether the nightly backup copies a bucket's originals.
  Assumed: a bucket once set stays (the storage card takes another bucket, not none); originals in the owner's bucket are not copied again by restic, which backs up the store and any originals still on disk. Reversible: yes.
  Blocks: nothing.

- G030 — node: n.loop — volatility: medium — last_reviewed: phase-4
  TP-11 names three clock windows (same weekday within 90 minutes, the same time of day standing in until a step has two weekly periods, the same date within a day and three hours); only the daily one is built, and the clock opens at the time of day of a past run in the window.
  Assumed: daily windows over page days (cut at 4 am); the turn opens at the earliest past run's time of day that the rule of succession holds for. Reversible: yes (loop/clock.go).
  Blocks: nothing (A13 needs the daily window).

- G031 — node: n.underlay — volatility: medium — last_reviewed: phase-4
  The lamp prices a heard word by its chance in the heard body, so a box that has heard little wakes on no landing: a word unseen before reaches the 10-bit line only once the underlay holds about ten thousand words (a few days of talk, or about a thousand short messages).
  Assumed: as specified; the first days, landings land and light nothing, and the owner's own words drive the agent. Reversible: yes (a prior for the heard body's frequencies, such as the English word frequencies the grammar already ships, would let a new box wake sooner).
  Blocks: nothing (A08 builds a lived-in body first).

- G032 — node: n.browser — volatility: low — last_reviewed: phase-5
  Resolved: each release carries plebian-browser-linux-<arch>.tar.gz, built by server/browser/bundle/build.sh from Node.js (pinned by nodejs.org's checksum), Playwright MCP 0.0.86 (npm ci against the committed lockfile) and the Chromium that Playwright pins; install fetches it by its .sha256 and installs Chromium's libraries through Playwright's own list (L0051). Still open: the fetch path itself runs only on a release; CI tests the same bundle built by the same script.
  Assumed: the release's bundle is the one acceptance built and tested from the same script and pins. Reversible: yes (install --browser-from <dir> or none).
  Blocks: nothing.

- G033 — node: n.loop — volatility: medium — last_reviewed: phase-4
  The n.loop guarantee that a call still running when its turn would end becomes a job (its result arriving later as an inbound) is not built: every call runs to its end inside the exchange, and a standing step's stop window blocks the exchange while it waits.
  Assumed: calls finish within the exchange (the runner's timeout bounds them); the "readings" inbound already carries results into a later exchange, which is where jobs would land. Reversible: yes.
  Blocks: long-running calls (a build, a long download) hold the loop until they end.

- G034 — node: n.tools — volatility: high — last_reviewed: phase-4
  The sandbox's networked runs share the host network, where Chromium's DevTools listens on loopback with no credential. Held now by time: a networked run refuses to start while the browser is up, and the browser closes when no task, live view or held request needs it. A pipe-only DevTools (or a network namespace without host loopback) would hold it by place instead.
  Blocks: a networked run while a held browser request waits for the owner.

- G035 — node: n.feeds — volatility: medium — last_reviewed: phase-4
  Dependencies in use without a probe row: go-imap v2.0.0-beta.9 and go-ical (n.feeds), coder/websocket v1.8.15 (n.api, n.browser), the sherpa-onnx Go binding v1.13.8 (n.audio; the speech probe ran the Python wheel 2.0.3, same models), tailscale.com v1.106.1 tsnet (n.api; needs the owner's tailnet), universal_ble 3.3.0 (n.app_device; needs the owner's phone and Omi). Each is exercised by its node's tests against fixtures (the IMAP fixture is built on go-imap itself, so those tests do not separate the library from the fixture).
  Assumed: the tests stand in for probes until a probe at real dimensions runs twice per arch (a Dovecot IMAP server with a year of mail and IDLE; the Go speech binding on the probe's audio; tsnet and BLE at first run). Reversible: yes.
  Blocks: nothing yet; a misbehaviour at the owner's dimensions would surface first on the box.

- G036 — node: n.recall — volatility: medium — last_reviewed: phase-4
  TP-T16 (flat per-turn work on a million-turn store) and TP-T17 (fewer re-tells and repeated struck steps than a plain recent window over a year-long thread) are not built as tests. TP-T16 needs a synthetic store at the grammar and use-table level (sealing a million turns through the loop is hours per run); TP-T17 needs a year-long replay thread and a fixture model that answers only from what is in view.
  Assumed: the structure L0001 names holds TP-T16 (ink is not a per-lookup sum over every use; the sqlite probe shows the indexed lookup itself grows 8× from 1K to 1M turns on both arches). Reversible: yes (both tests run on the store and the assembler from outside, as the other TP tests do).
  Blocks: the intent predicate's TP-T1…T17 clause.

- G037 — node: n.proof — volatility: medium — last_reviewed: phase-0
  The Omi simulator's offline storage needs values the firmware takes from hardware or from older sources not in the clone: the card size (the ring's capacity), live-persistence custody, and the 3.0.17-3.0.19 and pre-3.0.17 layouts and error bytes.
  Assumed: the card is sd_card.h MAX_STORAGE_BYTES (capacity 1,105,848 records); custody never grants LIVE_PERSIST (no LIVE_MARK); older firmware reuses the ring's 440-byte packed blocks with the wire shapes and status bytes Omi's app parsers expect (marked `gap:` in tests/fixtures/omi/storage.go). Reversible: yes.
  Blocks: none.

- G038 — node: n.proof — volatility: low — last_reviewed: phase-0
  The simulator's DIS serial and starting battery level are per unit, and the firmware source fixes neither.
  Assumed: serial 5045424C49414E31 (16 uppercase hex, the unit_id.h shape) and 100 percent, both flags of `fixture omi`. Reversible: yes.
  Blocks: none.

- G039 — node: n.app_device — volatility: medium — last_reviewed: phase-0
  Omi's app drives BLE through its own native Pigeon bridge, not a pub plugin; the usual plugin (flutter_blue_plus) carries a non-OSI license. Porting Omi's native Kotlin would be the closer match.
  Assumed: universal_ble 3.3.0 (BSD-3-Clause) behind DeviceTransport; reconnects use Android autoConnect after the first link. Reversible: yes (one class behind the seam).
  Blocks: nothing in CI (the simulator stands in); first contact with a real Omi (PL-08 probe).

- G040 — node: n.app_device — volatility: medium — last_reviewed: phase-0
  Offline files on the wire: the server as built takes u16 LE length + Opus frame per element and X-Omi-File-Timestamp in device ms (server/api/API.md states device milliseconds, as the code does). A stored read is split into runs at a device-clock step over 60 s (Omi ringRecordGapBreakSeconds), one POST per run; a record whose timestamp carries the uptime flag joins the current run; a run with no known time starts at the file's listed time, else now minus its frame count at the codec's frame rate.
  Assumed: the server code at 7f8ad53 is the contract. Reversible: yes (app/lib/device/omi_device.dart packRuns).
  Blocks: A06 if the server moves to per-packet timestamps.

- G041 — node: n.app — volatility: low — last_reviewed: phase-0
  Choices the API does not settle: kept orders are quoted as kind `line` (the quote kinds have no order); Deploy shows on a brief item whose state is `ready` (/v1/week lists no candidates); entry lines carry no time, so they quote but do not long-press into taps; the browser live view sends typed text as `text`, Enter on submit and Backspace from its own control (no Tab or Escape control); a notification is raised for every `notify: true` message even with Today on screen; the paired device's name is the OS name and host name.
  Assumed: as listed. Reversible: yes.
  Blocks: nothing.

- G042 — node: n.app — volatility: low — last_reviewed: phase-0
  Live audio has no per-frame acknowledgement: frames handed to a socket that then fails count as sent (at most the socket's buffer is lost on a drop; everything before is on disk in the spool until sent).
  Assumed: acceptable until the API acks frames. Reversible: yes.
  Blocks: nothing.

- G043 — node: n.app — volatility: low — last_reviewed: phase-0
  The app talks plain HTTP to the server's tailnet address (WireGuard encrypts the path), so the manifest allows cleartext traffic.
  Assumed: tailnet-only reach, as PL-07 specifies. Reversible: yes (a network security config scoped to 100.64.0.0/10 and the tailnet's DNS names).
  Blocks: nothing.

- G044 — node: n.app — volatility: medium — last_reviewed: phase-5
  The APK cannot be built in the build session (the Android SDK host is not reachable); `flutter build apk` runs only in CI. It compiles there, Kotlin included (app run 45990998691: assembleDebug passed after L0050); it has not yet run on the emulator or a phone.
  Assumed: the debug build's compile stands for the release build's. Reversible: yes.
  Blocks: nothing at build time; first run on a phone is the check.

- G045 — node: n.ops — volatility: medium — last_reviewed: phase-5
  This week's brief has no producer: the API's Week hook is never set, so the brief is always empty; the sweep, the mechanic and repository issues are not built; a deploy replays pages only (not requests), and a differing page blocks the deploy with no owner-approval path.
  Assumed: the updates card, the health block and the deploy gate stand without the brief. Reversible: yes.
  Blocks: the n.ops This week clauses.

- G046 — node: n.tools — volatility: medium — last_reviewed: phase-5
  The library, skills, `use describe` and owner-added MCP servers (with the unannotated-is-outside-change rule, n.tools D3) are not built; the band carries each tool's full definition.
  Assumed: the built-in tools and the browser sub-agent cover the scenarios. Reversible: yes.
  Blocks: n.tools D3; owner MCP servers.

- G047 — node: n.loop — volatility: medium — last_reviewed: phase-5
  A sub-agent call past its B is refused instead of dropping its oldest readings, browse results are not split into parts, and the night's read runs as an ordinary clock turn, not a sub-agent.
  Assumed: browse tasks stay within the sub-agent's B in the scenarios. Reversible: yes.
  Blocks: long browse tasks.

- G048 — node: n.ops — volatility: low — last_reviewed: phase-5
  bubblewrap ships as the build runner's Ubuntu 22.04 binary (apt), not built from a pinned tag with a checked hash, so the artifact depends on the runner's apt state.
  Assumed: Ubuntu 22.04's bubblewrap package. Reversible: yes (build from a pinned tag in CI).
  Blocks: byte-reproducible releases.

- G049 — node: n.app — volatility: low — last_reviewed: phase-5
  The app's halves of A05, A06, A09 and A13 have not run against the release server on the emulator (CI's Flutter is pinned to 3.50.1, the version the app was checked on).
  Assumed: the widget tests and the device-seam tests stand in until the emulator job runs against the release server. Reversible: yes.
  Blocks: the n.app→n.api and n.api→n.app seams.
