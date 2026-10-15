#!/usr/bin/env bash
set -euo pipefail

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
backend="${UDL_BIN:-${repo_root}/bin/udl}"
output_dir="${repo_root}/dist/dev"
output_app="${output_dir}/UDL-Dev.app"
entitlements="${repo_root}/macos/UDL/UDL.entitlements"
source_dir="${repo_root}/macos/UDL"

[[ "$(uname -s)" == "Darwin" ]] || {
  echo "the native development app can only be built on macOS" >&2
  exit 1
}
[[ -x "$backend" ]] || {
  echo "development backend is missing; run 'make build' first: $backend" >&2
  exit 1
}
command -v xcrun >/dev/null
command -v codesign >/dev/null
command -v plutil >/dev/null

case "$(uname -m)" in
  arm64) swift_target="arm64-apple-macosx14.0" ;;
  x86_64) swift_target="x86_64-apple-macosx14.0" ;;
  *)
    echo "unsupported development architecture: $(uname -m)" >&2
    exit 1
    ;;
esac

work_dir="$(mktemp -d "${TMPDIR:-/tmp}/udl-dev-app.XXXXXX")"
trap 'rm -rf "$work_dir"' EXIT
stage_app="${work_dir}/UDL-Dev.app"
contents="${stage_app}/Contents"
module_cache="${work_dir}/swift-module-cache"

mkdir -p "${contents}/MacOS" "${contents}/Resources" "$module_cache"
cp "${source_dir}/Info.plist" "${contents}/Info.plist"
plutil -replace CFBundleDevelopmentRegion -string en "${contents}/Info.plist"
plutil -replace CFBundleDisplayName -string "UDL Dev" "${contents}/Info.plist"
plutil -replace CFBundleExecutable -string UDL "${contents}/Info.plist"
plutil -replace CFBundleIdentifier -string com.jaa.udl.dev "${contents}/Info.plist"
plutil -replace CFBundleName -string UDL "${contents}/Info.plist"
plutil -replace CFBundleShortVersionString -string "${APP_VERSION:-0.1.0}" "${contents}/Info.plist"
plutil -replace CFBundleVersion -string "${APP_BUILD_NUMBER:-1}" "${contents}/Info.plist"
plutil -replace LSMinimumSystemVersion -string 14.0 "${contents}/Info.plist"

swift_sources=()
while IFS= read -r source; do
  swift_sources+=("$source")
done < <(find "$source_dir" -type f -name '*.swift' | sort)
[[ "${#swift_sources[@]}" -gt 0 ]] || {
  echo "no Swift application sources found under $source_dir" >&2
  exit 1
}

xcrun swiftc \
  -swift-version 6 \
  -strict-concurrency=complete \
  -parse-as-library \
  -module-cache-path "$module_cache" \
  -target "$swift_target" \
  "${swift_sources[@]}" \
  -o "${contents}/MacOS/UDL"

cp "$backend" "${contents}/Resources/udl"
chmod 755 "${contents}/MacOS/UDL" "${contents}/Resources/udl"

codesign --force --options runtime --sign - "${contents}/Resources/udl"
codesign --force --options runtime --sign - --entitlements "$entitlements" "$stage_app"
codesign --verify --deep --strict --verbose=2 "$stage_app"

mkdir -p "$output_dir"
rm -rf "$output_app"
ditto "$stage_app" "$output_app"

echo "Built development app -> $output_app"
echo "Open it with: open '$output_app'"
