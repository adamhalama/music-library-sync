#!/usr/bin/env bash
set -euo pipefail

if [[ $# -ne 1 ]]; then
  echo "usage: $0 <bundle.tar.gz>" >&2
  exit 2
fi

archive_path="$1"
tmpdir="$(mktemp -d)"
trap 'rm -rf "$tmpdir"' EXIT

tar -C "$tmpdir" -xzf "$archive_path"
bundle_dir="$(find "$tmpdir" -maxdepth 1 -type d -name 'udl-v*' | head -n 1)"
if [[ -z "$bundle_dir" ]]; then
  echo "unable to find extracted bundle directory" >&2
  exit 1
fi

if [[ ! -f "${bundle_dir}/NOTICES" ]]; then
  echo "bundle is missing NOTICES" >&2
  exit 1
fi

export PATH="${bundle_dir}/tools:${PATH}"
"${bundle_dir}/udl" version
"${bundle_dir}/udl" doctor
