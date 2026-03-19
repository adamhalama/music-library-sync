> Historical planning note: `sync --plan` shipped on this branch. Treat this file as design history; use the CLI help, README, and tests for the current contract.

### Interactive `sync --plan` Mode (SCDL v1)

**Summary**
Add an interactive planning mode to `udl sync` that fetches a capped window of remote tracks, compares against local/state, shows downloaded status, lets the user toggle download candidates, and then executes only selected tracks.  
Scope v1 is `adapter.kind=scdl` only, with a modular planner/selector architecture so future source adapters can plug in.

### CLI Contract
- Entry point: `udl sync --plan`.
- New flag: `--plan-limit <int>` (default `10`).
- `--plan-limit 0` means unlimited.
- `--plan-limit` is valid only with `--plan`; otherwise return exit code `2` with actionable usage text.
- `--plan` is interactive-only:
  - fail with exit code `2` if `--json` is set
  - fail with exit code `2` if `--no-input` is set
  - fail with exit code `2` if stdin/stdout are not TTY
- Multi-source behavior: one full-screen selector per source, in deterministic source order.
- Unsupported adapters in `--plan` mode: skip source with warning, continue run.
- `--dry-run --plan`: run preflight + selector, print selected summary, do not execute adapters or write state.

### Implementation Changes
- Add a planner abstraction for future extensibility:
  - source planning provider interface (build selectable rows + apply selection)
  - v1 provider implementation for SoundCloud `scdl`
- Add a full-screen TUI selector (Bubble Tea-based):
  - row fields: index, title, status (`downloaded`, `missing_known`, `missing_new`)
  - downloaded rows are visible but read-only (not toggleable)
  - default selected: all missing rows
  - keys: up/down (or j/k), space toggle, `a` select all toggleable, `n` clear toggleable, enter confirm, `q`/esc cancel
  - cancel exits as interrupted (`130`) with no state mutation
- SCDL planning and apply path:
  - preflight enumeration honors `--plan-limit` and does not fetch beyond the cap
  - classify rows from existing preflight/state/local-index data
  - on confirm, derive selected track indices in the fetched window
  - execute only selected tracks by injecting managed yt-dlp selectors into the run (`--playlist-items`, plus `--playlist-end` when limited)
  - reuse existing temporary state/archive filtering for selected known-gaps so redownloads work correctly
  - if nothing selected for a source: emit no-op finished status and skip adapter execution

### Output, Errors, Exit Codes
- Human-first output remains default; diagnostics/errors on stderr.
- During interactive selector, suppress compact progress rendering and hand terminal control to TUI; resume normal sync events afterward.
- Exit codes:
  - `0` success (including no-op selections)
  - `2` invalid usage / incompatible mode flags / non-interactive invocation
  - existing runtime/partial/dependency codes remain unchanged for execution failures
  - `130` user-canceled selector

### Test Plan
- CLI parsing/validation:
  - `--plan` with `--json`, `--no-input`, non-TTY -> exit `2`
  - `--plan-limit` default/zero/negative/without-`--plan`
- Planner behavior:
  - cap enforcement (no fetch beyond limit)
  - row status classification and read-only downloaded rows
  - default selection state for missing rows
- Apply behavior (scdl):
  - selected indices map to managed yt-dlp selection args
  - known-gap selected tracks are re-downloadable via temp state/archive filtering
  - unselected tracks in the capped window are not downloaded
  - empty selection produces source no-op success
- Multi-source orchestration:
  - sequential selector screens
  - non-scdl source skip warning in `--plan`
- Cancellation:
  - selector cancel returns interrupted code and makes no writes

### Assumptions and Defaults
- Full-screen TUI dependency is acceptable for this feature.
- v1 supports planning only for `scdl`; other adapters are intentionally skipped with warning.
- Limit semantics are per-source and hard-scoped: tracks beyond the cap are not fetched or considered in that run.
- Existing/downloading status is visible to the user, but already-downloaded tracks are read-only in v1.
