#!/usr/bin/env bash
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd "$SCRIPT_DIR/.." && pwd)"

DEFAULT_BASE="$HOME/Music/downloaded"
DEFAULT_SYNC_URL="https://soundcloud.com/janxadam/likes"

FREE_SOURCE_DIR=""
LIB_SOURCE_DIR=""
SMOKE_ROOT=""
RUN_SYNC_SMOKE=1
SYNC_URL="$DEFAULT_SYNC_URL"

usage() {
  cat <<'EOF'
Usage: bin/smoke-freedl-hardening.sh [options]

Runs isolated smoke tests for:
  - promote-freedl preview / ambiguity / apply-to-write-dir / in-place
  - codec+bitrate verification for promoted outputs
  - scdl-freedl stuck-ledger write path (forced browser-launch failure)

Options:
  --free-source-dir <path>   Source free-DL directory (default: auto-detect under ~/Music/downloaded/udl-freedl-*/tracks)
  --lib-source-dir <path>    Source library directory (default: auto-detect under ~/Music/downloaded/udl-freedl-test-*/sc-likes-test)
  --smoke-root <path>        Explicit sandbox root (default: ~/Music/downloaded/udl-smoke-YYYYMMDD-HHMMSS)
  --sync-url <url>           SoundCloud URL used for forced-launch-failure sync smoke
  --skip-sync-smoke          Skip scdl-freedl sync smoke (offline mode)
  -h, --help                 Show help
EOF
}

log() {
  printf '[smoke] %s\n' "$*"
}

fail() {
  printf '[smoke][FAIL] %s\n' "$*" >&2
  exit 1
}

require_bin() {
  local bin="$1"
  command -v "$bin" >/dev/null 2>&1 || fail "missing required binary: $bin"
}

detect_latest_dir() {
  local found=""
  if [ "$#" -eq 0 ]; then
    printf '%s' ""
    return 0
  fi
  found="$(ls -1td "$@" 2>/dev/null | head -n 1 || true)"
  printf '%s' "$found"
}

detect_latest_dir_by_name() {
  local found=""
  if [ "$#" -eq 0 ]; then
    printf '%s' ""
    return 0
  fi
  found="$(printf '%s\n' "$@" 2>/dev/null | LC_ALL=C sort | tail -n 1 || true)"
  if [ -n "$found" ] && [ ! -d "$found" ]; then
    found=""
  fi
  printf '%s' "$found"
}

detect_preferred_library_dir() {
  local candidate
  for candidate in \
    "$DEFAULT_BASE/TEST_COPY_udl-test-sc-clean_DAYSTEP" \
    "$DEFAULT_BASE/TEST_COPY_udl-test-sc-clean" \
    "$DEFAULT_BASE/udl-test-sc-clean"; do
    if [ -d "$candidate" ]; then
      printf '%s' "$candidate"
      return 0
    fi
  done
  detect_latest_dir_by_name "$DEFAULT_BASE"/udl-freedl-test-*/sc-likes-test
}

normalize_key() {
  local raw="$1"
  printf '%s' "$raw" \
    | tr '[:upper:]' '[:lower:]' \
    | sed -E 's/[^[:alnum:]]+/ /g; s/[[:space:]]+/ /g; s/^ //; s/ $//'
}

media_title_or_stem() {
  local file="$1"
  local title
  title="$(
    ffprobe -v error -show_entries format_tags=title -of default=noprint_wrappers=1:nokey=1 "$file" 2>/dev/null \
      | head -n 1 \
      | sed 's/[[:space:]]*$//'
  )"
  if [ -n "${title:-}" ]; then
    printf '%s' "$title"
    return 0
  fi
  local base
  base="$(basename "$file")"
  printf '%s' "${base%.*}"
}

build_index_file() {
  local dir="$1"
  local out="$2"
  : >"$out"
  find "$dir" -maxdepth 1 -type f \
    \( -iname '*.m4a' -o -iname '*.mp3' -o -iname '*.wav' -o -iname '*.aif' -o -iname '*.aiff' -o -iname '*.aac' -o -iname '*.flac' -o -iname '*.ogg' -o -iname '*.opus' \) \
    -print0 \
    | while IFS= read -r -d '' f; do
      local title
      local key
      title="$(media_title_or_stem "$f")"
      key="$(normalize_key "$title")"
      if [ -n "$key" ]; then
        printf '%s\t%s\n' "$key" "$f" >>"$out"
      fi
    done
}

run_cmd() {
  local name="$1"
  shift
  local log_file="$LOG_DIR/$name.log"
  log "running [$name]: $*"
  set +e
  "$@" >"$log_file" 2>&1
  local rc=$?
  set -e
  cat "$log_file"
  return "$rc"
}

assert_log_contains() {
  local log_file="$1"
  local pattern="$2"
  if ! grep -qE "$pattern" "$log_file"; then
    fail "expected pattern '$pattern' in $log_file"
  fi
}

write_sha_file() {
  local dir="$1"
  local out="$2"
  : >"$out"
  while IFS= read -r f; do
    shasum "$f" >>"$out"
  done < <(find "$dir" -maxdepth 1 -type f -name '*.m4a' | LC_ALL=C sort)
}

while [ $# -gt 0 ]; do
  case "$1" in
    --free-source-dir)
      FREE_SOURCE_DIR="${2:-}"
      shift 2
      ;;
    --lib-source-dir)
      LIB_SOURCE_DIR="${2:-}"
      shift 2
      ;;
    --smoke-root)
      SMOKE_ROOT="${2:-}"
      shift 2
      ;;
    --sync-url)
      SYNC_URL="${2:-}"
      shift 2
      ;;
    --skip-sync-smoke)
      RUN_SYNC_SMOKE=0
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

require_bin go
require_bin ffprobe
require_bin ffmpeg
if [ "$RUN_SYNC_SMOKE" -eq 1 ]; then
  require_bin yt-dlp
fi

if [ -z "$LIB_SOURCE_DIR" ]; then
  LIB_SOURCE_DIR="$(detect_preferred_library_dir)"
fi
if [ -z "$FREE_SOURCE_DIR" ]; then
  # shellcheck disable=SC2086
  FREE_SOURCE_DIR="$(detect_latest_dir_by_name $DEFAULT_BASE/udl-freedl-*/tracks)"
fi

[ -d "${FREE_SOURCE_DIR:-}" ] || fail "free source dir not found: ${FREE_SOURCE_DIR:-<empty>}"
[ -d "${LIB_SOURCE_DIR:-}" ] || fail "library source dir not found: ${LIB_SOURCE_DIR:-<empty>}"

if [ -z "$SMOKE_ROOT" ]; then
  SMOKE_ROOT="$DEFAULT_BASE/udl-smoke-$(date +%Y%m%d-%H%M%S)"
fi

log "repo root: $REPO_ROOT"
log "free source: $FREE_SOURCE_DIR"
log "library source: $LIB_SOURCE_DIR"
log "smoke root: $SMOKE_ROOT"

mkdir -p \
  "$SMOKE_ROOT/fixtures/free" \
  "$SMOKE_ROOT/fixtures/library" \
  "$SMOKE_ROOT/fixtures/library-inplace" \
  "$SMOKE_ROOT/fixtures/ambiguous/free" \
  "$SMOKE_ROOT/fixtures/ambiguous/library" \
  "$SMOKE_ROOT/out" \
  "$SMOKE_ROOT/out-mp3" \
  "$SMOKE_ROOT/out-wav" \
  "$SMOKE_ROOT/state" \
  "$SMOKE_ROOT/browser-downloads" \
  "$SMOKE_ROOT/logs"

LOG_DIR="$SMOKE_ROOT/logs"
FREE_INDEX="$SMOKE_ROOT/free.index.tsv"
LIB_INDEX="$SMOKE_ROOT/lib.index.tsv"
PAIR_FILE="$SMOKE_ROOT/pairs.tsv"
SELECTED_PAIRS="$SMOKE_ROOT/selected-pairs.tsv"

build_index_file "$FREE_SOURCE_DIR" "$FREE_INDEX"
build_index_file "$LIB_SOURCE_DIR" "$LIB_INDEX"

awk -F '\t' '
  NR==FNR {
    if (!($1 in lib)) {
      lib[$1] = $2
    }
    next
  }
  {
    if (($1 in lib) && !($1 in seen)) {
      print $1 "\t" lib[$1] "\t" $2
      seen[$1] = 1
    }
  }
' "$LIB_INDEX" "$FREE_INDEX" | LC_ALL=C sort >"$PAIR_FILE"

head -n 3 "$PAIR_FILE" >"$SELECTED_PAIRS"
SELECT_COUNT="$(wc -l <"$SELECTED_PAIRS" | tr -d ' ')"
if [ "$SELECT_COUNT" -lt 2 ]; then
  fail "need at least 2 title-matched pairs between free/library dirs (found: $SELECT_COUNT)"
fi

FIRST_PAIR_FREE=""
FIRST_PAIR_LIB=""
while IFS=$'\t' read -r key lib_path free_path; do
  [ -n "$key" ] || continue
  cp -f "$free_path" "$SMOKE_ROOT/fixtures/free/"
  cp -f "$lib_path" "$SMOKE_ROOT/fixtures/library/"
  cp -f "$lib_path" "$SMOKE_ROOT/fixtures/library-inplace/"
  if [ -z "$FIRST_PAIR_FREE" ]; then
    FIRST_PAIR_FREE="$free_path"
    FIRST_PAIR_LIB="$lib_path"
  fi
done <"$SELECTED_PAIRS"

[ -n "$FIRST_PAIR_FREE" ] || fail "unable to select first matched pair"
[ -n "$FIRST_PAIR_LIB" ] || fail "unable to select first matched library file"

FIRST_EXT=".${FIRST_PAIR_FREE##*.}"
cp -f "$FIRST_PAIR_FREE" "$SMOKE_ROOT/fixtures/ambiguous/free/alt-1$FIRST_EXT"
cp -f "$FIRST_PAIR_FREE" "$SMOKE_ROOT/fixtures/ambiguous/free/alt-2$FIRST_EXT"
cp -f "$FIRST_PAIR_LIB" "$SMOKE_ROOT/fixtures/ambiguous/library/"

log "selected fixture pairs: $SELECT_COUNT"
while IFS=$'\t' read -r key lib_path free_path; do
  printf '  - key=%s\n    lib=%s\n    free=%s\n' "$key" "$lib_path" "$free_path"
done <"$SELECTED_PAIRS"

cd "$REPO_ROOT"

run_cmd "01-go-test" go test ./... || fail "go test ./... failed"

run_cmd "02-promote-preview" \
  go run ./cmd/udl promote-freedl \
    --free-dl-dir "$SMOKE_ROOT/fixtures/free" \
    --library-dir "$SMOKE_ROOT/fixtures/library" \
    --target-format aac-256 \
    --min-match-score 78 \
    -v || fail "promote preview failed"
assert_log_contains "$LOG_DIR/02-promote-preview.log" 'mode=preview'
assert_log_contains "$LOG_DIR/02-promote-preview.log" '\[plan\]'
assert_log_contains "$LOG_DIR/02-promote-preview.log" 'PICHI - BO FUNK \[FREE DL\].m4a <= MASTER BOFUNK'

run_cmd "03-promote-ambiguous" \
  go run ./cmd/udl promote-freedl \
    --free-dl-dir "$SMOKE_ROOT/fixtures/ambiguous/free" \
    --library-dir "$SMOKE_ROOT/fixtures/ambiguous/library" \
    --min-match-score 60 \
    --ambiguity-gap 8 \
    -v || fail "promote ambiguous run failed"
assert_log_contains "$LOG_DIR/03-promote-ambiguous.log" 'ambiguous=1'
assert_log_contains "$LOG_DIR/03-promote-ambiguous.log" 'ambiguous-match'

write_sha_file "$SMOKE_ROOT/fixtures/library" "$LOG_DIR/library-write-dir-before.sha1"
run_cmd "04-promote-apply-write-dir" \
  go run ./cmd/udl promote-freedl \
    --free-dl-dir "$SMOKE_ROOT/fixtures/free" \
    --library-dir "$SMOKE_ROOT/fixtures/library" \
    --write-dir "$SMOKE_ROOT/out" \
    --target-format aac-256 \
    --min-match-score 78 \
    --apply \
    -v || fail "promote apply write-dir failed"
assert_log_contains "$LOG_DIR/04-promote-apply-write-dir.log" '\[done\]'

write_sha_file "$SMOKE_ROOT/fixtures/library" "$LOG_DIR/library-write-dir-after.sha1"
if ! diff -u "$LOG_DIR/library-write-dir-before.sha1" "$LOG_DIR/library-write-dir-after.sha1" >"$LOG_DIR/library-write-dir.diff"; then
  cat "$LOG_DIR/library-write-dir.diff"
  fail "--write-dir mode modified library fixture unexpectedly"
fi

OUT_COUNT="$(find "$SMOKE_ROOT/out" -maxdepth 1 -type f -name '*.m4a' | wc -l | tr -d ' ')"
if [ "$OUT_COUNT" -lt "$SELECT_COUNT" ]; then
  fail "expected at least $SELECT_COUNT output files, got $OUT_COUNT"
fi

PROBE_LOG="$LOG_DIR/05-promote-out-probe.log"
: >"$PROBE_LOG"
while IFS= read -r out_file; do
  [ -f "$out_file" ] || continue
  codec="$(ffprobe -v error -select_streams a:0 -show_entries stream=codec_name -of default=noprint_wrappers=1:nokey=1 "$out_file" | head -n 1)"
  bitrate="$(ffprobe -v error -select_streams a:0 -show_entries stream=bit_rate -of default=noprint_wrappers=1:nokey=1 "$out_file" | head -n 1)"
  printf '%s codec=%s bit_rate=%s\n' "$out_file" "$codec" "$bitrate" >>"$PROBE_LOG"
  if [ "$codec" != "aac" ]; then
    fail "unexpected codec for $out_file: $codec"
  fi
  if ! printf '%s' "$bitrate" | grep -Eq '^[0-9]+$'; then
    fail "missing numeric bitrate for $out_file: $bitrate"
  fi
  if [ "$bitrate" -le 0 ]; then
    fail "non-positive bitrate for $out_file: $bitrate"
  fi
done < <(find "$SMOKE_ROOT/out" -maxdepth 1 -type f -name '*.m4a' | LC_ALL=C sort)
cat "$PROBE_LOG"

run_cmd "05b-promote-mp3-320" \
  go run ./cmd/udl promote-freedl \
    --free-dl-dir "$SMOKE_ROOT/fixtures/free" \
    --library-dir "$SMOKE_ROOT/fixtures/library" \
    --write-dir "$SMOKE_ROOT/out-mp3" \
    --target-format mp3-320 \
    --replace-limit 1 \
    --min-match-score 78 \
    --apply \
    -v || fail "promote mp3-320 apply failed"
assert_log_contains "$LOG_DIR/05b-promote-mp3-320.log" '\[done\]'
MP3_FILE="$(find "$SMOKE_ROOT/out-mp3" -maxdepth 1 -type f -name '*.mp3' | head -n 1)"
[ -f "${MP3_FILE:-}" ] || fail "expected one .mp3 output in $SMOKE_ROOT/out-mp3"
MP3_CODEC="$(ffprobe -v error -select_streams a:0 -show_entries stream=codec_name -of default=noprint_wrappers=1:nokey=1 "$MP3_FILE" | head -n 1)"
[ "$MP3_CODEC" = "mp3" ] || fail "unexpected codec for $MP3_FILE: $MP3_CODEC"

run_cmd "05c-promote-wav" \
  go run ./cmd/udl promote-freedl \
    --free-dl-dir "$SMOKE_ROOT/fixtures/free" \
    --library-dir "$SMOKE_ROOT/fixtures/library" \
    --write-dir "$SMOKE_ROOT/out-wav" \
    --target-format wav \
    --replace-limit 1 \
    --min-match-score 78 \
    --apply \
    -v || fail "promote wav apply failed"
assert_log_contains "$LOG_DIR/05c-promote-wav.log" '\[done\]'
WAV_FILE="$(find "$SMOKE_ROOT/out-wav" -maxdepth 1 -type f -name '*.wav' | head -n 1)"
[ -f "${WAV_FILE:-}" ] || fail "expected one .wav output in $SMOKE_ROOT/out-wav"
WAV_CODEC="$(ffprobe -v error -select_streams a:0 -show_entries stream=codec_name -of default=noprint_wrappers=1:nokey=1 "$WAV_FILE" | head -n 1)"
if ! printf '%s' "$WAV_CODEC" | grep -Eq '^pcm_'; then
  fail "unexpected codec for $WAV_FILE: $WAV_CODEC"
fi

write_sha_file "$SMOKE_ROOT/fixtures/library-inplace" "$LOG_DIR/inplace-before.sha1"
find "$SMOKE_ROOT/fixtures/library-inplace" -maxdepth 1 -type f -name '*.m4a' | sed "s|$SMOKE_ROOT/fixtures/library-inplace/||" | LC_ALL=C sort >"$LOG_DIR/inplace-before.names"

run_cmd "06-promote-apply-inplace" \
  go run ./cmd/udl promote-freedl \
    --free-dl-dir "$SMOKE_ROOT/fixtures/free" \
    --library-dir "$SMOKE_ROOT/fixtures/library-inplace" \
    --target-format auto \
    --min-match-score 78 \
    --apply \
    -v || fail "promote apply inplace failed"
assert_log_contains "$LOG_DIR/06-promote-apply-inplace.log" '\[done\]'

write_sha_file "$SMOKE_ROOT/fixtures/library-inplace" "$LOG_DIR/inplace-after.sha1"
find "$SMOKE_ROOT/fixtures/library-inplace" -maxdepth 1 -type f -name '*.m4a' | sed "s|$SMOKE_ROOT/fixtures/library-inplace/||" | LC_ALL=C sort >"$LOG_DIR/inplace-after.names"

if ! diff -u "$LOG_DIR/inplace-before.names" "$LOG_DIR/inplace-after.names" >"$LOG_DIR/inplace-names.diff"; then
  cat "$LOG_DIR/inplace-names.diff"
  fail "in-place run changed file paths/names in library-inplace fixture"
fi
if diff -u "$LOG_DIR/inplace-before.sha1" "$LOG_DIR/inplace-after.sha1" >"$LOG_DIR/inplace-sha.diff"; then
  fail "in-place run did not change any file content"
fi
cat "$LOG_DIR/inplace-sha.diff"

if [ "$RUN_SYNC_SMOKE" -eq 1 ]; then
  SC_SMOKE_DIR="$SMOKE_ROOT/sc-free-launchfail"
  mkdir -p "$SC_SMOKE_DIR/tracks" "$SC_SMOKE_DIR/state"
  cat >"$SC_SMOKE_DIR/config.yaml" <<EOF
version: 1
defaults:
  state_dir: "$SC_SMOKE_DIR/state"
  archive_file: "archive.txt"
  command_timeout_seconds: 120
  continue_on_error: true
sources:
  - id: "sc-free"
    type: "soundcloud"
    enabled: true
    url: "$SYNC_URL"
    target_dir: "$SC_SMOKE_DIR/tracks"
    state_file: "sc-free.sync.scdl"
    adapter:
      kind: "scdl-freedl"
EOF

  if run_cmd "07-sync-launch-fail-ledger" \
      env UDL_FREEDL_BROWSER_APP="__UDL_SMOKE_INVALID_BROWSER_APP__" \
          UDL_FREEDL_BROWSER_DOWNLOAD_DIR="$SMOKE_ROOT/browser-downloads" \
      go run ./cmd/udl -c "$SC_SMOKE_DIR/config.yaml" sync --source sc-free -v; then
    fail "expected sync launch-fail smoke to return non-zero"
  fi

  assert_log_contains "$LOG_DIR/07-sync-launch-fail-ledger.log" 'browser launch failed'
  STUCK_LEDGER="$SC_SMOKE_DIR/state/sc-free.freedl-stuck.jsonl"
  [ -f "$STUCK_LEDGER" ] || fail "expected stuck ledger file at $STUCK_LEDGER"
  assert_log_contains "$STUCK_LEDGER" '"stage":"browser-launch"'
  assert_log_contains "$STUCK_LEDGER" '"source_id":"sc-free"'
  assert_log_contains "$STUCK_LEDGER" '"track_id":"'
  assert_log_contains "$STUCK_LEDGER" '"purchase_url":"'
  assert_log_contains "$STUCK_LEDGER" '"strategy":"browser-handoff"'
  log "stuck ledger: $STUCK_LEDGER"
else
  log "skipped sync smoke (--skip-sync-smoke)"
fi

cat <<EOF

[smoke] PASS
[smoke] smoke root: $SMOKE_ROOT
[smoke] logs:       $LOG_DIR
EOF
