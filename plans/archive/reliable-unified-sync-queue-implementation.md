# Reliable, Unified Sync Queue — Implementation Tracker

- **Overall status:** Done
- **Plan:**
  [reliable-unified-sync-queue-plan.md](./reliable-unified-sync-queue-plan.md)
- **Last updated:** 2026-08-03
- **Archived predecessor:**
  [gui-redesign-implementation.md](./gui-redesign-implementation.md)

This is the live execution record for the reliable unified sync queue. Update
status and evidence in the same change as implementation. A checked task means
the behavior is implemented **and** proportionally validated.

## Status Convention

Phase status must be one of:

- `Not started` — no implementation work has begun.
- `In progress` — implementation or validation is active.
- `Blocked` — a recorded decision, dependency, or external action is required.
- `In review` — implementation is complete and final validation remains.
- `Done` — every task and exit gate is complete.
- `Deferred` — deliberately removed, with the reason recorded in the decision log.

Checkboxes:

- `[ ]` not complete
- `[x]` implemented and validated

Do not check a task that is only coded. Add newly discovered work explicitly;
do not silently broaden an existing checkbox.

## Progress Dashboard

| Phase | Status | Depends on | Exit gate |
| --- | --- | --- | --- |
| 1. Cancellation correctness | Done | — | Startup races, prompt cancellation, acknowledgement, retry, and terminal cancellation tests pass |
| 2. Canonical oldest-first queue | Done | — | Every sync surface defaults oldest-first and native queue projection matches Go manifests |
| 3. Progress transport and backpressure | Done | 1 | Progress is capped at 10 Hz, terminal events remain lossless, and Stop stays responsive under flood |
| 4. Unified native sync table | Done | 2, 3 | The same source-order table survives Continue and completion with correct runtime merging |
| 5. Spotify backend I/O | Done | — | One media walk per invocation and cached atomic state updates are verified |
| 6. Integration and close-out | Done | 1–5 | Full automated and manual acceptance gates pass; documentation and evidence are complete |

## Cross-Cutting Definition of Done

- [x] Preserve unrelated worktree changes and keep each phase reviewable.
- [x] Add or update tests with every behavior change.
- [x] Keep wire-inbound Swift collections tolerant of Go `null` through `@DefaultEmpty` or `decodeIfPresent(...) ?? []`.
- [x] State the reason next to every newly disabled native control using `.constrained(by:)` or an equivalent visible note.
- [x] Route screen-owned failures through the Sync workflow state, not the session-level modal alert.
- [x] Keep completed-track state durable across cancellation.
- [x] Update protocol fixtures and compatibility documentation with wire changes.
- [x] Record exact validation commands and material manual evidence below.
- [x] Record every deviation from
      [reliable-unified-sync-queue-plan.md](./reliable-unified-sync-queue-plan.md)
      in the decision log; none were required.

---

## Phase 1 — Cancellation Correctness

**Status:** Done

**Exit gate:** Stop works before and after a run ID exists, never gets overwritten
by a late start response, reports acknowledgement/failure, and terminates with
exit code 130.

### State and request lifecycle

- [x] Add explicit `cancelRequested`, `cancelRequestInFlight`, and
      `cancelAcknowledged` state for sync runs.
- [x] Make confirmed Stop enter `.canceling` immediately, including while
      `sync.start` is awaiting its response.
- [x] If Stop precedes the run ID, send exactly one cancellation as soon as the
      ID arrives.
- [x] Restrict the successful start transition to `.starting → .running`; never
      overwrite `.canceling` or a terminal phase.
- [x] Keep pending UI response cancellation ordered before `run.cancel`.
- [x] Treat backend `run not found` as a benign completion race only when the
      pending UI cancellation reply was successfully delivered; otherwise show
      an actionable inline failure.
- [x] Handle `CancelRunResult.canceled`, transport errors, and backend errors
      explicitly instead of using `try?`.
- [x] On a failed cancellation request with a healthy connection, return to an
      actionable running state and allow retry without losing the table.
- [x] Ignore percentage progress while canceling, but continue routing track
      outcome, failure, and `run.finished` notifications.
- [x] After two seconds without a terminal notification, show “Still stopping”
      without killing the shared agent process or replaying work.
- [x] Preserve the existing Stop confirmation text and direct Command-period
      behavior.

### Automated validation

- [x] Swift: Stop before `sync.start` returns a run ID.
- [x] Swift: canceling is not revived by the start response.
- [x] Swift: repeated Stop actions send one cancellation request.
- [x] Swift: cancellation acknowledgement, retryable failure, and exit 130 map
      to distinct visible states.
- [x] Swift: pending plan/confirm/input prompts are dismissed without leaving Go
      blocked.
- [x] Go: registry still cancels pending UI work before the run context.
- [x] Go: subprocess cancellation kills the complete adapter process group and
      returns interrupted exit code 130.

### Phase evidence

- Commands: `go test ./internal/engine ./internal/agent` (pass); `make app-test-dev` (pass, included in the 65-test native suite); `make app-dev` (pass)
- Manual checks: Confirmed Stop while Go was blocked on `ui.selectRows`; the prompt reply was released, the run reached phase `canceled` with exit code 130, and the same table remained visible and read-only. Repeated runs exposed and fixed the late-acknowledgement race that could overwrite an already delivered terminal message.
- Notes: Implementation began with the native cancellation state/request lifecycle because progress transport and the unified table build on it. The startup race was real: the prior Stop path returned when `runID == nil`, and the later start response promoted the run to `.running`. `SyncRunState` now owns idempotent cancellation transitions; `AppState` owns only ordered I/O and the two-second watchdog. This Command-Line-Tools-only Mac has no Apple XCTest Swift module, so `make app-test-dev` compiles the real production module with testing enabled, supplies a test-target-only compatibility module, and executes the unchanged XCTest sources.

---

## Phase 2 — Canonical Oldest-First Queue

**Status:** Done

**Exit gate:** unspecified sync order resolves to oldest-first everywhere, an
explicit newest-first selection still wins, and Swift queue slots match the Go
execution manifest for every tested selection.

### Shared default

- [x] Add one engine-level `DefaultDownloadOrder` set to
      `DownloadOrderOldestFirst`; do not change the standalone Free DL default.
- [x] Replace implicit newest-first fallbacks in engine planning/execution,
      no-interaction manifests, agent source capabilities/interactions, and the
      plain CLI selector with the shared default.
- [x] Keep the TUI's existing oldest-first behavior but make it read the shared
      default instead of a local literal.
- [x] Make the native app initialize per-source order from the updated agent
      capability.
- [x] Preserve explicit `newest_first` and reject/normalize invalid values using
      the existing validation semantics.
- [x] Update JSON fixtures, golden payloads, help text, and tests that encode the
      previous implicit newest-first behavior.

### Native queue projection

- [x] Add a pure `PlanQueueProjection` that consumes source-order rows, selected
      indices, and download order.
- [x] Assign newest-first slots `1...N`, oldest-first slots `N...1`, and slot `0`
      to excluded or locked rows.
- [x] Recompute slots immediately after a checkbox or order change.
- [x] Preserve source indices independently from execution slots.
- [x] On backend runtime snapshots, prefer canonical `execution_slot` values
      over projected values.

### Automated validation

- [x] Go: defaults across engine, agent, CLI, and TUI.
- [x] Go: explicit newest-first override.
- [x] Go: oldest/newest manifests with sparse selections, leading omissions,
      locked rows, and zero selected rows.
- [x] Swift: matching queue projections for the same fixtures.
- [x] Swift: toggling a row renumbers only selected execution slots.
- [x] Swift: changing order reverses slot assignment without reordering rows.

### Phase evidence

- Commands: `GOCACHE=/tmp/udl-go-cache go test ./internal/engine ./internal/app ./internal/cli ./internal/agent` (pass); `make app-test-dev` (pass); `GOCACHE=/tmp/udl-go-cache make app-dev` (pass)
- Manual checks: The native configuration showed `Oldest first` for every controllable sync source. The editable SoundCloud table showed the two selected rows as source indices 23 and 39 with projected `run #2` and `run #1`; changing the picker to newest-first and back recomputed the projection without reordering rows.
- Notes: The first implementation made `NormalizeDownloadOrder` return the shared default for every non-oldest value, which incorrectly erased an explicit `newest_first`. It now handles both valid values explicitly and uses the shared default only for empty/invalid input. The Swift projection is O(rows) with one pre-sized index map and leaves source rows untouched.

---

## Phase 3 — Progress Transport and Backpressure

**Status:** Done

**Exit gate:** lifecycle and terminal delivery is lossless, percentage delivery
is latest-only at no more than 10 Hz, and cancellation stays responsive under a
synthetic high-volume adapter stream.

### Protocol v2

- [x] Increment the agent protocol version and update initialization fixtures.
- [x] Add `sync.progress` carrying run ID, source ID, the latest structured
      progress snapshot, and an optional fully resolved changed `TrackRow`.
- [x] Keep `sync.event` for source/track lifecycle, outcomes, failures, activity,
      and full source snapshots.
- [x] Coalesce `track.progress` events per run to a 100 ms minimum interval,
      retaining the newest pending value.
- [x] Flush pending progress before track completion/skip/failure, source
      terminal events, emitter shutdown, and `run.finished`.
- [x] Ensure row updates contain the resolved plan-row identity rather than the
      adapter's execution counter.
- [x] Adapt both normal sync and Free DL capture consumers of the shared agent
      sync emitter.
- [x] Preserve clear unsupported-version failure for mismatched native/backend
      protocol versions.

### Native transport and state application

- [x] Decode `sync.progress` separately from lossless notifications.
- [x] Use newest-only buffering for progress and lossless buffering for
      lifecycle/terminal notifications.
- [x] Merge a progress row update into the stored source table rather than
      replacing every row.
- [x] Keep notification decoding and diagnostic buffering off the main actor;
      perform only the final observable-state mutation on the main actor.
- [x] Stop publishing every backend stderr chunk as observable UI state.
- [x] Do not forward raw adapter stdout/stderr progress to agent stderr; retain
      bounded subprocess tails and persisted failure diagnostics.

### Automated validation

- [x] Go: 1,000 progress lines per second produce at most 10 progress frames per
      second.
- [x] Go: the newest percentage is delivered after coalescing.
- [x] Go: pending progress flushes before every terminal event.
- [x] Go: lifecycle/outcome events are never dropped or reordered.
- [x] Go: cancellation acknowledgement latency remains bounded during flood.
- [x] Swift: progress stream drops stale percentage frames but not terminal
      notifications.
- [x] Swift: v2 progress row updates merge into the intended source and row.
- [x] Swift: post-cancel progress does not change visible run state.

### Phase evidence

- Commands: `GOCACHE=/tmp/udl-go-cache go test ./internal/agent` (pass); `GOCACHE=/tmp/udl-go-cache go test -race ./...` (pass); `make app-test-dev` (pass); `GOCACHE=/tmp/udl-go-cache make app-dev` (pass)
- Measurements: A synthetic 1,000-event burst emits at most the first and newest progress frames; a forced pre-lifecycle flush observes the same 100 ms clock; the cancellation guard test acknowledges within its 100 ms bound while the flood producer is active.
- Notes: Progress uses a newest-only `AsyncStream`; lifecycle/terminal frames retain the lossless stream. Both streams decode in detached tasks, while only typed state application crosses to the main actor. Raw adapter output is retained by `SubprocessRunner` tails rather than mirrored onto agent stderr.

---

## Phase 4 — Unified Native Sync Table

**Status:** Done

**Exit gate:** Continue freezes the accepted queue without changing screens or
row positions, runtime state updates in place, and all planned sources remain
navigable.

### Persistent source-table model

- [x] Add per-source state containing plan rows, selection, source indices,
      projected/canonical execution slots, order, window, acceptance state,
      lifecycle, activity, and runtime status.
- [x] Create or rebuild the editable source state when `ui.selectRows` arrives.
- [x] Preserve remembered selection overrides and cursor behavior across plan
      rebuilds.
- [x] Make plan submission return success/failure so a failed reply unlocks the
      table and reports an inline Sync error.
- [x] Freeze the source state only after a successful non-rebuild Continue.
- [x] Merge full snapshots and compact progress updates by source ID and stable
      row identity.
- [x] Retain accepted and terminal source states until the user configures
      another run.

### Workspace behavior

- [x] Replace plan-versus-run routing with configuration versus unified-run
      routing.
- [x] Keep one source-order `Table` for review, execution, cancellation, and
      terminal results.
- [x] Render checkbox, source index, runtime status, plan class, `run #N`, title,
      and remote identity without introducing a second runtime row layout.
- [x] Match TUI labels: `pending`, `downloading`, `downloaded`, `skipped`,
      `failed`, `not-run`, and `have-it`, with `new`/`gap` and `run #N` secondary
      labels.
- [x] Default table sorting to source index. Allow browsing sorts while keeping
      `run #N` authoritative.
- [x] Before acceptance, offer All/New/Gaps/Have filters; after acceptance,
      offer All/In Run/Remaining/Downloaded/Skipped/Failed/Have over the same
      table.
- [x] Disable checkbox, order, window, selection-reset, and rebuild mutation
      controls after acceptance with a visible reason.
- [x] Keep search, filters, sorting, row navigation, source navigation, and
      failure-detail inspection enabled.
- [x] Keep whole-run progress, activity, and failures in the shared
      toolbar/status bar/inspector/table instead of separate run-only cards.
- [x] Keep Stop and its confirmation available throughout active planning and
      execution.

### Multi-source behavior

- [x] Auto-focus a source with a pending plan prompt before all other sources.
- [x] Otherwise auto-focus the actively downloading source.
- [x] Preserve an explicit sidebar selection until a new source needs input.
- [x] Allow the sidebar to revisit every source whose plan or runtime state is
      known.
- [x] Show queued/unreached sources honestly without fabricating rows.
- [x] Show a source-level lifecycle and “adapter exposes no track plan” empty
      state for non-plan-capable sources.

### Automated validation

- [x] Swift: table identity and row order survive successful Continue.
- [x] Swift: mutation controls lock while browsing controls remain enabled.
- [x] Swift: runtime progress/outcomes update the matching visible row.
- [x] Swift: deselected and locked rows never receive a run number.
- [x] Swift: rebuild replaces the draft without losing intended overrides.
- [x] Swift: multi-source focus priority and sidebar history.
- [x] Swift: terminal/canceled tables remain until reset.
- [x] Verify source-rule tests for disabled-control explanations and workflow
      error ownership; existing budgets/allowlists required no change.

### Phase evidence

- Commands: `make app-test-dev` (pass, 65 tests); `GOCACHE=/tmp/udl-go-cache make app-dev` (pass)
- Screenshots: Light-mode accessibility screenshots were inspected for editable, completed, canceled, multi-source, and non-plan tables; a retained terminal table was also inspected in dark appearance. They were kept as temporary QA artifacts rather than added to the repository, and the user's Appearance setting was restored to Auto.
- Manual checks: A bounded real dry run kept the identical source-order `Table` through Continue and successful completion, retained canonical run slots, removed checkboxes, disabled queue controls with visible reasons, and kept search/filter/sort navigation. A two-source dry run auto-focused source 2 when it needed input, kept source 1 accessible with lifecycle `Done`, and exposed/fixed the viewed-source position label. Canceling source 2 retained its draft read-only with exit code 130.
- Notes: `SyncView` now has only configuration and unified-workspace routing. `SyncSourceTableState` owns draft/accepted/runtime rows; successful wire delivery is the freeze boundary. Canonical backend snapshots replace projected slots, while compact progress replaces only its stable `source_id:index` row. Terminal delivery now normalizes any retained planning/running lifecycle so sidebar, header, and inspector cannot disagree after cancellation.

---

## Phase 5 — Spotify Backend I/O

**Status:** Done

**Exit gate:** deemix performs one target-library walk per track invocation,
state updates avoid repeated reads/parses, and cancellation durability remains
unchanged.

### Media snapshot tracking

- [x] Introduce a source-run media snapshot tracker initialized once before the
      first deemix invocation.
- [x] After every invocation, scan once, detect the changed media path against
      the previous snapshot, and advance the baseline.
- [x] Advance the baseline after unavailable/failed invocations without
      attributing their changes to the next successful track.
- [x] Preserve deterministic changed-path selection when multiple files differ.
- [x] Instrument or inject snapshot operations so tests assert walk counts.

### Cached Spotify state writer

- [x] Load the state payload, JSON prefix, line ordering, entries, and file mode
      once per source run.
- [x] Apply each completed track to the in-memory representation.
- [x] Atomically rewrite after every completed track using the existing temp-file
      and rename safety boundary.
- [x] Preserve v1 ID-only lines, v2 metadata, legacy spotdl JSON prefixes,
      comments/unknown lines, permissions, duplicate handling, and merge rules.
- [x] Keep a successfully downloaded track committed before starting the next
      track so cancellation resumes correctly.

### Automated validation

- [x] One initial walk plus one post-invocation walk per track, never two walks
      per track.
- [x] Successful, unavailable, failed, and canceled invocation baselines.
- [x] Updated/new media path detection and deterministic tie-breaking.
- [x] State writer compatibility with v1, v2, and spotdl-prefixed fixtures.
- [x] Exact per-track atomic persistence across simulated cancellation.
- [x] No regression in Spotify state backfill behavior.

### Phase evidence

- Commands: `GOCACHE=/tmp/udl-go-cache go test ./internal/engine` (pass)
- Benchmarks/measurements: Injected counters verify four walks for three invocations (one initial + one per invocation) and exactly one state-file read across three immediately durable upserts.
- Notes: A failed snapshot disables later path attribution rather than allowing a failed invocation's files to be attributed to a later success. The source-scoped writer preserves prefix/line layout and duplicate semantics while avoiding per-track read/parse work. Final review found that a new writer generated its v2 header only during the first persist and dropped it on the second; the header is now part of the cached prefix and a multi-write regression covers it alongside hybrid spotdl JSON preservation.

---

## Phase 6 — Integration and Close-Out

**Status:** Done

**Exit gate:** every completion criterion in
[reliable-unified-sync-queue-plan.md](./reliable-unified-sync-queue-plan.md) is met and the
commands/evidence below are recorded.

### Full validation

- [x] `go test ./...`
- [x] `go vet ./...`
- [x] `go test -race ./...`
- [x] Native Swift test suite passes with protocol v2 fixtures.
- [x] Development app builds and ad-hoc signs with `make app-dev`.
- [x] No generated/build artifacts are accidentally staged.

### Manual acceptance

- [x] Oldest-first is preselected for every sync-capable source.
- [x] Selected rows show correct `run #N` values before Continue.
- [x] The row marked `run #1` is the first track actually attempted.
- [x] Continue retains the same source-order table and locks only mutation
      controls.
- [x] Runtime status and progress update in place without a run-screen switch.
- [x] A second source needing input auto-focuses while the first source remains
      accessible in the sidebar.
- [x] Confirmed Stop immediately reads “Stopping,” including during startup and
      plan prompts.
- [x] Cancellation reaches exit code 130 locally within two seconds, or shows
      the explicit “Still stopping” state.
- [x] Completed tracks remain on disk and in state after cancel; the active
      partial track is absent.
- [x] A synthetic 1,000-line-per-second stream renders no more than 10 progress
      updates per second and leaves Stop responsive.
- [x] Light/dark screenshots and accessibility snapshots cover editable,
      accepted/running, canceled, completed, multi-source, and non-plan source
      states. The canceling transition completed too quickly for a stable frame;
      its immediate “Stopping” state and delayed “Still stopping” state are
      covered by the executable model tests instead of adding artificial delay
      to production.

### Documentation and release readiness

- [x] Update `docs/agent-protocol.md` for protocol v2 and `sync.progress`.
- [x] Update `docs/swiftui-parity.md` for the unified table and cancellation
      lifecycle.
- [x] Add any newly learned non-obvious invariant to `AGENTS.md`.
- [x] Confirm archived plans remain history-only and root planning links are
      current.
- [x] Set all completed phase statuses to `Done` and overall status to `Done`.

### Final evidence

- Commands: `GOCACHE=/tmp/udl-go-cache go test ./...`; `GOCACHE=/tmp/udl-go-cache go vet ./...`; `GOCACHE=/tmp/udl-go-cache go test -race ./...`; `make app-test-dev` (65 passed); `GOCACHE=/tmp/udl-go-cache make app-dev`; `git diff --check` (all pass). The test commands were run sequentially because the agent-subprocess protocol test treats any concurrent harness text on stdout as a protocol violation.
- Screenshots/clips: Temporary Computer Use screenshots and accessibility snapshots inspected editable, accepted/running, completed, canceled, two-source, and non-plan states in light appearance and a retained terminal table in dark appearance; no generated QA artifact was added to Git. The Appearance setting was restored to Auto and the QA app was closed.
- Performance measurements: The 1,000-event emitter test bounded cancellation acknowledgement below 100 ms, emitted only the first/newest progress frame for a burst, and verified forced lifecycle-boundary flushes cannot bypass the 100 ms interval. Spotify instrumentation measured one initial walk plus one walk per invocation and one state-file read across three durable writes.
- Release notes: Protocol v2 separates lossless lifecycle frames from newest-only progress; sync defaults oldest-first; the native accepted plan remains the runtime table; Spotify media/state I/O is source-scoped and cached.

---

## Decision Log

| Date | Decision | Reason | Impact |
| --- | --- | --- | --- |
| 2026-08-03 | Preserve source order and show execution order as `run #N` | Matches the established TUI model and keeps remote/source context stable | Native table does not reorder when order changes |
| 2026-08-03 | Default all sync surfaces to oldest-first | User preference; TUI already behaves this way | Plain CLI, agent, engine fallbacks, and native capability defaults change |
| 2026-08-03 | Keep one table through planning and runtime | Avoids context loss and makes the accepted plan auditable | Separate native run row presentation is retired |
| 2026-08-03 | Auto-focus active source with sidebar history | Keeps sequential planning obvious without stacking large tables | Per-source table state persists for the full run |
| 2026-08-03 | Keep Stop confirmation | Active partial download is destructive to discard | Queue acceptance has no extra confirmation; Stop still warns |
| 2026-08-03 | Separate progress from lossless events | Prevents stale progress backlog from starving UI and cancellation | Agent protocol version increases |
| 2026-08-03 | Let terminal delivery win over late cancellation replies | `run.finished` can race the `run.cancel` response | A late acknowledgement or error cannot overwrite the canceled terminal outcome |
| 2026-08-03 | Terminalize retained source-table lifecycles | Cancellation can finish while Go is blocked on a plan reply and no final source snapshot follows | Sidebar, header, and inspector agree on `not_run`, `canceled`, `failed`, or `finished` |

Add entries whenever implementation changes an approved behavior, interface, or
sequence. Do not rewrite prior entries; append a superseding decision.

## Blockers

- None. The Command-Line-Tools-compatible native runner executes the unchanged XCTest sources against the real production Swift module.
