#!/usr/bin/env bash
# lint-full.sh runs the whole golangci-lint suite and prints a per-linter
# summary. The scheduled CI job (.github/workflows/lint-full.yml) and local
# maintainers both use it; the per-diff job in test.yml only checks new code,
# this one surfaces the total backlog.
#
# Usage:
#   bash scripts/lint-full.sh            # writes golangci.txt, prints counts
#   bash scripts/lint-full.sh report.txt # custom report path
#
# Exit codes: 0 = clean, 1 = findings, 2 = golangci-lint not installed.
set -uo pipefail

REPO="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$REPO"

OUT="${1:-golangci.txt}"
export CGO_ENABLED="${CGO_ENABLED:-0}"

BIN="${GOLANGCI_LINT:-}"
if [ -z "$BIN" ]; then
  BIN="$(command -v golangci-lint || true)"
fi
if [ -z "$BIN" ]; then
  GOPATH_BIN="$(go env GOPATH 2>/dev/null)/bin/golangci-lint"
  [ -x "$GOPATH_BIN" ] && BIN="$GOPATH_BIN"
fi
if [ -z "$BIN" ] || [ ! -x "$BIN" ]; then
  echo "golangci-lint not found." >&2
  echo "Install it with:" >&2
  echo "  go install github.com/golangci/golangci-lint/v2/cmd/golangci-lint@latest" >&2
  exit 2
fi

echo "Running full lint (this can take several minutes)..."
status=0
"$BIN" run --config=.golangci.yml --timeout=15m --output.text.path="$OUT" ./... || status=$?

echo
echo "== report: $OUT =="
if [ -f "$OUT" ]; then
  total="$(grep -cE '^[^ ]+\.go:[0-9]+' "$OUT" || true)"
  echo "total findings: ${total:-0}"
  grep -oE '\((gosec|staticcheck|revive|wrapcheck|goconst|godot|nilerr|noctx|bodyclose|sqlclosecheck|exhaustive|nestif|prealloc|unparam|godox|unconvert|whitespace|rowserrcheck|nolintlint|godot|nakedret|goprintffuncname|tparallel|misspell)\)' "$OUT" \
    | tr -d '()' | sort | uniq -c | sort -rn || true
fi

case "$status" in
  0) echo "lint: clean"; exit 0 ;;
  *) echo "lint: findings present (exit $status)"; exit 1 ;;
esac
