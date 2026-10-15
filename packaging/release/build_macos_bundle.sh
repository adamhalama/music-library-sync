#!/usr/bin/env bash
set -euo pipefail

: "${VERSION:?VERSION is required}"
: "${UDL_BIN:?UDL_BIN is required}"

dist_dir="${DIST_DIR:-dist}"

if [[ -n "${APP_BUNDLE_PATH:-}" ]]; then
  mkdir -p "$dist_dir"
  app_name="UDL-v${VERSION}.app"
  app_path="${dist_dir}/${app_name}"
  archive_path="${dist_dir}/UDL-v${VERSION}-macOS-universal.zip"
  entitlements="${ENTITLEMENTS_PATH:-macos/UDL/UDL.entitlements}"

  lipo_info="$(lipo -info "$UDL_BIN")"
  [[ "$lipo_info" == *"arm64"* && "$lipo_info" == *"x86_64"* ]] || {
    echo "embedded udl must contain arm64 and x86_64 slices: $lipo_info" >&2
    exit 1
  }

  rm -rf "$app_path"
  ditto "$APP_BUNDLE_PATH" "$app_path"
  mkdir -p "${app_path}/Contents/Resources"
  cp "$UDL_BIN" "${app_path}/Contents/Resources/udl"
  chmod 755 "${app_path}/Contents/Resources/udl"

  sign_args=(--force --options runtime --sign -)
  codesign "${sign_args[@]}" "${app_path}/Contents/Resources/udl"
  codesign "${sign_args[@]}" --entitlements "$entitlements" "$app_path"
  codesign --verify --strict --deep --verbose=2 "$app_path"
  "${app_path}/Contents/Resources/udl" version

  rm -f "$archive_path"
  ditto -c -k --keepParent "$app_path" "$archive_path"

  shasum -a 256 "$archive_path" | awk '{print $1 "  " $2}' > "${archive_path}.sha256"
  echo "$archive_path"
  exit 0
fi

: "${GOARCH:?GOARCH is required for the CLI bundle}"
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
