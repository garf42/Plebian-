#!/usr/bin/env bash
# Probe: restic encrypted backup of a store file + audio, restored byte-identical. Run twice.
set -euo pipefail
R=${RESTIC:-restic}; W=$(mktemp -d); export RESTIC_PASSWORD=probe
mkdir -p "$W/src/audio"; head -c 50000000 /dev/urandom > "$W/src/audio/day.opus"; head -c 200000000 /dev/urandom > "$W/src/store.db"
for run in 1 2; do
  rm -rf "$W/repo" "$W/restore"
  $R init -r "$W/repo" -q; $R backup -r "$W/repo" -q "$W/src"; $R restore latest -r "$W/repo" -q --target "$W/restore"
  a=$(cd "$W/src" && find . -type f -exec sha256sum {} \; | sort); b=$(cd "$W/restore$W/src" && find . -type f -exec sha256sum {} \; | sort)
  [ "$a" = "$b" ] && echo "run$run identical" || { echo "run$run DIFFERS"; exit 1; }
done
rm -rf "$W"
