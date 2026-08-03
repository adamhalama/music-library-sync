# Native macOS GUI Redesign — Follow the Backend, Show the Seams

- **Status:** Complete — phases 1–9 delivered and verified
- **Target:** `macos/UDL` rebuilt to the Option 1 "Inspector" design, with every backend constraint made visually explicit
- **Last updated:** 2026-08-03
- **Implementation tracker:** [IMPLEMENTATION.md](./IMPLEMENTATION.md)
- **Design reference:** [docs/gui-redesign/app/](./docs/gui-redesign/app/) (`shell.css` is the design system source of truth)
- **Parity reference:** [docs/swiftui-parity.md](./docs/swiftui-parity.md)
- **Predecessor:** [plans/archive/native-macos-frontend-plan.md](./plans/archive/native-macos-frontend-plan.md)

## Purpose

The previous initiative delivered a working SwiftUI frontend over the `udl`
agent protocol and deliberately deferred all visual design. This plan is that
deferred pass, and it is both visual and interaction-level.

Today `macos/UDL` is a forced-dark "control room": a hand-rolled orange palette,
one flat list of eight destinations, and feature screens that are `ScrollView`s
of stacked `GroupBox`es. The approved mockups in `docs/gui-redesign/app/` are a
standard three-column macOS application — sidebar · content · inspector — with a
toolbar, a bottom status bar, real `Table`s, and an outcome-first Home.

The mockups were drawn without regard for what the agent protocol can actually
do, so parts of them are not buildable as drawn.

## Governing Principle

**The backend does not change. The frontend follows it, and the seams are
visible.**

Where a mockup and the protocol disagree, the protocol wins. The resulting
constraint then gets a deliberate visual treatment rather than being hidden
behind a silently disabled control, an unexplained empty state, or a fabricated
number. A user should be able to tell from the screen alone why something is
unavailable and what the application is waiting on.

Concretely, this rules out three failure modes that the current app and the
mockups each exhibit:

1. **Silent disabling.** A `.disabled(true)` with no stated reason is a defect.
2. **Ambiguous emptiness.** "Not run yet", "still checking", and "found nothing"
   must be visually distinct.
3. **Fabricated data.** Panels the protocol cannot fill are removed, not faked.

## Decisions Already Made

These are project constraints, not open design questions:

1. **No backend changes.** No file under `internal/` is touched. A Go diff means
   the plan was violated.
2. **System appearance.** Follow system light *and* dark with the system accent
   color. `.preferredColorScheme(.dark)` and `ControlRoomTheme` are removed.
3. **Home shows only backend-backed data.** The mockup's library size, tracks on
   disk, and run history have no protocol source and are dropped.
4. **Phased delivery.** Seven reviewable phases, each leaving the app runnable.
5. **Design system first.** No view outside `DesignSystem/` hardcodes a color or
   a font size.
6. **The Bubble Tea TUI stays frozen.** This is a native-app-only change.

## How the Application Runs Today

`UDLApp` owns one `AppState` (`@MainActor`, `ObservableObject`, 46 `@Published`
properties). `AppState.start()` launches `AgentProcess`, which spawns the
embedded `udl agent` and speaks NDJSON JSON-RPC over stdio through
`JSONRPCConnection`. `UDLClient` wraps all 37 methods in
`internal/agent/server.go: protocolMethods`.
`AppState.observe(connection:)` fans notifications into per-feature state and
routes inbound `ui.*` requests into `pendingPrompt`.

`DashboardView` is a two-column `NavigationSplitView`: a flat
`List(AppState.Destination.allCases, selection:)` with no grouping or badges,
and a `switch` on `appState.destination` for the detail column, defaulting to
`.doctor`.

### The sync run, which shapes everything

There is no plan-preview method. The flow is:

1. `SyncView`'s form calls `AppState.startSync()` → `sync.start` with `plan: true`.
2. Go plans **one source at a time**, sending a `ui.selectRows` request per source.
3. That lands in `appState.pendingPrompt` and is presented by `UDLApp` as a
   `.sheet` → `PendingPromptView` → `PlanSelectionPromptView`, a 900×610 modal.
4. **Go is blocked** on that request until the app replies. `rebuild: true`
   makes Go discard the selection, re-plan that source, and ask again.
5. Execution then streams `SourceSnapshot` rows, `StructuredProgressSnapshot`,
   and `OutputEvent`s into `syncRun`.

So `docs/gui-redesign/app/sync-plan.html` — a pre-run screen showing every
source's plan in the sidebar at once — is not reachable. The plan lives *inside*
a run, one source at a time, with the backend blocked while you look at it. The
redesign keeps that reality and makes it legible, rather than adding a
`sync.plan` RPC to make the mockup literally true.

## Constraint Catalogue

The core of this plan. Each row is a real protocol constraint and the agreed way
to make it legible. `IMPLEMENTATION.md` tracks these by ID.

| # | Backend constraint | Where | Visual treatment |
| --- | --- | --- | --- |
| C1 | No plan-preview method; plan rows exist only inside a run | `sync.start(plan: true)` | Home's primary action is **"Start dry run & plan"**, not "Review sync plan". The Sync header states that planning happens inside a run; dry run is the default so the action stays reversible. |
| C2 | Sources are planned **sequentially**, one `ui.selectRows` at a time | `internal/agent/methods_sync.go` | Sidebar source rows carry an explicit lifecycle chip: `Queued` · `Planning…` · **`Needs you`** · `Running` · `Done` · `Failed`. The header shows **"Source 2 of 4"**. Unplanned sources are dimmed and non-selectable with the reason inline: *"udl plans one source at a time."* |
| C3 | Go is **blocked** while any `ui.*` request is pending | `JSONRPCConnection` | A persistent accent banner above the plan table: *"Backend paused — waiting for your selection."* Progress meters render greyed and frozen rather than animating, so a stalled run is never mistaken for a working one. The status bar repeats the state. |
| C4 | Changing the plan window forces a **rebuild** that discards the selection | `SelectRowsResult.rebuild` | The window control states *"changes here re-plan this source and clear your selection"*; the primary button becomes **"Rebuild plan"** as soon as the value differs from `params.planWindow`. |
| C5 | Per-source capability flags | `SourceCapability.supportsPlan` / `.supportsPlanWindow` / `.supportsDownloadOrder` | Unsupported controls are **rendered, disabled, and explained** — never hidden. For example *"scdl has no first/latest window."* This is the `ConstraintNote` primitive. |
| C6 | Unlimited is encoded as plan limit `0` | `AppState.startSync()` | The field displays `∞` with a footnote stating it is sent as `plan_limit: 0`. |
| C7 | Free DL is **four separate RPCs with four run IDs** | `freedl.plan.start`, `freedl.capture.start`, `freedl.promotionPlan.build`, `freedl.promote.apply` | The phase stepper never auto-advances, and it has **four** steps, one per RPC — the mockup's three would have hidden the seam. Each step is an explicit action; completed phases stay visible and re-enterable. A phase you have not run reads `Not run yet` instead of showing an empty table, and a phase whose prerequisite does not exist is dimmed with the reason. |
| C8 | Free DL rows stream progressively; quality and availability arrive late | `freedl.plan.event` | Per-cell `ProgressView` placeholders that resolve in place, so a blank cell is never ambiguous between "still checking" and "nothing found". |
| C9 | The Rekordbox plan is an **opaque checksummed `JSONValue`** that must be re-sent verbatim | `rekordbox.plan` → `rekordbox.apply` | The checksum is shown in the inspector. Drift produces **"Regenerate plan"**, never a partial apply. The app must not decode and re-encode the plan (see the `omitempty` note in `AGENTS.md`). |
| C10 | Apply is fail-closed on blockers | `rekordbox.apply` | The primary button **names the blocker** — *"Rekordbox is open — quit it to apply"* — instead of being an unexplained disabled button. One `RekordboxApplyGate` derives the pill, the label, the dry-run state and the help text together. The two preconditions the protocol cannot report before an attempt (the Rekordbox process, database drift) render as **unknown**, never as a tick, and turn red only when a real backend refusal names them. |
| C11 | Playlists are cache-first; refresh is explicit; failure preserves the previous snapshot | `playlists.show`, `playlists.refresh` | A persistent label: *"Cached snapshot — opening never contacts Music.app."* Refresh has its own progress state, and on failure or cancellation the message explicitly says the previous snapshot survived. |
| C12 | Credentials are **metadata-only** and editors must start empty | `credentials.list` | The editor sheet states *"The existing value is never loaded or shown."* This guards the ARL double-paste bug recorded in `AGENTS.md`. |
| C13 | An environment variable can override Keychain | `CredentialStatus.storageSource` | The row is marked and its action becomes **"Move to Keychain"**, so the UI never implies the Keychain value is the one actually running. |
| C14 | Config writes are guarded by a read-content SHA | `config.writeFile` | A distinct "changed on disk since you opened it" conflict state offering reload or overwrite, not a generic save error. |
| C15 | The backend can exit or EOF at any time, and restart never replays mutating requests | `AgentProcess` | A shell-level recovery banner; after restart, in-flight workflow screens read *"not resumed — re-run to continue."* |
| C16 | The protocol reports no library statistics and no run history | — | Those mockup panels are **omitted**. Home's inspector shows only `sources.capabilities` counts, `session.initialize` paths and build, and doctor severity counts. The same rule removed the plan table's Artist and Time columns (`ui.selectRows` has neither field) and the run screen's Throughput panel; in each case the inspector states what udl does not report. |
| C17 | `askOnExisting`, `scanGaps`, `noPreflight`, `trackStatus`, and the top-level `planWindow` are supported by `SyncStartParams` but **hardcoded** in `AppState.startSync()` | `macos/UDL/Features/Sync/SyncModels.swift` | Surfaced in the Sync inspector under "Advanced" with their real defaults, so the GUI stops silently deciding on the user's behalf. |

## Target Shape

```text
┌ toolbar ─ title + monospace subtitle · segmented filter · search · ◧ ─┐
│ ┌ sidebar 216pt ─┬─ content ────────────────┬─ inspector 266pt ─┐    │
│ │ Workflows      │  Table / Form / Report   │  options          │    │
│ │ ─ contextual ─ │                          │  paths            │    │
│ │ (sources/jobs) │                          │  constraints      │    │
│ │                │                          │                   │    │
│ │ System (bottom)│                          │                   │    │
│ │ backend pill   │                          │                   │    │
│ └────────────────┴──────────────────────────┴───────────────────┘    │
└ status bar 46pt ─ summary totals · secondary · primary action ───────┘
```

- **Sidebar** — `Section("Workflows")`, an optional contextual section supplied
  by the active screen through `SidebarContext` (the `contextual` slot in
  `shell.js`), and `Section("System")` pinned to the bottom. In practice the
  pinned section is a second `List` sharing the selection binding, because
  SwiftUI cannot pin a trailing section inside one `List`.
  Badges come from one derived `AppState.attention` value read by the sidebar,
  Home, and Doctor alike.
- **Inspector** — `.inspector(isPresented:)` rather than a third split column,
  because it is per-screen and toggleable. Each workspace applies it itself; the
  visibility lives in one `ShellChrome` object so the toolbar button and ⌥⌘I
  agree across screens.
- **Status bar** — `.safeAreaInset(edge: .bottom)`.
- **Settings** — moves out of the sidebar into a real `Settings` scene (⌘,).
- **Destination** — gains `.home` and defaults to it instead of `.doctor`.

## Phases

| Phase | Content |
| --- | --- |
| 1 | Design system, including `ConstraintNote`; remove forced dark and `ControlRoomTheme` — **done** |
| 2 | Shell: grouped sidebar, badges, contextual slot, toolbar, status bar, inspector, Settings scene, commands, `AppState.attention` — **done** |
| 3 | Home — **done** |
| 4 | Sync plan docked out of the modal sheet; Sync run split out (C1–C4, C17) — **done** |
| 5 | Doctor and Credentials (C12, C13) — **done** |
| 6 | Free DL (C7, C8) and Rekordbox (C9, C10) — **done** |
| 7 | Playlists (C11), Config (C14), Onboarding polish — **done** |
| 8 | Documentation and close-out; carried verification of C4, the Sync light captures and Onboarding — **done** |
| 9 | Sweep the four defect classes phase 8 exposed, each with a guard — **done** |

Each phase is one pull request and leaves the application launchable.

Phase 8 was planned as documentation only. Verifying the two items phases 4 and
7 had carried turned it into a fixing phase too: dry run was not the default the
Sync screen claimed, onboarding's Finish Setup could not succeed and reported
nothing when it failed, and a fresh install raised a modal decode error on
launch. `IMPLEMENTATION.md` records each one.

## Invariants This Redesign Must Not Break

These are inherited from the completed frontend work and remain non-negotiable:

- Never preload an existing secret into an editable field.
- Never mutate a stored Rekordbox plan before verifying its checksum, and never
  decode and re-encode it.
- Never apply an incomplete Rekordbox plan or skip its backup gate.
- Never refresh Apple Music implicitly when opening a cached playlist, and never
  discard the last valid snapshot after a failed or canceled refresh.
- `answerPlanSelection` must set the source's plan window **before** the rebuild
  reply crosses the wire.
- `performOrderedCancellation` must send the pending UI reply **before**
  `run.cancel`.
- `initialPlanSelection` and `rememberPlanSelection` overrides continue to drive
  defaults across plan rebuilds.
- A restart never replays mutating requests.

## Out of Scope

- Any change under `internal/`, including a `sync.plan` preview method.
- Changes to the Bubble Tea TUI, which stays frozen.
- Windows or Linux graphical frontends.
- Developer ID signing or notarization; ad-hoc signing remains deliberate.
- Final iconography and app-icon artwork.

## Completion Criteria

All met as of 2026-08-02. The redesign is complete when:

- every screen matches the Option 1 mockups in structure, spacing, and control
  vocabulary, and renders correctly in both light and dark appearance;
- every constraint C1–C17 has its stated visual treatment, verified manually;
- no disabled control exists without an adjacent stated reason;
- no on-screen value lacks a protocol source;
- `git diff` shows no change under `internal/` and `go test ./...` passes
  unchanged;
- the protocol replay fixture still passes, including a new case covering the
  docked plan prompt; and
- `docs/swiftui-parity.md` is updated to describe the redesigned surfaces.
