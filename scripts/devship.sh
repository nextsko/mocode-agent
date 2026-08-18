#!/usr/bin/env bash
# devship.sh — the rapid dev loop in ONE command:
#   test → build → atomically replace the globally installed mocode → verify version.
#
# Usage:
#   scripts/devship.sh                 # go test ./internal/... then ship
#   scripts/devship.sh --fast          # skip tests (compile-only sanity via build)
#   scripts/devship.sh --pkgs ./internal/core/tools/...   # targeted tests only
#
# Why atomic replace: `cp` onto a running binary fails with "Text file busy";
# copy-then-rename(2) swaps the inode atomically, so a live mocode session
# keeps running the old image and the next launch picks up the new one.
set -euo pipefail

REPO="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$REPO"

FAST=0
TEST_PKGS=("./internal/...")
while [[ $# -gt 0 ]]; do
  case "$1" in
    --fast) FAST=1; shift ;;
    --pkgs) TEST_PKGS=("$2"); shift 2 ;;
    *) echo "devship: unknown flag '$1'" >&2; exit 1 ;;
  esac
done

# Self-healing Go env: works in bare shells where GOPATH/GOMODCACHE are unset
# (module cache errors otherwise), AND on hosts whose profile exports a bogus
# GOROOT (e.g. GOROOT==GOPATH==/root/go breaks `go vet`). Strategy: derive
# GOROOT from the go binary actually on PATH, then fix GOPATH collisions.
GO_BIN="$(command -v go || true)"
if [[ -n "$GO_BIN" ]]; then
  DERIVED="$(cd "$(dirname "$(readlink -f "$GO_BIN")")/.." && pwd)"
else
  DERIVED="/usr/local/go"
fi
if [[ -z "${GOROOT:-}" || ! -x "$GOROOT/bin/go" || "$GOROOT" == "${GOPATH:-}" ]]; then
  export GOROOT="$DERIVED"
fi
export PATH="$GOROOT/bin:$PATH"
export GOPATH="${GOPATH:-$HOME/go}"
[[ "$GOPATH" == "$GOROOT" ]] && export GOPATH="$HOME/go"
export GOMODCACHE="${GOMODCACHE:-$GOPATH/pkg/mod}"
export CGO_ENABLED="${CGO_ENABLED:-0}"
export GOEXPERIMENT="${GOEXPERIMENT:-greenteagc}"

# Locate the installed binary THROUGH the mocode on PATH — resolves the bun
# symlink chain to the real platform binary. npm installs a JS shim at
# bin/mocode.js that spawns @fromsko/mocode-cli-<os>-<arch>/mocode; if we land
# on the .js shim, resolve the platform package instead of clobbering it.
if ! command -v mocode >/dev/null 2>&1; then
  echo "devship: 'mocode' not on PATH; nothing to replace" >&2
  exit 1
fi
BIN_PATH="$(readlink -f "$(command -v mocode)")"
if [[ "$BIN_PATH" == *.js ]]; then
  # .../node_modules/@fromsko/mocode-cli/bin/mocode.js → sibling platform pkg
  SCOPE="$(cd "$(dirname "$BIN_PATH")/../.." && pwd)"   # .../node_modules/@fromsko
  PLAT="$(node -p 'process.platform+"-"+process.arch' 2>/dev/null || echo "linux-x64")"
  CAND="$SCOPE/mocode-cli-$PLAT/mocode"
  if [[ -f "$CAND" ]]; then
    BIN_PATH="$CAND"
  else
    echo "devship: cannot resolve platform binary for $PLAT at $CAND" >&2
    exit 1
  fi
fi

# 1) Tests.
if [[ "$FAST" -eq 0 ]]; then
  echo "==> go test ${TEST_PKGS[*]}"
  go test "${TEST_PKGS[@]}"
fi

# 2) Build (cached; warm rebuilds take seconds).
echo "==> go build → bin/mocode"
go build -buildvcs=false -trimpath -ldflags "-s -w" -o bin/mocode .

# 3) Atomic replace.
echo "==> replacing $BIN_PATH"
cp bin/mocode "$BIN_PATH.new"
chmod +x "$BIN_PATH.new"
mv "$BIN_PATH.new" "$BIN_PATH"

# 4) Verify + hygiene hints.
echo "==> $(mocode --version)"
if [[ -n "$(git status --porcelain 2>/dev/null)" ]]; then
  echo "⚠  dirty worktree: version shows a pseudo-version (vX.Y.Z-0.hash+dirty)."
  echo "   That is fine mid-dev; tag only at release time (task release)."
fi
echo "✔ shipped. Restart mocode for the new binary to take effect."
