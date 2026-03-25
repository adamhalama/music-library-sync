#!/usr/bin/env bash
set -euo pipefail

: "${VERSION:?VERSION is required}"
: "${GOARCH:?GOARCH is required}"
: "${UDL_BIN:?UDL_BIN is required}"
: "${SCDL_BIN:?SCDL_BIN is required}"
: "${YTDLP_BIN:?YTDLP_BIN is required}"

dist_dir="${DIST_DIR:-dist}"
release_dir="${dist_dir}/bundle-root"
bundle_name="udl-v${VERSION}-darwin-${GOARCH}"
bundle_root="${release_dir}/${bundle_name}"

rm -rf "$bundle_root"
mkdir -p "${bundle_root}/tools"

cp "$UDL_BIN" "${bundle_root}/udl"
cp "$SCDL_BIN" "${bundle_root}/tools/scdl"
cp "$YTDLP_BIN" "${bundle_root}/tools/yt-dlp"
chmod +x "${bundle_root}/udl" "${bundle_root}/tools/scdl" "${bundle_root}/tools/yt-dlp"

archive_path="${dist_dir}/${bundle_name}.tar.gz"
rm -f "$archive_path"
tar -C "$release_dir" -czf "$archive_path" "$bundle_name"
shasum -a 256 "$archive_path" | awk '{print $1 "  " $2}' > "${archive_path}.sha256"

echo "$archive_path"
