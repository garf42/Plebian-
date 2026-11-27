# node: n.audio

spec: PL-02, PL-04, PL-07 (sizes, transcription); Plebian bindings (taps, sources, questions code raises); AG-03 (`keep`); AG decision D6

## assume
[BINDING] origin: specified
- in: n.api — Opus frames with the Omi's packet numbers and arrival times; offline files with the Omi's file timestamps; the device-clock offset taken at connect; double-tap events with arrival times; taps pressed in the app
- in: n.store — the blob store (a directory or the owner's bucket), addressed by sha256: audio originals go there and nowhere else
- in: n.loop — keep calls naming a voice: its id and the span of the owner's words that names it, or names it as the owner's own

## guarantee
[BINDING] origin: specified
- out: stretches — conversations bounded by a pause longer than the usual gap in speech — as lines per speaker (a name, or an unknown-voice id), with wall times and a pointer to the audio original (Ogg Opus) in the blob store; taps with their times resolved to stretches; speech seconds per minute as audio arrives; each offline file's sha256 once durable; questions for unnamed voices and for the Omi gone silent
- transcription runs as audio arrives; a stretch lands when its conversation ends
- a voice heard in a second stretch with no name raises one question (a voice heard once raises none); once a keep call names it with the owner's words, later stretches carry the name (the owner's own voice is named the same way), and relabeling an earlier stretch is a newer reading that covers it
- speech seconds per minute go out as audio arrives (the ribbon's live fill) and are kept with the stretch when it lands, so the ribbon does not change when a conversation lands
- a gap in arriving audio longer than any the Omi has had raises one question (its silence watch)
- audio recorded out of range lands at its true wall time (device time plus offset), however late it syncs
- on a Pi 5-class board, transcription keeps pace with live audio
- audio originals are kept byte for byte

## oracle
type: relational
risk: semantic,reproducibility
fixtures: synthetic two-voice conversations with known words and speakers (`tests/fixtures/speech`, generated at test time); probe `tests/probes/speech`; MR-1…MR-3

## metamorphic relations
[BINDING] origin: specified
- MR-1: transcribe the same audio in two separate processes ⇒ identical words and speaker labels
- MR-2: shift the device clock and its offset together ⇒ identical wall times
- MR-3: lengthen the silences between turns (staying under the stretch-ending pause) ⇒ the same words

## demons
- D1: uses device time as wall time, ignoring the offset
  kill_test: tests/acceptance/a06_omi_offline
  negative_check: pending
  status: live
- D2: speaker ids are stable within a stretch but shuffle between stretches
  kill_test: tests/acceptance/a07_new_voice
  negative_check: pending
  status: live
- D3: transcribes only after the day ends, so nothing is live
  kill_test: tests/acceptance/a05_omi_live
  negative_check: pending
  status: live

## clauses
- [ADVISORY] origin: derived — probe (two espeak-ng voices, Parakeet-TDT 0.6B v2 int8, pyannote segmentation 3.0, TitaNet-small): two speakers found, labels and words identical across two runs; real-time factor 0.09 for ASR and 0.08 for diarization on two x86 cores; numbers come out as digits ("340") (ledger: L0002, L0005)

## open gaps
- G003 — Omi offline storage protocol by firmware version
- G014 — similarity at which a voice is already named
