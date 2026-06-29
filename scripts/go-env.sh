#!/usr/bin/env bash
# scripts/go-env.sh — source this file to put the real go1.26.4 toolchain
# on PATH and GOROOT. See AGENTS.md "Environment Quirks > Go toolchain
# version mismatch on Windows" for the full explanation.
#
# Usage:
#   . scripts/go-env.sh
#   go build ./...
#
# Or one-liner:
#   . ./scripts/go-env.sh && go build ./...

TOOLCHAIN="C:\\Users\\16143\\go\\pkg\\mod\\golang.org\\toolchain@v0.0.1-go1.26.4.windows-amd64"

if [ ! -x "$TOOLCHAIN/bin/go.exe" ] && [ ! -x "$TOOLCHAIN/bin/go" ]; then
    echo "go-env: toolchain not found at $TOOLCHAIN" >&2
    echo "go-env: see AGENTS.md 'Go toolchain version mismatch on Windows'" >&2
    return 1 2>/dev/null || exit 1
fi

export GOROOT="$TOOLCHAIN"
export PATH="$TOOLCHAIN/bin:$PATH"

go version
