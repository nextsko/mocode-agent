#!/usr/bin/env bash
set -euo pipefail

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$repo_root"

collect_files() {
  if [[ -n "$(git status --porcelain --untracked-files=normal 2>/dev/null || true)" ]]; then
    {
      git diff --name-only --diff-filter=ACMRTUXB -- '*.go'
      git ls-files --others --exclude-standard -- '*.go'
    } | awk 'NF' | sort -u
    return
  fi

  git ls-files '*.go'
}

mapfile -t files < <(collect_files)

if [[ ${#files[@]} -eq 0 ]]; then
  exit 0
fi

python3 - "$repo_root" "${files[@]}" <<'PY'
from __future__ import annotations

import pathlib
import re
import sys

repo_root = pathlib.Path(sys.argv[1])
files = [pathlib.Path(p) for p in sys.argv[2:]]

call_pattern = re.compile(r'\b(?:slog|log)\.\w+\(\s*"([^"\\]*(?:\\.[^"\\]*)*)"')

issues: list[str] = []
for rel_path in files:
    path = repo_root / rel_path
    try:
        text = path.read_text(encoding="utf-8")
    except FileNotFoundError:
        continue

    for lineno, line in enumerate(text.splitlines(), start=1):
        for match in call_pattern.finditer(line):
            message = bytes(match.group(1), "utf-8").decode("unicode_escape")
            if not message:
                continue
            first = message[0]
            if first.isalpha() and not first.isupper():
                issues.append(f"{rel_path}:{lineno}: log message should start with a capital letter: {message}")

if issues:
    print("\n".join(issues))
    sys.exit(1)
PY
