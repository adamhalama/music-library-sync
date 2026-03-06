#!/usr/bin/env bash
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd "$SCRIPT_DIR/.." && pwd)"

DEFAULT_BASE="$HOME/Music/downloaded"

SCDL_URL=""
SMOKE_ROOT=""
ALLOW_LARGE_SOURCE=0
SKIP_GO_TEST=0

LAST_LOG=""
LAST_RC=0
STATE_MAIN_FILE=""
DOWNLOAD_COUNT=0

usage() {
  cat <<'EOF'
Usage: bin/smoke-main-udl.sh --scdl-url <url> [options]

Runs isolated smoke tests for core UDL workflows (non-FreeDL):
  - init
  - validate (good + bad config)
  - doctor --json checks
  - sync (dry-run/apply/idempotent/scan-gaps/no-preflight)
  - continue_on_error partial-success behavior

Options:
  --scdl-url <url>        Required SoundCloud URL (fast profile: single-track URL)
  --smoke-root <path>     Explicit sandbox root
  --allow-large-source    Allow likes/profile/playlist-like URLs
  --skip-go-test          Skip go test ./... step
  -h, --help              Show help
EOF
}

log() {
  printf '[smoke-main] %s\n' "$*"
}

fail() {
  printf '[smoke-main][FAIL] %s\n' "$*" >&2
  exit 1
}

require_bin() {
  local bin="$1"
  command -v "$bin" >/dev/null 2>&1 || fail "missing required binary: $bin"
}

supports_scdl_ytdlp_args() {
  local bin="$1"
  "$bin" -h 2>&1 | tr '[:upper:]' '[:lower:]' | grep -q -- '--yt-dlp-args'
}

assert_log_contains() {
  local log_file="$1"
  local pattern="$2"
  if ! grep -Eq "$pattern" "$log_file"; then
    fail "expected pattern '$pattern' in $log_file"
  fi
}

assert_log_not_contains() {
  local log_file="$1"
  local pattern="$2"
  if grep -Eq "$pattern" "$log_file"; then
    fail "unexpected pattern '$pattern' found in $log_file"
  fi
}

run_cmd() {
  local step="$1"
  shift
  local log_file="$LOG_DIR/$step.log"
  LAST_LOG="$log_file"
  log "running [$step]: $*"
  set +e
  "$@" 2>&1 | tee "$log_file"
  LAST_RC=${PIPESTATUS[0]}
  set -e
}

assert_last_success() {
  if [ "$LAST_RC" -ne 0 ]; then
    fail "command failed (rc=$LAST_RC), see $LAST_LOG"
  fi
}

assert_last_go_exit_status() {
  local wanted_status="$1"
  if [ "$LAST_RC" -eq 0 ]; then
    fail "expected go run command to fail with exit status $wanted_status, but it succeeded"
  fi
  assert_log_contains "$LAST_LOG" "exit status $wanted_status"
}

is_large_source_url() {
  local raw="$1"
  local lower
  lower="$(printf '%s' "$raw" | tr '[:upper:]' '[:lower:]')"

  if printf '%s' "$lower" | grep -Eq '/likes([/?#]|$)|/sets/|/albums/|/reposts([/?#]|$)'; then
    return 0
  fi

  local path
  path="$(printf '%s' "$lower" | sed -E 's#^https?://[^/]+/?##')"
  path="${path%%\?*}"
  path="${path%%#*}"
  path="${path%/}"

  if [ -z "$path" ]; then
    return 0
  fi

  if ! printf '%s' "$path" | grep -q '/'; then
    return 0
  fi

  return 1
}

count_media_files() {
  local dir="$1"
  find "$dir" -type f \
    \( -iname '*.m4a' -o -iname '*.mp3' -o -iname '*.wav' -o -iname '*.aif' -o -iname '*.aiff' -o -iname '*.aac' -o -iname '*.flac' -o -iname '*.ogg' -o -iname '*.opus' \) \
    | wc -l | tr -d ' '
}

resolve_compatible_scdl_bin() {
  local candidate=""
  if [ -n "${UDL_SCDL_BIN:-}" ]; then
    if [ ! -x "$UDL_SCDL_BIN" ]; then
      fail "UDL_SCDL_BIN is set but not executable: $UDL_SCDL_BIN"
    fi
    if ! supports_scdl_ytdlp_args "$UDL_SCDL_BIN"; then
      fail "UDL_SCDL_BIN does not support --yt-dlp-args (requires scdl >= 3.0.0): $UDL_SCDL_BIN"
    fi
    printf '%s' "$UDL_SCDL_BIN"
    return 0
  fi

  while IFS= read -r candidate; do
    [ -n "$candidate" ] || continue
    [ -x "$candidate" ] || continue
    if supports_scdl_ytdlp_args "$candidate"; then
      printf '%s' "$candidate"
      return 0
    fi
  done < <(which -a scdl 2>/dev/null | awk '!seen[$0]++')

  return 1
}

while [ $# -gt 0 ]; do
  case "$1" in
    --scdl-url)
      SCDL_URL="${2:-}"
      shift 2
      ;;
    --smoke-root)
      SMOKE_ROOT="${2:-}"
      shift 2
      ;;
    --allow-large-source)
      ALLOW_LARGE_SOURCE=1
      shift
      ;;
    --skip-go-test)
      SKIP_GO_TEST=1
      shift
      ;;
    -h|--help)
      usage
      exit 0
      ;;
    *)
      fail "unknown option: $1"
      ;;
  esac
done

[ -n "${SCDL_URL:-}" ] || fail "--scdl-url is required"

if [ "$ALLOW_LARGE_SOURCE" -eq 0 ] && is_large_source_url "$SCDL_URL"; then
  fail "refusing likely large source URL in fast profile; use --allow-large-source to override"
fi

require_bin go
require_bin scdl
require_bin yt-dlp
require_bin jq
[ -n "${SCDL_CLIENT_ID:-}" ] || fail "SCDL_CLIENT_ID is required for real SoundCloud download smoke"

SCDL_BIN_PATH="$(resolve_compatible_scdl_bin || true)"
[ -n "${SCDL_BIN_PATH:-}" ] || fail "no compatible scdl binary with --yt-dlp-args found in PATH; install scdl >= 3.0.0"
export PATH="$(dirname "$SCDL_BIN_PATH"):$PATH"
log "using scdl binary: $SCDL_BIN_PATH"

if [ -z "$SMOKE_ROOT" ]; then
  SMOKE_ROOT="$DEFAULT_BASE/udl-smoke-main-$(date +%Y%m%d-%H%M%S)"
fi

case "$SMOKE_ROOT" in
  "$DEFAULT_BASE"/udl-smoke-main-*|"$DEFAULT_BASE"/udl-smoke-*)
    ;;
  *)
    if [ "$SMOKE_ROOT" = "$HOME" ] || [ "$SMOKE_ROOT" = "/" ]; then
      fail "unsafe --smoke-root value: $SMOKE_ROOT"
    fi
    ;;
esac

mkdir -p \
  "$SMOKE_ROOT/tracks/sc-main" \
  "$SMOKE_ROOT/tracks/sc-bad" \
  "$SMOKE_ROOT/state" \
  "$SMOKE_ROOT/logs" \
  "$SMOKE_ROOT/tmp/gocache" \
  "$SMOKE_ROOT/tmp/gotmp" \
  "$SMOKE_ROOT/tmp/xdg-cache" \
  "$SMOKE_ROOT/init"

LOG_DIR="$SMOKE_ROOT/logs"
STATE_MAIN_FILE="$SMOKE_ROOT/state/sc-main.sync.scdl"
BAD_SUFFIX="$(date +%s)"
BAD_URL="https://soundcloud.com/udl-smoke-invalid-${BAD_SUFFIX}/udl-smoke-invalid-${BAD_SUFFIX}"

# Keep tool/runtime caches isolated under the smoke sandbox.
export GOCACHE="$SMOKE_ROOT/tmp/gocache"
export GOTMPDIR="$SMOKE_ROOT/tmp/gotmp"
export XDG_CACHE_HOME="$SMOKE_ROOT/tmp/xdg-cache"

cat >"$SMOKE_ROOT/config.yaml" <<EOF
version: 1
defaults:
  state_dir: "$SMOKE_ROOT/state"
  archive_file: "archive.txt"
  continue_on_error: false
  command_timeout_seconds: 240

sources:
  - id: "sc-main"
    type: "soundcloud"
    enabled: true
    target_dir: "$SMOKE_ROOT/tracks/sc-main"
    url: "$SCDL_URL"
    state_file: "sc-main.sync.scdl"
    adapter:
      kind: "scdl"

  - id: "sc-bad"
    type: "soundcloud"
    enabled: true
    target_dir: "$SMOKE_ROOT/tracks/sc-bad"
    url: "$BAD_URL"
    state_file: "sc-bad.sync.scdl"
    adapter:
      kind: "scdl"
EOF

cat >"$SMOKE_ROOT/config.bad.yaml" <<EOF
version: 1
defaults:
  state_dir: "$SMOKE_ROOT/state"
  archive_file: "archive.txt"
  continue_on_error: false

sources:
  - id: "sc-invalid-adapter"
    type: "soundcloud"
    enabled: true
    target_dir: "$SMOKE_ROOT/tracks/sc-main"
    url: "$SCDL_URL"
    state_file: "sc-invalid-adapter.sync.scdl"
    adapter:
      kind: "not-a-real-adapter"
EOF

cat >"$SMOKE_ROOT/config.continue.yaml" <<EOF
version: 1
defaults:
  state_dir: "$SMOKE_ROOT/state"
  archive_file: "archive.txt"
  continue_on_error: true
  command_timeout_seconds: 240

sources:
  - id: "sc-bad"
    type: "soundcloud"
    enabled: true
    target_dir: "$SMOKE_ROOT/tracks/sc-bad"
    url: "$BAD_URL"
    state_file: "sc-bad.sync.scdl"
    adapter:
      kind: "scdl"

  - id: "sc-main"
    type: "soundcloud"
    enabled: true
    target_dir: "$SMOKE_ROOT/tracks/sc-main"
    url: "$SCDL_URL"
    state_file: "sc-main.sync.scdl"
    adapter:
      kind: "scdl"
EOF

log "repo root:   $REPO_ROOT"
log "smoke root:  $SMOKE_ROOT"
log "source URL:  $SCDL_URL"
log "bad URL:     $BAD_URL"

cd "$REPO_ROOT"

if [ "$SKIP_GO_TEST" -eq 0 ]; then
  run_cmd "01-go-test" go test ./...
  assert_last_success
else
  log "skipped go test step (--skip-go-test)"
fi

run_cmd "02-init" env XDG_STATE_HOME="$SMOKE_ROOT/init/xdg-state" go run ./cmd/udl -c "$SMOKE_ROOT/init/udl.yaml" init --force
assert_last_success
assert_log_contains "$LAST_LOG" 'Wrote config:'
assert_log_contains "$LAST_LOG" 'Ensured state dir:'
[ -f "$SMOKE_ROOT/init/udl.yaml" ] || fail "init config was not created"

run_cmd "03-validate-good" go run ./cmd/udl -c "$SMOKE_ROOT/config.yaml" validate
assert_last_success

run_cmd "04-validate-bad" go run ./cmd/udl -c "$SMOKE_ROOT/config.bad.yaml" validate
assert_last_go_exit_status 3
assert_log_contains "$LAST_LOG" 'unsupported adapter\.kind'

run_cmd "05-doctor-json" go run ./cmd/udl -c "$SMOKE_ROOT/config.yaml" doctor --json
assert_last_success
if ! jq -e '.' "$LAST_LOG" >/dev/null 2>&1; then
  fail "doctor --json output is not valid JSON"
fi
if ! jq -e '[.checks[] | select(.message | test("scdl found at"))] | length >= 1' "$LAST_LOG" >/dev/null; then
  fail "doctor json is missing scdl dependency check"
fi
if ! jq -e '[.checks[] | select(.message | test("yt-dlp found at"))] | length >= 1' "$LAST_LOG" >/dev/null; then
  fail "doctor json is missing yt-dlp dependency check"
fi
if ! jq -e '[.checks[] | select(.severity == "error" and ((.name == "auth") or (.name == "filesystem")))] | length == 0' "$LAST_LOG" >/dev/null; then
  fail "doctor reported auth/filesystem prerequisite errors for soundcloud smoke setup"
fi

run_cmd "06-sync-dry-run-main" go run ./cmd/udl -c "$SMOKE_ROOT/config.yaml" sync --source sc-main --dry-run -v
assert_last_success
assert_log_contains "$LAST_LOG" 'preflight: remote='
assert_log_contains "$LAST_LOG" 'planned='
assert_log_contains "$LAST_LOG" 'mode='

run_cmd "07-sync-apply-main" go run ./cmd/udl -c "$SMOKE_ROOT/config.yaml" sync --source sc-main -v
assert_last_success
[ -s "$STATE_MAIN_FILE" ] || fail "expected non-empty state file at $STATE_MAIN_FILE"
COUNT_AFTER_APPLY="$(count_media_files "$SMOKE_ROOT/tracks/sc-main")"
if [ "$COUNT_AFTER_APPLY" -lt 1 ]; then
  fail "expected at least one downloaded media file in $SMOKE_ROOT/tracks/sc-main"
fi

run_cmd "08-sync-idempotent-main" go run ./cmd/udl -c "$SMOKE_ROOT/config.yaml" sync --source sc-main -v
assert_last_success
assert_log_contains "$LAST_LOG" 'planned=0|up-to-date \(no downloads planned\)'
COUNT_AFTER_IDEMPOTENT="$(count_media_files "$SMOKE_ROOT/tracks/sc-main")"
if [ "$COUNT_AFTER_IDEMPOTENT" -ne "$COUNT_AFTER_APPLY" ]; then
  fail "idempotent sync changed media file count ($COUNT_AFTER_APPLY -> $COUNT_AFTER_IDEMPOTENT)"
fi

DELETE_CANDIDATE="$(find "$SMOKE_ROOT/tracks/sc-main" -type f | LC_ALL=C sort | head -n 1)"
[ -n "${DELETE_CANDIDATE:-}" ] || fail "no downloaded file found for scan-gaps repair test"
rm -f "$DELETE_CANDIDATE"
COUNT_AFTER_DELETE="$(count_media_files "$SMOKE_ROOT/tracks/sc-main")"
if [ "$COUNT_AFTER_DELETE" -ge "$COUNT_AFTER_IDEMPOTENT" ]; then
  fail "expected media file count to drop after deletion"
fi

run_cmd "09-scan-gaps-repair" go run ./cmd/udl -c "$SMOKE_ROOT/config.yaml" sync --source sc-main --scan-gaps -v
assert_last_success
assert_log_contains "$LAST_LOG" 'known_gaps=[1-9]|planned=[1-9]'
COUNT_AFTER_REPAIR="$(count_media_files "$SMOKE_ROOT/tracks/sc-main")"
if [ "$COUNT_AFTER_REPAIR" -lt "$COUNT_AFTER_IDEMPOTENT" ]; then
  fail "scan-gaps did not restore deleted media file count ($COUNT_AFTER_REPAIR < $COUNT_AFTER_IDEMPOTENT)"
fi

run_cmd "10-source-filter-ignores-bad" go run ./cmd/udl -c "$SMOKE_ROOT/config.yaml" sync --source sc-main --dry-run -v
assert_last_success
assert_log_not_contains "$LAST_LOG" '\[sc-bad\]'

run_cmd "11-continue-on-error-partial" go run ./cmd/udl -c "$SMOKE_ROOT/config.continue.yaml" sync -v
assert_last_go_exit_status 5
assert_log_contains "$LAST_LOG" '\[sc-bad\]'
assert_log_contains "$LAST_LOG" '\[sc-main\]'
assert_log_contains "$LAST_LOG" 'sync finished with failed sources'

run_cmd "12-no-preflight-sanity" go run ./cmd/udl -c "$SMOKE_ROOT/config.yaml" sync --source sc-main --no-preflight --dry-run -v
assert_last_success
assert_log_not_contains "$LAST_LOG" 'preflight: remote='

DOWNLOAD_COUNT="$(count_media_files "$SMOKE_ROOT/tracks/sc-main")"
cat <<EOF

[smoke-main] PASS
[smoke-main] smoke root:   $SMOKE_ROOT
[smoke-main] logs:         $LOG_DIR
[smoke-main] media files:  $DOWNLOAD_COUNT
[smoke-main] state file:   $STATE_MAIN_FILE
EOF
