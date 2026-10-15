> Archived 2026-07-30: this feature is complete and landed in `master` via commit `8350bbd`.

# Finish FreeDL, Playlist, and Rekordbox TUI Integration

**Status:** Planned
**Branch:** `feature/freedl-rekordbox-tui`
**Worktree:** `/Users/jaa/dev/utils/update-downloads-freedl-rekordbox-tui`
**Last updated:** 2026-07-28

## Goal

Finish the FreeDL, playlist, and Rekordbox TUI feature so it can be merged safely into `master`. The finished feature should preserve the branch's cache-first playlist workflow, FreeDL filtering, and backup-first Rekordbox design while resolving the remaining correctness, portability, usability, and documentation gaps.

## Current State

The feature is substantially implemented and its baseline automated checks pass:

- `go test ./...`
- `go vet ./...`
- `go test -race ./...`

The Apple Music snapshot refresh and read-only Rekordbox planning workflows also work with real data. The current real-library plan contains 139 tracks: 138 match local files and one is missing. The fail-closed policy correctly prevents the incomplete plan from being applied.

The branch is one commit behind `origin/master` and should be updated before the remaining work. A merge simulation found two straightforward conflicts:

- `AGENTS.md`: retain the relevant notes from both branches.
- `internal/engine/syncer.go`: register both the `deemix` and `scdl-freedl` plan providers.

## Planned Outcomes

### 1. Update the feature branch

Merge the current `origin/master` into the published feature branch. Use a merge commit rather than rewriting the branch history, then resolve the two known conflicts without dropping either branch's behavior.

### 2. Correct release-blocking behavior

- Replace hard-coded user-specific Rekordbox backup paths with a portable home-relative default.
- Make `rekordbox playlist-sync apply --backup-dir` an effective runtime override without mutating the checksummed plan.
- Strengthen standalone Rekordbox sync configuration validation.
- Compare playlist snapshots as multisets so duplicate tracks are handled correctly.

### 3. Finish blocked-plan and compact-screen UX

- Keep the existing fail-closed policy for incomplete Rekordbox plans.
- Show the actual blocking tracks, including artist, title, and expected path, in CLI and TUI output.
- Preserve the complete plan for diagnosis rather than silently creating a partial mirror.
- Make playlist detail useful at an 80×24 terminal size by keeping the selected track and path visible.
- Keep the playlist summary compact enough to leave room for track rows.

### 4. Clarify integrity and safety language

Describe Rekordbox plan files as checksummed or integrity-checked rather than signed. The current SHA-256 checksum detects modification but is not a cryptographic signature.

### 5. Complete documentation and release validation

- Document the `playlist` and `rekordbox` command surfaces, flags, dependencies, cache behavior, and backup safety model.
- Confirm Homebrew dependency checks account for the supported Python runtime.
- Add regression coverage for every corrected behavior.
- Re-run automated, isolated real-data, and manual TUI validation.
- Push the completed branch and verify CI before proposing it for merge.

## Required Public Behavior

- Opening the TUI remains cache-only; network refresh remains an explicit user action.
- FreeDL filtering and source planning continue to work alongside the newer Spotify/deemix planning added on `master`.
- Rekordbox apply remains backup-first and fail-closed.
- A CLI backup override works without invalidating a previously generated plan.
- New default backup paths are portable across users and machines.
- Existing user configuration files are not silently rewritten.
- Invalid standalone Rekordbox jobs fail during validation with actionable messages.
- Missing local tracks are visible to the user and prevent partial mirror application.

## Scope Boundaries

The following are not required to complete this feature:

- Full playlist create, edit, and delete CRUD in the TUI.
- Replacing YAML as the advanced configuration surface.
- Applying changes to a live Rekordbox database during development or validation.
- Relaxing the incomplete-plan failure policy.

## Completion Criteria

The feature is ready when:

- The branch includes current `master` without losing either plan provider.
- All identified correctness and portability issues are fixed and covered by tests.
- CLI and TUI blocker output identify missing tracks.
- Playlist detail remains useful at 80×24 and works at larger terminal sizes.
- Documentation matches the implemented command surface and safety behavior.
- `go test ./...`, `go vet ./...`, and `go test -race ./...` pass.
- Isolated Apple Music refresh and read-only Rekordbox planning checks pass.
- CI passes on the pushed feature branch.
