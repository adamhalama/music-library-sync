#!/usr/bin/env bash
set -Eeuo pipefail

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
source_app="${repo_root}/dist/dev/UDL-Dev.app"
install_root="${UDL_APP_INSTALL_DIR:-${HOME}/Applications}"
installed_app="${install_root}/UDL-Dev.app"
open_after_install=1
step=0
total_steps=5
stage_dir=""
backup_app=""
installed_new=0

usage() {
  cat <<'EOF'
Usage: packaging/dev/install_macos_app.sh [--no-open]

Build, install, and register UDL Dev with Spotlight. Safe to run repeatedly.

Options:
  --no-open  Do not launch the app after installation
  -h, --help Show this help

Environment:
  UDL_APP_INSTALL_DIR  Install directory (default: ~/Applications)
EOF
}

while (($#)); do
  case "$1" in
    --no-open) open_after_install=0 ;;
    -h|--help) usage; exit 0 ;;
    *) echo "Unknown option: $1" >&2; usage >&2; exit 2 ;;
  esac
  shift
done

if [[ "$(uname -s)" != Darwin ]]; then
  echo "This installer only runs on macOS." >&2
  exit 1
fi

progress() {
  local label="$1"
  local width=24
  local filled=$((step * width / total_steps))
  local empty=$((width - filled))
  local bar_fill bar_empty
  printf -v bar_fill '%*s' "$filled" ''
  printf -v bar_empty '%*s' "$empty" ''
  printf '\n[%s%s] %d/%d  %s\n' "${bar_fill// /#}" "${bar_empty// /-}" "$step" "$total_steps" "$label"
}

restore_previous_install() {
  if [[ -n "$backup_app" && -e "$backup_app" && ! -e "$installed_app" ]]; then
    mv "$backup_app" "$installed_app"
    backup_app=""
  fi
}

cleanup() {
  local exit_code=$?
  trap - EXIT INT TERM
  if ((exit_code != 0 && installed_new)); then
    rm -rf "$installed_app"
    installed_new=0
  fi
  restore_previous_install
  if [[ -n "$stage_dir" && -d "$stage_dir" ]]; then
    rm -rf "$stage_dir"
  fi
  if [[ -n "$backup_app" && -e "$backup_app" ]]; then
    rm -rf "$backup_app"
  fi
  if ((exit_code != 0)); then
    printf '\nInstall stopped; the previous installed app was preserved.\n' >&2
  fi
  exit "$exit_code"
}

cancel() {
  printf '\nCancellation requested; cleaning up...\n' >&2
  exit 130
}

trap cleanup EXIT
trap cancel INT TERM

step=1
progress "Checking required macOS tools"
for tool in go xcrun codesign plutil ditto mdimport open; do
  if ! command -v "$tool" >/dev/null 2>&1; then
    echo "Missing required tool: $tool" >&2
    exit 1
  fi
done

step=2
progress "Building the Go backend"
make -C "$repo_root" build

step=3
progress "Building and signing UDL Dev"
bash "${repo_root}/packaging/dev/build_macos_app.sh"

step=4
progress "Installing in ${install_root}"
mkdir -p "$install_root"
stage_dir="$(mktemp -d "${install_root}/.udl-dev-install.XXXXXX")"
staged_app="${stage_dir}/UDL-Dev.app"
ditto "$source_app" "$staged_app"
codesign --verify --deep --strict "$staged_app"

if [[ -e "$installed_app" ]]; then
  backup_app="${stage_dir}/UDL-Dev.previous.app"
  mv "$installed_app" "$backup_app"
fi
mv "$staged_app" "$installed_app"
installed_new=1

step=5
progress "Registering with Spotlight"
mdimport "$installed_app"

if ((open_after_install)); then
  open "$installed_app"
fi

installed_new=0
if [[ -n "$backup_app" && -e "$backup_app" ]]; then
  rm -rf "$backup_app"
  backup_app=""
fi

printf '\nInstalled successfully: %s\n' "$installed_app"
printf 'You can now open “UDL Dev” from Spotlight.\n'
