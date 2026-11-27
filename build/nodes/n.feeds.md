# node: n.feeds

spec: PL-07 (mail, calendar, texts, silence watch); AG-04 (texts sign-in); AG-06 (silence watch); constitution src.mail, src.calendar, src.texts

## assume
[BINDING] origin: specified
- in: src.mail — IMAP for reading (IDLE), SMTP for sending, one account per connection
- in: src.calendar — an iCalendar feed URL
- in: src.texts — the Google Messages linked-device protocol
- in: n.browser — the Google session cookies from a throwaway context, for pairing the texts link
- in: n.store — the blob store, addressed by sha256: messages as received go there and nowhere else

## guarantee
[BINDING] origin: specified
- out: messages and calendar entries as received (raw bytes kept, attachments included), each with sender or organizer and wall time, handed to the underlay; send paths: an SMTP send and a text send, each only on an approved draft; questions from the silence watch
- mail lands on IDLE push; the IDLE is re-issued within 29 minutes
- calendar entries land when written and again as they come due; the feed is fetched conditionally (ETag / Last-Modified) at half the owner's usual time to answer, so a due entry can't slip past one stop window
- the texts link pairs from the cookies plus an emoji the owner taps on the phone; the app shows the emoji
- each feed's silence watch opens one question when a quiet runs longer than the longest quiet that feed has ever had
- a mail draft is the complete message, Date and Message-ID included, so the bytes sent are the bytes shown; nothing re-sends a message on its own

## oracle
type: relational
risk: semantic
fixtures: fixture IMAP/SMTP server, fixture iCal feed with recurrences, time zones, edits and cancellations, fixture at the texts library's interface (`tests/fixtures/feeds`)

## metamorphic relations
[BINDING] origin: specified
- MR-1: deliver the same mailbox in two different orders ⇒ identical heard rows at the same wall times
- MR-2: re-serve an unchanged feed ⇒ nothing lands

## demons
- D1: a calendar entry lands once, never again as it comes due
  kill_test: tests/feeds/test_calendar_due_lands_again
  negative_check: pending
  status: live
- D2: SMTP sends without an approved draft
  kill_test: tests/acceptance/a10_feeds
  negative_check: pending
  status: live
- D3: the silence watch uses a fixed timeout
  kill_test: tests/feeds/test_silence_from_history
  negative_check: pending
  status: live

## clauses
(none yet)

## open gaps
- G005 — calendar feed refresh delay
- G006 — unofficial texts protocol
- G015 — Google sign-in in the agent's browser
