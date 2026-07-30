# Native macOS Frontend over the `udl` CLI Backend

- **Status:** Planned
- **Target:** Native SwiftUI application with full Bubble Tea TUI workflow parity
- **Last updated:** 2026-07-30
- **Implementation tracker:** [IMPLEMENTATION.md](./IMPLEMENTATION.md)
- **Parity reference:** [docs/tui.md](./docs/tui.md)

## Purpose

Build a native macOS SwiftUI frontend for `udl` without duplicating its product logic in Swift. The application will render state and collect user input while the existing Go application remains responsible for configuration, planning, sync execution, retries, credentials, playlist snapshots, Free DL workflows, Rekordbox operations, validation, and diagnostics.

The current Bubble Tea TUI remains supported and frozen as the terminal, headless, and SSH interface. This is an additive frontend, not a big-bang replacement.

Visual design, theming, animation, and icon work are intentionally deferred. This plan establishes the process boundary, protocol, reusable state, workflow parity, validation, and release packaging needed before a separate visual-design pass.

## Current Repository Context

At the start of this work:

- `udl` is a Go CLI whose product surface is concentrated in the Bubble Tea implementation under `internal/cli/tui_*.go`.
- The TUI and its supporting CLI files total roughly 20,000 lines; `internal/cli/tui_test.go` alone is 3,923 lines and is the strongest executable specification of TUI state transitions.
- Interactive sync, credentials, onboarding, the config editor, SoundCloud Free DL, Apple Music snapshots, and Rekordbox mirror workflows are not all available through equivalent one-shot CLI commands.
- `app.Interaction` is bidirectional: the engine can block mid-run on `Confirm`, `Input`, or `SelectRows`.
- `internal/rekordbox/bridge` already provides a useful JSON-over-stdio testing precedent.
- The current macOS release script packages a bare per-architecture `udl` binary; it does not yet build, sign, notarize, or staple an application bundle.
- There is currently no `internal/agent`, `internal/runstate`, or `macos` application directory.

Completed plans that previously occupied the repository root are retained as design history under [plans/archive](./plans/archive/).

## Decisions Already Made

These are project constraints, not open design questions:

1. **Transport:** a persistent, bidirectional JSON-RPC 2.0 subprocess using newline-delimited JSON over stdin/stdout.
2. **Backend process:** the app launches `Contents/Resources/udl agent`.
3. **Protocol streams:** stdin/stdout carry protocol frames only; stderr carries human-readable logs and child-process noise.
4. **Frontend role:** SwiftUI is a thin renderer and interaction surface. Reducers and business rules stay in Go.
5. **TUI lifecycle:** the Bubble Tea TUI remains available and behaviorally frozen, except for mechanical aliases needed by state extraction.
6. **Packaging:** `udl` is embedded in the `.app` bundle and signed before the outer application.
7. **Parity scope:** every workflow documented in `docs/tui.md` is targeted.
8. **Platform scope:** macOS only.

## Why the Backend Must Be Persistent

A one-shot `udl --json` process cannot provide parity. During a sync, Go may need to ask the frontend to:

- confirm an existing-track gap scan;
- complete Spotify browser authentication;
- enter a Deezer ARL;
- select and reorder plan rows;
- rebuild a source plan after its plan window changes; or
- cancel a prompt and unwind the active run.

The worker goroutine blocks until those interactions receive a reply. A long-lived JSON-RPC connection provides the required return path while preserving the existing `app.Interaction` contract.

## Target Architecture

```text
UDL.app
  SwiftUI views ← @Observable feature and application stores
          ↕
  UDLClient typed async facade
          ↕
  JSONRPCConnection actor
          ↕
  Process: Contents/Resources/udl agent
      stdin/stdout: NDJSON JSON-RPC 2.0 only
      stderr: logs and adapter output
          ↕
internal/agent
  connection framing and request correlation
  run registry and cancellation
  app.Interaction implementation
  thin RPC method groups
          ↕
internal/app and existing domain packages
  engine · config · auth · doctor · freedl · playlists · rekordbox/*
```

`internal/agent` must not import `internal/cli`. The Cobra `agent` command is a small composition shim that injects build information, IO, working directory, and dependencies into the agent package.

## Architectural Invariants

- Go remains the sole owner of planning, state reconciliation, matching, validation, safety gates, and workflow reducers.
- Swift wire models mirror stable Go DTOs; they do not become a second domain model.
- Protocol stdout must never contain logs, progress bars, prompts, or raw child-process output.
- Every long-running operation has a `runId`, terminal `run.finished` notification, and idempotent cancellation path.
- A pending server-to-client UI request is answered as canceled before its run context is canceled.
- Unknown JSON fields are tolerated by both sides to permit additive protocol evolution.
- Breaking wire changes require a protocol version or negotiated capability change.
- The TUI and CLI behavior remain unchanged while shared state is extracted.

## Milestone Sequence

Each milestone is independently testable and must satisfy its exit gate before dependent work begins.

### Milestone 0 — Extract Headless State from `internal/cli`

Create `internal/runstate` and move the pure sync reducer/state machinery out of the Bubble Tea package:

- the current sync run tracker and source state;
- plan rows, track row state, activity entries, failure state, runtime status, plan classes, run scopes, source lifecycle, and status filters;
- event detail conversion helpers;
- failure derivation;
- display-row and status-label helpers; and
- the track matching sequence: track ID, exact name, normalized name, then index-to-execution-slot.

Rename exported types to remove the `tui` prefix. Preserve frozen TUI call sites with aliases and function variables in `internal/cli`.

Also move reusable startup and planning state into `internal/app`:

- `DetectOnboardingState`;
- `DetectStartupAttention`; and
- `PlanSourceDetails`.

Add explicit JSON tags to the transport-facing engine, progress, and run-state DTOs, including plan rows, sync results, execution manifests, execution entries, SoundCloud preflight results, and track events.

**Safety constraint:** do not add or reorder fields on `playlistsync.Plan`. `VerifyPlanChecksum` re-marshals the parsed value, so an always-emitted field can invalidate plans written by earlier builds with an opaque checksum mismatch.

**Exit gate:** `go test ./...`, `go vet ./...`, and `go test -race ./...` pass with no behavior change. The extraction itself must not require changing existing behavioral assertions.

### Milestone 1 — Build the JSON-RPC Agent Transport

Add `internal/agent` and register `udl agent` in the root command.

The transport includes:

- newline framing with a scanner buffer sized for large plan payloads;
- a mutex-protected writer so frames cannot interleave;
- request/response ID correlation in both directions;
- a concurrency-safe run registry mapping `runId` to context and cancel function;
- JSON-RPC errors with stable machine-readable codes and useful messages;
- graceful EOF, protocol-error, client-disconnect, and shutdown handling; and
- an `app.Interaction` implementation that turns Go callbacks into `ui.*` requests.

Server-to-client interaction methods:

| Method | Parameters | Result |
| --- | --- | --- |
| `ui.confirm` | `runId`, `sourceId`, `prompt`, `defaultYes` | `confirmed`, `canceled` |
| `ui.input` | `runId`, `sourceId`, `prompt`, `mask` | `value`, `canceled` |
| `ui.selectRows` | `runId`, `sourceId`, `rows`, source details, download order, plan window | selected indices, download order, `canceled`, `rebuild`, plan window |

The same run and source may receive multiple `ui.selectRows` requests. When the client requests a rebuild, it must update both the engine reply and its own copied per-source plan window before rendering the replacement rows.

Notifications include:

- `sync.event`, carrying the original `output.Event`, a computed run-state source snapshot, and structured progress;
- `freedl.planEvent`;
- `run.finished`, carrying `runId`, result, error, and exit code; and
- `log`.

Cancellation follows the TUI ordering exactly: answer any pending `ui.*` request as canceled, then cancel the run context. Reversing that order can leave the worker blocked on its reply channel.

**Exit gate:** transport contract tests cover framing, correlation, concurrent writes, interaction round-trips, rebuild, cancellation, EOF, malformed frames, and stdout purity.

### Milestone 2 — Expose the Complete RPC Method Surface

Implement thin method groups in `internal/agent/methods_<feature>.go`. Methods call existing use cases and services rather than reimplementing CLI commands.

| Group | Methods |
| --- | --- |
| Session | `session.initialize`, `session.shutdown` |
| Config | `config.load`, `config.validate`, `config.readFile`, `config.writeFile` |
| Doctor | `doctor.run` |
| Credentials | `credentials.list`, `credentials.save`, `credentials.clear` |
| Startup | `startup.onboardingState`, `startup.attention` |
| Sources | `sources.capabilities` |
| Sync | `sync.start`, `sync.cancel` |
| Playlists | `playlists.list`, `playlists.providerList`, `playlists.show`, `playlists.refresh`, `playlists.saveDefinition`, `playlists.config.read`, `playlists.config.write` |
| Free DL | `freedl.config.read`, `freedl.config.write`, `freedl.plan.start`, `freedl.capture.start`, `freedl.promotionPlan.build`, `freedl.promote.apply` |
| Rekordbox | `rekordbox.config.read`, `rekordbox.config.write`, `rekordbox.deps.status`, `rekordbox.deps.ensure`, `rekordbox.deps.reset`, `rekordbox.inspect`, `rekordbox.plan`, `rekordbox.apply` |
| Runs | `run.cancel` |

Free DL capture must preserve the existing design: build a synthetic in-memory single-source `config.Config`, then run `app.SyncUseCase` with the `scdl-freedl` adapter into the buffer directory.

The README is part of the command contract. Its command-surface section must include `agent`, because existing CLI documentation tests inspect `readme.md`.

**Exit gate:** each method group has request/response contract tests, authorization is limited to the supplied project/config scope, and the complete method inventory is returned by `session.initialize`.

### Milestone 3 — Build the Swift Transport and First Vertical Slice

Add a checked-in Xcode project under `macos/` with:

```text
macos/UDL/
  UDLApp.swift
  Backend/
    AgentProcess.swift
    JSONRPCConnection.swift
    UDLClient.swift
    Wire/
  Model/
    AppState.swift
  Features/
    Home/
    Doctor/
    Credentials/
    Sync/
    Playlists/
    FreeDL/
    Rekordbox/
    ConfigEditor/
    Onboarding/
```

`JSONRPCConnection` is an actor. Outbound calls use checked throwing continuations keyed by request ID. Inbound `ui.*` requests become an `AsyncStream<UIRequest>`; the run coordinator publishes a pending prompt to an observable store, awaits the user response, and sends the matching JSON-RPC reply.

Model `output.Event.Details` with a typed Swift structure containing optional fields for known keys. Unknown fields must decode harmlessly.

The first end-to-end product slice is **Doctor + Credentials**. It proves app launch, process spawn, handshake, request/response correlation, wire decoding, Keychain access, error presentation, and relevant TCC behavior before long-running workflows are introduced.

**Exit gate:** Doctor and credential list/save/clear work from the app, Swift transport tests pass, agent crashes produce a recoverable app state, and no secrets appear in logs or editable preloaded fields.

### Milestone 4 — Reach Interactive Sync Parity

Implement the largest workflow against the shared Go run state:

- source multi-select;
- plan limit, including unlimited;
- dry run;
- timeout override;
- per-source download order;
- per-source plan window and rebuild;
- pre-run filters: all, will sync, missing new, known gap, already have;
- runtime filters: all, in run, remaining, downloaded, skipped, failed;
- live per-track and per-source progress;
- confirm and masked input prompts;
- cancellation during prompts and execution; and
- final result and failure details.

Swift renders server-computed rows and snapshots. It must not recreate the track matching, status transition, aggregate count, or failure-reduction algorithms.

**Exit gate:** scripted full-sync fixtures and manual app runs exercise selection, rebuild, auth/input, existing-track confirmation, progress, partial failure, and cancellation with results matching the TUI.

### Milestone 5 — Reach Playlists and Free DL Parity

Playlist behavior remains cache-first:

- opening the workflow reads only config and saved snapshots;
- Apple Music is accessed only after explicit refresh;
- failed or canceled refresh preserves the previous valid snapshot; and
- refreshed snapshot replacement remains atomic.

Free DL parity includes capture, streaming plan events, filters, selection, browser handoff, promotion planning, and apply. Selection overrides are keyed by `RemoteID` so plan re-merges preserve user intent.

**Exit gate:** cached playlists work offline, refresh preservation is covered by tests, Free DL re-merge keeps overrides, and existing `make smoke-freedl` scenarios still pass.

### Milestone 6 — Reach Rekordbox Parity

Expose:

- managed Python runtime status, install, repair, and reset;
- Rekordbox inspection;
- folder-mapping setup;
- checksummed plan generation and review;
- apply-blocker details;
- dry-run; and
- backup-before-apply.

The workflow remains fail-closed. Incomplete plans never write a partial mirror, checksum verification occurs before runtime overrides, Rekordbox must be closed, and destructive application is preceded by a successful backup.

**Exit gate:** fixture and isolated-copy tests cover dependencies, planning, blockers, checksum drift, process guard, backup failure, dry run, and successful apply. Development validation never writes to the live Rekordbox database.

### Milestone 7 — Reach Config Editor and Onboarding Parity

Implement:

- canonical single-file YAML editing;
- defaults editing;
- source create, edit, delete, and reorder;
- adapter arguments;
- feature config paths;
- validation before save;
- atomic writes; and
- the first-run onboarding and startup-attention routes.

Masked credential fields start empty with an explicit “replace” action. Existing secret values must never be loaded into `SecureField`; otherwise a pasted replacement can append to the hidden current secret.

**Exit gate:** canonical round-trips and invalid-config recovery are tested, onboarding routes match `DetectOnboardingState`, and startup badges match `DetectStartupAttention`.

### Milestone 8 — Package and Release

Extend `packaging/release/build_macos_bundle.sh` and the release workflow to:

- build `udl` for arm64 and x86_64;
- combine it into a universal binary with `lipo`;
- embed it at `Contents/Resources/udl`;
- ad-hoc sign the embedded binary first and the application second using the
  free `-` pseudo-identity and hardened runtime;
- produce checksums and smoke-test the packaged artifact; and
- add macOS CI for `xcodebuild build` and `xcodebuild test`.

The app will not use App Sandbox. It launches `scdl`, `yt-dlp`, Python, `ffmpeg`, `osascript`, and `/usr/bin/security`, and it accesses user-selected music, Downloads, and Rekordbox data. The release must include narrowly justified entitlements and usage descriptions, including Apple Events automation and Downloads access.

The public ZIP is intentionally unverified by Apple: it has no Developer ID
signature or notarization ticket. Documentation must say this plainly and
teach recipients to verify the published checksum and use macOS's one-time
**Privacy & Security → Open Anyway** exception only for the expected download.

**Exit gate:** a clean macOS machine can verify the checksum, approve the
unidentified app once, locate supported dependencies, complete Doctor, and
execute smoke workflows.

## macOS Environment Requirements

### PATH

Finder-launched applications inherit a minimal PATH and commonly omit `/opt/homebrew/bin`. Swift resolves the login-shell PATH once and passes it to `udl agent`; the agent defensively appends `/opt/homebrew/bin` and `/usr/local/bin`. `doctor.run` reports the effective PATH and resolved dependency locations.

### TCC and Music Automation

The `.app` receives its own Automation permission for Music.app. Detect Apple Events denial, explain which workflow needs it, and provide a System Settings deep link instead of surfacing only the raw AppleScript error.

### Working Directory

GUI applications do not start in a useful project directory. `udl agent` accepts an explicit `--working-dir`. The app exposes a project-directory setting and defaults it to the user home directory so user-level configuration applies without accidentally loading repository-local `.env` or YAML files.

### Keychain

The current implementation shells out to `/usr/bin/security`. Validate list/save/clear behavior from the signed app, redact values everywhere, and treat any changed ACL behavior as a release blocker.

### Rekordbox Process Detection

The guard matches process names containing `rekordbox`. Neither the app nor helper binaries may include that substring, or apply will incorrectly report that Rekordbox is running.

## Verification Strategy

Every phase records exact commands and evidence in `IMPLEMENTATION.md`.

- **Go baseline:** `go test ./...`, `go vet ./...`, `go test -race ./...`.
- **Agent contracts:** in-memory pipe tests for every method group and complete interaction sequences.
- **Golden wire data:** Go-generated JSON decoded by Swift, with Swift request fixtures decoded by Go.
- **Manual protocol smoke:** replay an NDJSON session through `udl agent --working-dir <fixture-dir>` and compare frames.
- **Swift:** `xcodebuild build` and `xcodebuild test`, including correlation, out-of-order responses, cancellation, restart, and Codable round-trips.
- **Existing product regression:** `make smoke-main` and `make smoke-freedl` remain unchanged and green.
- **Parity:** maintain `docs/swiftui-parity.md` from `docs/tui.md`, checked workflow by workflow.
- **Release:** verify inside-out ad-hoc signing, hardened runtime, checksum
  generation, documented unidentified-developer approval, dependency lookup,
  TCC denial/recovery, and clean-machine launch.

## Safety and Compatibility Rules

- Never expose credentials in protocol logs, crash reports, fixtures, screenshots, or Swift state descriptions.
- Never preload an existing secret into an editable field.
- Never mutate a stored Rekordbox plan before verifying its checksum.
- Never apply an incomplete Rekordbox plan or skip its backup gate.
- Never refresh Apple Music implicitly when opening a cached playlist.
- Never discard the last valid playlist snapshot after failed or canceled refresh.
- Never let adapter output reach protocol stdout.
- Never silently rewrite an existing user configuration during migration.
- Keep wire changes additive until a deliberate protocol-version change is approved.

## Out of Scope

- Visual design, theming, animation, and final iconography.
- Removing or substantially redesigning the Bubble Tea TUI.
- Windows or Linux graphical frontends.
- Rewriting Go workflow logic in Swift.
- `udl rekordbox order plan|apply|show`; it is specified separately and is not part of current TUI parity.

## Program Completion Criteria

The initiative is complete when:

- the signed SwiftUI app provides every workflow in `docs/tui.md`;
- Swift contains presentation and interaction coordination, but no duplicated workflow reducer logic;
- interactive prompts, plan rebuilds, progress, and cancellation work over the persistent protocol;
- playlist, credential, and Rekordbox safety invariants are preserved;
- the frozen TUI and existing CLI smoke tests still pass;
- Go, Swift, protocol, parity, and release validation are green;
- the universal application is ad-hoc signed, checksummed, and manually
  approved on a clean supported macOS installation; and
- operational and developer documentation describes the protocol, debugging, packaging, and recovery paths.
