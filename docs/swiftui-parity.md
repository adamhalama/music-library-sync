# SwiftUI parity

This document tracks native macOS parity against the frozen Bubble Tea
frontend. The plan and evidence for that work are archived under
`plans/archive/native-macos-frontend-plan.md` and
`plans/archive/native-macos-frontend-implementation.md`.

The deferred visual-design pass over these same surfaces is tracked by the
current root `PLAN.md` and `IMPLEMENTATION.md`; this document must be updated
when the redesign changes a surface described below. That pass is complete: the
sections below carry a **Redesigned surface** block naming the constraint each
screen now makes visible.

## Shell

Every workflow is hosted by one shell rather than a flat list of screens.

- `AppShellView` is a `NavigationSplitView` whose sidebar has a `Workflows`
  section, a per-screen contextual section supplied through `SidebarContext`,
  and a `System` section pinned to the bottom as a second `List` sharing the
  selection binding.
- Sidebar badges, Home and Doctor all read one derived `AppState.attention`.
- The toolbar's leading title is `.navigationTitle` + `.navigationSubtitle`; the
  inspector is `.inspector(isPresented:)` applied per screen with shared
  visibility in `ShellChrome`; the status bar is a `.safeAreaInset(edge:
  .bottom)` carrying the summary and the screen's primary action.
- Settings is a real `Settings` scene (⌘,), not a sidebar destination.
- Appearance follows the system in both light and dark with the system accent;
  the forced-dark `ControlRoomTheme` is gone. Colors, font sizes, radii and
  metrics live only in `macos/UDL/DesignSystem/`.
- **C15** — an unexpected exit or EOF raises a shell recovery banner, resolves
  every in-flight workflow's local state honestly, and records the affected
  destinations in `AppState.notResumed` so each screen reads "not resumed — the
  backend restarted and nothing was replayed" until it is started again.
- Any screen whose content has an unbounded intrinsic height (`Table`, `Form`)
  is hosted in `BoundedContent`. Without it the detail column outgrows the
  window and takes the toolbar, status bar and sidebar with it.

### Cross-cutting rules

Three rules hold on every screen, and each is enforced by a test in
`SourceRuleTests` because none of them can be checked at runtime:

- **A failure appears on the screen that caused it.** Each workflow reports
  through its own `AppState.WorkflowStatus` (message plus severity, with
  `.failure` and `.canceled` distinguished). The blocking modal is reserved for
  session-level failures no screen owns: backend launch, restart, an undecodable
  protocol frame, and a reply that cannot be delivered while the backend is
  blocked waiting for it.
- **A disabled control states its reason on screen.** `.constrained(by:)` is the
  sanctioned way to disable something; where its stacked note cannot fit — a
  toolbar item, a horizontal button row — the reason is on screen another way and
  the exception is recorded with its justification.
- **Every wire-inbound collection tolerates `null`.** Go marshals nil slices and
  maps as `null`; `@DefaultEmpty` decodes absent or null as empty across every
  DTO, so a fresh install — where all of them arrive null at once — is an empty
  screen rather than a decode failure.

## Home

Status: implemented; it is the default destination.

- One hero sentence derived from `sources.capabilities`, `startup.attention` and
  this session's run state, ordered no sources → prompt waiting → run active →
  blocking work → ready.
- The primary action is **Start dry run & plan** (C1), and the run it starts is
  a dry run by default on the Sync screen too.
- Four workflow cards whose pills each name their protocol source, and a "Needs
  attention" group fed by the same `DoctorResult` Check System renders.
- **C16** — library size, tracks on disk and run history are absent rather than
  fabricated, and the inspector says so in words.

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

Redesigned surface:

- Severity count tiles double as filters; the status bar states the active one.
- The list is grouped by severity worst-first, and the category filter lives in
  the sidebar's contextual section.
- The inspector shows status, severity, detail, remediation, the resolved
  dependency path when `doctor.run` reports one, and one deep-linking fix button
  routed through `DoctorFix`, which Home shares so the same check can never open
  two different screens.
- The mockup's Info tile is dropped: `methods_doctor.go` spells `info` as `ok`,
  so the tile could only ever read 0. The inspector states why there are three.

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

Redesigned surface (C12, C13):

- A four-column `Table` — credential, health, used by, stored in — with the
  sidebar's contextual section and a selection-driven inspector carrying the one
  action.
- **C12** — both the screen callout and the editor sheet state that the existing
  value is never loaded or shown; the buffer starts empty and is wiped on exit.
- **C13** — a credential an environment variable overrides is marked in the
  table, the inspector names the exact variable, and the action reads **Move to
  Keychain**, never "Replace". `external_override` is a health, not a storage
  source; testing `storage_source == "external_override"` had meant C13 never
  rendered at all.
- "Used by" has no protocol field. It is derived from `sources.capabilities`
  mirroring the adapter rules `internal/doctor` uses, and the inspector names the
  derivation rather than presenting it as reported data.

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

Redesigned surface (C1–C6, C17):

- `SyncView` is a router over one run: configure, plan (`SyncPlanView`), run
  (`SyncRunView`). The plan surface outranks the run surface, because while a
  `ui.selectRows` request is open the backend is doing nothing at all.
- **C1** — the plan prompt is docked in the workspace, not a `.sheet`.
  `AppState.modalPrompt` filters `.selectRows` out entirely, and a plan prompt
  routes the app to `.sync` as it arrives. `syncDryRun` defaults to `true`, so
  the run the app offers is always the reversible one whether it is started from
  Home or from the Sync screen.
- **C2** — sidebar lifecycle chips (`Queued` · `Planning…` · `Needs you` ·
  `Running` · `Done` · `Failed`), a "Source 2 of 4" header counter, and
  unplanned sources dimmed with the reason stated once per section. A source
  that never reported reads `Not run yet` once the run has ended.
- **C3** — a "Backend paused — waiting for your selection" banner, progress
  meters rendered greyed and frozen, and the same statement in the status bar.
- **C4** — the plan window control states that a change re-plans the source and
  clears the selection, and the primary button becomes **Rebuild plan** as soon
  as the value differs from `params.planWindow`.
- **C5** — window and order controls a source's capabilities do not support stay
  visible, disabled and explained, driven by `sources.capabilities` rather than
  an adapter-name guess.
- **C6** — the plan limit displays `∞` with a footnote that it is sent as
  `plan_limit: 0`.
- **C17** — `askOnExisting`, `scanGaps`, `noPreflight`, `trackStatus` and the
  run-wide `planWindow` are in the configure inspector's Advanced group with
  their real defaults. `askOnExisting` is a three-state policy because
  `ask_on_existing_set` makes "udl decides" a real value.
- The mockup's Artist and Time plan columns and the run screen's Throughput
  panel are dropped: `ui.selectRows` carries neither field and udl reports no
  elapsed time or transfer rate. The inspectors state each omission.
- The plan inspector shows the run's options read-only, because
  `SelectRowsResult` can carry only the selection, the order, the window, cancel
  and rebuild.

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

Redesigned surface (C11):

- Definitions populate the shell's contextual sidebar section; the content
  column is a six-column track `Table` with an All/Missing filter and search.
- The cache-first statement is a pinned callout, and refresh is behind a
  confirmation that states the snapshot on screen survives a failure.
- Every outcome that did not replace the snapshot carries its own severity and
  says the previous snapshot was preserved, so a canceled refresh cannot read
  like a successful one.
- No cached snapshot, an unreadable snapshot file, and a filter matching nothing
  render as three visually distinct states.
- The inspector exposes the definition's `default_freedl_job` and
  `default_rekordbox_target` as navigation handoffs, and states that there is no
  export because `playlists.list` reports no snapshot file path.
- Track durations are parsed from the locale-formatted AppleScript seconds that
  Music.app reports, sharing one parser with the Rekordbox plan.

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

Redesigned surface (C7, C8):

- **C7** — the phase stepper has **four** steps, one per RPC
  (`freedl.plan.start`, `freedl.capture.start`, `freedl.promotionPlan.build`,
  `freedl.promote.apply`), and never auto-advances. An unrun step reads "Not run
  yet" instead of showing an empty table; a step whose prerequisite does not
  exist is rendered, dimmed and told why; completed steps stay re-enterable.
- **C8** — per-cell `PendingValue` placeholders resolve in place while local
  quality and Free DL availability arrive, so a blank cell is never ambiguous
  between "still checking" and "nothing found". A row with no skip reason is
  `Checking`, never `Blocked`: `recomputeSelectable` only writes a reason once
  the source plan is enumerated.
- The inspector is the job form, writing `freedl.yaml` through
  `freedl.config.write`.
- The mockup's Unlimited plan-limit switch is dropped: `internal/freedl/config.go`
  normalises a job's `plan_limit: 0` to the defaults limit, so unlimited does not
  exist here and a toggle would silently mean 50. The Score column appears only
  on step 3, where `freedl.promotionPlan.build` actually reports it. Target
  format offers only what `validTargetFormat` accepts.

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

Redesigned surface (C9, C10):

- The screen is a runtime-health strip, a setup strip (database + inspection), a
  plan bar and the checksummed plan table with blockers inline, plus an
  All/Changes/Blocked filter and search over artist, title and path.
- Plan targets — the config default, folder mappings and playlist jobs — are one
  selection in the sidebar's contextual section, because `job_id` and
  `mapping_id` are mutually exclusive on the wire.
- **C9** — the plan stays an opaque `JSONValue`. The checksum is on the plan bar
  and in the inspector, and drift produces **Regenerate plan**, never a partial
  apply.
- **C10** — one `RekordboxApplyGate` derives the pill, the primary label, the
  dry-run state and the help text together, so the primary always names the
  blocker ("1 track missing — resolve to apply") instead of being an unexplained
  disabled button.
- The two preconditions the protocol cannot report before an attempt — the
  Rekordbox process and database drift — render as **unknown**, never as a tick.
  They turn red only when a real refusal names them; `RekordboxObstacle` is
  constructed from the backend message and nothing predicts it.
- Plan durations are the locale-formatted AppleScript seconds Music.app reports,
  parsed to m:ss through the same `MusicDuration` the Playlists table uses.

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

Redesigned surface (C14):

- Defaults and each source are objects in the shell's contextual sidebar, edited
  through `.formStyle(.grouped)` forms; reorder, duplicate and delete act on the
  draft only.
- The three `*bool` sync-policy fields are tri-state pickers. Unset stays unset
  through a save rather than collapsing to an explicit `false`.
- A SHA mismatch is its own state, not a save error: it names both SHAs, states
  that nothing was written, and offers reload or a confirmed overwrite. The
  overwrite path is a distinct call that deliberately omits the expected SHA.
- Validation runs in the app to block Save and attribute a problem to one source
  row; `config.validate` still runs on every save and remains the authority.
- The YAML tab is the file as read from disk, not a preview of unsaved edits:
  no method canonicalises a config without writing it.
- Credential editors still start with empty buffers; a regression asserts a
  96-character ARL remains 96 characters rather than appending a hidden value.
- A local ad-hoc app rendered the canonical defaults, all six source rows, and
  feature-config paths from the home-directory default without writing.

Redesigned onboarding surface:

- Finish Setup lives in the status bar with every other primary action, and
  states its own blocking reason on screen while it is disabled.
- The first source is drafted through the same `SourceEditorView` the config
  editor uses, so the tri-state policy vocabulary is identical. That editor now
  requires a state file, which `config.Validate` demands for both source types;
  without it the very first Finish Setup was refused by the backend with a
  message no screen rendered.
- A refused write is reported on the onboarding screen itself, listing the
  problems `config.writeFile` returned, rather than only in `configProblems`
  where only the Config screen would have shown it.
- Finishing setup writes the file and restarts the backend against it. That
  restart used to fail with "connection is closed" because `AgentProcess.stop()`
  left the previous generation in place until `Process.terminationHandler` ran;
  `stop()` now tears down synchronously and the handler ignores a generation that
  has already been replaced.

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
