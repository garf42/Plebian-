#!/usr/bin/env bash
# Builds the box's browser bundle: Node.js (pinned by checksum), Playwright MCP (npm ci
# against the committed lockfile) and the Chromium that Playwright version pins. Writes
# plebian-browser-linux-<amd64|arm64>.tar.gz and its .sha256 into the given directory.
# CI runs it for acceptance and for each release, so the box installs the bits CI tested.
set -euo pipefail
out=$(mkdir -p "$1" && cd "$1" && pwd)
here=$(cd "$(dirname "$0")" && pwd)
case "$(uname -m)" in
  aarch64) narch=arm64; parch=arm64 ;;
  x86_64) narch=x64; parch=amd64 ;;
  *) echo "unsupported machine $(uname -m)" >&2; exit 1 ;;
esac
. "$here/node.pin"
sha_var="sha256_$narch"; sha=${!sha_var}
work=$(mktemp -d); trap 'rm -rf "$work"' EXIT
B="$work/browser"; mkdir -p "$B/node/bin" "$B/mcp" "$B/chromium"
curl -fsSL -o "$work/node.tar.xz" "https://nodejs.org/dist/$version/node-$version-linux-$narch.tar.xz"
echo "$sha  $work/node.tar.xz" | sha256sum -c - >/dev/null
mkdir "$work/node" && tar -xJf "$work/node.tar.xz" -C "$work/node" --strip-components=1
cp "$work/node/bin/node" "$B/node/bin/node"
cp "$work/node/LICENSE" "$B/node/LICENSE" # Node's notice and those of what is built into it
cp "$here/package.json" "$here/package-lock.json" "$B/mcp/"
(cd "$B/mcp" && PATH="$work/node/bin:$PATH" npm ci --ignore-scripts --no-audit --no-fund --omit=dev)
PLAYWRIGHT_BROWSERS_PATH="$work/pw" "$B/node/bin/node" "$B/mcp/node_modules/playwright/cli.js" install chromium
cp -a "$work"/pw/chromium-*/chrome-linux* "$B/chromium/"
ls "$B"/chromium/*/chrome >/dev/null
name="plebian-browser-linux-$parch.tar.gz"
tar --sort=name --owner=0 --group=0 --numeric-owner --mtime=@0 -C "$work" -czf "$out/$name" browser
(cd "$out" && sha256sum "$name" > "$name.sha256")
echo "$out/$name"
