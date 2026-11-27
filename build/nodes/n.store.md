# node: n.store

spec: TP-03 (tables), TP-08, TP-T2, TP-T14; AG-05 (OS users), AG-06 (write-ahead); PL-07 (storage, cloud option)

## assume
[BINDING] origin: specified
- in: src.host — Linux with systemd; a root path chosen at install; a data directory on any mounted drive; optionally an S3-compatible bucket with credentials

## guarantee
[BINDING] origin: specified
- out: one SQLite file `<root>/data/store.db` holding every TP-03 table; a blob store (directory or S3-compatible bucket) holding audio originals and messages as received, each addressed by its sha256
- the store file is owned by the server user, mode 0600; opening it as the tools user fails with a permission error
- a row is durable before the call it records runs; after kill -9 every sealed turn is present
- every sealed turn and heard row rebuilds byte for byte from its spelling and matches the hash fixed at seal, before and after any respelling
- a consistent snapshot of the store can be taken while the server is serving, and restores to a store that passes the rebuild check
- migrations are forward-only and versioned; sealed content is byte-identical across a migration
- a blob read returns exactly the bytes written under its hash, from either backend

## oracle
type: derivable
risk: semantic,reproducibility
fixtures: `tests/store/` (synthetic sheets with known bytes); probe `tests/probes/sqlite`; MR-1, MR-2, MR-3

## metamorphic relations
[BINDING] origin: specified
- MR-1: write the same sheets in two separate processes ⇒ identical row hashes and identical spellings
- MR-2: snapshot, restore to a fresh root, rebuild ⇒ every hash matches
- MR-3: swap the blob backend (directory ↔ S3-compatible) ⇒ identical reads by hash

## demons
- D1: hashes the stored spelling instead of the rebuilt bytes, so a corrupt expansion still "verifies"
  kill_test: tests/store/test_rebuild_hash
  negative_check: seen red — --- FAIL: TestRebuildHash (hash taken over the stored spelling; a corrupted spelling verified)
  status: killed
- D2: creates the store under the default umask, readable by the tools user
  kill_test: tests/store/test_tools_user_denied
  negative_check: seen red — --- FAIL: TestToolsUserDenied (store created 0644 under the default umask)
  status: killed
- D3: buffers writes behind the call, so a crash loses the last sealed turn
  kill_test: tests/store/test_kill9_durable
  negative_check: seen red — --- FAIL: TestKill9Durable (writes buffered behind Seal; turns reported sealed were lost)
  status: killed

## clauses
- [ADVISORY] origin: derived — pure-Go SQLite (modernc v1.63.0) wrote 1M turns + 4M uses in 26–28 s and snapshotted 232 MB with VACUUM INTO in 1.2 s on two x86 cores; rerun the probe on arm64 CI before relying on these on a Pi (prefab: modernc.org/sqlite v1.63.0)

## open gaps
- none
