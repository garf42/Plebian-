# node: n.ops

spec: AG-02 (spend), AG-06 (watchdog, backups, health), AG-08, AG-09; PL-06 (This week), PL-07 (where it runs, cloud option, isolation, Tailscale), PL-08 (setup)

## assume
[BINDING] origin: specified
- in: n.api — the server program and its version endpoint
- in: n.store — snapshot and restore of the store and the blob store
- in: src.host — a Debian/Ubuntu or Raspberry Pi OS host with systemd; the owner's Tailscale (or Headscale) tailnet; optionally an S3-compatible bucket
- in: src.github — tags, CI, releases, issues

## guarantee
[BINDING] origin: specified
- out: release artifacts for linux/arm64 and linux/amd64 with pinned checksums (bubblewrap built among them); one program with install, update, uninstall, serve, pair, backup, restore and replay; a user-level service per account (the server; the tools user's runner) with a watchdog; the This week pipeline (sweep task → brief items → owner approvals → mechanic run or repository issue → candidate → replay → deploy)
- install writes nothing outside its root except its two system accounts and their linger flags; uninstall removes everything install created; a pre-existing service, directory and port on the host are untouched
- the server joins the owner's tailnet as its own device through Tailscale's embedded library (state under the root; install prints the sign-in link); the API listens only there and on localhost, and every port bound is one found free
- with a bucket configured, audio and originals live in the bucket and nothing else changes
- nightly backups (restic) go to a second disk and an offsite destination, encrypted with the owner's key; a restore into a fresh root replays a page byte-identical
- an update replays every page and request since the last deploy; any difference blocks the deploy until the owner approves it in This week; a failed start after deploy rolls back to the previous version
- the brief shows each feed's last landing, model reachability, spend per model per day, disk space and days left at the current rate, the last backup and the last restore drill
- release builds are reproducible from the tagged commit in public CI

## oracle
type: derivable
risk: reproducibility
fixtures: scenarios A01, A17, A18, A20; probe `tests/probes/backup`

## metamorphic relations
[BINDING] origin: specified
- MR-1: install, uninstall, install again ⇒ the same files and ports as the first install
- MR-2: back up, restore elsewhere, back up again ⇒ identical snapshot contents

## demons
- D1: installs a system-level unit file outside the root
  kill_test: tests/acceptance A01 (tests/demons/n.ops-D1.patch)
  negative_check: seen red (2026-11-27, demons run 45982484950) — writes_only_root_accounts_linger: install wrote outside its root: [/etc/systemd/system/plebian.service]
  status: killed
- D2: backs up the store file without its write-ahead log, so a restore loses the last turns
  kill_test: tests/ops/backup_test.go TestRestoreHoldsEveryTurn (and A17 in acceptance)
  negative_check: seen red (re-run 2026-11-27) — backup_test.go:92: the restored store holds 0 of 30 sealed turns
  status: killed
- D3: deploys without replaying
  kill_test: tests/acceptance A20 (tests/demons/n.ops-D3.patch)
  negative_check: seen red (2026-11-27, demons run 45982484950) — replayed_before_deploy: updates card "deployed 65e8f60… at 2026-11-27T10:30:19Z: replayed 0 pages identical"
  status: killed

## clauses
- [ADVISORY] origin: derived — probe: restic 0.20.0 restored 280 MiB (store + audio) byte-identical on two runs; repository 154 MB for random data (prefab: restic 0.20.0)

## open gaps
- G007 — unprivileged sandbox on Raspberry Pi OS
- G008 — release signing key custody
- G009 — Android developer verification registration
