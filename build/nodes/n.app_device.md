# node: n.app_device

spec: PL-02, PL-04; constitution src.omi; Omi app device code (MIT) `app/lib/services/devices/` (UUIDs in `models.dart`, `connectors/omi_connection.dart`), Omi firmware `button.c`, `haptic.c`, docs.omi.me/doc/developer/Protocol

## assume
[BINDING] origin: specified
- in: src.omi — the Omi over BLE: audio service 19B10000-E8F2-537E-4F6C-D104768A1214 (data …0001, codec …0002), button 23BA7924-0000-1000-7450-346EAC492E92 (trigger …7925), storage 30295780-4301-EABD-2904-2849ADFEAE43 (data …5781, control …5782), time sync 19B10030-… (write …0031, read …0032), haptic CAB1AB95-2EA5-4F4D-BB56-874B72CFC984 (…AB96), battery 0x180F, device information 0x180A
- in: n.app — the server's sha256 acknowledgement of each offline file

## guarantee
[BINDING] origin: specified
- out: to the app — audio frames (Opus, reassembled across MTU fragments, with packet numbers and arrival times), offline files (index, device timestamp, bytes), button events, battery level, firmware revision; to the Omi — time sync at connect, haptic level, delete of an offline file
- the protocol layer is ported from Omi's MIT device code with attribution; it makes no call to Omi's servers and carries no analytics
- the transport sits behind one interface; the Omi simulator implements it, and everything above the transport (parsing, reassembly, codec, buttons, storage sync) runs the same code in tests as on the phone
- a background service keeps the Omi link and the server link alive with the app closed
- an offline file is deleted from the Omi only after the server confirms its hash
- a double-tap while connected produces a tap and haptic level 1 (100 ms)
- packet numbers wrap at 65535 without dropping audio

## oracle
type: relational
risk: semantic
fixtures: the Omi simulator (`tests/fixtures/omi`) replaying synthetic speech as Opus packets, taps and offline files; Omi's protocol test vectors where available

## metamorphic relations
[BINDING] origin: specified
- MR-1: start the packet counter just below 65535 ⇒ the same audio arrives in order
- MR-2: change the negotiated MTU ⇒ identical reassembled frames

## demons
- D1: deletes the offline file as soon as it is read, before the server has it
  kill_test: tests/app_device/test_delete_after_confirm
  negative_check: pending
  status: live
- D2: drops the packet after a counter wrap
  kill_test: tests/app_device/test_u16_wrap
  negative_check: pending
  status: live
- D3: the simulator injects parsed frames above the parser, so the real parsing code never runs in tests
  kill_test: tests/app_device/test_simulator_drives_parser
  negative_check: pending
  status: live

## clauses
(none yet)

## open gaps
- G003 — Omi offline storage protocol by firmware version
