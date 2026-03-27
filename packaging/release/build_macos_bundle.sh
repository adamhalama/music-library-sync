#!/usr/bin/env bash
set -euo pipefail

: "${VERSION:?VERSION is required}"
: "${GOARCH:?GOARCH is required}"
: "${UDL_BIN:?UDL_BIN is required}"

dist_dir="${DIST_DIR:-dist}"
release_dir="${dist_dir}/bundle-root"
bundle_name="udl-v${VERSION}-darwin-${GOARCH}"
bundle_root="${release_dir}/${bundle_name}"

rm -rf "$bundle_root"
mkdir -p "$bundle_root"

cp "$UDL_BIN" "${bundle_root}/udl"
chmod +x "${bundle_root}/udl"

archive_path="${dist_dir}/${bundle_name}.tar.gz"
rm -f "$archive_path"
tar -C "$release_dir" -czf "$archive_path" "$bundle_name"
shasum -a 256 "$archive_path" | awk '{print $1 "  " $2}' > "${archive_path}.sha256"

echo "$archive_path"
