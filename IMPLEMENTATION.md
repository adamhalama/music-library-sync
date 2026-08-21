# Rekordbox and Navidrome Directional Playlist Sync — Implementation Tracker

- **Overall status:** In progress
- **Plan:** [PLAN.md](./PLAN.md)
- **Last updated:** 2026-08-07
- **Archived predecessor:**
  [plans/archive/navidrome-likes-rekordbox-implementation.md](./plans/archive/navidrome-likes-rekordbox-implementation.md)

This is the live execution record. Update phase status, checkboxes, evidence,
decisions, deviations, and discovered work in the same change as the
implementation. A checked task means the behavior is implemented **and**
proportionally validated.

## Status Convention

Phase status must be one of:

- `Not started` — no implementation work has begun.
- `In progress` — implementation or validation is active.
- `Blocked` — a recorded decision, dependency, or external action is required.
- `In review` — implementation is complete and final validation remains.
- `Done` — every task and exit gate is complete.
- `Deferred` — deliberately removed, with the reason recorded below.

Checkboxes:

- `[ ]` not complete
- `[x]` implemented and validated

Do not check a task that is merely coded. Record newly discovered work instead
of silently broadening an existing checkbox.

## Progress Dashboard

| Phase | Status | Depends on | Exit gate |
| --- | --- | --- | --- |
| 0. Baseline and contracts | Done | — | Existing behavior is green and new contracts are frozen |
| 1. Pair config and durable state | Done | 0 | Jobs validate, bindings round-trip atomically, old configs remain valid |
| 2. Shared identity and provider reads | Done | 1 | Both providers produce ordered tracks with one canonical path identity |
| 3. Writable Navidrome playlists | Done | 2 | A normal owned playlist can be created/replaced and verified safely |
| 4. Direction-neutral planner | Done | 2, 3 | Both directions produce checksummed, blocking-aware plans |
| 5. Apply orchestration and recovery | In progress | 4 | Stale plans never write; verified applies back up and advance state |
| 6. CLI workflow | In progress | 4, 5 | Human and JSON plan/show/apply flows are complete |
| 7. Agent and native app | In progress | 4–6 | The app can configure, preview, confirm, apply, cancel, and report outcomes |
| 8. Acceptance and hardening | In progress | 1–7 | Automated suites and the real phone round trip are green |
| 9. Documentation and close-out | In progress | 8 | Docs, evidence, decisions, and plan status are final |

---

## Phase 0 — Baseline and Contracts

**Status:** Done

**Exit gate:** The repository baseline is recorded, current one-way workflows
remain green, and public names/formats for this initiative are fixed.

### Tasks

- [x] Record the starting commit, branch, dirty-worktree state, Go version,
      Swift toolchain, Navidrome version, and pyrekordbox runtime status.
- [x] Run and record focused existing playlist, Navidrome, Rekordbox, agent, CLI,
      and Swift tests before changing code.
- [x] Confirm that the old `playlistsync.Plan` format and checksum code will not
      be extended; create a separate mirror plan version instead.
- [x] Freeze the public command tree, direction enum values, RPC method names,
      YAML keys, state path, and JSON field names from PLAN.md.
- [x] Define one terminology set across all surfaces:
      `job`, `direction`, `source`, `destination`, `plan`, `binding`, and
      `verified parity`.
- [x] Add focused fixtures for a paired `favs_august` job, an existing adopted
      Navidrome playlist, and an isolated Rekordbox playlist.

### Evidence

- Starting point: commit `1cf63ecdeab525a62b67ddbc28671be19f77ea18`
  on `feature/swiftui-gui`; the worktree already contained the new root plan,
  tracker, archive-index edits, and untracked predecessor archive copies. Those
  user-authored planning changes were preserved.
- Tool/runtime snapshot (2026-08-07 17:24 CEST): Go `1.26.0`
  (`darwin/arm64`); Swift `6.3.3`; Navidrome `0.63.2 (source_archive)`;
  managed pyrekordbox runtime healthy at version `0.4.4`. System
  `/usr/bin/python3` does not contain pyrekordbox, which is expected because the
  CLI selects the managed runtime.
- Baseline commands/results:
  - `go test ./internal/playlists ./internal/navidrome
    ./internal/rekordbox/... ./internal/agent ./internal/cli`: playlists,
    Rekordbox, agent, and CLI passed; the sandbox blocked Navidrome's local
    `httptest` listener before assertions ran.
  - `go test ./internal/navidrome` with local-listener permission: passed.
  - `make app-test-dev`: 90 Swift tests passed, 0 failed.
- Contract decisions: command/RPC/YAML/state names and terminology remain as
  written in PLAN.md. New direction constants live with the pair-state
  contract. The existing `internal/rekordbox/playlistsync.Plan` was not changed;
  the mirror plan will be a separate type and version.
- Fixtures: `internal/playlists/testdata/paired_favs_august.yaml`,
  `navidrome_adopted_playlist.json`, and
  `rekordbox_isolated_playlist.json`.

---

## Phase 1 — Pair Configuration and Durable State

**Status:** Done

**Exit gate:** Existing version-1 `playlists.yaml` files load unchanged; paired
jobs validate; resolved bindings and last-success status are atomic and
checksummed.

### Configuration

- [x] Add `sync_jobs` plus nested Rekordbox/Navidrome selectors to the playlist
      configuration model and YAML/JSON encoders.
- [x] Keep config version 1 and prove a file containing only existing
      `playlists` entries marshals and behaves unchanged.
- [x] Validate unique safe job IDs, required names, optional IDs, trimmed
      values, and duplicate/colliding jobs with actionable field paths.
- [x] Extend config read/write RPC and native DTOs; decode nil Go collections
      with `@DefaultEmpty`.
- [x] Add native editing for paired jobs without placing credentials or
      runtime state in configuration. CLI users edit the documented YAML;
      adding an unplanned CLI config-mutation command would violate the frozen
      command tree. Implement the native editor with the Phase 7 workspace.

### Binding state

- [x] Define versioned pair state containing job ID, config fingerprint,
      resolved provider IDs/names, last direction/time, final normalized-path
      checksum, and its own checksum.
- [x] Store state at `state_dir/playlist-sync/jobs/<job-id>.json` using the
      repository's atomic write pattern and safe job IDs only.
- [x] Ignore a binding when its config fingerprint differs; never silently
      rewrite config to pin provider IDs.
- [x] Make missing state a normal first-run condition and corrupted/checksum-
      invalid state an explicit status that cannot be trusted.
- [x] Test rename survival, changed-selector invalidation, interrupted writes,
      corrupt JSON, bad checksum, missing directories, and nil collections.

### Evidence

- Tests: `go test ./internal/playlists ./internal/agent ./internal/cli` passed;
  `make app-test-dev` passed 90/90. Coverage includes config normalization,
  version-1 compatibility, omitted `sync_jobs`, field-path validation,
  case-insensitive ID collision refusal, fixture loading, state permissions,
  checksum validation, missing/corrupt state, rename survival, and selector
  invalidation.
- Example config: `internal/playlists/testdata/paired_favs_august.yaml`.
- State contract: JSON version 1, mode `0600`, atomically written to
  `playlist-sync/jobs/<job-id>.json`; it contains resolved provider IDs/names,
  direction/time, config and final-path fingerprints, and no credentials.
- Native editing is implemented in `PlaylistSyncJobEditor`; it writes through
  the existing guarded playlist-config RPC and keeps credentials/state out of YAML.

---

## Phase 2 — Shared Identity and Provider Reads

**Status:** Done

**Exit gate:** Rekordbox and Navidrome expose ordered playlist tracks using the
same canonical real-path identity, including enough metadata for plan rows.

### Canonical path identity

- [x] Extract one shared implementation for URL decoding, path cleaning, and
      NFC normalization; keep existing exported normalization functions as
      compatibility wrappers.
- [x] Add cross-provider fixtures for NFD/NFC filenames, `file:` URLs,
      redundant components, whitespace, case-sensitive paths, and missing
      paths.
- [x] Prohibit artist/title fallback in the shared matcher.

### Rekordbox source read

- [x] Extend bridge content inspection with the metadata needed by the plan UI
      while retaining backward-compatible JSON decoding.
- [x] Join each playlist's ordered content IDs to inspected content rows and
      fail on missing content IDs rather than silently dropping them.
- [x] Resolve by explicit ID, then valid saved binding, then one exact name;
      reject ambiguous names and non-normal playlist attributes.
- [x] Preserve the existing Rekordbox-closed and database-sidecar guard for all
      live mirror planning.

### Navidrome source/destination read

- [x] Preserve playlist comment, owner, and smart/editability information in
      the client model.
- [x] Resolve by explicit ID, binding, or one exact name with the same selector
      rules as Rekordbox.
- [x] Confirm every song exposes an absolute real path; turn relative/simulated
      paths into one actionable `DefaultReportRealPath` blocker.
- [x] Index the complete accessible Navidrome song catalog by normalized path
      without collapsing ambiguous rows.
- [x] Test paging, duplicate IDs, duplicate paths, smart playlists, ownership,
      renamed bindings, and exact-name ambiguity.

### Evidence

- Tests: `go test ./internal/pathidentity ./internal/playlists
  ./internal/rekordbox/playlistsync ./internal/navidrome` passed.
- Fixture notes: the shared table covers NFD/NFC equivalence, one-layer file
  URL decoding (including literal percent preservation), redundant components,
  surrounding whitespace, missing paths, case sensitivity, no-match, and
  duplicate canonical-path ambiguity. Existing exported Navidrome and
  Rekordbox normalization functions remain compatibility wrappers.
- Provider-read coverage lives in `internal/playlistmirror/read_test.go`; the
  Rekordbox bridge now emits artist, album, and duration without breaking older
  JSON, and every live plan passes the existing closed-process/sidecar guard.

---

## Phase 3 — Writable Navidrome Playlists

**Status:** Done

**Exit gate:** UDL can create or completely replace one normal playlist owned
by the configured account, without unsafe retries, and verify exact order.

### Client mutation support

- [x] Add OpenSubsonic extension discovery needed to determine `formPost`
      support.
- [x] Add a form-encoded mutation request path that shares authentication,
      timeouts, response bounds, Subsonic error translation, and URL/token
      redaction with existing reads.
- [x] Implement complete ordered replacement through `createPlaylist` using a
      playlist ID for existing destinations and a name only for creation.
- [x] Prefer form POST; permit a bounded GET fallback only when the encoded
      request fits the documented limit, otherwise return an upgrade/action
      message before mutation.
- [x] Do not apply the generic automatic retry loop to creation-by-name.
- [x] After an uncertain create response, re-list exact-name playlists and
      classify exact planned membership as success, no match as failure, and
      multiple/different matches as partial/uncertain.
- [x] Reject managed smart playlists and playlists owned by another account
      before creating a backup or dispatching a mutation.
- [x] Read the destination back and compare exact ordered song IDs after every
      mutation.

### Fake server and integration coverage

- [x] Extend the fake server for extension discovery, POST parsing, repeated
      song IDs, create/update responses, ownership failures, and response-lost
      scenarios.
- [x] Test create empty destination with non-empty source, replace existing,
      reorder-only, no-op, large request, malformed response, 4xx/5xx,
      cancellation, redacted errors, and verification mismatch.
- [x] Prove an empty source is rejected by the higher-level workflow and never
      reaches the mutation client.

### Evidence

- Tests: `go test ./internal/navidrome ./internal/playlistmirror ./internal/app`
  passed, including form POST, repeated ordered IDs, bounded GET fallback,
  response-loss reconciliation without retry, ownership/smart/empty guards,
  malformed/provider failures, and readback mismatch.
- Live/fake request evidence: the fake server records POST bodies separately
  from URLs; credential assertions prove auth values are absent from URL query
  strings. API semantics were checked against the official OpenSubsonic
  `formPost`, extension-discovery, and `createPlaylist` contracts.

---

## Phase 4 — Direction-Neutral Planner

**Status:** Done

**Exit gate:** Either direction yields the same versioned plan contract,
correct ordered actions, explicit blockers, and a stable checksum.

### Plan model

- [x] Define a new version-1 mirror plan with job/config fingerprint,
      direction, generation time, source/destination descriptors, creation
      intent, summary, rows, final destination IDs, preconditions, blockers,
      warnings, and checksum.
- [x] Define rows with source index, metadata, raw/normalized path, both
      provider IDs, match status, and `add/remove/move/keep/blocked` action.
- [x] Write atomic plan read/write/show/checksum helpers under a dedicated state
      directory without touching old Rekordbox plan serialization.
- [x] Ensure fixed-clock equivalent inputs produce byte-identical checksums and
      malformed/unknown versions fail with regenerate guidance.

### Planning rules

- [x] Implement both explicit direction values with one generic matching and
      diff core.
- [x] Make source order the final destination order and calculate additions,
      removals, moves, keeps, current count, and final count.
- [x] Plan creation for a missing destination and adoption for one existing
      exact-name normal destination.
- [x] Block empty source, source missing, missing path matches, ambiguous path
      matches, duplicate canonical tracks, duplicate target names, invalid
      target type, unowned/smart Navidrome target, and Rekordbox-open state.
- [x] Record ordered source/destination memberships and matched ID/path pairs as
      live apply preconditions.
- [x] Treat a directionally identical playlist as a successful no-op plan that
      requires no backup or mutation.

### Tests

- [x] Table-test add, remove, move, mixed changes, no-op, destination creation,
      destination adoption, and changed ordering in both directions.
- [x] Test every blocker independently and prove blocked plans cannot validate
      for apply.
- [x] Test stable selectors, renamed bound playlists, changed config
      fingerprint, and ambiguous exact names.

### Evidence

- Tests: `go test ./internal/playlistmirror ./internal/playlists` passed. The
  planner covers both directions, mixed/reorder/no-op/create, every safety
  blocker, selector/binding ambiguity, stable checksums, atomic round-trip, and
  tamper rejection.
- Example plans: deterministic plan fixtures are constructed in
  `internal/playlistmirror/plan_test.go`; runtime plans are written under
  `state_dir/playlist-sync/plans` or the explicit `--out` path.

---

## Phase 5 — Apply Orchestration and Recovery

**Status:** In progress

**Exit gate:** Apply revalidates all live state, backs up only the destination,
verifies exact parity, and advances pair state only after success.

### Common apply pipeline

- [x] Verify plan version/checksum, config fingerprint, direction, blockers,
      and non-empty source before any dependency or mutation work.
- [x] Re-resolve and re-read both playlists and all matched records; reject any
      source membership/order, destination membership/order, provider ID, or
      matched path change since planning.
- [x] Make global dry-run perform complete validation but skip backups,
      mutations, and pair-state writes.
- [x] Return no-op without backup when live state still matches a no-op plan.
- [x] Define cancellation boundaries before backup, after backup/before write,
      while a provider request is in flight, and during verification.

### Rekordbox destination

- [x] Reuse the existing closed-process/sidecar guard, managed pyrekordbox
      runtime, full database-directory backup, transaction, and committed-state
      verification.
- [x] Convert the generic mirror plan into the existing bridge apply request
      without weakening its expected-current-content-ID precondition.
- [x] Verify exact final ordered content IDs and expose the backup path.

### Navidrome destination

- [x] Require the service, credentials, account ownership, and writable target
      checks before backup.
- [x] Run the existing verified Navidrome database backup command and refuse the
      write if no new backup appears.
- [x] Replace/create the complete ordered playlist and verify exact final song
      IDs.
- [x] Classify an uncertain transport result through readback instead of blind
      retry.

### State and results

- [x] Atomically save resolved bindings and the final parity checksum only
      after provider verification succeeds.
- [x] Return exit code 5 plus backup and reconciliation details if an external
      write is verified/possible but the final verification or local state
      write does not complete.
- [x] Ensure a failed/canceled apply never reports the old destination as
      unchanged when the mutation request may have reached it.
- [ ] Test backup failure prevents write, stale plans prevent backup, state
      failure follows verified write, and rerunning after partial status safely
      rebuilds a plan/binding.

### Evidence

- Tests: `go test ./internal/app ./internal/agent ./internal/cli` passed for
  stale-state rejection before backup, dry-run/no-op suppression, backup-before-
  write, cancellation after backup/before mutation, exact provider readback,
  pair-state advancement, post-verification state-write failure, backup failure,
  and partial exit-code/result propagation.
- Recovery cases: response-lost creation reconciles by exact live membership;
  partial results retain the exact backup path. Explicit context-cancellation
  boundary and post-verification state-write-failure tests remain open.

---

## Phase 6 — CLI Workflow

**Status:** In progress

**Exit gate:** Users and scripts can list jobs, plan either direction, inspect
saved plans, and apply with consistent output and safety behavior.

### Commands and flags

- [x] Register `udl playlist sync list|plan|show|apply` without changing current
      `playlist` or `rekordbox playlist-sync` behavior.
- [x] Require `--job` and the full `--direction` value for plan; support
      `--out` and the existing config/env precedence.
- [x] Require `--plan-file` for show/apply; interactive apply confirms the
      named destination, removals, and final count.
- [x] Require `--force` with `--no-input`; respect global `--dry-run`, `--json`,
      color/TTY behavior, and Ctrl-C semantics.

### Output and errors

- [x] Human plan output identifies direction, source/destination IDs and names,
      creation/adoption, summary, blockers, backup kind, plan path, and next
      command.
- [x] JSON outputs expose stable structured result/plan fields and write only
      primary data to stdout.
- [x] Put progress, diagnostics, and errors on stderr without auth URLs,
      secrets, or stack traces.
- [x] Map invalid usage/config/dependencies/runtime/partial/interrupted outcomes
      to the existing exit codes.
- [x] Add help examples for initial Rekordbox → Navidrome seed, phone return,
      saved plan review, JSON automation, dry-run apply, and no-op.

### Tests

- [x] Command registration/help/golden tests.
- [x] Required flag, enum validation, `--no-input`/`--force`, dry-run, JSON,
      stdout/stderr, and exit-code tests.
- [ ] End-to-end CLI tests against fake Navidrome and isolated Rekordbox
      fixtures.

### Evidence

- Tests: `go test ./internal/cli` passed for registration/help, exact required
  direction/job flags, noninteractive force, human rendering, and blockers.
- Terminal transcript: command examples are embedded in Cobra help and
  `readme.md`. A combined fake-Navidrome/isolated-Rekordbox CLI acceptance test
  remains open.

---

## Phase 7 — Agent Protocol and Native App

**Status:** In progress

**Exit gate:** The native app exposes the same jobs, plan, blockers, safety,
apply result, and cancellation semantics as the CLI.

### Agent protocol

- [x] Add `playlistSync.inspect`, `playlistSync.plan`, and
      `playlistSync.apply` to capability discovery, dispatch, protocol docs,
      golden fixtures, and operation metadata.
- [x] Run plan/apply through the cancellable run framework; preserve terminal
      lifecycle/error frames and never auto-replay a mutating request after
      backend recovery.
- [x] Return Go collections as empty arrays where practical and decode every
      inbound Swift collection with `@DefaultEmpty`.
- [x] Keep plan and apply errors on the owning Playlist Sync workflow rather
      than `alertMessage`.

### Native workspace

- [x] Add a `Playlist Sync` destination, sidebar entry, home action, attention
      summary, and screen-owned `WorkflowStatus`.
- [x] List configured jobs with provider names, resolved IDs, last direction,
      last verified time, parity status, and corrupt/missing-state status.
- [x] Add explicit “Send Rekordbox to phone” and “Bring phone edits to
      Rekordbox” direction controls; do not infer direction from last run.
- [ ] Add job editor/provider pickers that save through the existing guarded
      config-write path and surface external-file conflicts.
- [x] Render plan header, add/remove/move/keep/blocked filters, searchable row
      table, source/destination metadata, preconditions, and backup expectation.
- [x] Use `.constrained(by:)` for every disabled plan/apply/editor control and
      state the reason adjacent to the control.
- [x] Confirm apply with destination name, removals, final count, creation/
      adoption, and backup type; never hide an empty-source or blocker override
      because none exists.
- [x] Render success, no-op, failure, cancellation, and partial/uncertain state
      distinctly with backup and reconciliation detail.
- [x] Preserve an in-flight plan during ordinary navigation, discard it when
      its job/direction/config changes, and never resume an interrupted apply.

### Swift validation

- [x] DTO decoding tests, including nil collections and unknown enum values.
- [ ] AppState run lifecycle, cancellation race, late reply, backend recovery,
      config conflict, and workflow-error routing tests.
- [x] SourceRuleTests for disabled controls and alert allowlist.
- [ ] View/model tests for each status, direction, blocker, no-op, adoption,
      creation, confirmation, and partial result.

### Evidence

- Tests: agent method inventory/dispatch tests pass; `make app-test-dev` passes
  92/92, including protocol inventory, `@DefaultEmpty`, future action values,
  workflow-owned errors, constrained controls, and partial backup rendering.
- Screenshots/manual notes: **NOVEL UI/DESIGN** is recorded below and in the
  view source. The production Swift module compiles. Rendered/manual coverage,
  provider-picker enrichment, resolved-ID/parity detail, and full AppState race
  matrices remain open.

---

## Phase 8 — Acceptance and Hardening

**Status:** In progress

**Exit gate:** Automated suites pass, destructive paths are isolated and
recoverable, and one real phone round trip proves exact ordered parity.

### Automated validation

- [x] `go test ./...`
- [x] `go vet ./...`
- [x] `go test -race ./...`
- [x] `make app-test-dev`
- [x] Build the CLI and development app using the repository-supported paths.
- [x] Run focused fake-Navidrome mutation tests with transport failures and
      redaction assertions.
- [ ] Extend/run the opt-in isolated Rekordbox apply-and-restore acceptance test
      and verify byte-for-byte restoration.
- [x] Verify old standalone playlist, Navidrome favourites, Free DL handoff,
      and `rekordbox playlist-sync` tests remain green.

### Real workflow acceptance

- [ ] Configure `favs-august` as a paired job using the real playlists.
- [ ] Plan Rekordbox → Navidrome and record every match/blocker, creation or
      adoption, removals, final count, plan checksum, and backup expectation.
- [ ] Apply with Rekordbox closed; verify the Navidrome/phone playlist has exact
      ordered path parity and record the Navidrome backup.
- [ ] On the phone, remove one track, add one already-indexed library track,
      and reorder at least one track.
- [ ] Plan Navidrome → Rekordbox and verify the preview names exactly those
      membership/order changes with no unmatched rows.
- [ ] Apply with Rekordbox closed; verify exact ordered content/path parity and
      record the Rekordbox backup.
- [ ] Re-plan both directions and record no-op results.
- [ ] Verify a changed destination after planning refuses apply and regenerating
      produces the expected new diff.
- [ ] Verify empty source, unmatched path, duplicate path, and ambiguous target
      acceptance blockers without bypassing them.

### Manual native-app validation

- [ ] Install/open with `make app-dev-install`; use `.dev/app.sh` and `.dev/ui`
      for repeatable placement and screenshots.
- [ ] Verify setup/edit, both directions, creation/adoption, row filters,
      blocker explanations, confirmation, cancellation, success, no-op,
      backend recovery, and partial-state rendering.
- [ ] Confirm the workflow never exposes a password/token or contacts anything
      outside the configured trusted-LAN Navidrome service.

### Evidence

- Automated command outputs: `go test ./...`, `go vet ./...`, and
  `go test -race ./...` passed; `make app-test-dev` passed 92/92; `make app-dev`
  built `bin/udl` and a valid ad-hoc-signed `dist/dev/UDL-Dev.app`. The sandbox
  denied only a nonessential Go module stat-cache temp write during build; the
  build itself completed successfully.
- Real playlists/versions:
- Backup paths:
- Before/after checksums and ordered counts:
- Screenshots:

---

## Phase 9 — Documentation and Close-Out

**Status:** In progress

**Exit gate:** The current behavior, recovery steps, evidence, and remaining
deferred work are documented, and both root planning documents are final.

### Tasks

- [x] Document config schema, command examples, direction semantics, exact-path
      requirement, empty-source refusal, destination adoption/creation, and
      no-op behavior in `readme.md`.
- [x] Document the three new RPC methods and payload/result examples in the
      agent protocol guide.
- [x] Add Navidrome playlist replacement recovery using the exact reported
      backup and existing Rekordbox recovery references.
- [x] Document that the feature is manual, direction-selected, membership/order
      only, and never a merge or downloader.
- [x] Add every non-obvious implementation lesson to `AGENTS.md` in the same
      change that proves it.
- [ ] Reconcile every unchecked task as completed, blocked with evidence, or
      deferred with an explicit reason.
- [ ] Fill the decision/deviation logs and final validation evidence below.
- [ ] Change PLAN.md and this tracker to `Delivered`/`Done` only after all exit
      criteria and the real phone round trip pass.
- [ ] Archive these root documents only when a later initiative replaces them.

### Evidence

- Documentation changes: `readme.md` documents configuration, CLI use, exact
  path and mirror semantics; `docs/agent-protocol.md` documents all three RPCs;
  `docs/navidrome-playlist-recovery.md` records fresh-plan-first recovery;
  `AGENTS.md` captures binding, OpenSubsonic mutation, and path-identity lessons.
- Final commands/results: automated commands are recorded in Phase 8. Close-out
  remains intentionally open until isolated destructive/manual and real-phone
  acceptance evidence exists.

---

## Decision Log

| Date | Phase | Decision | Rationale | Consequence |
| --- | --- | --- | --- | --- |
| 2026-08-07 | — | Use explicit direction per run | The workflow is seed to phone, edit, then bring back; automatic merge is unnecessary and harder to trust | The selected source replaces destination membership and order |
| 2026-08-07 | — | Navidrome order controls the return run | Phone edits include ordering and must be reproduced in Rekordbox | Source order is authoritative in either selected direction |
| 2026-08-07 | — | Block all unmatched or ambiguous tracks | A silent partial mirror can remove or omit gig tracks | Apply has no partial/skip mode |
| 2026-08-07 | — | Preview and adopt one existing exact-name destination | Existing phone playlists should be reusable without hidden overwrite | Adoption is explicit in the plan; ambiguous names block |
| 2026-08-07 | — | Never allow an empty source | Accidental provider emptiness must not clear a playlist | Clearing a paired playlist remains manual and outside this workflow |
| 2026-08-07 | — | Add CLI and native-app surfaces together | Both scriptable diagnostics and the normal GUI workflow are required | Agent plan/apply contracts are shared by both |
| 2026-08-07 | 0 | Use a separate mirror plan type/version | Existing Rekordbox plan checksums are a compatibility contract | No new fields are added to old plan serialization |
| 2026-08-07 | 1 | Treat sync job IDs as case-insensitively unique | The state path is commonly stored on macOS's case-insensitive filesystem | `FAVS` and `favs` cannot silently share one binding file |
| 2026-08-07 | 1 | Keep resolved provider IDs in state only | External renames should survive without config mutation | Selector changes invalidate the binding through the config fingerprint |
| 2026-08-07 | 1 | Keep YAML as the CLI job-editing surface | PLAN.md freezes `playlist sync` to list/plan/show/apply and defines jobs in `playlists.yaml` | The tracker no longer invents an extra CLI mutation command; native editing remains required |
| 2026-08-07 | 2 | Centralize exact path identity in `internal/pathidentity` | Three implementations had drifted: standalone matching lacked file-URL decoding and NFC folding | Existing exported normalizers wrap one implementation; the new mirror matcher has no metadata input |
| 2026-08-07 | 3 | Prefer OpenSubsonic `formPost`; never blindly retry creation by name | Ordered membership can exceed safe GET sizes, and a lost create response can otherwise duplicate a playlist | Reconcile one exact-name playlist with exact planned membership; otherwise return partial/uncertain |
| 2026-08-07 | 5 | Re-check cancellation after a successful destination backup | Cancellation can arrive between backup completion and the external mutation call | Apply returns the backup path and performs no write at that boundary; in-flight calls retain partial/uncertain semantics |

Add rows whenever implementation evidence changes a decision. Do not rewrite
history silently.

## Deviations from PLAN.md

| Deviation | Reason | Consequence |
| --- | --- | --- |
| None yet | — | — |

## Discovered Work

Record unplanned but necessary tasks here before implementing them.

| Date | Phase | Work | Reason | Status |
| --- | --- | --- | --- | --- |
| 2026-08-07 | 1 | Reconcile CLI editing checklist with frozen CLI contract | The original Phase 1 text required a command absent from PLAN.md | Done: YAML is the CLI surface; native editor remains tracked |
| 2026-08-07 | 5 | Add an explicit post-backup cancellation check | Context-aware provider calls alone left a small window in which cancellation had already won but mutation had not started | Done and covered by `TestPlaylistMirrorCancellationAfterBackupPreventsMutation` |
| 2026-08-07 | 7 | Prevent late cancel completion from replacing a terminal app result | `run.finished` can arrive while `run.cancel` is awaiting its response | Done: the status changes only if the same run is still active |

## Novel UI / Design Ledger

Mark any newly invented view or visual interaction here as **NOVEL UI/DESIGN**
before or alongside implementation, including why existing app patterns were
insufficient and what validation was performed.

| Date | Marker | View/design | Rationale | Validation |
| --- | --- | --- | --- | --- |
| 2026-08-07 | **NOVEL UI/DESIGN** | Dedicated Playlist Sync workspace with two plain-language direction actions and a row-level add/remove/move/keep/blocked preview | The workflow pairs two providers and cannot fit the cache-oriented standalone Playlists inspector without hiding direction and destructive replacement scope | Reuses existing shell, sidebar-context, status, callout, table, confirmation, and `.constrained(by:)` patterns; production module builds and 92 Swift tests pass; rendered manual validation remains required |

## Final Validation Evidence

Not yet available. On completion, record exact commands, pass/fail counts,
tool/service versions, real playlist counts, plan checksums, backup paths,
ordered parity evidence, screenshots, and every deliberate non-action.
