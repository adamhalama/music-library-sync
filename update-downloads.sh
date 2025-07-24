#!/usr/bin/env bash
#
# update-downloads.sh
#
# A single script that:
#   1) cds into each directory
#   2) runs the specified command
#   3) prints everything directly to the CLI (stdout/stderr)
#

echo "===== Starting all downloads: $(date '+%Y-%m-%d %H:%M:%S') ====="

run_in_dir() {
  local target_dir="$1"
  shift
  local cmd=( "$@" )

  echo ""
  echo "----"
  echo "Entering directory: $target_dir"
  if ! cd "$target_dir"; then
    echo "ERROR: could not cd into $target_dir"
    return 1
  fi

  # Show the exact command we’re about to run
  echo "Running: ${cmd[*]}"
  echo ""

  # Run it; output (stdout+stderr) will go to your terminal
  "${cmd[@]}"

  # Return to previous working directory
  cd - >/dev/null 2>&1
  echo "----"
}

#
# 1) SoundCloud “likes”
#
run_in_dir "$HOME/Music/downloaded/sc-likes" \
  scdl -l "https://soundcloud.com/hopefullyes/likes" -f

#
# 2) Spotify “Technicko” playlist
#
run_in_dir "$HOME/Music/downloaded/spotify-technicko" \
  spotdl --threads 1 --bitrate 256k "https://open.spotify.com/playlist/0vO3PgmFMIvAuuQrjKo1XG?si=7f28534ddba84b41"

#
# 3) Spotify “FunkyBounce” playlist
#
run_in_dir "$HOME/Music/downloaded/spotify-funkybounce" \
  spotdl --threads 1 --bitrate 256k "https://open.spotify.com/playlist/0rhzW83MTzundrEEsIbLgr?si=85aa8ebb179941e0"

#
# 4) Spotify “Technochill” playlist
#
run_in_dir "$HOME/Music/downloaded/spotify-technochill" \
  spotdl --threads 1 --bitrate 256k "https://open.spotify.com/playlist/6OmLOgWjwddWVXYNc6Fu5s?si=fd1f41dd7d204d32"

echo ""
echo "===== All downloads finished: $(date '+%Y-%m-%d %H:%M:%S') ====="
