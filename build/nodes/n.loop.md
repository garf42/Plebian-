# node: n.loop

spec: TP-02; TP-07 (drafts, standing, open loops, kept orders, held call); TP-09 (night's read); Plebian bindings (tool classes, outside readings, kept orders with a time, questions code raises); TP-T8, TP-T9; AG-01, AG-05, AG-06, AG-07

## assume
[BINDING] origin: specified
- in: n.assemble — the request for the turn
- in: n.models — a streamed reply or tool call, with usage
- in: n.tools — each result's reading, exit code, class (read, local change, outside change) and whether its bytes came from beyond the machine
- in: n.api — owner messages (the only owner channel), signed admin actions (grants, deploys), the end of an owner's turn at the browser wheel
- in: n.underlay — landings with the loops they lit; questions code raises (a voice heard in a second stretch with no name, a feed gone silent)

## guarantee
[BINDING] origin: specified
- out: sealed sheets in turn order; tool runs; drafts shown byte-exact; owner-facing messages and questions; jobs; clock turns
- the sheet is sealed before its call runs; a change-making call interrupted mid-run is reported to the agent as interrupted and is never re-run on its own
- a local change is drafted when a heard row or outside reading is in view; an outside change is drafted unless its step has standing and nothing outside is in view; a step whose class can't be shown is an outside change
- a draft runs only when, in the exchange an owner message opened after it was shown, the agent repeats the drafted call word for word; then exactly the stored bytes run, whatever is in view (a browse draft: its held requests go out and the task resumes); any other call becomes a new draft
- in a turn opened by heard words, or holding an outside reading, read-class calls and ask run and every other new call is drafted; in a turn a landing opened, a read that leaves the machine (fetch, search, browse, an open-world MCP tool) is drafted too, since only an owner message, a kept order or a clock turn is a word to go out on (a job's turn keeps the opener of the turn that started it); the tool band does not change with what is in view
- a question code raises reaches the owner as the agent's own ask, so it opens and closes like any question
- a call still running when another inbound arrives becomes a job with an id, and the loop takes the inbound
- standing is raised only by a grant signed with a paired device key; no tool the agent holds reaches the grant path; the agent can only lower standing
- a step with standing runs after a stop window as long as the owner's usual time to answer, announced when called
- owner messages are gathered until a pause longer than the owner's usual gap between messages
- clock turns follow Laplace's rule in TP's schedule; a kept order with a time opens a turn early by the owner's usual time to answer
- once the owner has asked for the day's entry on two nights, the clock opens the night's read; it hands the day's underlay, open loops and kept orders to a sub-agent and the entry lines keep the heard rule
- a sub-agent can change nothing its parent turn couldn't; it holds its task, its tools and its own readings, never the parent's bands or lookup; its requests stay within its model's B (a long reading arrives in parts, its oldest readings leave first, and once its own words alone would pass B it stops and returns what it has)

## oracle
type: relational
risk: semantic,reproducibility
fixtures: scripted fixture provider (`tests/fixtures/model`) and the acceptance scenarios A03, A04, A08, A12, A13, A14, A16; MR-1…MR-3

## metamorphic relations
[BINDING] origin: specified
- MR-1: run a scenario twice in separate processes with the scripted provider ⇒ identical sealed turns
- MR-2: replace the one outside reading in view with neutral local text ⇒ the same change-making call flips from drafted to run, and nothing else changes
- MR-3: run a scenario on each wire format ⇒ identical sealed content apart from provider ids

## demons
- D1: the draft rule checks heard rows but not web pages or message bodies
  kill_test: tests/loop/test_outside_reading_drafts
  negative_check: seen red — --- FAIL: TestOutsideReadingDrafts (an outside reading in view let a local change run)
  status: killed
- D2: a tool output beginning "owner:" is taken as an owner message
  kill_test: tests/loop/test_owner_channel_only
  negative_check: seen red — --- FAIL: TestOwnerChannelOnly (a reading beginning owner: became an owner message and sent the draft)
  status: killed
- D3: an agent tool can write the standing table
  kill_test: tests/loop/test_standing_signed_only
  negative_check: seen red — --- FAIL: TestStandingSignedOnly (keep raised standing)
  status: killed
- D4: an interrupted send is retried after restart
  kill_test: tests/acceptance/a16_crash_safety
  negative_check: pending
  status: live
- D5: a draft is re-composed from the latest context when approved
  kill_test: tests/feeds/d2_test.go TestSMTPOnlyOnApprovedDraft (the sent bytes are the shown draft's, Message-ID included)
  negative_check: seen red — TestSMTPOnlyOnApprovedDraft: shown bytes sent: false (approval re-composed the mail from the call: a new Message-ID)
  status: killed
- D6: a draft runs when the agent repeats it with no owner message since it was shown
  kill_test: tests/loop/test_draft_needs_owner_message
  negative_check: seen red — --- FAIL: TestDraftNeedsOwnerMessage (a repeated draft ran with no owner message since it was shown)
  status: killed
- D7: a landed mail asks the agent to fetch an address with the owner's calendar in it, and the fetch runs because fetch is a read
  kill_test: tests/loop/test_landing_turn_reads_stay_home
  negative_check: seen red — --- FAIL: TestLandingTurnReadsStayHome (a landing's turn fetched past the box)
  status: killed

## clauses
(none yet)

## open gaps
- G012 — sub-agent model default
- G018 — a page read in an owner-opened turn steering a later outbound read
