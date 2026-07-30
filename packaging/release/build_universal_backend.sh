#!/usr/bin/env bash
set -euo pipefail

: "${VERSION:?VERSION is required}"

commit="${COMMIT:-$(git rev-parse HEAD)}"
build_date="${BUILD_DATE:-$(date -u +%Y-%m-%dT%H:%M:%SZ)}"
output="${OUTPUT:-dist/udl-universal}"
work_dir="${WORK_DIR:-dist/universal-backend}"

mkdir -p "$work_dir" "$(dirname "$output")"

ldflags="-s -w -X main.version=${VERSION} -X main.commit=${commit} -X main.date=${build_date}"
for arch in arm64 amd64; do
  GOOS=darwin GOARCH="$arch" CGO_ENABLED=0 \
    go build -trimpath -ldflags "$ldflags" -o "${work_dir}/udl-${arch}" ./cmd/udl
done

lipo -create "${work_dir}/udl-arm64" "${work_dir}/udl-amd64" -output "$output"
chmod 755 "$output"
lipo -info "$output"
