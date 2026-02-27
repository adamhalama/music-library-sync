#!/usr/bin/env zsh
set -euo pipefail

# Applies SoundCloud-like ordering to local files by setting:
# 1) filesystem creation date (Date Created) via SetFile
# 2) embedded content date (Date Released-style tag) via ContentCreateDate
#
# It preserves original download chronology in Date Modified by default.
# A run manifest is written under experiments/rb-date-order/runs/<timestamp>/.

TARGET_DIR="/Users/jaa/Music/downloaded/TEST_COPY_udl-test-sc-clean"
LIKES_URL="https://soundcloud.com/janxadam/likes"
MODE="both" # created | released | both
GRANULARITY="second" # second | day
RELEASE_FORMAT="datetime" # datetime | date
PRESERVE_MTIME=1
DRY_RUN=0

SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
RUN_ROOT="$SCRIPT_DIR/runs"
TAB=$'\t'

usage() {
  cat <<'EOF'
Usage:
  apply_sc_order_dates.zsh [options]

Options:
  --target-dir <path>   Directory containing local audio files
  --likes-url <url>     SoundCloud likes URL used as source order
  --mode <mode>         created | released | both (default: both)
  --granularity <g>     second | day (default: second)
  --release-format <f>  datetime | date (default: datetime)
  --no-preserve-mtime   Do not force-restore original Date Modified
  --dry-run             Generate manifests without modifying files
  -h, --help            Show this help
EOF
}

normalize_title() {
  local raw="${1:-}"
  raw="${raw//：/:}"
  print -r -- "$raw" \
    | tr '[:upper:]' '[:lower:]' \
    | sed -E 's/[[:space:]]+/ /g; s/^ +//; s/ +$//'
}

while (( $# > 0 )); do
  case "$1" in
    --target-dir)
      TARGET_DIR="$2"
      shift 2
      ;;
    --likes-url)
      LIKES_URL="$2"
      shift 2
      ;;
    --mode)
      MODE="$2"
      shift 2
      ;;
    --granularity)
      GRANULARITY="$2"
      shift 2
      ;;
    --release-format)
      RELEASE_FORMAT="$2"
      shift 2
      ;;
    --no-preserve-mtime)
      PRESERVE_MTIME=0
      shift
      ;;
    --dry-run)
      DRY_RUN=1
      shift
      ;;
    -h|--help)
      usage
      exit 0
      ;;
    *)
      echo "Unknown argument: $1" >&2
      usage >&2
      exit 1
      ;;
  esac
done

if [[ "$MODE" != "created" && "$MODE" != "released" && "$MODE" != "both" ]]; then
  echo "Invalid --mode: $MODE (expected created|released|both)" >&2
  exit 1
fi
if [[ "$GRANULARITY" != "second" && "$GRANULARITY" != "day" ]]; then
  echo "Invalid --granularity: $GRANULARITY (expected second|day)" >&2
  exit 1
fi
if [[ "$RELEASE_FORMAT" != "datetime" && "$RELEASE_FORMAT" != "date" ]]; then
  echo "Invalid --release-format: $RELEASE_FORMAT (expected datetime|date)" >&2
  exit 1
fi

if [[ ! -d "$TARGET_DIR" ]]; then
  echo "Target directory not found: $TARGET_DIR" >&2
  exit 1
fi

for cmd in yt-dlp jq ffprobe exiftool stat date; do
  if ! command -v "$cmd" >/dev/null 2>&1; then
    echo "Missing required command: $cmd" >&2
    exit 1
  fi
done

if [[ "$MODE" == "created" || "$MODE" == "both" ]]; then
  if ! command -v SetFile >/dev/null 2>&1; then
    echo "Missing required command for created mode: SetFile" >&2
    exit 1
  fi
fi

mkdir -p "$RUN_ROOT"
RUN_ID="$(date +%Y%m%d-%H%M%S)"
OUT_DIR="$RUN_ROOT/$RUN_ID"
mkdir -p "$OUT_DIR"

LIKES_TSV="$OUT_DIR/sc_likes.tsv"
MAPPED_PRE="$OUT_DIR/mapped_pre.tsv"
MAPPED_SORTED="$OUT_DIR/mapped_sorted.tsv"
APPLIED_TSV="$OUT_DIR/applied.tsv"
UNMATCHED_TSV="$OUT_DIR/unmatched.tsv"
RUN_META="$OUT_DIR/run_meta.txt"

{
  echo "run_id=$RUN_ID"
  echo "target_dir=$TARGET_DIR"
  echo "likes_url=$LIKES_URL"
  echo "mode=$MODE"
  echo "granularity=$GRANULARITY"
  echo "release_format=$RELEASE_FORMAT"
  echo "preserve_mtime=$PRESERVE_MTIME"
  echo "dry_run=$DRY_RUN"
  echo "run_started_at=$(date '+%Y-%m-%d %H:%M:%S %z')"
} > "$RUN_META"

echo "Fetching live SoundCloud likes order..."
yt-dlp --flat-playlist -J "$LIKES_URL" \
  | jq -r '.entries | to_entries[] | [(.key + 1), .value.id, .value.title, (.value.url // .value.webpage_url // "")] | @tsv' \
  > "$LIKES_TSV"

typeset -A idx_by_exact
typeset -A id_by_exact
typeset -A sc_title_by_exact
typeset -A sc_url_by_exact

typeset -A idx_by_norm
typeset -A id_by_norm
typeset -A sc_title_by_norm
typeset -A sc_url_by_norm

while IFS=$'\t' read -r idx sc_id sc_title sc_url; do
  [[ -z "${sc_title:-}" ]] && continue
  if [[ -z "${idx_by_exact[$sc_title]-}" ]]; then
    idx_by_exact[$sc_title]="$idx"
    id_by_exact[$sc_title]="$sc_id"
    sc_title_by_exact[$sc_title]="$sc_title"
    sc_url_by_exact[$sc_title]="$sc_url"
  fi

  norm="$(normalize_title "$sc_title")"
  if [[ -n "$norm" && -z "${idx_by_norm[$norm]-}" ]]; then
    idx_by_norm[$norm]="$idx"
    id_by_norm[$norm]="$sc_id"
    sc_title_by_norm[$norm]="$sc_title"
    sc_url_by_norm[$norm]="$sc_url"
  fi
done < "$LIKES_TSV"

files=()
while IFS= read -r -d $'\0' file; do
  files+=("$file")
done < <(
  find "$TARGET_DIR" -maxdepth 1 -type f \
    \( -iname '*.m4a' -o -iname '*.mp3' -o -iname '*.flac' -o -iname '*.wav' -o -iname '*.aiff' -o -iname '*.aac' \) \
    -print0 | sort -z
)

if (( ${#files[@]} == 0 )); then
  echo "No audio files found in: $TARGET_DIR" >&2
  exit 1
fi

print -r -- "file${TAB}local_title${TAB}sc_index${TAB}sc_id${TAB}sc_title${TAB}sc_url${TAB}orig_birth${TAB}orig_mtime${TAB}orig_mtime_epoch${TAB}orig_tag_date" > "$MAPPED_PRE"
print -r -- "file${TAB}local_title${TAB}note" > "$UNMATCHED_TSV"

matched_count=0
max_mtime_epoch=0

for file in "${files[@]}"; do
  local_title="$(ffprobe -v error -show_entries format_tags=title -of default=nw=1:nk=1 "$file" | head -n1 || true)"
  if [[ -z "${local_title:-}" ]]; then
    local_title="${file:t:r}"
  fi

  sc_index="${idx_by_exact[$local_title]-}"
  sc_id="${id_by_exact[$local_title]-}"
  sc_title="${sc_title_by_exact[$local_title]-}"
  sc_url="${sc_url_by_exact[$local_title]-}"

  if [[ -z "${sc_index:-}" ]]; then
    local_norm="$(normalize_title "$local_title")"
    sc_index="${idx_by_norm[$local_norm]-}"
    sc_id="${id_by_norm[$local_norm]-}"
    sc_title="${sc_title_by_norm[$local_norm]-}"
    sc_url="${sc_url_by_norm[$local_norm]-}"
  fi

  if [[ -z "${sc_index:-}" ]]; then
    print -r -- "${file}${TAB}${local_title}${TAB}not-found-in-likes" >> "$UNMATCHED_TSV"
    continue
  fi

  orig_birth="$(stat -f '%SB' -t '%Y-%m-%d %H:%M:%S %z' "$file")"
  orig_mtime="$(stat -f '%Sm' -t '%Y-%m-%d %H:%M:%S %z' "$file")"
  orig_mtime_epoch="$(stat -f '%m' "$file")"
  orig_tag_date="$(ffprobe -v error -show_entries format_tags=date -of default=nw=1:nk=1 "$file" | head -n1 || true)"

  if (( orig_mtime_epoch > max_mtime_epoch )); then
    max_mtime_epoch="$orig_mtime_epoch"
  fi

  print -r -- "${file}${TAB}${local_title}${TAB}${sc_index}${TAB}${sc_id}${TAB}${sc_title}${TAB}${sc_url}${TAB}${orig_birth}${TAB}${orig_mtime}${TAB}${orig_mtime_epoch}${TAB}${orig_tag_date}" >> "$MAPPED_PRE"
  matched_count=$((matched_count + 1))
done

if (( matched_count == 0 )); then
  echo "No local files matched live likes titles. See: $UNMATCHED_TSV" >&2
  exit 1
fi

{
  head -n1 "$MAPPED_PRE"
  tail -n +2 "$MAPPED_PRE" | sort -t $'\t' -k3,3n
} > "$MAPPED_SORTED"

base_day="$(date -r "$max_mtime_epoch" '+%Y-%m-%d')"
day_end_epoch="$(date -j -f '%Y-%m-%d %H:%M:%S' "$base_day 23:59:59" '+%s')"

{
  echo "base_day=$base_day"
  echo "base_day_end_epoch=$day_end_epoch"
  echo "matched_count=$matched_count"
} >> "$RUN_META"

print -r -- "file${TAB}local_title${TAB}sc_index${TAB}sc_id${TAB}sc_title${TAB}sc_url${TAB}orig_birth${TAB}orig_mtime${TAB}orig_mtime_epoch${TAB}orig_tag_date${TAB}synthetic_datetime${TAB}new_birth${TAB}new_mtime${TAB}new_mtime_epoch${TAB}mtime_status${TAB}new_tag_date" > "$APPLIED_TSV"

while IFS=$'\t' read -r file local_title sc_index sc_id sc_title sc_url orig_birth orig_mtime orig_mtime_epoch orig_tag_date; do
  [[ "$file" == "file" ]] && continue

  if [[ "$GRANULARITY" == "day" ]]; then
    synth_epoch=$((day_end_epoch - ((sc_index - 1) * 86400)))
  else
    synth_epoch=$((day_end_epoch - (sc_index - 1)))
  fi
  synth_exif="$(date -r "$synth_epoch" '+%Y:%m:%d %H:%M:%S')"
  synth_exif_day="$(date -r "$synth_epoch" '+%Y:%m:%d')"
  synth_setfile="$(date -r "$synth_epoch" '+%m/%d/%Y %H:%M:%S')"
  synth_iso="$(date -r "$synth_epoch" '+%Y-%m-%dT%H:%M:%S%z')"
  synth_date="$(date -r "$synth_epoch" '+%Y-%m-%d')"

  if (( DRY_RUN == 0 )); then
    if [[ "$MODE" == "released" || "$MODE" == "both" ]]; then
      if [[ "$RELEASE_FORMAT" == "date" ]]; then
        exiftool -overwrite_original -P "-ContentCreateDate=${synth_exif_day} 00:00:00" "$file" >/dev/null
      else
        exiftool -overwrite_original -P "-ContentCreateDate=$synth_exif" "$file" >/dev/null
      fi
    fi
    if [[ "$MODE" == "created" || "$MODE" == "both" ]]; then
      SetFile -d "$synth_setfile" "$file"
    fi

    if (( PRESERVE_MTIME == 1 )); then
      # Force-restore mtime to original epoch so download chronology remains stable.
      touch -t "$(date -r "$orig_mtime_epoch" '+%Y%m%d%H%M.%S')" "$file"
    fi
  fi

  new_birth="$(stat -f '%SB' -t '%Y-%m-%d %H:%M:%S %z' "$file")"
  new_mtime="$(stat -f '%Sm' -t '%Y-%m-%d %H:%M:%S %z' "$file")"
  new_mtime_epoch="$(stat -f '%m' "$file")"
  mtime_status="changed"
  if [[ "$new_mtime_epoch" == "$orig_mtime_epoch" ]]; then
    mtime_status="preserved"
  fi
  new_tag_date="$(ffprobe -v error -show_entries format_tags=date -of default=nw=1:nk=1 "$file" | head -n1 || true)"

  print -r -- "${file}${TAB}${local_title}${TAB}${sc_index}${TAB}${sc_id}${TAB}${sc_title}${TAB}${sc_url}${TAB}${orig_birth}${TAB}${orig_mtime}${TAB}${orig_mtime_epoch}${TAB}${orig_tag_date}${TAB}${synth_iso}${TAB}${new_birth}${TAB}${new_mtime}${TAB}${new_mtime_epoch}${TAB}${mtime_status}${TAB}${new_tag_date}" >> "$APPLIED_TSV"
done < "$MAPPED_SORTED"

{
  echo "run_finished_at=$(date '+%Y-%m-%d %H:%M:%S %z')"
  echo "out_dir=$OUT_DIR"
} >> "$RUN_META"

echo "Done."
echo "Run output: $OUT_DIR"
echo "Applied manifest: $APPLIED_TSV"
if [[ -s "$UNMATCHED_TSV" ]]; then
  echo "Unmatched entries (if any): $UNMATCHED_TSV"
fi
