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

export HOME="${tmpdir}/home"
export XDG_CONFIG_HOME="${tmpdir}/xdg-config"
export XDG_STATE_HOME="${tmpdir}/xdg-state"
export PATH="/usr/bin:/bin:/usr/sbin:/sbin"
mkdir -p "${HOME}" "${XDG_CONFIG_HOME}" "${XDG_STATE_HOME}"
mkdir -p "${HOME}/Music/downloaded"
cat > "${tmpdir}/udl.yaml" <<EOF
version: 1
defaults:
  state_dir: "${XDG_STATE_HOME}/udl"
sources:
  - id: soundcloud-smoke
    type: soundcloud
    enabled: true
    target_dir: "${HOME}/Music/downloaded/soundcloud-smoke"
    url: "https://soundcloud.com/example-user"
    state_file: "soundcloud-smoke.sync.scdl"
    adapter:
      kind: scdl
EOF
(
  cd "$tmpdir"
  "${bundle_dir}/udl" version
  doctor_output="$("${bundle_dir}/udl" doctor --config "${tmpdir}/udl.yaml" || true)"
  printf '%s\n' "$doctor_output"
  printf '%s\n' "$doctor_output" | grep -F 'scdl not found in PATH'
  printf '%s\n' "$doctor_output" | grep -F 'yt-dlp not found in PATH'
)
