# SwiftUI parity

This document tracks native macOS parity against the frozen Bubble Tea
frontend. `PLAN.md` remains the high-level goal; detailed progress and evidence
live in `IMPLEMENTATION.md`.

## Foundation

| Area | Status | Notes |
| --- | --- | --- |
| Xcode project | Implemented, validation pending full Xcode | Checked-in app and unit-test targets, Debug/Release configurations, shared app/test schemes, macOS 14 deployment target |
| Backend process | Implemented | Development binary override, embedded resource lookup, explicit working directory, login-shell PATH plus Homebrew fallbacks, bounded/redacted stderr |
| JSON-RPC transport | Implemented | Actor isolation, 8 MiB NDJSON framing, correlation, out-of-order replies, typed notification/UI streams, disconnect cleanup |
| Typed client | In progress | Centralized 37-method inventory; Doctor, Credentials, and interactive Sync use typed DTOs. Later workflow slices retain forward-compatible `JSONValue` surfaces |
| Recovery | Implemented | Unexpected exit and EOF surface as recoverable state; restart never replays mutating requests |

## Doctor

Status: implemented for the first vertical slice.

- Initializes protocol v1 and verifies the complete method inventory.
- Renders healthy, warning, and blocked checks with detail and remediation.
- Displays effective PATH and resolved dependency locations.
- Keeps backend launch/protocol failure distinct from an unhealthy Doctor
  report.
- Supports refresh and deliberate backend restart.

Manual validation still required from an ad-hoc-signed development app:

- TCC behavior when Music/Rekordbox inspection is involved.
- Finder launch with a minimal inherited environment.

Local ad-hoc application validation completed the embedded-backend handshake
and rendered a real 31-check Doctor report. Deliberately terminating the child
backend produced the no-replay recovery alert, and Restart Backend restored the
connected Doctor screen.

## Credentials

Status: implemented for the first vertical slice.

- Lists presence, health, storage source, and failure metadata only.
- Replacement editors always start empty; existing values are never loaded.
- SoundCloud/Deezer use one secure replacement field; Spotify uses a plain
  client ID plus secure client-secret field.
- Save and cancel clear editable string buffers as far as Swift permits.
- Clear requires destructive confirmation.
- Keychain denial is surfaced as a user-facing failure without backend error
  detail or secret values.

Manual validation still required from an ad-hoc-signed development app:

- Save and clear each credential kind against macOS Keychain.
- Deny and later grant Keychain access.
- Inspect Console/crash diagnostics to confirm no submitted value is present.

## Interactive Sync

Status: implementation complete for the primary UI path; manual parity
validation remains open.

| TUI capability | SwiftUI status | Notes |
| --- | --- | --- |
| Source multi-select | Implemented | Loaded from `sources.capabilities`; per-source choices survive plan rebuilds |
| Plan limit / unlimited | Implemented | Unlimited is encoded as plan limit `0` |
| Dry run / timeout | Implemented | Non-negative values are validated before `sync.start` |
| Per-source order | Implemented | Newest-first and oldest-first are sent by source |
| Per-source plan window | Implemented | First/latest controls appear only for supporting sources |
| Plan rows | Implemented | Go classifications, locked state, and defaults are rendered without reclassification |
| Plan filters | Implemented | all, will sync, missing new, known gap, already have |
| Plan rebuild | Implemented | Swift source state changes before the rebuild response crosses the wire |
| Runtime rows | Implemented | Go `SourceSnapshot` rows and lifecycle are authoritative |
| Runtime filters | Implemented | all, in run, remaining, downloaded, skipped, failed |
| Runtime activity | Implemented | Separate bounded event list; protocol frames are never displayed as logs |
| Structured progress header | Implemented | Typed global and current-track meters render the backend snapshot without recomputation |
| Confirmation / masked input | Implemented | Cancel replies are explicit typed canceled results |
| Cancellation ordering | Implemented | Pending UI reply is sent before `run.cancel` |
| Terminal states | Implemented | success, partial failure, dependency failure, failure, and cancellation remain visible |
| Scripted/manual parity | Partial | The fixture covers selection, rebuild, confirmation, execution, and completion; manual validation needs full Xcode |

## Playlists

Status: native workflow implemented; destructive native validation pending.

- Opening the feature performs cache/config reads only.
- Definitions, missing/invalid snapshots, checksums, refresh time, and cached
  tracks are visible without contacting Music.app.
- Provider discovery and refresh are explicit cancellable actions.
- Refresh terminal handling reloads only after success and reports that the
  previous valid snapshot survived failure or cancellation.
- Definition and whole-config writes use the backend's validated canonical
  writers.
- A local ad-hoc app rendered the existing 96-track snapshot without an
  explicit provider action. The app minimum width now preserves both the main
  navigation sidebar and the nested playlist columns.

## Free DL

Status: primary plan → capture → promote workflow implemented; browser-handoff
and native fixture/manual validation pending.

- Reads/writes the standalone config through the scoped backend methods.
- Streams planning stages and rows, with filters and `RemoteID`-keyed user
  overrides reapplied after every merge.
- Capture uses the synthetic `scdl-freedl` backend path and renders the same
  server-computed source snapshots used by Sync.
- Promotion review shows action, score, destination, and blockers. Apply is
  intentionally routed through the backend's authoritative plan rebuild and
  existing backup/file-safety logic.

## Rekordbox

Status: primary safety workflow and isolated destructive-path recovery
validation complete; full-Xcode native visual validation remains pending.

- Shows managed Python/pyrekordbox health and supports cancellable ensure/reset.
- Reads and writes the standalone canonical config and provides folder-mapping
  setup without embedding user paths in defaults or fixtures.
- Runs read-only inspection and renders counts.
- Displays the complete checksummed plan, including all blocker rows and
  expected paths; a blocker disables both dry-run and apply.
- Keeps the plan as its original `JSONValue` for apply. This avoids
  decode/re-encode changes that could invalidate older plans when optional
  fields are added.
- Labels checksum failures as integrity failures. Actual apply remains governed
  by backend checksum-before-overrides, process-closed, backup-before-write,
  drift, bridge, and post-write verification checks.
- An opt-in acceptance run copied the repository sandbox database to a
  temporary root, exercised the real process guard, backup, pyrekordbox apply,
  and post-write verification, then restored from the reported backup path.
  The restored directory matched the pre-apply copy byte-for-byte; no live
  Rekordbox path was mutated.

## Config Editor and Onboarding

Status: implementation complete; first-run native routing remains pending an
isolated full-Xcode or clean-machine context.

- The main editor reads one scoped file and never writes on open.
- Defaults and complete source records support edit, create, delete, reorder,
  enablement, sync policy, and adapter arguments.
- Save validates in Go, uses the canonical atomic writer, and returns structured
  problems. A read-content SHA prevents an external modification from being
  overwritten.
- First launch is routed from `startup.onboardingState` before
  config-dependent workflows load. Home is the default context; selecting a
  project deliberately restarts the backend in that directory.
- READY/ATTENTION/BLOCKED startup state comes directly from
  `startup.attention`; credential actions open the metadata-only Credentials
  workflow.
- Setup drafts remain memory-only until explicit completion, and replacement of
  an invalid existing config requires confirmation.
- Credential editors still start with empty buffers; a regression asserts a
  96-character ARL remains 96 characters rather than appending a hidden value.
- A local ad-hoc app rendered the canonical defaults, all six source rows, and
  feature-config paths from the home-directory default without writing.

## Validation environment

- CI/release Xcode: 16.4 (16F6), explicitly selected from the GitHub
  `macos-15` runner; the bundled Swift toolchain is the release compiler.
- Swift compiler: Apple Swift 6.3.3.
- Application sources: Swift 6 strict-concurrency compilation passed.
- Local app: disposable universal-backend bundle launched, initialized, was
  signed ad hoc, passed strict nested signature verification, recovered from a
  deliberate backend exit, and shut down cleanly.
- Project file: `plutil -lint` passed.
- Shared schemes: XML validation passed.
- Full `xcodebuild build`, `xcodebuild test`, XCTest compilation, and Thread
  Sanitizer are pending because this machine currently has only Command Line
  Tools selected and no `Xcode.app`; `xcodebuild` reports that a full Xcode
  developer directory is required. Ad-hoc inside-out bundle signing and strict
  signature verification pass; Developer ID and notarization are deliberately
  out of scope.
