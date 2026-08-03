#!/usr/bin/env bash
# Dev harness for driving UDL-Dev.app on the built-in MacBook display.
# When an external monitor is attached it is the main display and is left alone.
set -euo pipefail
root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
app="${root}/dist/dev/UDL-Dev.app"
here="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"

[[ -x "${here}/screens" ]] || xcrun swiftc -O "${here}/screens.swift" -o "${here}/screens"
read -r BX BY BW BH < <("${here}/screens")
# Inset the window inside the built-in display, leaving room for the menu bar.
WX=$((BX + 20)); WY=$((BY + 40)); WW=$((BW - 60)); WH=$((BH - 80))

case "${1:-}" in
  launch)
    osascript -e 'tell application "UDL" to quit' >/dev/null 2>&1 || true
    pkill -x UDL >/dev/null 2>&1 || true
    sleep 1
    open -a "$app"
    sleep 4
    "$0" place
    ;;
  place)
    osascript <<OSA
tell application "System Events" to tell process "UDL"
  set frontmost to true
  if (count of windows) > 0 then
    set position of window 1 to {$WX, $WY}
    set size of window 1 to {$WW, $WH}
  end if
end tell
OSA
    ;;
  shot)  screencapture -x -R"${BX},${BY},${BW},${BH}" "${2:?usage: app.sh shot <png>}"; echo "$2" ;;
  win)   screencapture -x -R"${WX},${WY},${WW},${WH}" "${2:?usage: app.sh win <png>}"; echo "$2" ;;
  quit)  osascript -e 'tell application "UDL" to quit' >/dev/null 2>&1 || pkill -x UDL || true ;;
  appearance)
    osascript -e "tell application \"System Events\" to tell appearance preferences to set dark mode to ${2:-true}"
    ;;
  *) echo "usage: app.sh {launch|place|shot <png>|win <png>|quit|appearance true|false}" >&2; exit 2 ;;
esac
