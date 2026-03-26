#!/usr/bin/env bash
set -euo pipefail

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"

if ! command -v rg >/dev/null 2>&1; then
  echo "ripgrep (rg) is required" >&2
  exit 1
fi

module_path="$(cd "$repo_root" && go list -m -f '{{.Path}}')"
deps="$(
  cd "$repo_root" &&
    go list -deps -f '{{if not .Standard}}{{.ImportPath}}{{end}}' ./... |
    rg -v "^${module_path}(/|$)" |
    rg -i '(^|/)(scdl|yt[-_]?dlp|youtube[-_]?dl)(/|$)' ||
    true
)"

if [[ -n "$deps" ]]; then
  echo "unexpected library dependency on bundled tools detected:" >&2
  echo "$deps" >&2
  exit 1
fi

echo "verified: bundled tools are not imported as Go libraries"
