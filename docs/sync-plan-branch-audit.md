# `feature/sync-plan` Branch Audit

Audit date: March 19, 2026

Base comparison: `origin/master`

## Summary

The branch is functionally coherent and test-green. It delivers three major user-visible capabilities:
- interactive `udl sync --plan` for `adapter.kind=scdl`
- a full-screen Bubble Tea TUI for `sync`, `doctor`, `validate`, and `init`
- structured track-progress events and compact progress rendering shared across normal CLI sync and TUI sync

Repo validation at audit time:
- `go test ./...`
- `go vet ./...`

## Functional Changes And Status

### 1. Interactive `sync --plan` for SoundCloud `scdl`

Status: finished for the declared v1 scope

Implemented behavior:
- `udl sync --plan` and `--plan-limit`
- interactive-only validation (`--json`, `--no-input`, and non-TTY are rejected)
- per-source track selection UI
- selected-track execution via managed playlist item selection
- no-op success when nothing is selected
- unsupported adapters are skipped with warning
- selected known-gap handling no longer deletes preserved local/state entries

Scope notes:
- v1 remains intentionally limited to `adapter.kind=scdl`
- unsupported adapters are not a gap versus the stated scope

### 2. TUI workflow shell

Status: finished as an additive v1 shell

Implemented behavior:
- `udl tui`
- workflow screens for `sync`, `doctor`, `validate`, `init`
- in-app plan selector
- in-app confirm/input prompts
- cancellation during runs and prompts
- compact live progress plus compact activity history during sync

Scope notes:
- `promote-freedl` and `version` are still not exposed inside the TUI
- sync output-style knobs are still CLI-only

### 3. Structured progress/event pipeline

Status: mostly finished, with fallback cleanup still remaining

Implemented behavior:
- adapter log parser seam and registry
- concrete parsers for `scdl`, `deemix`, and `spotdl`
- additive public track events (`track_started`, `track_progress`, `track_done`, `track_skip`, `track_fail`)
- shared structured progress tracking used by compact sync output and TUI sync output

Remaining follow-up:
- compact output still keeps some backend-specific raw-line fallback/suppression logic
- full removal of regex-driven fallback parsing is not finished yet

### 4. App-layer workflow/use-case split

Status: finished for current command coverage

Implemented behavior:
- app-layer use cases for `sync`, `doctor`, `validate`, and `init`
- interaction abstractions shared between CLI and TUI

## Findings

### User-facing docs that were stale before this audit

- `readme.md` was missing `sync --plan` and `--plan-limit` from the documented sync flag surface.
- `docs/tui.md` did not describe the current compact progress/activity rendering in the TUI sync view.
- `docs/roadmap.md` still described adapter log normalization as entirely future work even though the parser seam, normalized track events, and shared structured progress state are already present.

### Planning docs that are now historical, not current work items

- `docs/plans/architecture-sequencing.md`
- `docs/plans/sync-first-plan.md`
- `docs/plans/three-stage-big-plan.md`

These are still useful as design history, but they should not read like active current-state specs.

## Audit Conclusion

No unfinished feature was found inside the branch's declared v1 scope for `sync --plan` or the TUI shell. The main incomplete area is the remaining raw-log fallback inside compact rendering; that is follow-up hardening work, not a broken branch deliverable.
