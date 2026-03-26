#!/usr/bin/env bash
set -euo pipefail

if [[ $# -ne 3 ]]; then
  echo "usage: $0 <url> <sha256> <output-path>" >&2
  exit 2
fi

url="$1"
expected_sha="$2"
output_path="$3"

mkdir -p "$(dirname "$output_path")"
curl -fsSL "$url" -o "$output_path"
actual_sha="$(shasum -a 256 "$output_path" | awk '{print $1}')"
if [[ "$actual_sha" != "$expected_sha" ]]; then
  echo "sha256 mismatch for $url" >&2
  echo "expected: $expected_sha" >&2
  echo "actual:   $actual_sha" >&2
  exit 1
fi
chmod +x "$output_path"
