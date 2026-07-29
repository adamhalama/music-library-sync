# FreeDL, Playlist, and Rekordbox TUI Implementation Tracker

**Overall status:** In progress  
**Branch:** `feature/freedl-rekordbox-tui`  
**Last updated:** 2026-07-29

This file is the working implementation checklist for [PLAN.md](./PLAN.md). Update phase statuses, check tasks off, and record validation evidence as the feature progresses.

## Status Convention

- Use unchecked tasks for work that has not been completed: `- [ ]`
- Use checked tasks only after implementation and relevant validation: `- [x]`
- Set each phase to `Not started`, `In progress`, `Blocked`, or `Done`.
- When a task changes, edit its wording rather than keeping an obsolete requirement.
- Record non-obvious decisions and deviations in the decision log.

## Progress

| Phase | Status | Exit condition |
| --- | --- | --- |
| 0. Update from `master` | Done | Merge complete and both providers registered |
| 1. Portability and plan integrity | Done | Defaults and backup override fixed |
| 2. Validation and snapshot correctness | Done | Invalid jobs and duplicates covered |
| 3. CLI and TUI completion | In progress | Blockers visible and 80×24 useful |
| 4. Documentation and packaging | Not started | Public behavior and dependencies documented |
| 5. Final validation | Not started | Full test matrix and CI pass |

## Phase 0 — Update from `master`

**Status:** Done

- [x] Confirm the target worktree is clean before merging.
- [x] Fetch the latest remote refs.
- [x] Merge `origin/master` into `feature/freedl-rekordbox-tui` with a merge commit.
- [x] Resolve `AGENTS.md` by retaining the applicable notes from both branches.
- [x] Resolve `internal/engine/syncer.go` by registering both:
  - `deemix` with `NewSpotifyDeemixPlanProvider()`
  - `scdl-freedl` with `NewSCDLPlanProvider()`
- [x] Add or update a registry test that proves both providers are available.
- [x] Run the focused engine tests after conflict resolution.
- [x] Confirm the merge did not alter cache-first playlist or fail-closed Rekordbox behavior.

## Phase 1 — Portability and Plan Integrity

**Status:** Done

### Portable Rekordbox backup defaults

- [x] Replace the hard-coded `/Users/jaa/Music/rb-library-export` default with a portable `~/Music/rb-library-export` default.
- [x] Resolve `~` at runtime using the current user's home directory.
- [x] Update the config default, generated template, runtime planning path, and test expectations together.
- [x] Preserve explicitly configured backup directories.
- [x] Do not silently rewrite existing user configuration files.
- [x] Add tests for default expansion and explicit-path preservation.

Likely files:

- `internal/rekordbox/playlistsync/plan.go`
- `internal/rekordbox/syncconfig/config.go`
- `internal/config/template.go`
- Associated config and Rekordbox tests

### Make `--backup-dir` a real apply-time override

- [x] Reproduce the current checksum mismatch with a valid stored plan and `apply --backup-dir`.
- [x] Keep the loaded plan immutable while validating its checksum.
- [x] Validate the original plan before calculating runtime overrides.
- [x] Calculate the effective backup directory with this precedence:
  1. CLI `--backup-dir`
  2. Backup directory stored in the plan
  3. Resolved config/default backup directory
- [x] Use the effective directory when creating the backup without writing it back into the plan.
- [x] Ensure dry-run reports the effective path without modifying the database.
- [x] Add regression tests for the override, checksum validation, and precedence rules.

### Correct integrity terminology

- [x] Replace user-facing “signed plan” wording with “checksummed plan” or “integrity-checked plan.”
- [x] Keep SHA-256 checksum verification behavior unchanged unless a separately reviewed authentication design is introduced.
- [x] Update CLI help, TUI copy, errors, and documentation consistently.

## Phase 2 — Validation and Snapshot Correctness

**Status:** Done

### Strengthen standalone Rekordbox configuration validation

- [x] Validate required database, backup, and playlist-related paths.
- [x] Validate job identifiers and reject empty or duplicate IDs.
- [x] Validate folder source/target selectors and standalone job target selectors.
- [x] Validate mirror mode and other enumerated values.
- [x] Reject structurally incomplete jobs before planning begins.
- [x] Produce errors that identify the invalid job and field.
- [x] Add table-driven tests for valid and invalid standalone configurations.
- [x] Confirm legacy valid configurations continue to load.

### Compare snapshots as multisets

- [x] Replace set-only snapshot comparison with count-aware comparison.
- [x] Report added and removed occurrences correctly when a playlist contains duplicates.
- [x] Preserve deterministic output ordering.
- [x] Add tests covering:
  - identical snapshots with duplicates
  - one duplicate occurrence added
  - one duplicate occurrence removed
  - reordered but otherwise identical snapshots

## Phase 3 — CLI and TUI Completion

**Status:** In progress

### Expose incomplete-plan blockers

- [ ] Keep incomplete plans blocked from apply.
- [ ] Preserve the full planned mirror rather than silently reducing it to matched tracks.
- [ ] Add structured blocker information sufficient to render artist, title, and expected local path.
- [ ] Show blocker rows in CLI plan and apply error output.
- [ ] Show blocker rows in the TUI plan review/error state.
- [ ] Keep summary counts visible: total, matched, and missing.
- [ ] Ensure long paths are truncated or wrapped without hiding track identity.
- [ ] Add tests for single and multiple missing tracks.
- [ ] Add a test proving the blocked plan cannot produce a partial mirror.

Current real-data example:

- Playlist tracks: 139
- Matched local tracks: 138
- Missing track: `Netherworld — Atalantis`
- Expected behavior: display the missing item and refuse apply

### Make playlist detail useful at 80×24

- [ ] Reproduce the compact-terminal layout where the Tracks header is visible but track rows are not.
- [ ] Reduce the vertical footprint of the playlist summary to one concise line where possible.
- [ ] Reserve enough height for at least the selected track row at 80×24.
- [ ] Keep the selected track's resolved path visible through truncation, wrapping, or a compact detail line.
- [ ] Preserve the richer layout at 120×40 and larger sizes.
- [ ] Add layout/model tests for 80×24 and a larger terminal.
- [ ] Manually inspect navigation, selection, scrolling, empty states, and refresh feedback at both sizes.

## Phase 4 — Documentation and Packaging

**Status:** Not started

- [ ] Update the README command overview to include `playlist` and `rekordbox`.
- [ ] Document playlist cache creation, explicit refresh, comparison, and offline/cache-first behavior.
- [ ] Document FreeDL filtering and its relationship to normal source planning.
- [ ] Document Rekordbox dependency checks, plan generation, dry-run, backup-first apply, and fail-closed behavior.
- [ ] Document the effective `--backup-dir` precedence.
- [ ] Document portable default paths and how `~` is resolved.
- [ ] Document plan checksum semantics without implying authentication.
- [ ] Add a short TUI workflow section with expected compact and full-size behavior.
- [ ] Confirm Homebrew checks and instructions use the supported `python@3.12` dependency.
- [ ] Review generated config examples and CLI help for consistency.
- [ ] Add exact validation commands and representative terminal output to the eventual pull request.

## Phase 5 — Final Validation

**Status:** Not started

### Automated checks

- [ ] `go test ./...`
- [ ] `go vet ./...`
- [ ] `go test -race ./...`
- [ ] Run any focused integration tests added for playlist snapshots and Rekordbox planning.

### Isolated real-data checks

Use temporary output and backup locations. Do not apply to a live Rekordbox database.

- [ ] Refresh an Apple Music playlist snapshot into an isolated temporary directory.
- [ ] Validate the generated snapshot checksum and track count.
- [ ] Generate a read-only Rekordbox plan against the local music library.
- [ ] Confirm missing tracks are named in CLI output.
- [ ] Confirm a plan with missing tracks is blocked during dry-run apply.
- [ ] Generate or fixture a valid complete plan and verify `--backup-dir` works without a checksum mismatch.
- [ ] Confirm the chosen backup path is the requested override.

### Manual TUI checks

- [ ] Open the TUI without network access and confirm it uses cached playlist data.
- [ ] Trigger an explicit refresh and confirm progress and errors are understandable.
- [ ] Validate playlist detail at 80×24.
- [ ] Validate playlist detail at 120×40.
- [ ] Validate Rekordbox blocker presentation.
- [ ] Validate FreeDL selection/filter presentation after the merge from `master`.

### Branch and CI

- [ ] Review the final diff for unrelated or user-specific paths.
- [ ] Confirm no secrets, tokens, keys, or live database backups are included.
- [ ] Push `feature/freedl-rekordbox-tui`.
- [ ] Verify all CI checks pass.
- [ ] Record CI links and any platform-specific results in the pull request.

## Baseline Evidence

These checks describe the branch before the finishing implementation begins:

- [x] `go test ./...` passed.
- [x] `go vet ./...` passed.
- [x] `go test -race ./...` passed.
- [x] Isolated Apple Music snapshot refresh produced a valid 139-track snapshot.
- [x] Read-only Rekordbox planning found 138 matches and one missing track.
- [x] Dry-run apply correctly refused the incomplete plan.
- [x] The `--backup-dir` checksum mismatch was reproduced.
- [x] A merge simulation identified only the two conflicts listed in Phase 0.

## Decision Log

| Date | Decision | Reason |
| --- | --- | --- |
| 2026-07-28 | Merge `origin/master` instead of rebasing | The branch is published/shared and already has merge history. |
| 2026-07-28 | Keep playlist opening cache-only | Network work should remain explicit and predictable in the TUI. |
| 2026-07-28 | Keep incomplete Rekordbox plans fail-closed | A silent partial mirror could remove or omit tracks unexpectedly. |
| 2026-07-28 | Treat the plan as checksummed, not signed | The existing unkeyed SHA-256 checksum provides integrity detection, not authenticity. |
| 2026-07-28 | Keep full playlist CRUD out of scope | YAML remains the advanced editing surface for this feature. |

## Implementation Notes

- The live-library missing track is a data blocker, not proof of a planner defect. Use it to validate blocker UX, but do not weaken apply safety to bypass it.
- Never use the live Rekordbox database for apply validation. Use fixtures or isolated copies.
- If implementation reveals a new behavior change, add it to `PLAN.md`, update the relevant task here, and record the decision above.

## Work Log

### 2026-07-29

- Began implementation in the dedicated `feature/freedl-rekordbox-tui` worktree.
- Confirmed the worktree contains no unrelated changes; only `PLAN.md` and this tracker are initially untracked.
- Confirmed the branch is nine commits ahead and one commit behind the locally known `origin/master`.
- Committed the planning baseline before integration.
- Fetched `origin` and merged `origin/master`; the only conflicts were the predicted `AGENTS.md` notes and built-in plan-provider registrations.
- Preserved all three built-in providers (`scdl`, `scdl-freedl`, and `deemix`) and added a regression test for the registry.
- Validation after conflict resolution:
  - `go test ./internal/engine/...`
  - `go test ./internal/cli ./internal/rekordbox/...`
- Started Phase 1.
- Replaced all runtime, standalone-config, generated-template, and test defaults that contained a user-specific Rekordbox backup directory.
- Changed apply so checksum validation happens against the original plan before any runtime override is resolved.
- Added `EffectiveBackupDir` to the apply result so dry-run and callers can report the effective CLI/plan/config choice without mutating the plan.
- Confirmed precedence with regression tests: CLI override, then plan backup directory, then configured/default directory.
- Updated user-facing TUI wording from “signed plan” to “checksummed plan”; no remaining user-facing “signed plan” text was found.
- Phase 1 validation:
  - `go test ./internal/app ./internal/cli ./internal/config ./internal/rekordbox/playlistsync ./internal/rekordbox/syncconfig`
- Started Phase 2.
- Strengthened standalone config validation for absolute/resolvable defaults, folder selectors, playlist-name mappings, job identifiers, job targets, and supported modes.
- Clarified the selector requirement while implementing: standalone playlist jobs are destinations and receive their source from the selected snapshot, so they require a Rekordbox target but not a duplicate Music source selector. Folder mappings still require both source and target selectors.
- The stricter validation exposed one synthetic TUI test fixture that omitted the defaults supplied by every real config load; updated the fixture rather than weakening production validation.
- Replaced set-based snapshot comparison with occurrence counts so duplicate additions and removals are accurate.
- Phase 2 validation:
  - `go test ./internal/playlists ./internal/rekordbox/syncconfig ./internal/cli`
- Started Phase 3.
