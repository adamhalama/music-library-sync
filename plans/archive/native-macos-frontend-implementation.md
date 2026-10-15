# Native macOS Frontend Implementation Tracker

- **Overall status:** Complete (archived 2026-07-30)
- **Plan:** [native-macos-frontend-plan.md](./native-macos-frontend-plan.md)
- **Parity source:** [docs/tui.md](../../docs/tui.md)
- **Last updated:** 2026-07-30

This is the live, editable execution record for the native SwiftUI frontend. Check tasks only after the implementation and its proportional validation are complete. When scope or behavior changes, update this file and `PLAN.md` in the same change.

## Status Convention

Phase status must be one of:

- `Not started` — no implementation work has begun.
- `In progress` — work is active and at least one task is complete.
- `Blocked` — progress requires a recorded decision, dependency, or external action.
- `In review` — implementation is complete and awaiting review or final validation.
- `Done` — every required task and exit gate is complete.
- `Deferred` — deliberately removed from the active sequence with a reason in the decision log.

Checkbox convention:

- `[ ]` not complete
- `[x]` implemented and validated

Do not use a checked box to mean “coded but untested.” Add new tasks when implementation uncovers missing work; do not hide scope by broadening an already-checked task.

## Progress Dashboard

| Milestone | Status | Depends on | Exit gate |
| --- | --- | --- | --- |
| 0. Extract reusable Go state | Done | — | Behavior-preserving Go suite green |
| 1. JSON-RPC agent transport | Done | 0 | Bidirectional protocol contracts green |
| 2. Complete RPC method surface | Done | 1 | Every method group tested and documented |
| 3. Swift foundation + Doctor/Credentials | In review | 1, core of 2 | First app slice works end to end |
| 4. Interactive Sync | In review | 0–3 | TUI-equivalent sync scenarios pass |
| 5. Playlists + Free DL | In review | 2–3 | Cache and re-merge invariants pass |
| 6. Rekordbox | In review | 2–3 | Fail-closed isolated-copy scenarios pass |
| 7. Config Editor + Onboarding | In review | 0, 2–3 | Canonical save and routing parity pass |
| 8. Packaging + Release | In review | 3–7 | Ad-hoc ZIP and clean-machine approval |

Planning baseline:

- [x] Current high-level plan created.
- [x] Current implementation tracker created.
- [x] Completed plans moved to `plans/archive`.
- [x] Create `docs/swiftui-parity.md` when Milestone 3 begins.

## Dependency Sequence

```text
M0 reusable Go state
  ↓
M1 agent transport
  ↓
M2 RPC methods ──────────────┐
  ↓                         │
M3 Swift foundation         │
  ├→ M4 Sync                │
  ├→ M5 Playlists + Free DL │
  ├→ M6 Rekordbox           │
  └→ M7 Config + Onboarding │
               ↓            │
          M8 Packaging ←────┘
```

Milestones 4–7 may proceed in parallel only after the shared transport and store conventions are stable. Milestone 8 can begin with development packaging during Milestone 3, but release completion waits for parity.

## Cross-Cutting Definition of Done

Apply these requirements to every milestone:

- [x] New behavior has focused tests at the lowest useful layer.
- [x] Existing behavior remains covered; tests are not weakened to make extraction pass.
- [x] Errors have stable machine-readable representation and actionable user-facing text.
- [x] Protocol stdout remains valid NDJSON with no log contamination.
- [x] Logs and fixtures contain no credentials, tokens, private playlist data, or live database content.
- [x] Context cancellation and process cleanup have no leaked goroutines or child processes.
- [x] Public behavior and developer documentation are updated in the same change.
- [x] Exact validation commands and results are recorded in the milestone evidence section.
- [x] Any deviation from `PLAN.md` is recorded in the decision log.

2026-07-30 completion audit evidence: focused Go and checked-in Swift tests
cover every new state/protocol/safety seam; the unchanged behavioral suites and
both product smoke suites pass. Protocol subprocess and in-memory tests parse
stdout exclusively as NDJSON, while adapters route output to stderr. Fixture
scans found only deliberate fake redaction-test strings and example-domain
data—no live credentials, private playlists, or databases. Disconnect,
pending-prompt, adapter-execution, refresh, and run-registry cancellation tests
all terminate under race detection. Public protocol, parity, release, and
recovery docs plus the decision log were updated alongside implementation.

## Milestone 0 — Extract Reusable Go State

- **Status:** Done
- **Primary area:** `internal/runstate`, `internal/app`, mechanical `internal/cli` compatibility layer
- **Risk:** behavior drift in the already-working TUI

### 0.1 Establish the extraction baseline

- [x] Record the current commit and working tree state.
- [x] Run and record `go test ./...`.
- [x] Run and record `go vet ./...`.
- [x] Run and record `go test -race ./...`.
- [x] Capture package dependencies for the tracker, startup detection, and plan-source header state.
- [x] Confirm the selected reducer files do not depend on Bubble Tea or Lip Gloss.

### 0.2 Create `internal/runstate`

- [x] Add package documentation describing `runstate` as frontend-neutral derived sync state.
- [x] Move `tuiSyncRunTracker` and tracked source state from `internal/cli/tui_sync_tracker.go`.
- [x] Move row, activity, failure, runtime status, plan class, run scope, lifecycle, and filter types.
- [x] Move all associated constants.
- [x] Move `ObserveEvent`, source snapshot, aggregate counts, elapsed label, and last-failure behavior.
- [x] Preserve matching priority:
  - [x] track ID
  - [x] exact track name
  - [x] normalized track name
  - [x] event index mapped through `ExecutionSlot`
- [x] Move detail conversion helpers for string, integer, and float event values.
- [x] Move failure-state derivation.
- [x] Move display-row conversion and status-label helpers.
- [x] Export names without the `tui` prefix.
- [x] Add package-level documentation for status transitions and matching fallbacks.

### 0.3 Preserve the frozen TUI

- [x] Add type aliases in `internal/cli` for every moved type used by the TUI.
- [x] Add constant aliases or compatibility declarations.
- [x] Add function variables/wrappers for moved helper functions.
- [x] Keep existing TUI call sites mechanically unchanged.
- [x] Move tracker tests into `internal/runstate`.
- [x] Preserve existing TUI integration tests without changing behavioral expectations.

### 0.4 Extract reusable application state

- [x] Move onboarding detection to `app.DetectOnboardingState`.
- [x] Move startup attention detection to `app.DetectStartupAttention`.
- [x] Move plan-source header data to `app.PlanSourceDetails`.
- [x] Define frontend-neutral result types for onboarding and startup attention.
- [x] Add focused tests in `internal/app`.
- [x] Replace TUI implementations with aliases or thin calls to the new app functions.

### 0.5 Stabilize transport DTOs

- [x] Add explicit JSON tags to `engine.PlanRow`.
- [x] Add explicit JSON tags to `engine.SyncResult`.
- [x] Add explicit JSON tags to `engine.ExecutionManifest`.
- [x] Add explicit JSON tags to `engine.ExecutionEntry`.
- [x] Add explicit JSON tags to `engine.SoundCloudPreflight`.
- [x] Add explicit JSON tags to `progress.TrackEvent`.
- [x] Add explicit JSON tags to exported `internal/runstate` snapshots and rows.
- [x] Add JSON golden tests for all transport DTOs.
- [x] Confirm `CanonicalizeExecutionManifest` remains structural and unchanged.
- [x] Confirm `playlistsync.Plan` has no added, removed, or reordered fields.
- [x] Add a regression test proving a pre-existing checksummed plan still verifies.

### Milestone 0 exit gate

- [x] `go test ./...` passes.
- [x] `go vet ./...` passes.
- [x] `go test -race ./...` passes.
- [x] No product behavior, keybinding, rendering, event, or exit-code change is present.
- [x] Reviewer confirms `internal/runstate` imports no Bubble Tea, Lip Gloss, or CLI package.

**Evidence / notes:**

- Baseline commit: `8350bbdc757f58175d27f18daac2ed24854300d3`.
- Baseline working tree was already dirty with the plan/archive reorganization and GUI design documents. Those changes are treated as pre-existing user work and are not implementation output.
- The default Go build cache is outside the managed workspace and was unreadable in the sandbox. Validation therefore uses isolated `GOCACHE` directories under `/tmp`; this changes cache location only, not test behavior.
- `env GOCACHE=/tmp/udl-go-cache-test go test ./...` passed on 2026-07-30 (about 30 seconds).
- `env GOCACHE=/tmp/udl-go-cache-vet go vet ./...` passed on 2026-07-30 (about 18 seconds).
- `env GOCACHE=/tmp/udl-go-cache-race go test -race ./...` passed on 2026-07-30 (about 31 seconds).
- Tracker/reducer dependencies are `config`, `engine`, and `output`; the extraction candidates contain no Bubble Tea or Lip Gloss imports. Bubble Tea/Lip Gloss remain in the CLI model and render shells.
- Before extraction, startup detection depended on CLI `AppContext`, global credential inspection functions, and current-process config discovery. The reusable APIs now accept explicit paths, working directory, environment, and credential inspectors; CLI wrappers preserve the original defaults.
- The frozen TUI directly inspects `interactiveTracker.startedAt` from rendering and tests. The CLI compatibility tracker therefore mirrors that timestamp while delegating reducer state and behavior to `runstate.Tracker`; changing those callers would make the extraction unnecessarily behavioral.
- `env GOCACHE=/tmp/udl-go-cache-test go test ./...` passed after the initial `runstate` extraction on 2026-07-30.
- Transport DTOs use explicit `snake_case` JSON field names, matching the existing `output.Event` wire convention. Focused golden field-name tests cover engine DTOs, progress track events, and run-state source snapshots.
- `CanonicalizeExecutionManifest` was not changed. `playlistsync.Plan` was not changed, and the existing `TestPlanChecksumStaysStableForPlansWithoutDuplicates` regression remains green.
- `env GOCACHE=/tmp/udl-go-cache-vet go vet ./...` and `git diff --check` passed after the extraction and DTO tag changes.
- Onboarding intentionally treats an enabled standalone Free DL job or valid Rekordbox configuration as a configured product even when the main config has no sync sources. An initially non-isolated test discovered the user's enabled Free DL configuration; focused tests now supply an explicit missing Free DL path to prove the no-source route without changing this invariant.
- `env GOCACHE=/tmp/udl-go-cache-test go test ./...` passed at the Milestone 0 exit gate on 2026-07-30.
- `env GOCACHE=/tmp/udl-go-cache-vet go vet ./...` passed at the Milestone 0 exit gate on 2026-07-30.
- `env GOCACHE=/tmp/udl-go-cache-race go test -race ./...` passed at the Milestone 0 exit gate on 2026-07-30.
- `go list -deps ./internal/runstate`, source inspection, and `rg` confirm `internal/runstate` imports no Bubble Tea, Lip Gloss, or `internal/cli`.
- `git diff --check` passed, and `internal/rekordbox/playlistsync/plan.go` remains untouched.

## Milestone 1 — JSON-RPC Agent Transport

- **Status:** Done
- **Primary area:** `internal/agent`, `internal/cli/agent.go`
- **Risk:** deadlock, stdout corruption, lost cancellation, unbounded frames

### 1.1 Define and document the wire contract

- [x] Document protocol version `1` and negotiation behavior.
- [x] Use JSON-RPC 2.0 objects with one complete JSON object per line.
- [x] Define request, response, error, and notification envelopes.
- [x] Define allowed request ID types and collision behavior.
- [x] Define stable JSON-RPC and application error codes.
- [x] Define maximum inbound frame size based on worst-case plan fixtures plus headroom.
- [x] Define behavior for unknown methods, invalid params, malformed JSON, oversized frames, and duplicate IDs.
- [x] Define additive field compatibility and protocol-version failure behavior.
- [x] Add a redaction policy for params, errors, and log notifications.

### 1.2 Implement connection framing

- [x] Add `internal/agent/conn.go`.
- [x] Read input on a dedicated goroutine.
- [x] Increase `bufio.Scanner` capacity and return a specific oversized-frame error.
- [x] Serialize all writes through one mutex-protected writer.
- [x] Flush every frame.
- [x] Correlate outbound server-to-client requests with pending reply channels.
- [x] Correlate inbound client-to-server responses safely.
- [x] Reject or handle duplicate response IDs deterministically.
- [x] Unblock all pending calls when the client disconnects.
- [x] Ensure malformed client input cannot panic the process.
- [x] Ensure concurrent progress notifications cannot interleave JSON.

### 1.3 Implement lifecycle and run registry

- [x] Add `internal/agent/runs.go`.
- [x] Generate opaque unique `runId` values.
- [x] Store context, cancel function, lifecycle state, and pending UI request per run.
- [x] Make register, lookup, finish, and cancel concurrency-safe.
- [x] Make cancellation idempotent.
- [x] Remove completed runs from the registry.
- [x] Emit exactly one terminal `run.finished` notification per accepted run.
- [x] Cancel all active runs on session shutdown or connection loss.
- [x] Add leak/race tests for repeated start/cancel/finish sequences.

### 1.4 Implement `app.Interaction` over JSON-RPC

- [x] Add `internal/agent/interaction.go`.
- [x] Map `Confirm` to `ui.confirm`.
- [x] Map `Input` to `ui.input`, including explicit mask metadata.
- [x] Map `SelectRows` to `ui.selectRows`.
- [x] Include `runId` and `sourceId` in every UI request where available.
- [x] Include plan rows, source details, download order, and plan window for selection.
- [x] Decode selected indices, order, cancel, rebuild, and new plan window.
- [x] Validate selection replies before returning them to the engine.
- [x] Support repeated selection requests for a plan-window rebuild.
- [x] Reject a reply belonging to a different run or source.
- [x] On cancel, reply to the pending UI request first and cancel context second.
- [x] Test client disconnect while each interaction kind is pending.

### 1.5 Guarantee stdout purity

- [x] Construct the agent application context with normal output redirected to stderr.
- [x] Redirect child-process stdout and stderr away from protocol stdout.
- [x] Never construct a Bubble Tea program in the agent path.
- [x] Call app use cases directly instead of invoking Cobra subcommands.
- [x] Verify interactive sync bypasses CLI-only TTY and `--json` guards without weakening those guards for normal CLI use.
- [x] Add a subprocess test that fails on any non-JSON stdout line.
- [x] Verify panics and fatal errors are reported on stderr and/or as a valid terminal frame.

### 1.6 Register the command

- [x] Add a small `internal/cli/agent.go` composition shim.
- [x] Register `udl agent` in `newRootCommand`.
- [x] Add `--working-dir` with explicit validation and deterministic default behavior.
- [x] Inject build information without making `internal/agent` import `internal/cli`.
- [x] Keep the command hidden from casual workflows only if documentation tests and support needs permit it; otherwise document it as an internal frontend API.
- [x] Add CLI parsing and exit-code tests.

### 1.7 Transport contract tests

- [x] Test request/response round-trip.
- [x] Test server-to-client request/response round-trip.
- [x] Test out-of-order responses.
- [x] Test concurrent requests and notifications.
- [x] Test malformed and oversized frames.
- [x] Test unknown methods and invalid params.
- [x] Test graceful shutdown and abrupt EOF.
- [x] Test full interaction sequence: `ui.selectRows` → rebuild → `ui.selectRows` → `ui.confirm`.
- [x] Test cancellation with a UI request pending.
- [x] Test cancellation during adapter execution.
- [x] Run the transport tests under `-race`.

### Milestone 1 exit gate

- [x] All transport contract tests pass.
- [x] A scripted client can initialize, invoke a fixture run, interact, cancel, and shut down.
- [x] Captured stdout parses as NDJSON without exceptions.
- [x] No blocked goroutines remain after disconnect or cancellation.

**Evidence / notes:**

- Protocol v1 is documented in `docs/agent-protocol.md`. Frames are bounded at 8 MiB and use the repository's existing `snake_case` wire convention.
- Active duplicate request IDs fail with `-32003`; the first response for an outbound ID wins and later duplicate/unknown responses are ignored.
- Server-originated IDs use opaque `s-<number>` values; run IDs are independent 128-bit random hexadecimal values.
- The connection reader runs separately from request handlers, while a single mutex-protected writer flushes complete NDJSON frames. Context cancellation closes a closable input to avoid an idle scanner goroutine leak.
- `RunRegistry.Cancel` cancels the pending local UI call before canceling the run context. `Interaction` maps that ordering to `context.Canceled`, and the race test asserts the UI callback observes a live run context.
- `udl agent` is a visible, documented internal frontend API. `--working-dir` resolves and validates an absolute directory, temporarily applies it for config discovery, and restores the caller's directory in in-process tests.
- `env GOCACHE=/tmp/udl-go-cache-test go test ./internal/agent ./internal/cli` passed on 2026-07-30.
- `env GOCACHE=/tmp/udl-go-cache-race go test -race ./internal/agent` passed on 2026-07-30.
- `TestScriptedClientRunInteractsCancelsAndShutsDown` covers initialize → selection rebuild → replacement selection → confirmation → execution wait → cancel → one `run.finished` → shutdown. The fixture execution uses the same generic run context/cancellation primitive that adapter-backed methods use.
- `TestAgentSubprocessStdoutIsProtocolOnlyNDJSON` launches the real command in a child process, performs initialize and shutdown incrementally, parses every stdout line as JSON, and verifies bounded exit.
- Handler panics are recovered as redacted `-32603` protocol errors; panic values are not copied into the frame.
- Full `go test ./...`, `go vet ./...`, `go test -race ./...`, and `git diff --check` passed after the Milestone 1 transport implementation on 2026-07-30.
- The production `sync.start` path calls `app.SyncUseCase` directly with protocol interactions enabled regardless of TTY. CLI `sync --plan` guards remain untouched.
- Protocol stdin is never passed to adapters. The default subprocess runner sends both child stdout and stderr to the agent's stderr stream while structured events use JSON-RPC notifications.
- `TestSyncStartCancellationInterruptsAdapterAndKeepsProtocolStructured` starts a real engine adapter execution through an injected runner, cancels it over `sync.cancel`, observes exit code 130, and verifies structured `sync.event` output.
- The adapter cancellation fixture confirmed existing path safety: target and state directories must already exist; the agent does not silently create user library roots.
- Full `go test ./...`, `go vet ./...`, `go test -race ./...`, and `git diff --check` passed again after the production sync transport slice.

## Milestone 2 — Complete RPC Method Surface

- **Status:** Done
- **Primary area:** `internal/agent/methods_*.go`, existing app/domain packages
- **Risk:** duplicating CLI behavior or bypassing safety validation

### 2.1 Session and capabilities

- [x] Implement `session.initialize`.
- [x] Return protocol version, application build info, supported method list, and feature capabilities.
- [x] Return effective working directory and relevant config discovery paths.
- [x] Implement graceful `session.shutdown`.
- [x] Reject non-session calls before successful initialization.
- [x] Add compatibility tests for unsupported protocol versions.

### 2.2 Config

- [x] Implement `config.load`.
- [x] Implement `config.validate`.
- [x] Implement `config.readFile`.
- [x] Implement `config.writeFile`.
- [x] Use `config.Load`, `Validate`, `LoadSingleFile`, `MarshalCanonical`, and `SaveSingleFile`.
- [x] Preserve comments/formatting only where existing APIs promise it; document canonical rewrite behavior.
- [x] Validate before save and write atomically.
- [x] Return structured field errors.
- [x] Restrict file operations to explicit config/project paths supplied by the initialized session.
- [x] Test invalid YAML, invalid schema, missing file, permissions, and atomic-write failure.

### 2.3 Doctor

- [x] Implement `doctor.run` through `app.DoctorUseCase`.
- [x] Include effective PATH and resolved dependency locations.
- [x] Return structured check names, severity, status, details, and remediation.
- [x] Preserve existing doctor exit-code meaning.
- [x] Test zero-source, missing dependency, invalid credential, and healthy scenarios.

### 2.4 Credentials

- [x] Implement `credentials.list` using inspection APIs only.
- [x] Return presence/status metadata, never secret values.
- [x] Implement `credentials.save`.
- [x] Implement `credentials.clear`.
- [x] Redact input params from logs and JSON-RPC error data.
- [x] Prevent secret values from appearing in debug descriptions or crash reports.
- [x] Test SoundCloud, Spotify, and Deezer credential kinds supported by the repository.
- [x] Test Keychain command failure and user denial.

### 2.5 Startup and source capabilities

- [x] Implement `startup.onboardingState`.
- [x] Implement `startup.attention`.
- [x] Implement `sources.capabilities`.
- [x] Report support for plan, plan window, download order, and defaults per source.
- [x] Add golden tests matching the TUI's current READY/ATTENTION/BLOCKED routing.

### 2.6 Sync and generic runs

- [x] Implement `sync.start` returning immediately with `runId`.
- [x] Build `app.SyncUseCase` directly with agent IO and interaction.
- [x] Stream `sync.event` with original event, run-state snapshot, and structured progress snapshot.
- [x] Implement `sync.cancel` as a compatibility alias or delegate to `run.cancel`.
- [x] Implement generic `run.cancel`.
- [x] Emit structured terminal result, error, and exit code through `run.finished`.
- [x] Preserve partial-success and dependency-failure exit codes.
- [x] Test simultaneous independent runs or explicitly reject them with a documented error.

### 2.7 Playlists

- [x] Implement `playlists.list`.
- [x] Implement `playlists.providerList`.
- [x] Implement `playlists.show`.
- [x] Implement explicit `playlists.refresh`.
- [x] Implement `playlists.saveDefinition`.
- [x] Implement `playlists.config.read`.
- [x] Implement `playlists.config.write`.
- [x] Keep list/show cache-only.
- [x] Preserve the previous valid snapshot on failed or canceled refresh.
- [x] Test duplicate tracks, snapshot checksums, cancellation, and provider errors.

### 2.8 Free DL

- [x] Implement `freedl.config.read`.
- [x] Implement `freedl.config.write`.
- [x] Implement `freedl.plan.start`.
- [x] Forward `CapturePlanEvent` as `freedl.planEvent`.
- [x] Implement `freedl.capture.start`.
- [x] Recreate the existing synthetic single-source config construction.
- [x] Run capture through `app.SyncUseCase` with `scdl-freedl`.
- [x] Implement `freedl.promotionPlan.build`.
- [x] Implement `freedl.promote.apply`.
- [x] Preserve overrides by `RemoteID` across streamed re-merges.
- [x] Detect adapter stack traces that exit zero and report failure.
- [x] Test browser handoff, buffer paths, cancellation, and promotion safety.

### 2.9 Rekordbox

- [x] Implement `rekordbox.config.read`.
- [x] Implement `rekordbox.config.write`.
- [x] Implement `rekordbox.deps.status`.
- [x] Implement `rekordbox.deps.ensure`.
- [x] Implement `rekordbox.deps.reset`.
- [x] Implement `rekordbox.inspect`.
- [x] Implement `rekordbox.plan`.
- [x] Implement `rekordbox.apply`.
- [x] Reuse `app.RekordboxPlaylistSyncUseCase`, runtime resolver, bridge client, and music reader.
- [x] Preserve checksum validation before runtime overrides.
- [x] Preserve the process-closed, complete-plan, and backup-before-write gates.
- [x] Return structured blocker rows.
- [x] Test fixtures and isolated database copies only.

### 2.10 Documentation and method contracts

- [x] Add `agent` to the README command surface.
- [x] Keep README tokens required by `tui_test.go` and `promote_freedl_docs_test.go`.
- [x] Add a protocol reference with method params, results, notifications, errors, and versioning.
- [x] Add request/response golden fixtures for every method group.
- [x] Ensure `session.initialize` method inventory matches the documented inventory.

### Milestone 2 exit gate

- [x] Every listed RPC method is implemented or deliberately deferred with an approved plan change.
- [x] Every method has happy-path, validation-error, and cancellation/failure coverage where applicable.
- [x] `go test ./...`, `go vet ./...`, and `go test -race ./...` pass.
- [x] The README documentation-contract tests pass.

**Evidence / notes:**

- 2026-07-30: session initialization now reports protocol/build/method inventory,
  config discovery scope, and transport capabilities.
- 2026-07-30: config RPCs use the canonical, atomic config APIs and reject paths
  outside the initialized session scope plus symbolic-link config targets.
- 2026-07-30: credential RPC results expose status metadata only. Mutation errors
  intentionally discard backend error text because Keychain failures can echo
  secret-bearing command arguments.
- 2026-07-30: startup and source capability RPCs delegate to extracted app state
  and engine predicates. Doctor zero-source tests cannot assume exactly two
  checks on a machine with an existing Rekordbox database; Rekordbox checks may
  legitimately follow the two onboarding checks.
- 2026-07-30: playlist list/show are cache-only; provider discovery and refresh
  are cancellable runs. A canceled refresh leaves the prior checksummed snapshot
  intact.
- 2026-07-30: Free DL client plans are not filesystem authority. Capture buffer
  and log roots are re-derived from the configured job, run IDs reject traversal,
  and promotion apply rebuilds an authoritative plan before merging selection
  state. Streamed plan rows merge selection overrides by `RemoteID`.
- 2026-07-30: Rekordbox apply validates the signed, complete plan before passing
  runtime or backup overrides to the existing use case. The use case remains the
  owner of process-closed checks, precondition re-inspection, backup-before-write,
  and post-apply order verification; agent errors add structured blocker rows.
- 2026-07-30: protocol v1 advertises 37 methods. The checked-in golden inventory
  covers every method group and is compared directly with `session.initialize`.
  Full `go test ./...`, `go vet ./...`, and `go test -race ./...` gates passed.
  Scenario coverage is layered: agent contract tests exercise RPC boundaries,
  while existing config/doctor/engine/Free DL/Rekordbox domain tests exercise
  permission, dependency, browser handoff, cancellation, and backup safety.

## Milestone 3 — Swift Foundation and First Vertical Slice

- **Status:** In review
- **Primary area:** `macos/`
- **First slice:** Doctor + Credentials
- **Risk:** process lifecycle, actor isolation, Keychain/TCC differences

### 3.1 Project foundation

- [x] Add a checked-in Xcode project under `macos/`.
- [x] Set a deployment target and supported macOS policy.
- [x] Define Debug, Release, and test schemes.
- [x] Add `UDLApp.swift`.
- [x] Add an observable root `AppState`.
- [x] Add feature directories for all planned workflows.
- [x] Add a development-only mechanism to locate a locally built `udl`.
- [x] Add a release mechanism that resolves `Contents/Resources/udl`.

### 3.2 `AgentProcess`

- [x] Spawn the embedded backend with `Process`.
- [x] Create stdin, stdout, and stderr pipes.
- [x] Pass the explicit project working directory.
- [x] Resolve the login-shell PATH once and pass the result in the child environment.
- [x] Add defensive PATH entries without dropping existing entries.
- [x] Capture bounded stderr logs with secret redaction.
- [x] Detect launch failure, unexpected exit, and protocol EOF.
- [x] Terminate gracefully on app shutdown, then force termination after a bounded timeout.
- [x] Implement deliberate restart after crash without silently replaying mutating requests.
- [x] Surface backend version and state in the app.

### 3.3 `JSONRPCConnection`

- [x] Implement as a Swift actor.
- [x] Frame one JSON value per line.
- [x] Correlate outbound requests with checked throwing continuations.
- [x] Resume each continuation exactly once.
- [x] Handle out-of-order responses.
- [x] Decode notifications into typed event streams.
- [x] Decode inbound `ui.*` requests into `AsyncStream<UIRequest>`.
- [x] Send matched results or JSON-RPC errors.
- [x] Fail pending continuations on disconnect.
- [x] Support task cancellation without losing the wire reply obligation.
- [x] Add bounded message sizes and useful decode diagnostics.

### 3.4 Typed client and wire models

- [x] Add `UDLClient` with one async method per RPC method.
- [x] Mirror Go DTO envelopes with `Codable` Swift types; retain checksummed
  Rekordbox plan bodies as lossless `JSONValue`.
- [x] Model timestamps consistently.
- [x] Model `output.Event.Details` as optional known fields.
- [x] Ignore unknown additive fields.
- [x] Preserve unknown enum values where forward compatibility requires it.
- [x] Decode Go-generated golden fixtures.
- [x] Make Go decode Swift-generated request fixtures.
- [x] Generate or validate method names centrally to prevent string drift.

### 3.5 UI request coordination

- [x] Add observable pending-prompt state.
- [x] Present confirm requests and return explicit cancel state.
- [x] Present plain and masked input requests.
- [x] Present row selection with details, order, and plan window.
- [x] Apply a changed plan window to local per-source state before requesting rebuild.
- [x] Reply to pending prompts before sending run cancellation.
- [x] Clear prompts on terminal result, disconnect, or session shutdown.
- [x] Prevent a stale view from answering a later request.

### 3.6 Doctor vertical slice

- [x] Initialize the session and load capabilities.
- [x] Invoke `doctor.run`.
- [x] Render check status, detail, and remediation.
- [x] Display effective PATH and dependency resolution.
- [x] Distinguish backend launch/protocol failure from an unhealthy doctor result.
- [x] Provide refresh/retry.
- [x] Test healthy, warning, blocked, and backend-crash states.

### 3.7 Credentials vertical slice

- [x] List credential presence without loading values.
- [x] Save a replacement credential from an initially empty secure field.
- [x] Clear a credential with confirmation.
- [x] Keep secret values out of observable debug output.
- [x] Clear editable memory after save/cancel as far as Swift permits.
- [x] Explain Keychain failure and permission denial.
- [ ] Validate credential mutation behavior from an ad-hoc-signed development app.

### 3.8 Swift tests

- [x] Test request correlation and out-of-order replies.
- [x] Test disconnect with pending continuations.
- [x] Test notification decoding.
- [x] Test all `ui.*` request round-trips.
- [x] Test task and run cancellation ordering.
- [x] Test unknown fields and enum values.
- [x] Test Go/Swift golden JSON round-trips.
- [x] Test AgentProcess launch, exit, and bounded restart.
- [ ] Run Thread Sanitizer on transport and store tests where practical.

### Milestone 3 exit gate

- [x] Doctor works end to end from SwiftUI through the embedded Go process.
- [ ] Credential list/save/clear works without exposing existing values.
- [x] Backend crash and restart are understandable and recoverable.
- [ ] `xcodebuild build` and `xcodebuild test` pass.
- [x] `docs/swiftui-parity.md` exists and records Doctor/Credentials status.

**Evidence / notes:**

- 2026-07-30: macOS 14 is the current deployment floor. The project contains
  shared Debug/Release app and test schemes and uses automatic/ad-hoc development
  signing settings with hardened runtime enabled.
- 2026-07-30: Apple Swift 6.3.3 directly type-checked the complete application
  source set. `plutil` validated the project and `xmllint` validated both shared
  schemes.
- 2026-07-30: every client method now accepts and returns a named Codable wire
  envelope. Checksummed Rekordbox plan bodies deliberately remain Codable
  `JSONValue` so a Swift decode/re-encode cannot add defaults or drift the Go
  checksum contract.
- 2026-07-30: full `xcodebuild build`, `xcodebuild test`, XCTest compilation,
  signing, TCC/Keychain manual checks, and Thread Sanitizer remain open because
  this host has no `Xcode.app`; `xcode-select` points to Command Line Tools and
  `xcodebuild` refuses app-project work in that environment.
- 2026-07-30: a Swift 6 complete-concurrency executable harness ran the real
  `AgentProcess` through launch, protocol initialization, deliberate restart,
  graceful shutdown, and exit-code-17 crash detection. The app surfaces backend
  state, explains that no mutating request was replayed, and provides an
  explicit Restart Backend action.
- 2026-07-30: a disposable ad-hoc-signed application bundle launched the
  universal embedded backend from `Contents/Resources/udl`, initialized the
  real protocol against the home-directory context, and rendered Doctor's 31
  checks, effective dependency results, three warnings, and zero blockers.
  Terminating only the child backend exposed and fixed a recovery-alert race:
  unexpected connection EOF is now distinguished by the observer task not
  being canceled, rather than a racy snapshot of process state. The live alert
  explained that no write was replayed; its Restart Backend action restored the
  connected Doctor screen.
- 2026-07-30: live startup also exposed Go's valid `null` encoding for empty
  onboarding detail lines and Rekordbox folder/job slices. Swift now normalizes
  those optional wire collections to empty arrays, with checked-in regression
  payloads matching the real Go responses. The complete app source then passed
  Swift 6 strict-concurrency compilation and strict ad-hoc signature
  verification.

## Milestone 4 — Interactive Sync Parity

- **Status:** In review
- **Primary areas:** `macos/UDL/Features/Sync`, sync RPC/runstate integration
- **Risk:** largest workflow; selection rebuild and cancel ordering

### 4.1 Run configuration

- [x] Load available sources and capabilities.
- [x] Support source multi-select.
- [x] Support plan limit and unlimited mode.
- [x] Support dry run.
- [x] Support timeout override.
- [x] Support per-source download order.
- [x] Support per-source plan window.
- [x] Preserve per-source options when rebuilding the overall plan.
- [x] Validate options before starting a run.

### 4.2 Plan selection

- [x] Render Go-supplied rows without reclassification.
- [x] Support toggleable and locked rows.
- [x] Preserve default selection.
- [x] Support filters:
  - all
  - will sync
  - missing new
  - known gap
  - already have
- [x] Return selected indices and download order.
- [x] Request rebuild after plan-window change.
- [x] Apply the new window to both engine reply and copied Swift source state.
- [x] Preserve cursor/selection where stable identifiers allow it.
- [x] Handle cancel without state writes.

### 4.3 Runtime state and progress

- [x] Consume server-computed run-state snapshots.
- [x] Consume structured progress snapshots.
- [x] Render per-track state and failure detail.
- [x] Render per-source lifecycle and aggregate counts.
- [x] Support runtime filters:
  - all
  - in run
  - remaining
  - downloaded
  - skipped
  - failed
- [x] Render bounded activity/log output separately from protocol frames.
- [x] Preserve final state after `run.finished`.
- [x] Represent success, partial failure, dependency failure, interruption, and cancellation distinctly.

### 4.4 Prompts and authentication

- [x] Handle existing-track confirmations.
- [x] Handle Spotify browser authentication handoff.
- [x] Handle Deezer ARL replacement in an empty secure field.
- [x] Handle any source-specific input prompts.
- [x] Cancel pending prompts before canceling run context.
- [x] Recover if the backend exits while a prompt is visible.

### 4.5 Sync validation

- [x] Add scripted full-run protocol fixture.
- [x] Cover selection → rebuild → confirmation → execution.
- [x] Cover cancellation at selection, prompt, and execution stages.
- [x] Cover dry run and no-op selection.
- [x] Cover Spotify retry behavior that removes `--headless` for clearer browser auth.
- [x] Cover zero-exit deemix stack traces as failures.
- [x] Compare final counts and row states with equivalent TUI scenarios.
- [x] Run `make smoke-main` unchanged.

### Milestone 4 exit gate

- [x] All sync options and filters in `docs/tui.md` are checked in `docs/swiftui-parity.md`.
- [ ] Scripted and manual scenarios match TUI results.
- [x] Cancellation leaves no child process, blocked prompt, or active run registry entry.
- [x] Existing CLI and TUI sync tests remain green.

**Evidence / notes:**

- 2026-07-30: added typed source capabilities, sync request, selection,
  run-state, activity, and terminal DTOs plus the native Sync configuration and
  run-detail surfaces.
- 2026-07-30: selection renders Go-owned classifications and locked/default
  state. A window rebuild first updates Swift's copied per-source option, then
  replies to Go, preserving the existing engine/TUI invariant.
- 2026-07-30: plan rows use stable remote IDs (with URL/index fallbacks) to
  preserve selection overrides and the focused row across a plan rebuild.
- 2026-07-30: the scripted NDJSON fixture enforces selection → rebuild →
  stable-ID reselection → confirmation → execution → completion ordering.
- 2026-07-30: structured progress now has typed Swift DTOs and visible global
  and current-track meters sourced directly from the backend snapshot.
- 2026-07-30: full `make smoke-main` passed against one public single-track
  SoundCloud source in an isolated `/tmp` root. It exercised init, validation,
  Doctor, real preflight/download, idempotence, deliberate deletion and gap
  repair, source filtering, partial failure, and no-preflight.
- 2026-07-30: application source type-check passes with Apple Swift 6.3.3.
  A live ad-hoc app rendered the source-independent Doctor, Credentials,
  Playlists, Free DL, and Configuration surfaces. The Computer Use
  accessibility service disconnected while inspecting the Sync view even
  though both app and backend remained alive; scripted/manual TUI parity and a
  full-Xcode UI pass therefore remain open.

## Milestone 5 — Playlists and Free DL Parity

- **Status:** In review
- **Primary areas:** `Features/Playlists`, `Features/FreeDL`
- **Risk:** accidental network access, snapshot loss, selection loss during stream re-merge

### 5.1 Playlist browsing and definitions

- [x] List cached playlist definitions without provider access.
- [x] Show saved snapshot contents and metadata.
- [x] Show missing/invalid snapshot state clearly.
- [x] List supported providers.
- [x] Save playlist definitions with validation.
- [x] Read and write standalone playlist config canonically.
- [x] Support explicit refresh only.
- [x] Show refresh progress and cancellation.
- [x] Atomically replace the snapshot after successful refresh.
- [x] Preserve the previous valid snapshot after refresh failure or cancellation.
- [x] Compare duplicate tracks by occurrence count.

### 5.2 Free DL capture planning

- [x] Read and write Free DL config.
- [x] Start capture planning and consume streamed `freedl.planEvent`.
- [x] Render filters and row state from backend events.
- [x] Key user selection overrides by `RemoteID`.
- [x] Reapply overrides after each streamed plan merge.
- [x] Handle browser handoff and download watcher status.
- [x] Handle cancellation without corrupting buffer state.

### 5.3 Capture and promotion

- [x] Start capture through the synthetic `scdl-freedl` source.
- [x] Render run-state and progress consistently with Sync.
- [x] Build promotion plans.
- [x] Render conflicts, destinations, and blockers.
- [x] Apply promotion with existing file-safety behavior.
- [x] Treat known stack-trace output as failure even when the adapter exits zero.
- [x] Preserve deterministic ordering and state writes.

### 5.4 Playlist and Free DL validation

- [x] Test opening the feature with network disabled.
- [x] Test explicit successful refresh.
- [x] Test failed and canceled refresh preserving the prior snapshot.
- [x] Test duplicates in snapshot comparisons.
- [x] Test streamed re-merge with selection overrides.
- [x] Test browser handoff cancel and success.
- [x] Test capture and promotion failure paths.
- [x] Run `make smoke-freedl` unchanged.

### Milestone 5 exit gate

- [ ] Playlist and Free DL items in `docs/swiftui-parity.md` are complete.
- [x] Offline/cache-first and last-valid-snapshot invariants have automated coverage.
- [x] Free DL selection survives streamed re-merges.
- [x] Existing playlist and Free DL CLI/TUI tests remain green.

**Evidence / notes:**

- 2026-07-30: native Playlists loads `playlists.list` and
  `playlists.config.read` only on open. Provider listing and refresh are
  separate, explicit cancellable runs. Terminal failure/cancel text states
  that the last valid snapshot remains active.
- 2026-07-30: native Free DL consumes streamed planning events, keeps
  selection overrides keyed by `RemoteID`, and reapplies them to each row and
  complete-plan merge. Capture uses the synthetic backend workflow; promotion
  apply sends the reviewed plan through the backend's authoritative rebuild.
- Existing Go coverage proves cache-only list/show, atomic refresh preservation
  on provider failure/cancellation, duplicate occurrence comparison, streamed
  selection preservation, authoritative promotion rebuild, and file backup
  safety.
- 2026-07-30: full `make smoke-freedl` passed against two generated
  lossless/low-quality matched pairs plus a public Hypeddit-linked SoundCloud
  track. It covered preview, ambiguity, write-dir immutability, AAC/MP3/WAV
  output, in-place replacement, forced browser-launch failure, and stuck-ledger
  fields. A stale assertion had expected the old reversed `free <= library`
  display; it was corrected to the current `library <= free` direction without
  weakening any safety assertion. Native manual validation remains open.
- 2026-07-30: the disposable native app opened the cached 96-track playlist
  snapshot without an explicit provider action and loaded the configured Free
  DL job. Live visual inspection found that the original 980-point app minimum
  allowed the nested playlist split view to crush the main sidebar to an icon
  sliver; the minimum is now 1120 points, matching the declared sidebar,
  playlist-list, and detail-column geometry.

## Milestone 6 — Rekordbox Parity

- **Status:** In review
- **Primary area:** `Features/Rekordbox`
- **Risk:** user-library mutation; all write paths remain fail-closed

### 6.1 Managed dependency workflow

- [x] Show Python runtime status and supported version.
- [x] Install/ensure the managed runtime with progress.
- [x] Reset/repair the managed runtime.
- [x] Report bridge/import failures with remediation.
- [x] Support cancellation without leaving a half-active process.

### 6.2 Configuration and inspection

- [x] Read and write Rekordbox standalone config.
- [x] Validate database, backup, mapping, and job paths.
- [x] Inspect the selected database read-only.
- [x] Implement the folder-mapping workflow.
- [x] Keep user-specific absolute paths out of defaults and fixtures.
- [x] Detect TCC/Music automation denial where setup depends on Music.app.

### 6.3 Plan and review

- [x] Generate a complete checksummed plan.
- [x] Show total, matched, and missing counts.
- [x] Show blocker artist, title, and expected path.
- [x] Preserve the full planned mirror when blockers exist.
- [x] Prevent incomplete plans from becoming partial apply requests.
- [x] Support dry-run review.
- [x] Explain checksum drift as integrity failure, not signature failure.

### 6.4 Apply safety

- [x] Verify checksum before applying runtime overrides.
- [x] Check that no process name containing `rekordbox` is running.
- [x] Ensure the app/helper names do not trigger that guard.
- [x] Require a successful backup before any database write.
- [x] Respect effective backup-directory precedence without mutating the plan.
- [x] Abort on missing tracks, drift, bridge failure, or backup failure.
- [x] Return structured apply outcome and recovery information.
- [x] Never use the live database for development apply validation.

### 6.5 Rekordbox validation

- [x] Test managed runtime healthy/install/reset/failure states.
- [x] Test inspection and planning with fixtures.
- [x] Test blocker rendering for one and multiple tracks.
- [x] Test checksum mismatch.
- [x] Test process-open guard.
- [x] Test backup failure before write.
- [x] Test dry run.
- [x] Test successful apply on an isolated database copy.
- [x] Verify backup restoration procedure.

### Milestone 6 exit gate

- [ ] Rekordbox items in `docs/swiftui-parity.md` are complete.
- [x] Automated tests prove incomplete plans and backup failures cannot write.
- [x] Manual validation uses an isolated copy and records its recovery result.
- [x] Existing Rekordbox CLI and TUI tests remain green.

**Evidence / notes:**

- 2026-07-30: native Rekordbox now covers managed-runtime status/ensure/reset,
  canonical configuration and folder mappings, read-only inspection, complete
  plan review, dry-run, and guarded apply. The app preserves the checksummed
  plan as raw `JSONValue`; it does not decode/re-encode the plan before apply,
  avoiding accidental checksum drift from additive optional fields.
- 2026-07-30: implementation found nested protocol structs that still emitted
  exported Go names despite the v1 snake_case contract. Added explicit JSON
  tags for Rekordbox result/path DTOs, Free DL probes, and structured progress,
  plus regression tests. Focused Go suites and the full Swift source type-check
  pass.
- 2026-07-30: process-name matching now has direct coverage for Rekordbox and
  neutral `UDL`/`udl` names; an injected backup failure proves the bridge apply
  is never called.
- 2026-07-30: runtime tests cover absent and healthy managed environments,
  successful ensure/reset dispatch, unhealthy status, and distinct terminal
  failures for install and reset.
- 2026-07-30: the native plan adapter was compiled and executed directly
  against one and multiple blocker rows; missing, ambiguous-path, and
  duplicate-path rows block while a matched keep row does not. The equivalent
  XCTest is checked in for full-Xcode CI.
- 2026-07-30: `docs/rekordbox-recovery.md` records the fail-safe restoration
  procedure and isolated-copy apply/restore acceptance sequence.
- 2026-07-30: the real backend returned a healthy managed pyrekordbox 0.4.4
  runtime and a default config whose empty `folders`/`jobs` slices were `null`.
  The native decoder now accepts that exact wire shape. The accessibility
  inspection service disconnected on this feature view, so isolated-copy
  visual acceptance remains open rather than inferred from the live backend
  response.
- 2026-07-30: added and explicitly ran the opt-in
  `TestRekordboxIsolatedApplyAndRestore` with the managed pyrekordbox 0.4.4
  runtime. It copied the repository sandbox database into `t.TempDir`, ran the
  real process-closed guard, built a checksummed plan, created a complete
  backup, and changed fixture playlist `all` from zero to two tracks through
  the real Python bridge. The test then quarantined the applied directory,
  restored from the exact reported backup path, re-inspected the playlist, and
  proved every restored file matched the pre-apply SHA-256 map byte-for-byte.
  The repository fixture and live Rekordbox library were never opened for
  write.

## Milestone 7 — Config Editor and Onboarding Parity

- **Status:** In review
- **Primary areas:** `Features/ConfigEditor`, `Features/Onboarding`, startup application flow
- **Risk:** destructive config rewrite, invisible credential append

### 7.1 Config editor

- [x] Load a single canonical YAML configuration.
- [x] Edit defaults.
- [x] Create, edit, delete, and reorder sources.
- [x] Edit source URL, target directory, state file, sync policy, and adapter arguments.
- [x] Expose feature config paths where the TUI does.
- [x] Validate before save and map field errors to controls.
- [x] Save atomically using Go's canonical writer.
- [x] Confirm destructive source deletion.
- [x] Recover from external file changes or write conflicts.
- [x] Never silently rewrite configuration merely by opening the editor.

### 7.2 Onboarding

- [x] Route first launch from `startup.onboardingState`.
- [x] Render READY/ATTENTION/BLOCKED state from `startup.attention`.
- [x] Select project directory with home-directory default.
- [x] Guide dependency checks and configuration creation.
- [x] Guide credentials without preloading secrets.
- [x] Validate completion before leaving onboarding.
- [x] Support cancel/resume without corrupting config.

### 7.3 Credential input invariant

- [x] Existing secret presence is represented as metadata only.
- [x] Replacement `SecureField` always starts empty.
- [x] Paste replaces the empty buffer; it never appends to a hidden value.
- [x] Saving an empty field requires an explicit clear action rather than implicit deletion.
- [x] Add a regression test for a 96-character ARL replacement not becoming 192 characters.

### 7.4 Config/onboarding validation

- [x] Test canonical load/write round-trips.
- [x] Test invalid YAML and structured validation errors.
- [x] Test source CRUD and reorder.
- [x] Test atomic-write failure preserving the original file.
- [x] Test external modification conflict behavior.
- [x] Test every onboarding route.
- [x] Test startup attention parity with TUI fixtures.
- [x] Test masked replacement and explicit clear.

### Milestone 7 exit gate

- [ ] Config Editor and Onboarding items in `docs/swiftui-parity.md` are complete.
- [x] Invalid or failed saves preserve the existing file.
- [x] Credential replacement regression coverage passes.
- [x] Existing TUI config/onboarding tests remain green.

**Evidence / notes:**

- 2026-07-30: Config Editor loads only on entry, edits defaults and complete
  source records, supports CRUD/reorder with deletion confirmation, exposes
  feature paths, and writes only after explicit validation/save.
- 2026-07-30: `config.readFile` now returns `content_sha256`; Swift returns it
  as `expected_content_sha256`. An external-change mismatch returns conflict
  and a Go regression test proves the external bytes remain untouched.
- 2026-07-30: onboarding is evaluated before config-dependent feature loads,
  defaults to the home directory, supports project reselection/restart, and
  validates the first source before canonical creation. Replacing an invalid
  config requires a destructive confirmation.
- Existing Go tests cover canonical/atomic writes, invalid YAML, all onboarding
  reducer routes, startup attention, and masked-value separation.
- 2026-07-30: the complete native source set plus an executable regression
  harness compiled in Swift 6 complete-concurrency mode; the 96-character
  replacement check executed successfully. This also exposed and fixed a real
  isolation issue by marking ordered cancellation coordination `@MainActor`.
  The checked-in XCTest remains for full-Xcode CI.
- 2026-07-30: the disposable app loaded the canonical main config from the
  home-directory default and rendered all defaults, six complete source rows,
  edit/delete controls, and feature-config paths without writing. Onboarding
  route execution still requires an isolated first-run context in full-Xcode
  or clean-machine acceptance.

## Milestone 8 — Packaging and Release

- **Status:** In review
- **Primary areas:** `packaging/release`, `.github/workflows`, Xcode project settings
- **Risk:** unclear unidentified-developer install flow, invalid nested signing,
  missing PATH/TCC behavior

### 8.1 Development application bundle

- [x] Define the `.app` bundle identifier and version mapping.
- [x] Add `Info.plist` usage descriptions.
- [x] Add narrowly scoped entitlements:
  - Apple Events automation
  - Downloads access as required by the supported flow
- [x] Keep App Sandbox disabled and document why.
- [x] Embed a locally built `udl` at `Contents/Resources/udl`.
- [x] Verify the app never depends on repository working directory.
- [x] Add a local ad-hoc signing path for development.

### 8.2 Universal backend and release build

- [x] Build `udl` for `darwin/arm64`.
- [x] Build `udl` for `darwin/amd64`.
- [x] Combine binaries with `lipo`.
- [x] Verify both slices with `lipo -info`.
- [x] Preserve build version, commit, and date metadata.
- [x] Extend `packaging/release/build_macos_bundle.sh` for the application bundle.
- [x] Keep artifact paths deterministic.
- [x] Produce SHA-256 checksums.

### 8.3 Free ad-hoc signing

- [x] Ad-hoc sign the embedded `udl` binary first with hardened runtime.
- [x] Ad-hoc sign frameworks/helpers if introduced.
- [x] Ad-hoc sign the outer `.app` last.
- [x] Verify nested signatures with `codesign`.
- [x] Require no Apple Developer ID, certificate, private key, or notary secret.
- [x] State plainly that the artifact is unidentified and not notarized.
- [x] Document checksum verification and macOS's one-time **Open Anyway** flow.

### 8.4 CI and release workflow

- [x] Add a macOS CI job for `xcodebuild build`.
- [x] Add a macOS CI job for `xcodebuild test`.
- [x] Keep the existing Go CI job intact.
- [x] Add Go `vet` and race validation where the release policy requires it.
- [x] Build and test the universal embedded backend.
- [x] Smoke-test `Contents/Resources/udl version`.
- [x] Smoke-test app launch and session handshake.
- [x] Upload the ad-hoc-signed universal ZIP and checksums.
- [x] Decide and document coexistence with existing CLI/Homebrew artifacts.
- [x] Update release documentation and rollback procedure.

### 8.5 Clean-machine acceptance

- [ ] Install on a clean supported Intel Mac or equivalent CI/VM environment.
- [ ] Install on a clean supported Apple Silicon Mac.
- [ ] Confirm the unidentified-developer warning and documented **Open Anyway** recovery.
- [ ] Confirm Doctor reports effective PATH and dependencies accurately.
- [ ] Confirm Homebrew dependencies are found from a Finder launch.
- [ ] Confirm Music Automation prompt, denial explanation, Settings link, and recovery.
- [ ] Confirm Keychain list/save/clear.
- [ ] Confirm project-directory selection and home default.
- [ ] Confirm playlist cache open without network.
- [ ] Confirm a non-destructive sync smoke.
- [ ] Confirm Rekordbox process guard does not mistake the app for Rekordbox.

### Milestone 8 exit gate

- [ ] `xcodebuild build` and `xcodebuild test` pass in CI.
- [ ] Go test, vet, and race suites pass for the release commit.
- [x] Artifact is universal, ad-hoc signed, and checksummed.
- [ ] Clean-machine Intel and Apple Silicon acceptance is recorded.
- [ ] `docs/swiftui-parity.md` has no required unchecked workflow.
- [x] Release and rollback documentation is complete.

**Evidence / notes:**

- 2026-07-30: added explicit plist/automation entitlement, hardened-runtime
  project settings, documented unsandboxed rationale, and an Xcode build phase
  that embeds `udl` at `Contents/Resources/udl`.
- 2026-07-30: `build_universal_backend.sh` produced a real test binary with
  `x86_64 arm64` slices and preserved version/commit/date metadata.
- 2026-07-30: the final working-tree artifact pass rebuilt
  `/tmp/udl-universal-final`; `file`, `lipo -archs`, and `udl version` confirmed
  both architectures and embedded build metadata. Go emitted harmless
  sandbox-only module stat-cache warnings while producing a successful binary.
- 2026-07-30: app packaging was exercised against a synthetic bundle with
  ad-hoc inside-out signing, strict signature verification, embedded-backend
  smoke, ZIP creation, and a verified SHA-256 sidecar.
- 2026-07-30: without `Xcode.app`, the complete Swift source was compiled into
  a disposable real `.app`, paired with the universal backend, signed inside
  out ad hoc with hardened runtime, and verified with
  `codesign --verify --deep --strict`. Finder-style application launch
  completed the protocol handshake and feature preload; graceful quit stopped
  the child backend.
- 2026-07-30 scope decision: distribution intentionally uses only the free
  ad-hoc identity `-`; Developer ID, notarization, stapling, and Apple
  Gatekeeper verification are not product requirements. The release job needs
  no Apple secrets. It signs nested content inside-out, verifies the signature,
  uploads a checksummed universal ZIP alongside unchanged CLI/Homebrew
  artifacts, and uses the end-user install guide as release notes. A recipient
  is told that the app is unidentified and must verify the expected checksum
  before using macOS's one-time **Open Anyway** exception. Clean-machine
  acceptance of that exact flow remains open.
- 2026-07-30: the revised no-secrets packaging path ran successfully against
  the universal backend and disposable real app. The emitted ZIP passed its
  SHA-256 sidecar check; strict nested `codesign` verification passed;
  `codesign -dvv` reported `Signature=adhoc` and
  `TeamIdentifier=not set`; and the embedded backend retained both `x86_64`
  and `arm64` slices.
- 2026-07-30: the implementation-time manual `swiftc` fallback is now a
  supported `make app-dev` target for hosts with Command Line Tools but no full
  Xcode. It rebuilds Go, compiles the current-architecture Swift app in strict
  concurrency mode, materializes a `UDL Dev` plist with
  `com.jaa.udl.dev`, embeds the backend, signs and verifies inside-out ad hoc,
  and writes `dist/dev/UDL-Dev.app`. A real launch reached the connected Doctor
  screen with backend version `dev`, and graceful quit succeeded.
- Final local validation passed with isolated caches:
  `GOCACHE=/tmp/udl-go-cache-test go test ./...`,
  `GOCACHE=/tmp/udl-go-cache-vet go vet ./...`, and
  `GOCACHE=/tmp/udl-go-cache-race go test -race ./...`. Direct Swift source
  type-check, project/plist/scheme validation, shell/YAML syntax checks, and
  `git diff --check` also pass.

## Full Validation Matrix

Run the applicable rows at each milestone and the full matrix before release.

| Area | Command or method | Required |
| --- | --- | --- |
| Go unit/integration | `go test ./...` | Every milestone |
| Go static analysis | `go vet ./...` | Every milestone |
| Go race detection | `go test -race ./...` | Every milestone |
| Main smoke suite | `make smoke-main` | M4 onward |
| Free DL smoke suite | `make smoke-freedl` | M5 onward |
| Swift build | `xcodebuild build …` | M3 onward |
| Swift tests | `xcodebuild test …` | M3 onward |
| Protocol manual | Replay fixture session through `udl agent` | M1 onward |
| Golden contracts | Go ↔ Swift JSON fixtures | M3 onward |
| Parity | `docs/swiftui-parity.md` review | M3 onward |
| Ad-hoc signing | inside-out `codesign --sign -` + `codesign --verify …` | M8 |
| Install warning | checksum verification + documented **Open Anyway** flow | M8 |

## Open Decisions

Record a decision before implementation depends on it:

- [x] Minimum supported macOS version.
- [x] Xcode/Swift version pinned for CI and release.
- [x] Bundle identifier and display name.
- [x] Whether `udl agent` is visible in normal CLI help or documented as an internal command.
- [x] Maximum protocol frame size.
- [x] Whether multiple concurrent long-running runs are supported or rejected in protocol v1.
- [x] App distribution format (`.zip`, `.dmg`, or both) while retaining checksums.
- [x] Long-term relationship between `.app` distribution and the existing Homebrew CLI formula.
- [x] Exact System Settings deep link supported across target macOS versions.

## Decision Log

| Date | Decision | Reason | Consequence |
| --- | --- | --- | --- |
| 2026-07-30 | Use a persistent bidirectional JSON-RPC subprocess | Interactive engine callbacks require a return channel | Implement server-to-client `ui.*` requests |
| 2026-07-30 | Keep the Bubble Tea TUI frozen and supported | It remains the terminal/SSH interface and avoids a risky rewrite | Shared state extraction uses compatibility aliases |
| 2026-07-30 | Embed `udl` in the application bundle | The frontend and backend must ship as one compatible unit | Sign nested binary before the app |
| 2026-07-30 | Keep workflow state and reducers in Go | Prevent behavior drift across two implementations | Swift renders computed snapshots |
| 2026-07-30 | Target full TUI workflow parity | Partial parity would strand product-critical workflows | Milestones cover Sync, Playlists, Free DL, Rekordbox, Config, and Onboarding |
| 2026-07-30 | Do not use App Sandbox | Required subprocess and filesystem integrations are incompatible with the sandboxed model | Hardened runtime, TCC descriptions, and careful entitlements remain required |
| 2026-07-30 | Use `snake_case` for transport DTO fields | Existing `output.Event` already establishes the wire convention | Go and future Swift golden fixtures share explicit stable names |
| 2026-07-30 | Inject credential inspectors and config discovery inputs into reusable startup state | Startup reducers must be testable without hidden CLI globals, real Keychain state, or ambient project files | CLI supplies existing defaults; agent calls can provide explicit session scope |
| 2026-07-30 | Support macOS 14 and later with `com.jaa.udl` / `UDL` | SwiftUI APIs used by the native workflow are stable there and the bundle needs a durable identity | Clean-machine acceptance covers Intel and Apple Silicon on supported releases |
| 2026-07-30 | Keep `udl agent` visible in CLI help | The protocol backend is diagnosable and its role can be documented without pretending it is a user workflow | Help text identifies it as the native frontend backend |
| 2026-07-30 | Cap protocol frames at 8 MiB and permit concurrent server runs | Large plans need a bounded frame; independent cancellable run IDs already provide isolation | Swift feature stores prevent duplicate same-feature starts while the registry remains general |
| 2026-07-30 | Ship the app as a checksummed ZIP alongside CLI/Homebrew | ZIP preserves the signed bundle and avoids displacing established CLI installation | Release publishes both app and architecture-specific CLI artifacts |
| 2026-07-30 | Use the macOS 14 Automation privacy deep link | It provides a direct recovery path for Music Apple Events denial | Error copy also includes the manual Settings path if Apple changes the URL |
| 2026-07-30 | Pin CI and release builds to Xcode 16.4 (16F6) on `macos-15` | The GitHub runner image provides that stable path and changing defaults must not silently change the compiler | Both workflows select `/Applications/Xcode_16.4.app`; its bundled Swift toolchain is the release compiler |
| 2026-07-30 | Keep checksummed Rekordbox plan bodies as Codable `JSONValue` inside typed envelopes | Re-encoding an evolving native model can alter omitted fields and invalidate a valid older plan | Client calls remain typed while Go retains checksum and schema authority |

## Risk Register

| Risk | Mitigation | Status |
| --- | --- | --- |
| Protocol stdout contaminated by child logs | Redirect all non-protocol output to stderr; subprocess purity test | Open |
| Cancellation deadlocks on pending UI reply | Reply canceled first, then cancel run context; scripted contract test | Open |
| Swift duplicates reducer behavior | Export server-computed runstate snapshots; parity review | Open |
| Large plans exceed scanner defaults | Explicit frame limit and oversized fixture tests | Open |
| GUI PATH cannot find Homebrew tools | Resolve login-shell PATH, append standard prefixes, report in Doctor | Open |
| TCC denial appears as opaque AppleScript failure | Detect denial and provide Settings remediation | Open |
| Existing secrets append invisibly in masked fields | Never preload; explicit empty replacement control and regression test | Open |
| Playlist refresh destroys valid cache | Atomic replacement only after success; cancellation tests | Open |
| Rekordbox partial or unbacked write | Keep checksum, completeness, process, and backup gates fail-closed | Open |
| Rekordbox guard matches app/helper name | Prohibit `rekordbox` in shipped process names; packaging test | Open |
| Ad-hoc bundle signature is invalid or mistaken for author identity | Deterministic inside-out signing, CI verification, checksums, and explicit unidentified-app documentation | Open |
| Wire drift between Go and Swift | Golden fixtures generated/decoded by both implementations | Open |

## Work Log

### 2026-07-30

- Created the native macOS frontend high-level plan.
- Created this implementation tracker.
- Archived completed root Free DL/playlist/Rekordbox planning documents.
- Consolidated completed sync and TUI redesign plans under `plans/archive`.
- Captured a green Milestone 0 baseline at commit `8350bbdc757f58175d27f18daac2ed24854300d3`.
- Confirmed the sync reducer extraction seam is frontend-neutral and began Milestone 0.
- Added `internal/runstate` with exported, JSON-tagged state DTOs, reducer behavior, matching fallbacks, failure/detail conversion, and direct tests.
- Preserved the existing TUI through aliases and a thin tracker adapter; the unchanged CLI test suite remains green.
- Added explicit transport JSON tags and golden field-name tests without changing execution-manifest canonicalization or the checksummed Rekordbox plan.
- Extracted onboarding detection, startup attention, and plan-source details into `internal/app` with frontend-neutral DTOs and focused tests.
- Completed the Milestone 0 exit gate: full Go tests, vet, race tests, import-boundary inspection, and diff validation pass.
- Implemented the complete protocol method surface and native SwiftUI slices
  for Doctor, Credentials, Sync, Playlists, Free DL, Rekordbox, Config, and
  Onboarding while retaining Go-owned workflow state.
- Added universal backend/app packaging, free ad-hoc signing automation, app
  CI, release/install documentation, and a checked-in Xcode 16.4 toolchain pin.
- Removed the paid Developer ID/notarization path from release scope and from
  GitHub Actions; public app releases are now checksummed, explicitly
  unidentified universal ZIPs with no Apple secrets.
- Added `make app-dev` / `make app-dev-open` so the native app can be built and
  run with Command Line Tools alone; full Xcode is no longer required for the
  everyday development loop.
- Fixed a discovered Rekordbox planning regression that cleared unrelated
  onboarding/config state, and moved those resets to true session teardown.
- Added stable sync-plan selection/cursor retention, typed visible progress,
  ordered rebuild protocol coverage, transport naming regressions, Rekordbox
  blocker/process/backup tests, and optimistic config-write conflict detection.
- Revalidated `go test ./...`, `go vet ./...`, `go test -race ./...`, direct
  Swift source type-check, plist/project/scheme/script/workflow syntax, and
  `git diff --check`.
- Ran both unchanged product smoke suites successfully, compiled and launched a
  disposable native app with the universal embedded backend, fixed real null
  collection decoding and unexpected-disconnect recovery races, verified
  restart recovery through the live alert, and corrected the minimum-width
  contract found during playlist visual inspection.
- Added and ran an opt-in isolated Rekordbox destructive-path acceptance test;
  the real guard/backup/pyrekordbox/apply stack mutated only a temporary
  fixture copy and restoration from the reported backup was byte-identical.
