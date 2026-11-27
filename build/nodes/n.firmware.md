# node: n.firmware

spec: PL-02 step 2, PL-04, PL-06 (firmware over Bluetooth); Omi firmware `omi/firmware/omi/src/lib/core/button.c`, `src/haptic.c`

## assume
[BINDING] origin: specified
- in: src.omi — Omi's firmware source (MIT) at the release tag matching the owner's device, buildable with its documented toolchain

## guarantee
[BINDING] origin: specified
- out: a DFU package equal to Omi's release at that tag plus one change: a double-tap made with no phone connected plays the 500 ms haptic
- the source diff against the tag touches only the double-tap branch
- a double-tap with a phone connected behaves exactly as in the stock firmware
- the package is built in public CI from the pinned tag and installs from the app over the same DFU path Omi's app uses
- when CI cannot build it, the build ships without it and the app hides the firmware card; nothing else depends on this node

## oracle
type: derivable
risk: semantic
fixtures: the button state machine compiled for the host with a fake haptic and a fake connection flag (`tests/firmware/`)

## metamorphic relations
[BINDING] origin: specified
- MR-1: replay a recorded tap sequence with the connection flag on ⇒ identical notifications to the stock firmware

## demons
- D1: plays the long buzz on every double-tap, connected or not
  kill_test: tests/firmware/test_long_buzz_only_disconnected
  negative_check: pending
  status: live

## clauses
(none yet)

## open gaps
- G004 — the owner's Omi model and firmware version
