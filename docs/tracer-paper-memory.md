# Tracer Paper Memory

Plebian's memory and context harness. Every AI turn is a sheet of trace laid over the last; each request carries only what still reads through, and arithmetic decides what reads through. No model sits in the memory path and the agent never manages its own context.

Terms used throughout: **owner** (the one person Plebian works for), **agent** (the model-driven loop), **harness** (the code around it), **turn** (one model call), **sheet** (the record of one turn), **fragment** (a span of a sheet: a sentence, a line or a code block), **label** (a repeated phrase stored once), **reading** (a tool's output), **underlay** (everything overheard: talk, mail, texts, calendar).

---

## Title block

| | |
|---|---|
| Subject | Context for long-running agents |
| Scale | 1 sheet = 1 AI turn (one model call) |
| Clock | Turns, not hours |
| Budget | Starts at 32K tokens, moves between 16K and 200K |
| Underlay | Talk, mail, texts and calendar, kept as received |
| Reach | Reads everything; changes things on the owner's word, or on a step's standing |
| Memory path | Code only; one read-only lookup, on the underlay, returning row ids |

---

## TP-01 · The stack

- Each sheet is one AI turn, newest on top; the top sheet is the turn being assembled.
- Older sheets settle into **strata** at each merge (which waits for the owner's next message); the request holds less of each older sheet.
- Beneath the stack lies the **underlay**: everything the agent overhears, said or written, kept whole. Nothing on it darkens or covers a sheet.
- The underlay arrives in **landings**. A landing opens at most one turn, for the open loops its words light.
- A cue lights an underlay line only when chance can't explain the match. What lasts is the line written using it, held as heard until the owner settles it.

### Visibility

`v = S × L × τ` — the spotlight ranks by `v × λ`.

- **S · ink**
  - Ink lives on the wording. Each label the owner or the agent writes is a **use** whose trace fades as `(turns since)^−d`, with `d = k × (its ink at that moment) + α`. A use on top of strong ink fades fast; a use after a long gap fades slowly.
  - Each word takes the ink of the smallest label around it; a fragment's ink is the average over its words, each weighted by its information.
  - A first sighting counts once the second arrives.
  - A tool's output counts once, when it arrives, and never holds a place in the strata: the tail and the lamp reach it, and what lasts is the line written using it.
  - Being shown is not a use; being overheard is not a use. What is heard inks only the underlay, on a clock of its own: one tick per stretch of talk or per message.
  - Source: Pavlik & Anderson's spacing model; Shannon information.
- **L · light**
  - Product over later sheets of `(1 − c)`, where `c` is the share of the fragment's information a later sheet retraces in labels while changing the rest, zeroed at the store's own median.
  - A lone shared word is not a retrace, so small talk sharing a word with a fact never covers it.
  - The owner or the agent repeating a whole fragment is a use, not a cover. A tool repeating a reading: the newer reading covers the older.
  - Only sheets laid after the latest whole repeat count.
  - An open loop is covered only by a newer reading of the same failure, until a success closes it (TP-07).
  - The underlay lies beneath every sheet, so nothing heard covers a line of the thread; heard lines cover each other.
  - Source: Beer–Lambert; shared labels.
- **τ · trust**
  - `(1 + a) / (2 + a + b)` from successes `a` and failures `b`.
  - A call is judged by its wording: each outcome is charged to the labels of the call it settles, and a call's trust is its words' records weighted by information, as ink is.
  - Other lines are charged by `brightness × resemblance` to an action they were in view for; the agent's lines also by the owner's next message. Their successes flow down a lineage on shared words; their failures stay put.
  - Readings sit at 1. A span repeating the agent's own earlier words keeps the agent's record, so a claim written to a file and read back gains nothing (taint tracking).
  - Slot classes never count here. Untested wording sits at ½. A heard line stays at ½: it keeps its speaker as its writer, and no outcome settles it.
  - Taint carries: what the agent writes in a turn heard words opened stays at ½ and covers nothing until a message of the owner's retraces it.
  - Source: Beta counts, word by word, as in a spam filter.
- **λ · lamp**
  - `λ = N^r`: `N` is the number of fragments in the store; `r` is the share of a fragment's information found in the cue, counted against at least the store's median line, so two common words can't pin a two-word line. A cue that pins a fragment outweighs any age gap.
  - Two beams with half the light each: the inbound and the agent's own last words, so neither topic crowds out the other.
  - The owner's words reach the lamp through the round trip, so a typo still finds its fragment.
  - A lit line brings back its latest exchange: the owner's words and the agent's, in order, with the call a reported result answers — an answer returns with its question, a report with its action.
  - The lamp prints how often the inbound's rarest words were ever used, in the thread and in the underlay apart, with first and last dates, so "never said here, heard once" can be said and meant.
  - The lamp is all that crosses to the underlay, and there it lights by surprise, not share: a heard line is lit only when the cue's words in it carry 14 bits (odds about 1 in 16,000 by chance) and one of them carries 10; each word counts the lesser of what it says in the thread and how unlikely it is in a line that long.
  - The owner's words reach the underlay through its source's round trip (typed *joists* finds the *joysts* it was heard as); the line returns with the talk or thread around it, marked with who said it and when.
  - Source: rational analysis (history × context); BLAST's chance scores.

---

## TP-02 · One turn, step by step

1. **Inbound arrives.** One of:
   - the owner's messages, gathered until the owner pauses longer than their usual gap between messages;
   - a tool's result;
   - the clock: a call that ran in this window in most periods since it began (at least twice) hasn't run yet;
   - a landing whose words light an open loop or a kept order — one turn for all it lit: transcripts, mail and texts as they arrive, and a calendar entry when written and again as it comes due (TP-07).
2. **Outcome ledger settles first.** A tool's result settles its call by the signals its step has learned mean failure; the charge goes to the call's wording and to what was in view; a failure is written as a redline. The owner's messages settle what the agent sent since the owner's last ones, once the agent's next move shows whether a step is being redone; the owner's answer to a draft settles the draft (TP-07).
3. **Round trip, for what the owner types.** Each phrase is matched to the closest label and rebuilt from it. When label + small change rebuilds the exact text and is shorter to write down than the text fresh (priced at the word's ordinary English frequency), it is recorded that way: a typo counts as the word it misspells; a real word new to the thread stays itself. A word the thread has never used is looked up in the stand-in table, whose row a small model, shown only that word, wrote once from outside the memory (TP-03A).
4. **Write sheet n** as one row, spelled in labels, loose words and changes, with a hash of its exact bytes.
5. **Index.** Anything now seen twice becomes a label (its first sighting counts too). If it is the exact variant of an earlier near-match, that near-match settles as an update and covers the old line. The owner's and the agent's words add uses. A tool's output counts once; when it repeats an earlier reading it covers that reading instead. Earlier fragments whose labels the sheet reuses get covers; the one it retraces most becomes its parent.
6. **Merge check.** Only the budget folds the tail, and only as an exchange opens: if the request would pass B, the tail folds into recent strata. The newest sheet never folds. Within an exchange the request only grows, so nothing a reply was reasoned from changes under it; when the exchange closes, its spotlights and the model's thinking leave.
7. **Assemble the request** in cache order: tool list and a system prompt of fixed rules, deep strata, recent strata, tail, and last one user turn holding the inbound, with the spotlight and what followed quoted beside it (TP-04).
8. **The model reads and writes** a reply or a tool call.
   - It has no memory tools: it can't pin, summarize or forget. Its keep call can only mark an order in the owner's own words (TP-07). Nothing in the thread is its to look up.
   - Its one lookup reads the underlay by words, who, source and time; what comes back is a pointer: the rows found are quoted for that request and stored as their ids, so nothing heard is copied into the stack.
   - A call that would change anything, written while a heard row is in view (a landing, a lookup's result, or a line the lamp lit), is drafted instead of run. A turn heard words opened can read, message the owner and draft, but change nothing (Meta's Rule of Two: untrusted input and private data, but no power to change anything without the owner).
   - Its thinking is never stored.
9. **Index the model's words** exactly as written. A partial match with a buried fragment brings it and the rest of its latest exchange into the next request (pattern completion). Wording whose record is under the strike line, and rarer than the store's median word, brings back the call that failed with it, next to its redline (signature check).
10. **Seal the sheet.** Its bytes never change; any later respelling with newer labels must match its hash.
11. **Tool runs** — or a sub-agent runs it, when that tool's readings have run longer than the median line or it has none yet.
    - A call is drafted instead when it would change something with a heard row in view, or reach past the agent's machine for a step without standing: it waits for the owner's word, shown exactly as it would run (TP-07).
    - The harness keeps the raw output and exit code of every command a sub-agent runs, as readings; the sub-agent's summary counts as the agent's own words.
    - A sub-agent that reads the underlay is handed the open loops and kept orders with it, and its summary keeps the heard rule.
    - The result opens a later turn, matched to its call by job id when the job outlasts the turn — unless the call repeats the failure it is answering and is held (TP-07).

---

## TP-03 · Rows, not sheets

The paper is the picture; the store is one SQLite file per agent. Each AI turn is one row that rebuilds exactly what the model saw and wrote; fragments point into it by byte offset. The compression is a grammar (TP-03A). The labels that store the text also count its ink, keep each call's record, measure covers and settle near-misses.

- **Underlay rows:** each stretch of talk, message and calendar entry is a row of its own, exact and hashed, with who said or wrote it and when, beside the original it came from (the audio, or the message as received).
- Sheets and underlay share labels and nothing else. Each source keeps its own uses, covers, classes, medians and word frequencies, so nothing heard moves a number in the thread and a flood of mail moves none in the talk.
- **Storage:** a row is stored as its spelling in labels, loose words and changes, with a hash of its exact bytes fixed at seal. The sleep pass may respell old rows with newer labels; every respelling must reproduce that hash (as git repacks objects without changing their names).

### Worked example — sheet 31 sealed

1. Write: one new fragment from the agent, `Staging: copy the prod config, port 8080`, indexed exactly as written; its new words are written fresh.
2. Labels: `port 8080` is already a label (written by the agent on sheets 6, 13, 14, 22; the error on sheet 14 counted once when it arrived). This is its sixth use; every fragment holding it darkens a little.
3. Covers: each of those fragments has part of its wording retraced, so each gets a small cover; the one retraced most becomes the parent.
4. Check: `port 8080` has been in one call (sheet 13), which failed, so its record is 0.33, under the strike line. Writing it again brings that call back: request 32 shows sheet 13 struck through, next to its redline.

### Tables

| Table | One row per | Changes when |
|---|---|---|
| `turns` | Model call: inbound, output, ids the spotlight showed, spelling, hash of exact bytes | Bytes never change; respelled only in the sleep pass, checked against the hash |
| `fragments` | Span of a turn or of a heard stretch: offsets, tokens, writer (owner, agent, tool, or a voice/sender overheard), whether heard words opened its turn | Written at seal; light, parent and open flag update on events |
| `symbols` | Word or label: expansion, uses, information, record in calls | At seal when seen twice; record when a call settles |
| `pairs` | Adjacent pair of symbols and where it occurs | At seal |
| `uses` | Occurrence of a label in owner/agent writing, or in heard speech on the underlay's own count; `d` fixed at that moment; the change if a near-match | At seal; a near-match settles when its variant recurs |
| `frames` | Slot: symbols on either side, fillers seen in it | At seal |
| `classes` | Fillers merged as one thing | Sleep pass; split when members appear together |
| `stand_ins` | Word the thread had never used: the words a small model proposed | Written once, when the word first arrives |
| `outcomes` | Action result: exit code, error, owner's answer to a draft, owner's next message (incl. a redo it led to), what it settles | When the result arrives |
| `trust` | Line other than a call: success and failure counts | On outcomes only |
| `bands` | Layer: rendered bytes, sha256, turn built | At a merge or cold rebuild |
| `heard` | Stretch of talk, message or calendar entry: source, wall-clock time, each speaker's/sender's words as transcribed or received, hash of exact bytes, pointer to the original | Written once when it lands, however late or out of order; a later transcript of the same audio, or an edit to a message/entry, is a newer reading that covers it |
| `standing` | Step that reaches past the agent's machine: drafts, how the owner settled each, whether it has standing, the grant that gave it | Raised only by a grant the owner makes outside the thread; lowered at once by a run the owner stops or has redone, a failure, or the agent |

---

## TP-03A · Labels and the round trip

The store compresses itself the way a note-taker who hates writing anything twice would: the second time something appears it gets a label, and from then on it is written as the label. Labels can hold labels, so phrases build into longer phrases and recurring procedures. The store is a grammar; expanding it returns the exact text.

- **Labels:** a pair of words/labels seen twice becomes a label; a label left with one use is folded back in (Sequitur, 1997; chunking; BPE tokenizers work the same way). Labels form at once; the next sleep pass respells earlier sheets in them. A first sighting counts as soon as the second arrives.
- **Ink, word by word:** each word takes the ink of the smallest label around it (which was traced every time a larger one was). One sentence can be half dark, half faint. A word in no label has only its own write. A fragment's ink is the information-weighted average over its words.
- **The round trip** (owner's typed text; heard speech; mail and texts):
  - Each phrase is traced to the closest label and back. A label + small change is kept only when it rebuilds the exact text and is shorter to write down than the text fresh (noisy-channel rule; the store is the dictionary; often-used labels are cheap to point at).
  - Edit costs start even and are refit in the sleep pass from changes the store recorded. Writing a word fresh costs its ordinary English frequency, so a real word new to the thread is never "corrected" into a familiar one; a known word plus an ending or possessive is the same word (*restarted* also cues *restart*).
  - Tool output and the agent's words skip the round trip (neither misspells; to them a file name or date one character off is a different thing).
  - Heard speech takes it on edit costs of its own (a recognizer mishears by sound where a thumb slips by key); the spellings the owner has typed are its dictionary. Mail and texts take typing's round trip, on costs fit apart from the owner's.
- **Settled by recurrence:** a near-match is provisional. A variant gets its own label once the other writer uses it word for word (usually the model, in its next reply); it then becomes an update traced over the old line. A typo, even a habitual one, is never written back, so it stays a use of the word it misspells; a wrong match undoes itself the same way.
- The round trip changes what counts as a repeat, never what the model reads. Every rendered line is the original text.
- **Slots:** recurring frames (`copy the ___ to staging`) collect the words that fill them. In the sleep pass two fillers merge only when merging makes the store shorter to write down and they don't appear together more often than chance. Words that appear together (*from staging to prod*, *either 8080 or 8443*) are contrasts. A class moves ink, cover and lamp, never trust; it splits if its members start appearing together.

| Slip | Caught by | Example |
|---|---|---|
| Typo or word form | Round trip | `confg` counts as a use of config; `restarted` also cues restart |
| Misheard word | Round trip on speech's edit costs | A name spelled three ways by the recognizer counts as the owner's typed spelling; a real word misheard (fifty for fifteen) stays as heard |
| Value that changed | Round trip and its own label when typed; covering when a tool or the agent writes it | 8081 drawn over the 8080 line as an update |
| Different words, same thing | Slots | prod config / live settings fill the same frames |
| Same kind, different thing | Kept apart because they co-occur | staging and prod; succeeded and failed |
| Rewording in words the thread never used | Stand-ins, then the model's reply | A small model shown only those words proposes others once, into a table; the thread keeps the ones it has used, at half the cue's weight; it never sees the store and a replay reads the same rows. The reply then answers in established words, which darkens the old label (lexical entrainment) |

One rule decides every step here: whichever version is shorter to write down wins (minimum description length).

---

## TP-04 · The request the model reads

Drawn at turn 48 of a simulated run, B = 32K: tools + system 6K · deep strata 4.4K · recent strata 6.7K · tail 4.8K · user turn 2.6K · headroom 7.4K.

- Each cache breakpoint caches everything above it. A change inside a band invalidates that band and everything below it, never what sits above; so bands run slowest-changing to fastest, and a band is rewritten only between exchanges.
- The system prompt holds only fixed rules. Everything assembled per turn rides in the user turn as quoted record, because system text carries the app's own authority.
- Heard rows ride in the user turn and nowhere else: a lit line is quoted with the talk or thread around it, who said it and when, and never enters a band or the tail.
- A change a frozen band can't show yet (a strike, a newly kept order) rides in the spotlight until the next rebuild (as a database tombstone overrides a frozen record until compaction).
- **What followed:** the spotlight reads the outcome ledger forward. Past exchanges where the owner said what the inbound says (sharing ≥ ⅓ of its information) bring back the calls that came next, tallied by how often each followed and how often it worked, same season first. A reply too short to match is read through the question it answers; a message too short to match anything through what was last heard; a tool's result through its exchange's last request minus the steps already done; a clock turn through that window's past runs. A call that waited for the owner's reply shows the owner's last two; a draft its step's tally; a struck call what it was redone as. This is pattern completion run forward: how the agent anticipates.

| Band | In the API request | Rewritten when |
|---|---|---|
| Tools + system | Tool list read from the machine (tools, scripts, skills, sub-agent tasks — a line each) and a system prompt of fixed rules | At a rebuild after the list changes; a new tool arrives sooner as the API's tool addition |
| Deep strata | First user turn, quoted, kept orders first | At a promotion (every 4th merge) or a cold rebuild |
| Recent strata | Same turn, next block | At every merge or a cold rebuild |
| Tail | Messages, verbatim | Appended within an exchange; its spotlights and thinking leave when it closes |
| Spotlight + inbound | One user turn: the inbound (a tool's result first) with the spotlight quoted beside it (open loops, kept orders and strikes waiting for a rebuild, what followed, lit lines, heard lines lit past chance, use counts) | Every request |

- Cache lifetimes: the first three breakpoints use the 1-hour lifetime, the tail 5 minutes (longer-lived entries must come first). After an idle gap longer than an hour there is no cache to protect, so the harness re-ranks both strata at no extra cost; the tail stays as it was (only the budget folds it).

---

## TP-05 · Merges and the budget

- The tail grows a sheet per turn. A merge folds it into recent strata only as an exchange opens, so inside an exchange the request can pass B and drops back when the next opens (a sawtooth).
- Every fourth merge is a promotion that also rewrites deep strata.
- B starts at 32K and grows 8K when the owner re-tells something the next step up would have shown, at most once per 20 outcomes.
- Failures, redos and re-tells of lines that were in view feed a CUSUM; when it crosses, B drops by a quarter.
- After an idle gap past the cache lifetime, both strata are re-ranked at no extra cost and the tail keeps growing.

## TP-06 · The taper

There is no knee: retrieval holds deep into a long window, while reasoning over what is in the window slides from a few thousand tokens on and keeps sliding.

- NoLiMa (questions sharing no keywords with answers): by 32K, 10 of 12 models fell below half their short-context score; GPT-4.1 kept 82% at 32K and 67% at 128K.
- RULER (finding and tracing): Gemini 1.5 Pro kept 98% at 128K; Llama 3.1 70B fell to 69%.
- Du et al. 2025: with retrieval made perfect, length alone cost 14–85% across five models; Chroma's context-rot tests found the same slide on simple tasks.
- **How the budget is set**
  - Floor 16K: the best NoLiMa model's effective length. Start 32K: retrieval still holds and the best model lost under a fifth on reasoning. Ceiling 200K: a stop, not a target; growth needs evidence. Each is capped at the model's own context limit.
  - Between them the harness measures its own taper from **re-tells** (the owner telling the agent again something the store held; saying or writing it to someone else is not one).
  - Re-tell of a line one step up would have shown → window too small → B grows 8K, at most once per 20 outcomes.
  - Re-tell of a line that was in view → the model missed what it was shown (the taper itself). Those, failed calls and redos feed a CUSUM (Page's test) that catches a rise too small for any window of 20 to separate; B shrinks by a quarter when it crosses.
  - Any other re-tell is the lamp's miss and leaves B alone; so does a correction of a line written from heard speech (that miss is the recognizer's, so speech's edit costs are refit instead).
  - Growth waits for a signal because harm from length is quiet (unlike TCP, where loss is loud).
  - When the model behind the agent changes, B restarts at 32K and the controller finds the new taper on its own. Each model keeps its own controller state.

---

## TP-07 · Redlines and open loops

People learn some things wrong and keep believing them until reality pushes back. The harness gives the agent the same correction, in code, from outcomes it can observe without a model.

- **Tool results** count by exit code and their own words, read the way the step has learned: a signal counts as failure for a step once it has come before that step being redone more often than not (by the clock's rule of succession, starting from the signal's rate across all steps). So grep finding nothing, or a log full of old tracebacks, fails nothing.
- **The owner's messages** count too. Each new message settles what the agent sent since the owner's last: whatever it passes over is approved (one success shared across those lines by their information); whatever it retraces is left as it was (telling agreement from objection would mean reading tone). When the owner's message leads the agent to run the same step again with other arguments, the first call counts as failed. (Grounding: going on to the next thing accepts what was said.)
- **Only what the owner addresses to the agent grounds anything.** What it overhears, said or written, approves nothing, rejects nothing and gives no order — an overhearer takes no part in the exchange.

| Written by | Ink | Trust |
|---|---|---|
| Owner or agent | Every write is a use, repeats included | Charged when in view for an action; the agent's lines also by the owner's next message |
| The agent's call | The same | Judged by its wording's record |
| A tool | Counts once; a repeat covers the older reading; never a place in the strata | Sits at 1 and settles the others, except where it repeats the agent's own words |
| A voice or sender, overheard | Inks the underlay only, on its own clock; never in the strata or the tail | Stays at ½, as does what the agent writes from it until the owner retraces it: no outcome settles it, and it settles nothing |

- Repeating something moves ink, never trust — so the agent can't talk itself into its own errors (repetition makes people believe false claims).
- **A call carries its record.** Each outcome is charged to the wording of the call it settles; the next call using the same words starts with their history. Wording found in wins and losses alike evens out; wording found only in losses keeps the blame (Graham's spam filter). A long record barely moves on one loss, so blame lands on what is unfamiliar; no tool needs a table of its own.
- A redo that adds words teaches the whole **step** (the tool and its first argument, or a dispatched task's first words): any call of that step without them is struck until it works, in every topic the step serves.
- Other lines are judged only by what happened while they were in view.
- **Drafts and standing**
  - A call that would change something while a heard row is in view, or reach past the agent's machine for a step without standing, is **drafted**: shown to the owner exactly as it would run, with its step's tally of drafts the owner ran as written, changed or turned down.
  - A draft is a question and closes the way a question does; only a message of the owner's settles it. Run as drafted → success for its wording and step; changed → a redo (the draft failed); turned down → failed.
  - What runs is the drafted bytes, whatever is in view by then, so nothing read afterwards can alter what the owner saw.
  - **Standing** is granted outside the thread, where the agent can't write, and only by the owner. The admin brief ranks steps by record τ. Once the owner has given standing to a step of one tool, that tool's other steps are offered as their record reaches the line the owner drew (the bar is the one the owner's own grants set). A declined offer returns only when the step's record beats where it stood when declined. The agent can give standing back but never take it.
  - With standing, a call written with nothing heard in view runs on its own, announced as it is called and run after a stop window as long as the owner's usual time to answer the agent.
  - Standing holds while the step's record stays at or above where it was granted; a run the owner stops or has redone, or one that fails, counts against it like any outcome.
  - Anything written with someone else's words in view is always drafted.
  - Grounding: entrustable professional activities (autonomy granted one activity at a time, on observed record).
- **Open loops** (Zeigarnik effect)
  - A redline is an open loop; so is a question the agent asks the owner (a call whose reading is the owner's reply).
  - While a loop is open its clock is stopped and later sheets don't cover it. An open redline is a reading: it holds no strata place and has first claim on the spotlight.
  - A failure closes when a call of the same step succeeds, or when another success retraces at least a third of it. A newer reading of the same failure takes its place; a closed redline comes back only with its struck call.
  - A question closes when the owner's reply retraces it, or, if the reply is too short to retrace anything, when it is the newest one open.
  - **Heard wake:** what is heard lights an open loop or a kept order when three lines of it hold the loop's words (or their stand-ins at half weight) with more surprise than chance: each word counts the lesser of what it says in the thread and how unlikely it is in that much of its source; the line is set where closed loops (which nothing can answer any more) would light about once in four days (target–decoy). A landing's lit loops ride into the one turn it opens, and that turn closes nothing: the answer the owner gave someone else, aloud or in writing, lights the question; the owner's reply to the agent closes it.
  - The owner can close a loop by dismissing it. A loop left open longer than any loop of its step, or any question, has stayed open is released.
- **Kept orders** (prospective memory): an order the owner gives, in their own words, is a loop only the owner closes. The agent answers it with a keep call that points at the owner's words, and releases it only with a call that points at the owner's words again, so the order stands as said. The order sits first in the deep strata, riding in the spotlight until the next rebuild puts it there.
- **Calendar entries** are orders kept elsewhere: an entry lands when written and again as it comes due, early by the owner's usual time to answer the agent, and wakes only what it lights.
- **Strikes:** a contradicted line is struck through but never hidden (a hidden error gets relearned); in a frozen band its strike rides in the spotlight until the next rebuild. Whenever new writing reuses struck wording rarer than the store's median word, the call that failed with it comes back with its redline (signature check).
- **Held call** (circuit breaker): if the agent answers a failure by repeating the same call word for word while its wording is struck, the call doesn't run; its redline comes back as the result; issuing it once more runs it. Only exact repeats are held (a warning that fires on everything gets overridden by reflex — alert fatigue).

---

## TP-08 · A chat that never ends

- The context never grows: B caps each exchange as it opens, the ceiling caps every request, and each merge resets the tail. What grows is the store, and that growth stays off the reply path.
- Three events change the store: a use, a cover and an outcome. Each updates a counter on a label or fragment, so work before each reply depends on the new sheet and the fragments its distinctive labels reach, not on how long the chat has run.
- Underlay writes happen whenever a stretch or message lands (however late or out of order), raise none of the three events on a sheet, cost a reply nothing and call no model; a landing opens at most one turn however much it holds. Watching mail and texts costs the same: code fetches them; a model is called only when what lands lights something.

| Over an unbounded chat | Behavior |
|---|---|
| Request | Capped at B as each exchange opens, forever |
| Tail | Grows a sheet per turn, resets at each merge |
| Strata | Fixed shares of B |
| Work before each reply | Flat: the new sheet's round trip, the fragments its distinctive labels reach, counter updates |
| Work at each merge | Flat: the recent pool holds at most four merges of sheets |
| Store | Grows with the chat (about 1 MB/day compressed at 500 calls/day, estimate) |
| Underlay | Grows with what is heard (about 0.3 MB of transcript per five hours of talk, estimate); audio and messages as received are kept outside the store |
| Sleep pass | Grows with the store; runs when idle, never on the reply path |

- **Sleep pass:** runs after 10 minutes idle, or every 500 turns on a snapshot if the agent never idles. It ranks the deep pool into a shortlist (so promotions stay cheap), links lineage across the store, refits the decay constants, edit costs, the owner's usual gap between messages and usual time to answer the agent, merges slot classes and splits any whose members now co-occur, respells old rows with newer labels against their hashes, and resamples the covering zero point. It changes structure only and records no new uses (a class merge only pools uses that happened). The underlay gets the same pass on its own numbers.
- **No turn can blow the budget:** an oversized tool result is cut into fragments when written; inside the tail those fragments fill to the line like everything else. A tool that prints the same thing every run covers its own old readings, so its boilerplate never builds ink; output that never repeats holds no strata place. Nothing heard enters a band or the tail or is copied into the stack; only a line that beats chance takes spotlight room.

---

## TP-09 · Long-term memory

Deep strata are the part of long-term memory in view without a cue (like one's own address). The store is the rest. Nothing is ever deleted: a fragment never used again loses its place in view but stays reachable by a strong enough cue. The underlay is the part the agent took no part in, kept word for word and reached only by a cue.

- A fact restated in new words keeps its history, because ink lives on labels: a restatement keeps the full history of every phrase it reuses; only new phrases start fresh (a version keeping 70% of the last one's wording counts earlier versions at 0.7, 0.49, 0.34…). Reconsolidation is the brain's version.
- Spacing-aware decay: cramming fades; things recurring across months become close to permanent (Bahrick's permastore).
- Simulated year (~20,000 turns): under fixed decay with nothing carried, a crammed fact still outranks last month's note a year later and a reworded fact is no stronger than one new note; under this design cramming sinks below last month's note and both recurring facts stay above it.

| In the brain | In the harness | What it does |
|---|---|---|
| Hippocampus: episodes, fast and detailed | Store and tail | Every turn kept verbatim, permanently |
| Neocortex: slow learning of what recurs | Deep strata | What keeps coming back stays in view |
| Spacing effect, permastore | Spacing-aware decay | Cramming fades; recurrence lasts |
| Chunking | Labels | A sequence seen twice becomes one unit; units nest |
| Reconsolidation | Shared labels | A restatement carries the ink of the wording it keeps |
| Word kinds from frames | Slot classes | Words filling the same slots count as one, unless they co-occur |
| Habituation; sensory memory | Readings | A tool's repeats cover instead of darken; its output lasts only through the lines that used it |
| Overhearing; one's own name across a room | The underlay; talk or mail lighting an open loop past chance | The overheard stays out of view until a cue or an unfinished task picks it out, and keeps who said it |
| Law of effect; schemas | A call's record, by step | The next call with the same words starts with their history; a correction reaches every topic the step serves |
| Cue-dependent recall; fan effect | Lamp, λ = N^r | A distinctive cue reaches anything; common words reach nothing |
| Pattern completion; predictive maps | A lit line's exchange; what followed | A partial match brings back the whole exchange and the steps that usually came next |
| Zeigarnik; prospective memory | Open loops; kept orders | A failure or unanswered question stays in view until resolved; a kept order stays until the owner drops it |
| Skill turning automatic | The clock; standing | What the owner asks for in the same window twice, the clock starts asking for; a step whose record reaches the owner's grant line is offered standing |
| Savings in relearning | Old uses still count | Something used again comes back faster than something new |
| Available but not accessible | Nothing is deleted | Faded means unreachable without a cue, not gone |
| Sleep consolidation | Sleep pass | Builds structure offline and adds no ink |
| Retelling the day | Nightly entry | The agent reads the day's underlay against what is open and writes what it held, kept as heard until the owner retraces it |

- **What code can't do:** a brain compresses many episodes into a rule none of them states; code can't write that sentence. The nearest it gets is the grammar's top labels and what followed, in words actually used, with how often each worked — so any gist here is real wording, never a new sentence. The agent can write that sentence in an ordinary turn: ask for the day's entry on two nights and the clock keeps asking. Each night it opens a turn; the lookup hands the day's underlay to a sub-agent with the open loops and kept orders; the agent writes a short entry on what the day's talk and mail answered or set off, what the owner committed to (aloud or in writing), and what needs the owner's word. The entry keeps the heard rule until a message of the owner's retraces it; lit, it returns with the call it answers, which leads back to the verbatim.

---

## Plebian bindings

How the harness meets the rest of Plebian. Each line extends a rule above without changing it.

- **Outside readings.** A reading whose bytes came from beyond the agent's machine (web fetch, search results, browser snapshots, open-world MCP results, a message body fetched by a tool, a file opened from the outside folder) counts as someone else's words for the draft rule, exactly as a heard row does. Bytes that reach the disk keep the mark: they live in the outside folder, and a call that opens it gives an outside reading. It stays a reading in the thread (trust 1 as a record of what was returned); it is not underlay.
- **Tool classes.** Every tool step is one of: *read* (changes nothing anywhere), *local change* (changes only the agent's own workspace), *outside change* (can change state beyond the machine). The draft rule reads this class: local changes are drafted only with heard rows or outside readings in view; outside changes are drafted unless the step has standing and nothing outside is in view. A step whose class can't be shown is an outside change. A read that leaves the machine (fetch, search, browse, open-world MCP) goes out on the word that opened its turn — an owner message, a kept order, a clock turn; in a turn a landing opened it is drafted, since reaching out is communicating.
- **Browser actions.** In the browser, an agent action whose handling sends a request that isn't a read is an outside change; a read is a safe method (GET, HEAD, OPTIONS, TRACE, QUERY) or a GraphQL request whose document is in its bytes and whose selected operations are all queries. The request is held and drafted with its exact method, URL and body, and what one action set off is one draft; a beacon or ping, which no page waits on, is dropped instead. Reads never wait. A web page is always outside words, so a held browser request is always drafted.
- **Kept orders with a time.** A keep call may carry the time the owner's words name; the clock opens a turn for that order early by the owner's usual time to answer the agent — the same lead a calendar entry gets.
- **Marks.** A mark (a double-tap on the Omi or a long-press in the app) is a pointer row into the underlay: wall time, how it was made, the stretch around it once that stretch lands, and the owner's optional note in the owner's words. A mark is not a use and settles nothing; the night's read reports every mark.
- **Questions code raises.** A voice heard in a second stretch with no name, or a feed quiet longer than it has ever been, opens a turn of its own; the agent puts it to the owner as an ask, so it opens and closes as TP-07's questions do. An answer that names a voice is pointed at with a keep call, as an order is.
- **Sources of heard rows.** Talk (the Omi, through the phone), mail, texts and calendar. Each keeps its own clock, medians, frequencies and edit costs.

---

## Parameter schedule

| Parameter | Value | Where it comes from |
|---|---|---|
| Clock | AI turns | One model call each, opened once the owner pauses longer than their usual gap between messages. Idle time never fades anything. The underlay ticks once per stretch of talk (a stretch ends at a pause longer than the usual gap in speech) and once per message |
| Cold start | 20 samples | Default priors until the store has 20 of its own: median line 5 words, usual gap one minute (standing in for the owner's time to answer too), a loop held a day, a failure signal at its rate across all steps |
| Decay per use | k = 0.232, α = 0.167 | Pavlik & Anderson's fitted values as a start, refit by the sleep pass to the agent's own reuse history |
| Labels | seen twice | Sequitur |
| Word weight | −log₂ frequency | Shannon information |
| Near-match | shorter write-up | Typed text and heard speech; MDL against ordinary English frequency |
| Edit costs | even, then refit | Noisy-channel spell-checking; owner's typing, speech, others' mail and texts each keep their own; speech's also learn from corrections of lines written from heard speech |
| Stand-ins | half weight | Default. Written once by a small model shown only one word: a word the owner types that the thread never used (kept where the thread has used the stand-in), or a word of an open loop (for the wake) |
| Slot merge | shorter, and apart | Merge when it shortens the store and fillers don't co-occur more than chance; split when they start to |
| Lineage | one parent | The earlier fragment it retraces most; a non-call line inherits successes on shared words, so a changed number starts untested |
| Lamp | λ = N^r | Two beams, half the light each; on the underlay a line is lit at 14 bits matched with one word past 10 (Default), scored against chance as BLAST scores an alignment |
| Covering zero point | store median | Median retraced share between random pairs, resampled in the sleep pass |
| Trust prior | Beta(1, 1) | Uniform; untested wording ½; heard lines stay ½, as does what the agent writes from them until the owner retraces it; readings 1 |
| Strike line | τ < 0.4 | Default. Below it a line renders struck; struck wording rarer than the median word brings back the call that failed with it |
| Held call | once | Exact repeats only; issuing again runs it |
| Draft | the owner's word | Change-making call with heard rows or outside readings in view, an outside change without standing, or a read leaving the machine in a turn a landing opened, runs only as drafted, on a message of the owner's (Rule of Two) |
| Standing | the owner's line | Offered once a step's τ reaches the lowest at which the owner has granted a step of the same tool; holds while τ stays at or above where granted; granted only by the owner |
| Stop window · lead | the owner's time to answer | Fit by the sleep pass; until the store has its own, a calendar entry comes due at the reminder set on it |
| Budget floor / start / ceiling | 16K / 32K / 200K | NoLiMa best effective length / RULER effective length / a stop |
| Budget moves | +8K, ×0.75 | Default. Up on a re-tell the next step would have shown, at most once per 20 outcomes; down when a CUSUM on failures, redos and in-view re-tells detects a 10-point rise over their rate at 32K, tuned to about one false alarm per 1,000 outcomes (Page). Corrections of heard-speech lines refit speech's edit costs and never reach the CUSUM. A new model restarts at 32K |
| Strata fill after a merge | half of the rest | Default. Half of what B leaves after tool list, system prompt and spotlight cap; 40% deep / 60% recent; kept orders first in deep, then ranked by v (scaled down for lines lighter than the median line, readings excluded); the tail grows into the other half |
| Promotion | every 4th merge | LSM-tree size ratio of 4 |
| Deadband | 5% | Default. Stops fragments at the line from flickering and breaking the cache |
| Spotlight cap | about 10% of B | Default. Filled in order: open loops; strikes and kept orders waiting for a rebuild; what followed; then lit lines, the exchanges they bring back, and heard lines lit past chance (a third each; room heard lines leave goes to lit lines); use counts ride with lit lines |
| What followed | ≥ ⅓ shared | Default. Over all information, inbound and past request; same season (±1 month) first, escaping to the whole year by its own novelty (prediction by partial matching) |
| Heard wake | decoy line | Default counts (three lines; once in four days). Three lines lighting an open loop or kept order past the line where closed loops (decoys) light about once in four days; each word counts the lesser of its thread information and −log₂ of its chance in that much of its source; a landing's lit loops share one turn, which closes nothing; a calendar entry lands again as it comes due |
| Clock turn | (k + 1) / (n + 2) > ½ | Laplace's rule of succession: the call ran in this window in k of the n periods since it began, k ≥ 2, and a run the clock opened counts only when the owner answered it. A window (Default widths) is the same weekday within 90 minutes (the same time of day standing in until it has two periods of its own), or the same date within a day and three hours where that has guessed better |
| Sleep pass | 10 min idle or 500 turns | Default; results land between exchanges |
| Cache lifetimes | 1 h · 5 m | First three breakpoints 1 h, tail 5 m; longer-lived entries must come first (provider rule; providers with automatic prefix caching need no breakpoints) |

Rows marked *Default* are conventions awaiting a fit from the store's own history; each is listed in `build/gaps.md`.

---

## Tests

Each is a property of the built system, checkable from outside it.

1. **No model in the memory path** — the memory package imports no model client; the build fails if one appears. Stand-ins and transcripts reach it as plain rows, each written once from outside.
2. **No memory tools** — nothing in the agent's tool list writes the memory tables; only the lookup reads any (heard rows only, returned as ids). The build fails if a tool does more. The store belongs to its own system user, so not even the agent's shell can open it. A keep call writes nothing: the harness records it like any call, and it can point only at the owner's words.
3. **Extractive only** — every rendered line is a byte-exact span of a sealed turn or a heard stretch, a gap marker, or a count the harness computes from the store (marked as its own). Near-matches and slot classes never change what is rendered. A lit report always comes with the call it answers.
4. **Budget holds** — every request is counted before it is sent; one that opens an exchange never exceeds B; none exceeds the ceiling (an exchange that reaches it folds what came before it).
5. **Shown is not used** — rendering adds no ink; a tool repeating a reading adds none; no reading is placed in the strata. Nothing heard writes a use, cover or outcome on a sheet, holds a place in a band or the tail, or is copied into the stack.
6. **Repetition never moves trust** — only outcome records write trust counters; readings carry none. A line written in a turn heard words opened holds ½ until a message of the owner's retraces it.
7. **Overheard never instructs** — no heard line opens an exchange in the owner's name, settles a line, closes a loop or counts as a re-tell; no change-making call runs while a heard row is in view (it is drafted). The build fails if the outcome ledger or the budget controller reads the underlay, or if the controller counts a correction of a line written from it.
8. **Drafts run as shown** — a draft runs byte for byte as the owner saw it, only on a message of the owner's; anything else is a new draft.
9. **Standing comes only from the owner** — no code path raises a step's standing except a grant made outside the thread; the build fails if a tool the agent holds can reach it. The agent can only lower it.
10. **Classes never touch trust** — no slot class is read when charging outcomes or closing loops.
11. **Held once** — a struck call repeating word for word the call whose failure it answers does not execute; issued again, it does.
12. **Repeats are labels** — after each sleep pass no pair of symbols appears twice in the grammar and no label has fewer than two uses.
13. **Cache breaks on boundaries** — cache diagnostics on consecutive requests report a divergence only at a band boundary.
14. **Nothing is deleted** — every turn and heard row rebuilds byte for byte from its spelling and matches the hash fixed at seal, before and after any respelling.
15. **Idle is free** — elapsed time never decides what fades or folds; wall-clock time only addresses the underlay and times turns and stop windows. With no turns nothing folds; a sleep pass leaves the same store after ten minutes idle as after ten days.
16. **Flat per-turn work** — on a synthetic million-turn store, assembling a request takes no longer than on a thousand-turn store, within noise.
17. **Remembers** — replayed against a thread a year long, the agent draws fewer re-tells and fewer repeats of a struck step than it does on the same thread under a plain recent window of the same B.

---

## Sources

- Anderson & Schooler (1991), Reflections of the environment in memory.
- Rational analysis of memory: need ∝ history × context.
- Pavlik & Anderson, An ACT-R model of the spacing effect (fitted c and a).
- Bahrick (1984), Semantic memory content in permastore.
- Nader, Schafe & LeDoux (2000), reconsolidation.
- McClelland, McNaughton & O'Reilly (1995), complementary learning systems.
- Tulving & Pearlstone (1966), availability vs accessibility.
- Zeigarnik; Goschke & Kuhl (1993) on intentions.
- Fan effect; Fazio, Rand & Pennycook (2019), repetition and perceived truth.
- Graham (2002), A plan for spam.
- Hong, Troynikov & Huber (2025), Context rot; Du et al. (2025), length hurts despite perfect retrieval; NoLiMa (ICML 2025); RULER (COLM 2024).
- Anthropic docs: prompt caching; extended thinking; mid-conversation system messages and tool changes.
- Nevill-Manning & Witten (1997), Sequitur; Shannon (1948); Rissanen (1978), MDL; Kernighan, Church & Gale (1990), noisy-channel spelling; Sennrich et al. (2016), BPE.
- Mintz (2003), frequent frames; Lin et al. (2003), contrast in "from X to Y".
- Clark & Schaefer (1989), grounding; Brennan & Clark (1996), lexical entrainment; Schober & Clark (1989), overhearers; Moray (1959), the cocktail-party effect.
- Pro Git, packfiles; Fowler, circuit breaker; van der Sijs et al. (2006), alert overrides; Rankin et al. (2009), habituation; Thorndike, law of effect; Atkinson & Shiffrin (1968); Stachenfeld, Botvinick & Gershman (2017), predictive map; Cleary & Witten (1984), PPM; Tse et al. (2007), schemas.
- Speer, wordfreq; Page (1954), CUSUM; Karlin & Altschul (1990), match significance; Elias & Gygi (2007), target–decoy; Meta AI (2025), Agents Rule of Two; O'Neil et al. (1996), LSM-tree; Newsome & Song (2005), taint analysis; ten Cate (2018), entrustable professional activities; Fitts & Posner (1967); RFC 9110 §9.2.1, safe methods.
