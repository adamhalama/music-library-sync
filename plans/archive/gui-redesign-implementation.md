# GUI Redesign Implementation Tracker

- **Overall status:** Complete — phases 1–9 done
- **Plan:** [gui-redesign-plan.md](./gui-redesign-plan.md)
- **Design reference:** [docs/gui-redesign/app/](../../docs/gui-redesign/app/)
- **Last updated:** 2026-08-03

This is the live, editable execution record for the native macOS GUI redesign.
Check a task only after the implementation *and* its proportional validation are
complete. When scope or behavior changes, update this file and `PLAN.md` in the
same change.

## Status Convention

Phase status must be one of:

- `Not started` — no implementation work has begun.
- `In progress` — work is active and at least one task is complete.
- `Blocked` — progress requires a recorded decision, dependency, or external action.
- `In review` — implementation is complete and awaiting review or final validation.
- `Done` — every required task and the exit gate are complete.
- `Deferred` — deliberately removed from the active sequence, with a reason in the decision log.

Checkbox convention:

- `[ ]` not complete
- `[x]` implemented and validated

Do not use a checked box to mean "coded but untested." Add new tasks when
implementation uncovers missing work; do not hide scope by broadening an
already-checked task.

## Progress Dashboard

| Phase | Status | Depends on | Exit gate |
| --- | --- | --- | --- |
| 1. Design system | Done | — | App builds and launches with system light/dark; no `ControlRoomTheme` remains |
| 2. Shell | Done | 1 | Sidebar, toolbar, inspector, status bar, and Settings scene work on every existing screen |
| 3. Home | Done | 2 | Home is the default destination and shows only protocol-backed values (C16) |
| 4. Sync | Done | 2 | Plan prompt is docked, fixture replay passes, C1–C4 and C17 verified manually |
| 5. Doctor + Credentials | Done | 2 | Severity filtering works; C12 and C13 verified |
| 6. Free DL + Rekordbox | Done | 2 | C7–C10 verified |
| 7. Playlists + Config | Done | 2 | C11 and C14 verified |
| 8. Documentation + close-out | Done | 1–7 | `docs/swiftui-parity.md` updated; completion criteria in `PLAN.md` all met |
| 9. Defect-class sweep | Done | 8 | Each of phase 8's four defects eliminated as a class, with a guard that fails if it returns |

## Constraint Coverage

Every constraint in `PLAN.md` must be visibly implemented and manually
confirmed. This table is the single place to see what is still invisible to the
user.

| ID | Constraint | Phase | Implemented | Verified |
| --- | --- | --- | --- | --- |
| C1 | Plan exists only inside a run | 4 | [x] | [x] |
| C2 | Sequential per-source planning | 4 | [x] | [x] |
| C3 | Backend blocked while a prompt is pending | 4 | [x] | [x] |
| C4 | Window change forces a rebuild | 4 | [x] | [x] |
| C5 | Per-source capability flags | 4 | [x] | [x] |
| C6 | Unlimited encoded as `plan_limit: 0` | 4 | [x] | [x] |
| C7 | Free DL is four separate RPCs | 6 | [x] | [x] |
| C8 | Free DL rows stream progressively | 6 | [x] | [x] |
| C9 | Rekordbox plan is opaque and checksummed | 6 | [x] | [x] |
| C10 | Apply is fail-closed and names the blocker | 6 | [x] | [x] |
| C11 | Playlists are cache-first | 7 | [x] | [x] |
| C12 | Credential editors start empty | 5 | [x] | [x] |
| C13 | Environment variable overrides Keychain | 5 | [x] | [x] |
| C14 | Config write SHA conflict | 7 | [x] | [x] |
| C15 | Backend exit and non-replay | 2 | [x] | [x] |
| C16 | No library statistics or run history | 3 | [x] | [x] |
| C17 | Hardcoded sync parameters surfaced | 4 | [x] | [x] |

## Cross-Cutting Definition of Done

Apply to every phase:

- [x] No file under `internal/` is modified; `git diff --stat internal/` is empty.
- [x] `go test ./...` and `go vet ./...` pass unchanged.
- [x] Every new Swift file is registered in `macos/UDL.xcodeproj/project.pbxproj`.
- [x] No color or font size is hardcoded outside `macos/UDL/DesignSystem/`.
- [x] Every `.disabled(` in the diff pairs with a `ConstraintNote` or a self-explanatory label.
- [x] Each touched screen is screenshotted in **both** light and dark appearance. *(Home,
      Doctor, Credentials, the shell, Free DL, Rekordbox, Playlists and Config were
      captured in both during phases 3–7. Phase 8 closed the two gaps: the Sync
      configure, plan and run surfaces in light, and Onboarding in both, reached by
      launching the app with `XDG_CONFIG_HOME` pointed at an empty directory so the
      user's real config was never involved.)*
- [x] Documentation is updated in the same change.

Note on the fourth item: `Typography` (in `Metrics.swift`) had to exist before the
"no hardcoded font size" rule could be enforced. It is the font counterpart of
`Theme`, and every screen now uses its tokens.

---

## Phase 1 — Design system

**Status:** Done
**Exit gate:** the app builds and launches under system light and dark, and
`ControlRoomTheme` no longer exists. — **met**

New directory `macos/UDL/DesignSystem/`.

- [x] `Theme.swift` — semantic colors mapped to system colors: `--win` →
      `.controlBackgroundColor`, `--sidebar`/`--chrome` → `.windowBackgroundColor`,
      `--sunken` → `.underPageBackgroundColor` (**corrected in phase 5**: that color
      resolves to #969696 under Aqua; it is a low-alpha `labelColor` tint now),
      `--sep` → `.separatorColor`,
      `--text-2`/`--text-3` → `.secondaryLabelColor`/`.tertiaryLabelColor`,
      `--accent` → `.accentColor`.
- [x] `Theme.swift` — `ok`/`warn`/`err`/`info`/`idle` as the only literals, each with a
      light and a dark value taken from `docs/gui-redesign/app/shell.css`, resolved
      through `NSColor(name:dynamicProvider:)` so they follow the appearance live.
- [x] `Metrics.swift` — 26pt table rows, 46pt status bar, 216pt sidebar, 266pt
      inspector, 9/11pt radii.
- [x] `ConstraintNote.swift` — the primitive behind C5/C6, plus the `.constrained(by:)`
      modifier that dims a control, keeps it visible, disables it, and states the reason.
- [x] `LifecycleChip.swift` — C2's states, with an `init(wire:)` over
      `SourceSnapshot.lifecycle`, shared by Sync, Free DL, and Rekordbox.
- [x] `WaitingBanner.swift` — C3's "backend paused" banner, plus `FrozenProgressView`
      so a blocked run's meters render greyed instead of animating.
- [x] `StatusPill.swift` (`StatusPill`, `CountBadge`) and `SeverityBadge.swift`
      (`SeverityBadge`, `SeverityTile`) over one shared `Severity` enum.
- [x] `SourceGlyph.swift` — `.glyph.sc/.sp/.am/.rb`, keyed off `SourceCapability.sourceType`.
- [x] `Callout.swift`, `PathField.swift` (+ `MonoValue`), `StatusBar.swift`
      (+ `SummaryCount`, `SummaryLine`, the `.udlStatusBar` modifier).
- [x] Remove `ControlRoomTheme` and `.preferredColorScheme(.dark)`; the file that held
      them (`Features/Home/DashboardView.swift`) is deleted outright.
- [x] Retint every existing screen. No `.orange`, `.white.opacity(_)`, `Color.red`, or
      raw `.font(.system(size:))` remains outside `DesignSystem/`.

### Added beyond the plan

Three additions were needed to satisfy rules the plan states elsewhere:

- **`Typography` (in `Metrics.swift`)** — "no view outside `DesignSystem/` hardcodes a
  font size" is unenforceable without a font token set. It is the font half of `Theme`.
- **`Cards.swift`** — `Card`, `SectionHeader`, `FieldRow`, `WorkspaceHeader`. Every
  screen needed the same `.card` / `.field` / `h2.sec` shapes; without these the retint
  would have re-implemented them seven times.
- **`EmptyStateView.swift`** — failure mode 2 ("ambiguous emptiness") needs a primitive
  that renders `notRun` / `working` / `empty` / `failed` / `blocked` differently. Also
  carries `PendingValue`, the per-cell placeholder C8 requires.

**Evidence:**

- `xcrun swiftc -swift-version 6 -strict-concurrency=complete -typecheck` over all 40
  sources: clean.
- `make app-dev` builds and ad-hoc signs `dist/dev/UDL-Dev.app`; launched successfully.
- Light and dark screenshots captured by toggling the system appearance (the app has no
  appearance override of its own any more — that is the point of the phase).

---

## Phase 2 — Shell

**Status:** Done
**Exit gate:** sidebar, toolbar, inspector, status bar, and the Settings scene
work on every existing screen; C15 verified. — **met**

- [x] `DashboardView` replaced by `Features/Shell/AppShellView.swift`: a two-column
      `NavigationSplitView` whose detail column is the active workspace.
- [x] `Features/Shell/SidebarView.swift` — `Section("Workflows")`, the contextual
      section, `Section("System")` pinned to the bottom, backend status pill footer.
- [x] `SidebarContext` so each screen can supply its contextual list.
- [x] `AppState.attention` — one derived value (doctor severities, unhealthy
      credentials, Rekordbox blockers, Free DL candidates, playlists without a
      snapshot, config problems, live run) feeding sidebar badges, Home, and Doctor.
- [x] `AppState.Destination` gains `.home`; default changes from `.doctor` to `.home`.
      Case labels now match the mockups: `Run Sync`, `SoundCloud Free DL`,
      `Rekordbox Sync`, `Check System`, `Advanced Config`.
- [x] `Features/Shell/ToolbarModifier.swift` — `.workspaceToolbar`, `.workspaceInspector`,
      `InspectorSection`.
- [x] `StatusBar` applied via `.safeAreaInset(edge: .bottom)` (`.udlStatusBar`).
- [x] `Features/Settings/SettingsView.swift` as a real `Settings` scene (⌘,);
      Settings is no longer a sidebar destination.
- [x] Commands: ⌘R start dry run & plan, ⌘. cancel, ⌥⌘S sidebar, ⌥⌘I inspector,
      ⌘1–5 workflows.
- [x] **C15** — shell-level recovery banner; after restart, in-flight screens read
      "Not resumed — the backend restarted and nothing was replayed. Re-run to continue."

### Deviations found while implementing

1. **The toolbar leading title is native, not custom.** A custom
   `ToolbarItem(placement: .navigation)` title block rendered *in addition to* the
   window title, so "udl" appeared twice. Replaced with
   `.navigationTitle(_) + .navigationSubtitle(_)`, which produces exactly the
   mockup's title-plus-monospace-subtitle and costs nothing.
2. **The inspector is per screen, not shell-level.** Each workspace applies
   `.workspaceInspector { … }` itself, because its content depends on that screen's
   own `@State`. Visibility lives in a `ShellChrome` object owned by `UDLApp`, so the
   toolbar button and ⌥⌘I drive the same value across screens.
3. **"System pinned to the bottom" is a second `List`.** A single `List` cannot pin a
   trailing section, so the sidebar is two `List`s bound to the same selection: the
   scrolling one (Workflows + contextual) and a fixed-height System one.
4. **`SidebarContext` is a store plus a modifier, not a plain environment value.**
   The contextual rows must drive per-screen selection, so the context carries its
   `select` closure and is published through `SidebarContextStore` by
   `.sidebarContext(_:)`, which also clears it when the screen goes away.
   `PlaylistsView` adopts it now; Sync adopts it in phase 4. **Correction (phase 4):
   this deviation claimed the adoption proved the slot works. It did not — the
   section never rendered, for the `onAppear`/`onDisappear` ordering reason recorded
   under phase 4. Phase 4 fixed it; it renders for both Playlists and Sync now.**
5. **C15 replaced the disconnect alert rather than adding to it.** An unexpected EOF
   used to raise a modal alert. It now terminates every in-flight workflow's local
   state honestly (`syncRun` → failed, Free DL/Rekordbox operations cleared, playlist
   status stating the previous snapshot survived), records the affected destinations in
   `AppState.notResumed`, and shows the shell banner. `notResumed` deliberately
   survives `restart()`; it is cleared per workflow only when that workflow is started
   again.

**Evidence:**

- Launched app shows grouped sidebar with a live `3` warning badge on Check System, the
  backend pill reading `Connected / backend dev`, the inspector, and the status bar.
- ⌘4 navigates to Rekordbox Sync and the sidebar selection follows (screenshot).
- Toolbar shows `udl` + `/Users/jaa/.config/udl/config.yaml` as title/subtitle.

---

## Phase 3 — Home

**Status:** Done
**Exit gate:** Home is the default destination and every value on it has a
protocol source. — **met**

- [x] `Features/Home/HomeView.swift` — hero sentence computed from
      `sources.capabilities`, `startup.attention`, and this session's run state.
- [x] Primary action **"Start dry run & plan"** (C1 wording), secondary "Check system".
- [x] Two-column `LazyVGrid` of four workflow cards with status pills.
- [x] "Needs attention" group fed by the same `DoctorResult` Doctor renders, with
      rows deep-linking to Credentials or Doctor.
- [x] Inspector: source counts, doctor totals, backend build, config and state paths.
- [x] **C16** — library size, tracks on disk, and recent runs are absent, not faked.
      The inspector says so in words: *"udl reports no library size or track count, so
      neither is shown."*

### Notes

- Every card pill names its source: `sources.capabilities` for the sync count,
  `freedl.config.read` for enabled jobs, `rekordbox.deps.status` for runtime health,
  `playlists.list` for playlist counts. Where a response has not arrived, the pill says
  so ("Config not loaded", "Runtime unknown") rather than showing a zero.
- The hero headline is deliberately ordered: no sources → prompt waiting → run active →
  blocking work → ready. Only the last one is a "good news" sentence.
- The credential deep link is a keyword match over the check name and detail, because
  `DoctorCheck` carries no machine-readable remediation target. If that proves brittle,
  the honest fix is a protocol field — which is out of scope here, so the fallback is
  always "Details" into Doctor.

**Evidence:**

- Home is the default destination on launch (screenshot: "6 sources ready to plan",
  0 errors · 3 warnings · 28 passed in the status bar).
- Inspector shows Configured 6 / Plan-capable 3 / Windowed 1 and four real paths.

---

## Phase 4 — Sync

**Status:** Done
**Exit gate:** the plan prompt is docked, the fixture replay passes, and
C1–C4/C17 are manually verified. Highest-risk phase. — **met**; the light
capture and C4 were carried into phase 8 and closed there.

`SyncView.swift` is now a three-way router over one run — configure
(`SyncConfigureView`, in the same file), plan (`SyncPlanView`), run
(`SyncRunView`). The plan surface wins over the run surface, because while a
`ui.selectRows` request is open the backend is doing nothing at all: the plan
*is* what is happening.

### Plan surface

- [x] `Features/Sync/SyncPlanView.swift` — re-hosts `PlanSelectionPromptView` out
      of the `.sheet` and into the Sync workspace, same data and same reply path.
- [x] `Table(of: PlanRow.self, selection:)` with checkbox / `#` / Title /
      status pill / Remote ID columns. **Artist and Time are absent** — see
      deviation 2.
- [x] Locked rows state their reason on click rather than doing nothing
      (`PlanRow.lockReason`, surfaced by a lock button, not a dead checkbox).
- [x] Toolbar: segmented filter (All / New / Gaps / Have with counts) and `.searchable`.
- [x] Inspector: reply controls, the run's fixed values, and source paths —
      see deviation 3 for why the limit/timeout are read-only here.
- [x] Status bar: totals, "Reset to defaults", primary "Continue" / "Rebuild plan".
- [x] Keyboard: space toggles, ⌘A selects all toggleable, arrows move the cursor.
- [x] **C1** — the plan prompt no longer reaches `.sheet(item:)` at all;
      `AppState.modalPrompt` filters `.selectRows` out, and a plan prompt routes
      the app to `.sync` as it arrives.
- [x] **C2** — sidebar lifecycle chips plus a "Source 2 of 4" header counter;
      unplanned sources dimmed with the inline reason.
- [x] **C3** — "Backend paused" banner; progress meters greyed and frozen while pending.
- [x] **C4** — window control warns that a change re-plans and clears the
      selection; primary button becomes "Rebuild plan" when the value differs.
- [x] **C5** — unsupported window/order controls rendered, disabled, and explained,
      driven by `sources.capabilities` rather than an adapter-name guess.
- [x] **C6** — `∞` display with a footnote that `plan_limit: 0` is sent.
- [x] **C17** — `askOnExisting`, `scanGaps`, `noPreflight`, `trackStatus`, and
      top-level `planWindow` surfaced under "Advanced" in the configure inspector.
- [x] Manual pass in dark appearance against a live `sync.start` dry run.
- [x] Light appearance — captured in phase 8.

### Run surface

- [x] `Features/Sync/SyncRunView.swift` split out of `SyncView.swift`; one screen
      for running, cancelled, and finished.
- [x] Two determinate `ProgressView`s from `StructuredProgressSnapshot`; a track
      with no reported percentage says so instead of rendering 0%.
- [x] Collapsible per-source sections over a `TrackRow` table.
- [x] Activity log as monospaced rows keyed by outcome color.
- [x] Failures pinned above the log so they survive scrollback.
- [x] Status bar: totals plus Stop with a `.confirmationDialog` while active;
      "Configure another run" when finished. ⌘. stays the Run menu's command —
      see deviation 4.
- [x] Confirm and masked-input prompts remain real modals (`PendingPromptView`,
      now trimmed to exactly those two kinds).
- [x] Manual pass in dark appearance on a finished run.
- [x] Light appearance — captured in phase 8 on a cancelled run.

### Invariant regression checks

- [x] `answerPlanSelection` still sets the plan window before the rebuild reply is
      sent — untouched; `SyncPlanView.submit` calls it rather than reimplementing it.
- [x] `performOrderedCancellation` still replies to the pending request before `run.cancel`.
- [x] `initialPlanSelection` / `rememberPlanSelection` overrides still survive rebuilds.
- [x] New `macos/UDLTests` case: `testDockedPlanPromptRepliesBeforeCancelingRun`
      drives a real `ui.selectRows` through a pipe and asserts the reply frame is
      on the wire before the cancel step runs.
- [x] New `WireModelTests` cases for the C17 wire spelling, the
      `AskOnExistingPolicy` defaults, and `PlanRow`'s label/lock vocabulary.

### Deviations found while implementing

1. **The plan prompt is decoded in `AppState`, not in the view.** The sidebar,
   the header counter, and the status bar all need to know which source udl is
   blocked on, so `AppState.planPrompt` decodes `SelectRowsParams` once in a
   `didSet` on `pendingPrompt`. That is also where the app routes itself to
   `.sync`: a docked plan that arrives while you are on another screen would
   otherwise be a `Needs you` badge with nothing behind it.
2. **The mockup's Artist and Time columns do not exist.** `ui.selectRows` sends
   index, title, status, remote ID and remote URL — no artist, no duration.
   Under failure mode 3 they are dropped, and the plan inspector has a
   "Columns" note saying why, rather than leaving the omission unexplained.
3. **The plan inspector shows the run's options read-only.** The mockup puts a
   plan-limit stepper and a dry-run toggle on the plan screen. `SelectRowsResult`
   carries only `selected_indices`, `download_order`, `canceled`, `rebuild` and
   `plan_window`, so nothing else can travel back mid-run. Editable controls
   there would have been a lie; they are shown under "Sent with this run" with a
   note that `sync.start` fixed them.
4. **⌘. is not duplicated onto the Stop button.** The Run menu already owns ⌘.,
   and binding it a second time locally would have let the menu item win and
   silently bypass the confirmation dialog. The button confirms; the menu command
   is explicit enough on its own.
5. **`askOnExisting` needed three states, not a toggle.** `SyncStartParams` has
   both `ask_on_existing` and `ask_on_existing_set`, so "leave it to udl" is a
   real third state. `AskOnExistingPolicy` models it, and its default reproduces
   exactly what the GUI hardcoded before (`false` / `false`).
6. **The sidebar states an unavailable reason once per section, not once per
   row.** Four queued sources each carrying "udl plans one source at a time."
   turns an explanation into noise. `SidebarView` now renders the note for the
   first unavailable row only; every row still dims and still carries the reason
   as its tooltip.
7. **`SyncRunState` gained `requestedSourceIDs`.** Sources udl has not reached
   emit no events at all, so without the list sent to `sync.start` there is no
   honest way to say "Source 2 of 4" or to show a queued source that has never
   reported anything.

### Two shell bugs this phase exposed and fixed

**1. An unbounded `Table` destroys the whole window.** A `Table` reports the
ideal height of every row it holds and does not shrink to a smaller proposal.
Left unbounded inside the split view's detail column, a 50-row plan made the
column outgrow the window: rows drew straight through the toolbar, the status bar
fell off the bottom, and AppKit squeezed the sidebar to nothing. Bisected against
the live app — it is not `.searchable`, not the sidebar context, and not
cross-object writes during view update; each was ruled out by its own rebuild.
Wrapping the plan content in a `GeometryReader` and giving the stack a definite
height fixes it, because a definite height is what a `Table` needs in order to
scroll instead of expand.

`FreeDLView`, `PlaylistsView` and `RekordboxView` host their tables in the same
unbounded shape and carry the same latent bug. They are rebuilt in phases 6 and
7; the same treatment belongs there.

**2. The sidebar's contextual slot never worked.** Phase 2 recorded
`PlaylistsView` as proving the slot; it did not — the section has never rendered.
`SidebarContextModifier` published from `onAppear` and cleared unconditionally
from `onDisappear`, and SwiftUI runs the *outgoing* screen's `onDisappear` after
the *incoming* screen has published. Confirmed with `NSLog` against the running
app:

```
UDLDBG set context=Run sources items=6
UDLDBG sidebar sees Run sources items=6
UDLDBG clear
UDLDBG sidebar sees nil items=-1
```

Two changes: publish from `onChange(of:initial:)`, which is delivered after the
body rather than inside the update that created the view, and clear on disappear
only when the store still holds *this* screen's context. C2's sidebar is the
first thing that has ever actually rendered there.

**3. "Queued" is a lie once the run is over.** A source that never reported still
read `Queued` on a finished run, implying work that was still coming.
`syncSourceLifecycle` now returns `Not run yet` unless the run is genuinely
active.

### Already landed in the phase 1 retint (not the phase gate)

`SyncView` was retinted and given the shell, and three constraint notes came along
because they were nearly free: the C3 waiting banner plus frozen progress meters, the
C5 `.constrained(by:)` treatment on the per-source window and order pickers, and the C6
`plan_limit: 0` footnote. `PendingPromptView` gained the C3 banner, the C4 rebuild
warning, and the same C5 treatment on its window picker.

**Evidence:**

- `xcrun swiftc -swift-version 6 -strict-concurrency=complete -typecheck` over all
  42 sources: clean.
- `make app-dev` builds and ad-hoc signs `dist/dev/UDL-Dev.app`.
- `git diff --stat internal/` is empty; `go test ./...` and `go vet ./...` pass.
- Configure surface captured running (dark): the C17 "Advanced" inspector shows
  plan window, on-existing policy, scan gaps, skip preflight and track status with
  their notes, and the status bar reads `6 selected · 6 configured · Live run —
  files are written`.
- Docked plan surface captured running (dark) against a real `sync.start` dry run:
  50 SoundCloud rows, `All 50 / New 15 / Gaps 0 / Have 35` segmented counts, lock
  glyphs on `already_downloaded` rows, `New`/`Have` status pills, and the inspector's
  paths and Columns note.
- **C1/C3** — no sheet appears; the plan is the workspace. The waiting banner reads
  "Backend paused — waiting for your selection", the whole-run meter renders frozen,
  and the status bar repeats the state.
- **C2** — the sidebar's `Run sources` section lists all six sources: `soundcloud-likes`
  reads `Needs you` and is the only selectable row; the other five are dimmed with
  `Queued` chips and one inline "udl plans one source at a time." The header reads
  `Source 1 of 6`.
- **C5** — the plan inspector's window picker is visible, disabled and explained
  ("scdl has no first/latest window"), driven by `sources.capabilities`.
- **C17** — the configure inspector's Advanced group shows plan window, on-existing
  policy, scan gaps, skip preflight and track status with their notes and a reset link.
- Run surface captured on a finished run: per-source cards reading `Not run yet` /
  "No events yet" for sources udl never reached, the Activity empty state
  ("The run produced no output events"), the success callout, and an inspector with
  This run / Advanced / Totals / Not available (C16).
- **C4 was implemented but not seen live in this phase.** Only `spotify-technicko`
  (deemix) supports a window, and the run finished before its prompt was reached.
  Phase 8 ran a dry run scoped to that source alone and verified it; see the phase 8
  evidence.

---

## Phase 5 — Doctor and Credentials

**Status:** Done
**Exit gate:** severity filtering works; C12 and C13 verified. — **met**

- [x] Doctor: severity count tiles double as filters (`@State var severityFilter: Severity?`);
      pressing one narrows the list and the status bar states the active filter.
- [x] Doctor: `List(selection:)` grouped by severity, worst first; the category
      filter moved to the sidebar's contextual section.
- [x] Doctor: inspector shows status, severity, detail, remediation, the resolved
      dependency path when `doctor.run` reports one, and one deep-linking fix button.
- [x] Doctor: status-bar primary label follows the worst severity — "Start dry run
      anyway (N unresolved)" when any check is an error.
- [x] Doctor: `DoctorFix` extracted so Home and Doctor route the same check to the
      same screen; `HomeView.isCredentialCheck` deleted.
- [x] Doctor: checks run on entry when the session has none, matching the TUI.
- [x] Credentials: four-column `Table` — credential, health, used by, stored in.
- [x] Credentials: sidebar contextual section, selection-driven inspector with the
      one action, and the precedence callout pinned under the table.
- [x] Credentials: `.sheet` editor with `SecureField`, an explicit "Show value"
      toggle, and a destructive `.confirmationDialog` on clear.
- [x] **C12** — the screen callout *and* the sheet state that the existing value is
      never loaded or shown; the buffer starts empty and is wiped on the way out.
- [x] **C13** — override rows are marked in the table, the inspector names the exact
      environment variable, and the action reads "Move to Keychain".
- [x] Manual pass in **both** light and dark appearance.

### Deviations found while implementing

1. **The mockup's Info tile can never be non-zero, so it is gone.**
   `internal/agent/methods_doctor.go` derives each check's `status` from its
   severity and spells `info` as `ok`. An Info tile would therefore have been a
   permanently dead control. Doctor shows Errors · Warnings · Passed, and the
   inspector states why there is no fourth tile.
2. **"Used by" has no protocol field, so it is derived, not invented.**
   `credentials.list` is metadata-only. The column comes from
   `sources.capabilities`, mirroring the adapter rules `internal/doctor` uses to
   decide which credential checks to emit (SoundCloud + `scdl*` → client ID;
   Spotify + `deemix` → ARL and Spotify app). When nothing uses a credential the
   cell says so, and the inspector names the derivation.
3. **`external_override` is a warning, not a pass.** `credentialSeverity` used to
   rank it `.ok`, which made a credential the app does not manage look managed.
   Only `.error` feeds `unhealthyCredentials`, so the sidebar badge is unchanged.
4. **The wire spelling of an override was wrong.** The old code tested
   `storage_source == "external_override"`, a value the backend never sends:
   `storage_source` is `""` / `env` / `keychain` / `spotdl_config` / `unknown`, and
   `external_override` is a *health*. `CredentialStatus.isExternallyOverridden`
   now tests all three real spellings, which is why C13 finally shows live.
5. **Doctor's severity/category filters live in the view, not `AppState`.** They
   are per-screen presentation state with no protocol source, so putting them in
   the shared object would have made them survive navigation for no reason.

### Three shell bugs this phase exposed and fixed

**1. The sidebar's contextual rows were not clickable.** Phase 4 fixed *rendering*
the contextual section; the rows still did nothing when clicked. They used
`.onTapGesture` inside a `List(selection:)`, whose own row hit-testing swallows the
gesture. They are `Button`s with `.buttonStyle(.plain)` now. `PlaylistsView` carried
the same latent bug and is fixed by the same change.

**2. `GeometryReader` over-reports the height a `Table` may use.** The phase 4
treatment bounded the table with `proxy.size.height`, but `GeometryReader`'s size
spans the whole frame including the safe area the 46pt status bar occupies. Anything
stacked under the table was drawn beneath the status bar and clipped — the C12/C13
callouts on Credentials, half off the window. `.layoutPriority` and
`.safeAreaInset` both failed to reclaim it because a `Table` does not yield height
to a sibling. `DesignSystem/BoundedContent.swift` now subtracts
`proxy.safeAreaInsets` and is the one container every table-hosting screen uses;
`SyncPlanView` adopts it too.

**3. `Theme.sunken` was a mid grey in light appearance.** It mapped to
`NSColor.underPageBackgroundColor`, which resolves to #969696 under Aqua, turning
every card header and severity tile into a grey slab. Phase 1 recorded light/dark
captures but this went unnoticed because the tiles only reached a light screen here.
It is now a low-alpha `labelColor` tint, reproducing `shell.css`'s #f7f7fa / #252528
while still following the appearance.

**Evidence:**

- `xcrun swiftc -swift-version 6 -strict-concurrency=complete -typecheck` over all
  45 sources: clean.
- `make app-dev` builds and ad-hoc signs `dist/dev/UDL-Dev.app`.
- `git diff --stat internal/` is empty; `go test ./...` and `go vet ./...` pass.
- Doctor captured live in **light and dark**: `Errors 0 · Warnings 3 · Passed 28`
  tiles over a `WARNINGS · 3` / `PASSED · 28` grouped list, category counts in the
  sidebar (`dependency 8 checks · worst warnings`, `auth 4 checks`, …), and the
  inspector's check detail plus effective PATH and resolved dependencies.
- **Severity filter** — clicking Warnings rings the tile, narrows the list to the
  three warnings, and the status bar adds `Filtered to warnings.`
- **Category filter** — clicking `auth` in the sidebar narrows to its 4 checks,
  moves the selection to the first visible one, and the status bar reads
  `Filtered to auth.`
- **C12** — the Credentials screen carries the "existing value is never loaded or
  shown" callout, and the editor repeats it above an empty `SecureField`.
- **C13 seen live** — `SoundCloud client ID` reads health `external override`,
  stored in `environment variable`, the inspector names `SCDL_CLIENT_ID`, and both
  the inspector and the status bar offer **Move to Keychain**, never "Replace". The
  clear action carries the note that the override keeps winning until it is unset.
- **Used by, derived** — `soundcloud-likes` and `godski-unreleased` for the client
  ID, `spotify-technicko` for the ARL and the Spotify app credentials.
- New `WireModelTests` cases: `testDoctorFixRoutingIsSharedAndFallsBackToDetails`,
  `testCredentialConsumersMirrorTheBackendAdapterRules`,
  `testExternalOverrideIsMarkedAndNeverOfferedAPlainReplace`.

---

## Phase 6 — Free DL and Rekordbox

**Status:** Done
**Exit gate:** C7–C10 verified. — **met**

### Free DL

- [x] Free DL: phase stepper as `@State` with explicit per-step buttons
      (`Features/FreeDL/FreeDLPhaseBar.swift`); one table across all steps with
      per-step columns.
- [x] Free DL: inspector becomes the job form writing `freedl.yaml` through
      `freedl.config.write` (`Features/FreeDL/FreeDLJobForm.swift`).
- [x] Free DL: promote keeps its `.confirmationDialog` and names the plan's real
      `backup_root`.
- [x] **C7** — no auto-advance; an unrun step reads "Not run yet"; completed steps
      stay visible and re-enterable; a step whose prerequisite does not exist is
      rendered, dimmed, and told why.
- [x] **C8** — per-cell `PendingValue` placeholders while local quality and Free DL
      availability resolve, plus `FreeDLPlanRow.isStillResolving` so a streaming row
      reads `Checking`, never `Blocked` — see deviation 3.
- [x] `BoundedContent` adopted, closing the latent unbounded-`Table` bug phase 4
      recorded against this screen.
- [x] Manual pass in **both** light and dark appearance against a live
      `freedl.plan.start`.

### Rekordbox

- [x] Rekordbox: restyled to runtime health strip → setup strip (database + inspection)
      → plan bar → checksummed plan table with blockers inline.
- [x] Sidebar contextual "Plan targets": the config default, folder mappings and
      playlist jobs as one selection, because `job_id` and `mapping_id` are mutually
      exclusive on the wire.
- [x] Segmented All / Changes / Blocked filter and `.searchable` over artist, title
      and path.
- [x] **C9** — the plan stays an opaque `JSONValue`; the checksum is on the plan bar
      *and* in the inspector; drift produces "Regenerate plan", never a partial apply.
- [x] **C10** — one `RekordboxApplyGate` derives the pill, the primary label, the
      dry-run state and the help text together, so they cannot disagree. The primary
      always names the blocker.
- [x] `RekordboxConfigEditor` extracted to its own file.
- [x] Manual pass in **both** light and dark appearance against a live
      `rekordbox.plan` over 139 rows.

### Deviations found while implementing

1. **The stepper has four steps, not the mockup's three.** C7 is stated as "four
   separate RPCs with four run IDs", and `freedl.promotionPlan.build` and
   `freedl.promote.apply` are two of them. Collapsing them into one "Promote"
   step would have hidden exactly the seam C7 exists to show. Step 3 reviews and
   selects promotion rows; step 4 shows what would be written, where the original
   is copied, and is the only step behind a confirmation.
2. **There is no "Unlimited" plan-limit switch, because it would be a lie.**
   `internal/freedl/config.go` normalises a job's `plan_limit: 0` to the
   *defaults* plan limit (50). Unlike the sync plan limit (C6), a Free DL job has
   no unlimited value, and the job form states this next to the field. The
   mockup's Unlimited toggle is dropped rather than shipped as a control that
   silently means 50.
3. **A streaming row is `Checking`, not `Blocked`.** Rows are emitted before their
   probes land, and `recomputeSelectable` only writes a skip reason once the source
   plan has been enumerated — so a row can be `selectable: false`, probe
   *available*, and carry no reason at all. Rendering that as Blocked is a claim udl
   has not made. `isStillResolving` treats the *absence of a reason* as the signal,
   since a genuinely blocked row always carries one. Caught on a live run: the first
   build showed 26 rows reading `Blocked` while the probes were still in flight.
4. **The mockup's Score column is absent from the plan and capture steps.**
   `freedl.plan.event` has no match score and no artist; the score exists only once
   `freedl.promotionPlan.build` has run. It appears on step 3, and the plan
   inspector's "Columns" note says why it is not on steps 1 and 2.
5. **Target format is auto / wav / mp3-320 / aac-256.** `validTargetFormat` rejects
   the mockup's FLAC option, so it is not offered and the form says what udl accepts.
6. **Two Rekordbox apply preconditions are shown as *unknown*, not as ticks.**
   The mockup ticks "Rekordbox is closed" and "snapshot checksum matches" before any
   attempt. The protocol reports no process state and no database state until a run
   reaches `CheckRekordboxClosed`, so both render with a `questionmark.circle` and
   the sentence that udl checks them at apply time. They only turn red when a real
   backend refusal names them — `RekordboxObstacle` classifies the actual message
   strings from `internal/rekordbox/playlistsync/plan.go`, and nothing predicts them.
7. **`RekordboxObstacle` is a new piece of `AppState`, and it is set only from a
   refusal.** "Rekordbox is running" arrives as a *run* failure, not an RPC error, so
   `applyRekordboxFinished` classifies inspect and apply failures; checksum drift and
   a refused partial mirror arrive synchronously from `rekordbox.apply` and are
   classified there. A successful inspect or apply clears it, because that step
   passed the gate it names.
8. **The plan's Time column needed parsing to be a time at all.** `PlanRow.duration`
   is whatever `duration of t as text` produced in Music.app's AppleScript — seconds
   as a real in the *system* locale, so it arrives as `266.029` or `266,029`. The
   first live capture rendered `266,029…` in a column headed Time. It is parsed to
   m:ss and falls back to the raw string rather than showing something udl did not
   report.
9. **The status bar reports changed *rows* plus removals separately.** The plan
   summary's `will_remove` counts Rekordbox content with no Music row, so it can
   never appear in the table. Reporting one "changes" number made the segmented
   filter (115) and the status bar (117) disagree on a live plan; they are now two
   labelled numbers.
10. **`Theme.separatorStrong` was added to the design system.** `shell.css`'s
    `--sep-2`. The stepper's unfilled connector and an unreached step's ring vanish
    against `--sunken` when drawn with `--sep`.

### Already landed in the phase 1 retint (not the phase gate)

Free DL's inspector already showed a per-phase lifecycle and the plan table already
used `PendingValue` placeholders for the streaming Local and Free DL columns (C7/C8 in
spirit). Rekordbox already showed the checksum in the inspector with the "never decode
and re-encode" note (C9) and a primary button naming the blocker (C10). This phase
added the stepper, the single cross-step table, the job-form inspector, the
runtime-health strip, the plan bar, the apply gate, and the two honestly-unknown
preconditions.

**Evidence:**

- `xcrun swiftc -swift-version 6 -strict-concurrency=complete -typecheck` over all
  48 sources: clean. The new `WireModelTests` cases typecheck against those sources
  through an XCTest shim, since full Xcode is not installed here.
- `make app-dev` builds and ad-hoc signs `dist/dev/UDL-Dev.app`.
- `git diff --stat internal/` is empty; `go test ./...` and `go vet ./...` pass.
- **C7 seen live** — a real `freedl.plan.start` over `soundcloud-free-dl` finished with
  50 rows and the screen *stayed on step 1*: the Plan bubble turned accent with a
  checkmark reading `Done`, Capture became selectable reading `Not run yet`, and
  Promotion plan and Promote stayed dimmed with "A promotion plan is built from a
  finished capture run." The inspector lists all four steps with their own RPC name and
  the live run ID.
- **C8 seen live** — mid-run, unresolved cells render a spinner and the word `checking`
  in both the Local and Free DL columns with a blue `Checking` status pill, while
  resolved rows read `not in library` / `aac 160k` and `available · hypeddit.com` /
  `unsupported host` / `no free DL`. The status bar counted `36 still checking`
  separately from `14 no gain or no free DL`; on completion, `17 selected · 17 upgrades
  · 33 no gain or no free DL`.
- **C9 seen live** — `rekordbox.plan` over 139 rows: the plan bar reads
  `Favourites → fav_imports` / `plan 1 · generated 2026-08-01T10:39:29Z · checksum
  49fb6ba0…17a8`, and the inspector shows the full `checksum_sha256` with the
  never-decode-and-re-encode note.
- **C10 seen live** — the plan had one missing track. The pill read `Apply blocked`,
  Dry run was disabled with its reason, and the primary read **"1 track missing —
  resolve to apply"**. Clicking it switched the filter to `Blocked`, leaving exactly
  the one blocking row on screen (`Netherworld — Atalantis · missing · skip`) with the
  status bar reading `Filtered to blocked.`
- Inspector counts `44 ADD · 71 MOVE · 1 BLOCKED`, Rows 139 / Matched 138 / Missing 1 /
  Ambiguous 0, and the four apply preconditions: two resolved (✓ checksum, ✕ complete
  mirror) and two honestly unknown.
- Both screens captured in **light and dark** appearance.
- New `WireModelTests` cases: `testFreeDLRowDistinguishesStillCheckingFromNothingFound`,
  `testStreamingRowIsCheckingNotBlocked`,
  `testFreeDLQualitySummaryNamesProbeFailureRatherThanRenderingBlank`,
  `testFreeDLPhasesNameTheirOwnRPC`,
  `testRekordboxPlanPresentationKeepsTheSentValueVerbatim`,
  `testRekordboxFolderPlanSumsPerOperationSummaries`,
  `testRekordboxApplyGateNamesEveryBlocker`,
  `testRekordboxObstacleClassifiesRealBackendMessages`.
- **Not exercised live:** `freedl.capture.start`, `freedl.promotionPlan.build`,
  `freedl.promote.apply` and a real `rekordbox.apply`. Each writes files or a
  database, and none was run unprompted. Their surfaces were verified in their
  "Not run yet" and blocked states, which is what C7 and C10 govern.

---

## Phase 7 — Playlists and Config

**Status:** Done
**Exit gate:** C11 and C14 verified. — **met**

### Playlists

- [x] Sidebar contextual "Snapshots" section; snapshot head with the definition,
      the Music playlist and when the cache was taken; All / Missing segmented
      filter with counts; `.searchable` over artist, track, album and path.
- [x] Six-column track `Table` (`#` · Artist · Track/local path · Album · Time ·
      Local), `BoundedContent`, closing the latent unbounded-`Table` bug phase 4
      recorded against this screen.
- [x] Inspector: snapshot detail with the checksum, the **handoff** section
      (`default_freedl_job` / `default_rekordbox_target` with Open or Map…), the
      definition, the feature config path, and the "Not available" note.
- [x] `PlaylistDefinitionEditor` extracted to its own file and extended to edit
      an existing definition, including both handoff mappings.
- [x] **C11** — the cache-first callout is pinned under the table; refresh is
      behind a `.confirmationDialog` that states the snapshot on screen survives
      a failure; every non-replacing outcome carries
      `PlaylistStatus.preservedPreviousSnapshot` and says so.
- [x] Three distinct empty states: no cached snapshot (`notRun`), unreadable
      snapshot file (`failed`), and no matching track (`empty`).
- [x] Manual pass in **both** light and dark appearance against the live cache.

### Config

- [x] Sidebar contextual "Config objects": Defaults plus every source, with a
      `failed` chip on a source a local check attributes a problem to.
- [x] `ConfigDefaultsForm` and `ConfigSourceForm` as `.formStyle(.grouped)`;
      move up / move down / duplicate / delete live on the source form.
- [x] **Tri-state `Picker`** (`TriStateBool`) for the three `*bool` policy
      fields, with the note that unset is a real value; `SourceEditorView` uses
      it too.
- [x] Read-only YAML tab — see deviation 2 for what it can honestly show.
- [x] Live validation (`ConfigValidation`) in the inspector, attributed to a
      source where possible, blocking Save; the backend still validates on save
      and stays the authority.
- [x] **C14** — `AppState.ConfigConflict`, set only by a real `-32003`, with its
      own banner naming both SHAs and offering **Reload from disk** or
      **Overwrite…** behind a confirmation. `saveMainConfig(overwritingExternalChanges:)`
      is a separately named call, not a retry.
- [x] Dirty tracking (`Revert`, "Unsaved changes"), ⌘S on Save.
- [x] `SourceEditorView` extracted to its own file.
- [x] Manual pass in **both** light and dark appearance.

### Onboarding

- [x] Primary action moved into the status bar with a step summary, matching
      every other screen; the "no file is written until Finish Setup" note stays.
- [x] Adopts the new `SourceEditorView`, so the first source is drafted with the
      same tri-state policy vocabulary the config editor uses.
- [x] Captured live in **both** light and dark — in phase 8, against a disposable
      `XDG_CONFIG_HOME`. That pass found three real defects; they are recorded under
      phase 8 rather than backdated here.

### Deviations found while implementing

1. **The mockup's "Export snapshot…" button is dropped.** `playlists.list`
   reports no snapshot file path — only `playlists.refresh` returns one — and
   there is no export method. A working button there would be this app writing a
   file it invented rather than one udl produced. The inspector's "Not
   available" section says exactly that, the same treatment C16 gives Home.
2. **The YAML tab shows the file as read, not a preview of the draft.** The
   mockup calls it "a read-only preview of exactly what will be written".
   `config.validate` returns only `{"valid": true}`, and `config.writeFile`
   returns canonical text *after* it has saved, so there is no method that
   canonicalises without writing. Rendering the draft would mean guessing at
   udl's YAML output. The tab shows `ConfigFileResult.content`, says so, and
   turns its callout to a warning when the draft differs.
3. **`MusicDuration` was extracted to the design system.** Playlists' `duration`
   is the same `duration of t as text` AppleScript field Rekordbox parses in
   phase 6 — seconds as a real in the system locale. Two copies of that parser
   would have been two chances to render `266,029` under a column headed Time.
4. **A source id is read-only in the inline form.** `MainConfigSource.id` names
   the state file and is what `--source` matches, so renaming in place would
   orphan state rather than rename it. The form states this and points at
   Duplicate, which takes the next free `-copy` id.
5. **"Missing locally" needed a second note.** `missing_local` is what udl saw
   when the snapshot was *taken*, not now. Without saying so, a file deleted
   since the last refresh reads as on disk, which looks like a bug rather than
   the cache being a cache.
6. **`playlistStatusMessage` became `playlistStatus`.** C11 requires a failed or
   canceled refresh to read differently from a successful one, and a bare string
   rendered every outcome as the same `.info` callout. The value now carries a
   `Severity` and a `preservedPreviousSnapshot` flag, and the screen states the
   preservation itself rather than relying on the message's wording.

### Two bugs this phase exposed and fixed

**1. `BoundedContent` is needed for a `Form`, not only a `Table`.** Config's
`Form` sat unbounded in the detail column and happened to fit. Stacking the C14
conflict banner above it pushed the column past the window: the splitter group
measured 1452×3660 at position (60, −1283) and the sidebar, toolbar and status
bar all left the screen — the phase 4 failure exactly, from a `Form` rather than
a `Table`. Caught live the first time the conflict banner rendered. `Form` has
the same "reports the ideal height of every row and does not shrink" behaviour,
so it needs the same container.

**2. A `SidebarContext.select` closure must write through the projected
binding.** `ConfigEditorView` first assigned to `selection` directly, the way
`RekordboxView` does. The rows rendered, the click reached the button — the
accessibility tree confirmed `button 1 of UI element 1 of row 9` under the
pointer — and the selection never changed, because the closure captured the view
value the context was built from. Capturing `$selection` first, as `PlaylistsView`
does, fixes it. Phase 6's Rekordbox target rows carry the same latent bug; it was
never caught there because the evidence exercised the *filter*, not the target
list.

### Already landed in the phase 1 retint (not the phase gate)

Playlists lost its in-window `HSplitView` column: definitions now populate the sidebar's
contextual section, and the C11 cache-first callout is on screen. Config's inspector
shows the read-content SHA with the C14 explanation, and a "changed on disk" status
message renders as a warning callout. This phase added the track table with its filter
and search, the handoff inspector, the three distinct empty states, the refresh
confirmation, the grouped forms with reorder/duplicate/delete, the tri-state pickers,
the YAML tab, the attributed validation, and the real conflict state.

**Evidence:**

- `xcrun swiftc -swift-version 6 -strict-concurrency=complete -typecheck` over all
  53 sources: clean. The new `WireModelTests` cases typecheck against those sources
  through the XCTest shim, since full Xcode is not installed here.
- The pure logic behind those cases was additionally **executed** against the real
  app sources (`MusicDuration`, `missing_local` decoding, `TriStateBool` encoding,
  `ConfigValidation`, `duplicated`, `MainConfig` equality): 22/22 assertions pass.
- `make app-dev` builds and ad-hoc signs `dist/dev/UDL-Dev.app`.
- `git diff --stat internal/` is empty; `go build ./...`, `go vet ./...` and
  `go test ./...` pass.
- **C11 seen live** — Playlists over the real cache: `Favorites`, `apple_music ·
  Favourites · cached Jun 19, 2026 at 22.19`, `All 96 / Missing 0`, the Time column
  reading `4:26` / `6:17` rather than `266,029`, `On disk` pills, and the pinned
  callout "Cached snapshot — opening never contacts Music.app." The status bar reads
  `96 cached tracks · 0 missing locally · cache intact`. The refresh confirmation
  reads "…replaces the cached snapshot only after the complete result validates. A
  failed or canceled refresh keeps the snapshot that is on screen now." It was
  **dismissed, not accepted**: a real refresh contacts Music.app and rewrites a
  snapshot, and none was requested.
- Inspector shows the snapshot checksum, both "Missing locally" notes, the handoff
  rows resolving to `soundcloud-free-dl` and `fav_imports` with **Open**, the
  definition, `playlists.yaml`, and the "there is no export" note.
- **C14 seen live** — with a draft edit pending, `config.yaml` was appended to
  outside the app and ⌘S pressed. The banner read **"config.yaml changed on disk
  since UDL read it. Nothing was written."**, naming both SHAs (`005afe132b89` vs
  `311ec29ca93e`) and offering **Reload from disk** and **Overwrite…**; the
  inspector's SHA note turned to a warning; the status bar read `Changed on disk —
  nothing written.` The file's SHA was **unchanged** after the refusal. Clicking
  Reload cleared the conflict and the dirty state. **Overwrite was never clicked.**
  The config file was backed up before the test and restored to its original SHA
  afterwards.
- Config form captured live: grouped Defaults, and `soundcloud-likes` showing the
  read-only ID with its reason, the segmented Type, and the three tri-state pickers
  reading `Yes` / `No` / `No` above the `*bool` note.
- YAML tab captured: the file as read, with the "udl has no method that
  canonicalises a config without writing it" callout.
- Both screens captured in **light and dark** appearance; Save renders visibly
  disabled when the draft is clean.
- **Onboarding was not captured live.** Reaching it needs a missing or invalid
  `config.yaml`, which on this machine is the user's real one. It typechecks, and
  its only behavioural change is that the existing Finish Setup action moved into
  the status bar; the routing that precedes config-dependent workflows is untouched.
- New `WireModelTests` cases: `testAbsentMissingLocalMeansTheFileWasFound`,
  `testMusicDurationIsSharedAndNeverInventsATime`,
  `testPlaylistStatusMarksEveryOutcomeThatPreservedTheSnapshot`,
  `testTriStateBoolRoundTripsUnsetSeparatelyFromFalse`,
  `testConfigValidationAttributesProblemsToTheirSource`,
  `testDuplicatingASourceTakesAFreeID`,
  `testConfigWriteConflictCarriesBothSHAs`.

---

## Phase 8 — Documentation and close-out

**Status:** Done
**Exit gate:** `docs/swiftui-parity.md` updated; every completion criterion in
`PLAN.md` met. — **met**

This phase was supposed to be documentation only. Closing the two carried
verification items first turned it into a bug-fixing phase as well: onboarding
had never been run, and running it end to end broke three times.

### Carried verification closed

- [x] **C4 seen live.** A dry run scoped to `spotify-technicko` (the only deemix
      source) alone. The plan arrived with the window control **enabled** at
      `Latest` and the note "Changing the window re-plans this source and clears
      your selection"; changing it to `First` turned the note amber and the
      primary from **Continue** to **Rebuild plan**. Pressing it re-planned the
      same run ID with an entirely different 50 rows and `Selected 0 of 50`.
- [x] **Sync in light appearance** — configure, docked plan, and run.
- [x] **Onboarding live in light and dark**, including the source editor sheet,
      a real `config.writeFile`, and the restart onto Home.

### Bugs this phase found and fixed

**1. Dry run was not the default it is documented to be.** `SyncConfigureView`
carries the comment "dry run is the default, so the plan is always reachable
through a reversible action", but `AppState.syncDryRun` initialised to `false`.
Only `startDryRunPlan()` — Home's button and ⌘R — forced it on, so reaching Run
Sync from the sidebar offered a **live** run with the status bar reading "Live
run — files are written". This is C1's own wording contradicting C1's behaviour;
the default is now `true`.

**2. Onboarding's Finish Setup could never succeed, and said nothing.** Three
separate faults stacked:

- `SourceEditorView` treated the state file as optional. `config.Validate`
  requires one for *both* source types the editor offers, so the write was
  refused with `source "sc-demo" state_file is required for soundcloud`. The
  editor now requires it, `ConfigValidation` mirrors the rule so the config
  screen blocks Save on it too, and the disabled primary names it.
- The refusal was invisible. `saveMainConfig` records it in `configProblems` and
  `configStatusMessage`, both of which only the Config screen renders — so
  onboarding looked untouched and only a badge on `Advanced Config` moved.
  Onboarding now renders the refused problems itself.
- With the config finally written, `restart()` failed with **"Backend
  disconnected: connection is closed"** and left the app with a wiped sidebar
  and no status bar. `AgentProcess.stop()` closed the connection but left
  `process` and `client` set until `Process.terminationHandler` hopped to the
  main actor; `restart()` calls `launch()` immediately, and `launch()` returns
  the *existing* client while `process` is non-nil. It therefore handed back the
  client of the connection it had just closed. `stop()` now tears the generation
  down synchronously, and the termination handler guards on
  `self.process === process` so a late callback cannot delete its replacement.

**3. A fresh install raised a modal on first launch.** `playlists.config.read`
returns `"playlists": null` when no `playlists.yaml` exists — Go's nil slice —
and `PlaylistConfig.playlists` was a non-optional array, so `loadPlaylists()`
threw and `AppState` raised "Playlist cache could not load: The data couldn't be
read because it is missing." `AGENTS.md` already carried this exact lesson
(line 38 of that file); one DTO had not followed it. It decodes to `[]` now, and
the AGENTS entry names the offending method so the next reader does not have to
rediscover which one it was.

**4. Two disabled primaries stated their reason only in `.help(_)`.** Finish
Setup and the source editor's Use source. A tooltip is not an adjacent stated
reason, which is failure mode 1 in `PLAN.md`. Both now render a `ConstraintNote`
next to the button naming the first unmet requirement.

### Documentation

- [x] `docs/swiftui-parity.md` gains a **Shell** and a **Home** section and a
      "Redesigned surface" block for Doctor, Credentials, Sync, Free DL,
      Rekordbox and Onboarding, each naming the constraints that screen makes
      visible and the mockup panels dropped for want of a protocol source.
- [x] `AGENTS.md` gains five lessons: the `playlists.config.read` null list, the
      `AgentProcess.stop()` teardown ordering, the `state_file` requirement, the
      "a disabled control states its reason on screen" rule, and how to drive the
      app for manual verification (`.dev/app.sh`, `.dev/ui`, and the fact that
      SwiftUI `TextField` bindings commit on focus loss, so an accessibility
      write needs a following Tab).
- [x] Every constraint row is checked in both columns.
- [x] Every completion criterion in `PLAN.md` is met.

### Added beyond the plan

`.dev/ui.swift` (compiled to `.dev/ui`) joins the existing `.dev/app.sh`
harness: a CGEvent click/type/key tool in the flipped global coordinate space
`screencapture -R` uses, so a point measured on a screenshot can be clicked
directly. Without it the verification in this phase would have needed the user
to drive the app by hand.

**Evidence:**

- `xcrun swiftc -swift-version 6 -strict-concurrency=complete -typecheck` over all
  53 sources: clean. `WireModelTests` and `AgentProcessTests` typecheck against
  those sources through the XCTest shim.
- `make app-dev` builds and ad-hoc signs `dist/dev/UDL-Dev.app`.
- `git diff --stat internal/` is empty; `go vet ./...` and `go test ./...` pass.
- **C4 seen live** (dark → light both captured): `spotify-technicko`, run
  `6c34bce5…`, `All 50 / New 0 / Gaps 6 / Have 44` before the rebuild and
  `Gaps 2 / Have 48` after it, with the selection cleared to `0 of 50`. Stopping
  the run from the plan surface left `Run cancelled · Skipped`, exit code 130.
- **Onboarding seen live**: `no sources` state against an empty
  `XDG_CONFIG_HOME`, the four numbered steps, the source editor with its
  tri-state pickers and the "Default leaves the key out of the file entirely"
  note, `Ready to write the first config file.`, and — after Finish Setup — the
  canonical `config.yaml` on disk and the app on Home reading `1 source ready to
  plan`, `0 errors · 1 warnings · 10 passed`, `0 playlists`, with no modal.
- New tests: `testASourceWithoutAStateFileIsRejectedBeforeItIsSent`,
  `testPlaylistConfigDecodesANullDefinitionListAsEmpty`, and a restart assertion
  added to `AgentProcessTests.testLaunchHandshakeGracefulStopAndRestart` that
  fails against the old teardown ordering.
- **Not exercised:** nothing was run against the user's real config that writes.
  The C4 run was a dry run and was cancelled; onboarding wrote only into a
  disposable `XDG_CONFIG_HOME` under the scratch directory. `~/.local/state/udl`
  pre-existed and was not modified.

---

## Validation Commands

Run the relevant subset per phase and record the result in that phase's
**Evidence** section.

```sh
# Build and launch the native app (Command Line Tools are sufficient)
make app-dev-open

# Prove the backend was not touched
git diff --stat internal/
go test ./...
go vet ./...

# Protocol replay (requires full Xcode)
xcodebuild test -project macos/UDL.xcodeproj -scheme UDLTests
```

Full Xcode is not installed on the primary development machine, so
`xcodebuild build`/`test` runs in CI (`.github/workflows/macos-app.yml`).
`packaging/dev/build_macos_app.sh`, invoked by `make app-dev`, compiles the
current-architecture sources and ad-hoc signs the bundle for local visual checks.

### Manual constraint verification

- **C2/C3** — start a multi-source dry run; confirm exactly one source reads
  `Needs you` while the others read `Queued`, and that progress meters are
  visibly frozen while the prompt is open.
- **C4** — change the window on a deemix source; the primary button must become
  "Rebuild plan".
- **C5** — open an `scdl` source; the window control must be visible, disabled,
  and explained.
- **C15** — kill the backend mid-run; confirm the recovery banner and the
  non-resumption message.

## Phase 9 — Sweep the four defect classes

**Status:** Done
**Exit gate:** each of the four defects phase 8 found is eliminated as a *class*,
with a guard that fails if it returns. — **met**

Phase 8 fixed four defects found by driving onboarding and C4 for the first
time. Each was an instance of a class the rest of the app had never been swept
for. An audit found all four classes live:

| Class | Surface before the sweep |
| --- | --- |
| Go `null` collection → non-optional Swift collection | 31 non-optional array fields; 10 Go producers marshal nil with no `omitempty` |
| Disabled control with no on-screen reason | 30 bare `.disabled(` sites in 16 files |
| Failure written where the acting screen cannot show it | 30 `alertMessage =` sites funnelling into one modal |
| A default written twice | `resetSyncAdvanced()` repeating the five `@Published` literals |

No file under `internal/` is touched: adding `omitempty` in Go would have fixed
the first class at source, but the protocol is frozen and the rule already lives
on the Swift side.

### D — one source of truth for the sync defaults

- [x] `SyncDefaults` added to `SyncModels.swift`; the `@Published` initialisers
      and `resetSyncAdvanced()` both read it, so the two routes cannot disagree.
- [x] `testSyncDefaultsAreTheDocumentedC1AndC17Values` pins the constants **and**
      the wire payload a default run sends, `dry_run: true` included. This is the
      test that would have caught phase 8's first defect.
- [x] `testSyncStateStartsAtAndResetsToTheDefaults` covers both routes.

### A — Go `null` collections decode as empty

- [x] `@DefaultEmpty` in `Backend/Wire/WireModels.swift`, over an
      `EmptyRepresentable` protocol so one wrapper serves arrays *and*
      dictionaries — `feature_config_paths`, `capabilities` and
      `resolved_dependencies` are nil-able maps with the same defect.
- [x] Applied to every wire-inbound collection across `WireModels`,
      `SyncModels`, `PlaylistModels`, `FreeDLModels`, `RekordboxModels` and
      `ConfigModels`. Outbound-only params and non-`Codable` view state are
      deliberately excluded.
- [x] The two hand-written tolerant decoders (`PlaylistConfig` from phase 8,
      `RekordboxSyncConfig`) are replaced by the wrapper, so there is one
      mechanism rather than three.
- [x] `testDefaultEmptyDecodesNullAndEncodesABareCollection` and
      `testEveryStartupResultDecodesWithAllCollectionsNull`.

### C — a failure appears on the screen that caused it

- [x] `AppState.WorkflowStatus` (message + severity), `ExpressibleByStringLiteral`
      so the ~20 informational assignments stayed unchanged and only failures
      name a severity. `.failure` and `.canceled` distinguish "went wrong" from
      "you stopped it".
- [x] `freeDLStatusMessage`/`rekordboxStatusMessage` become
      `freeDLStatus`/`rekordboxStatus`; Doctor and Credentials gain the sinks
      they never had.
- [x] 25 of the 30 `alertMessage =` sites re-routed. The five that remain are
      session-level: `start`, `restart`, `refreshPlanPrompt`, `answerPrompt`,
      `answerPlanSelection`.
- [x] Free DL and Rekordbox render a failure as a `Callout` in the content
      column, not only as inspector text — the inspector can be closed.
- [x] `testOnlySessionLevelFailuresReachTheModalAlert` reads `AppState.swift`,
      maps every `alertMessage` site to its enclosing function, and fails on
      anything outside the allowlist.

### B — every disabled control states its reason

- [x] `AppState.busyReason` — one phrase naming the run that holds a control,
      derived from exactly the state `isAnythingRunning` reads.
- [x] 30 bare sites down to 19, each remaining one budgeted and justified.
      Conversions use the existing `.constrained(by:)` and the
      `FreeDLJobForm` note pattern; nothing new was added to the design system.
- [x] Toolbar reload buttons state their reason by *becoming* it — the label
      swaps to "Running checks…" / "Reloading…" with a spinner.
- [x] **C7 improved as a side effect**: the Free DL phase bar renders each
      unreachable step's reason as the step's own subtitle instead of a tooltip.
- [x] `testEveryDisabledControlStatesItsReasonOnScreen` budgets bare
      `.disabled(` per file with a written justification for each budget.

### Deviations found while implementing

1. **A property wrapper's memberwise initialiser is fragile in a way that is not
   documented anywhere obvious.** `@DefaultEmpty` first carried
   `init(wrappedValue: Value = .emptyValue)`; every construction site stopped
   compiling because Swift then synthesises memberwise initialisers taking
   `DefaultEmpty<Value>` rather than `[Value]`. Adding a separate `init()`
   reproduced it exactly. The rule is that **no** initialiser may be callable
   with no arguments. Bisected with a five-line probe rather than guessed.
2. **A file-level "does this file mention a reason" check is worthless.** The
   first version of the disabled-control guard passed immediately, because a
   file with one `ConstraintNote` and five bare disables satisfies it.
   Per-site with a per-file budget is what actually found the 30.
3. **`.constrained(by:)` cannot be used everywhere.** It stacks the note under
   the control, which is right for forms and status bars and wrong for toolbar
   items and horizontal button rows. Those keep a bare `.disabled(` and state
   the reason another way; the test's budget records which and why, so the
   exception is a decision rather than an oversight.
4. **Per-row controls get one reason, not one per row.** The phase 4 sidebar
   lesson applied again: Free DL's row toggles and Rekordbox's strip buttons
   share a single note rather than repeating it per control.
5. **Two of the three tolerant decoders already existed.**
   `RekordboxSyncConfig` had hand-written `decodeIfPresent` since before the
   redesign. The class was not that nobody knew the rule — it was that the rule
   was applied by hand, three times, and missed everywhere else.

**Evidence:**

- `xcrun swiftc -swift-version 6 -strict-concurrency=complete -typecheck` over
  all app sources and, through the XCTest shim, the test sources: clean.
- The decoding and defaults assertions were additionally **executed** against the
  real app sources by compiling them into a runner: **21/21 passed**, covering
  null and absent collections for `InitializeResult`, `DoctorResult`,
  `CredentialsListResult`, `SourceCapabilitiesResult`, `PlaylistListResult`,
  `ProviderPlaylistListResult`, `PlaylistConfig`, `FreeDLConfig`,
  `RekordboxSyncConfig`, `RekordboxInspectResult`, `MainConfig`,
  `OnboardingState`, `SelectRowsParams`, `SourceSnapshot`, plus the
  `SyncDefaults` constants.
- Both source rules were run against the tree: `alertMessage` appears only in the
  five allowlisted functions; no file exceeds its bare-`.disabled(` budget.
- `make app-dev` builds and ad-hoc signs `dist/dev/UDL-Dev.app`.
- `git diff --stat internal/` is empty; `go vet ./...` and `go test ./...` pass.
- **Fresh install re-verified** — a second empty `XDG_CONFIG_HOME` launched
  straight to onboarding with no modal, which is the end-to-end proof of
  workstream A: every nil collection arrives at once on that path.
- **Real config re-verified** — 6 sources, 28 checks passed, 1 playlist, runtime
  ready: the wrapper did not change how populated data decodes.
- **B seen live in light and dark** — Config's status bar shows "The draft
  matches the file that was read." under a dimmed Revert and "The draft matches
  the file on disk." under a dimmed Save; Rekordbox's row filter reads "Generate
  a plan to filter its rows."; the Free DL stepper reads "freedl.plan.start has
  not produced a capture plan yet.", "A promotion plan is built from a finished
  capture run." and "There is no promotion plan to apply." on steps 2, 3 and 4.
- Home is unchanged when idle, which is the point: `busyReason` is nil and
  `.constrained(by: nil)` adds nothing.
- **Not exercised:** no failing RPC was provoked against the real backend, so the
  re-routed failure callouts were verified by reading the routes and by the
  allowlist test rather than by forcing each error live. Nothing that writes was
  run against the user's real configuration.

---

## Decision Log

### 2026-08-03 — phase 9, the four classes swept

- Treated each of phase 8's four defects as a class rather than an instance,
  because three of the four had already been fixed by hand somewhere else in the
  app and missed everywhere else. A rule applied by hand is not a rule.
- Chose a property wrapper over per-type `init(from:)` for the null-collection
  class: 20-odd hand-written decoders would have been 20-odd chances to forget
  one, which is exactly how the class arose.
- Extended it to dictionaries as well as arrays. Go marshals a nil *map* as
  `null` too, and `feature_config_paths`, `capabilities` and
  `resolved_dependencies` are all non-optional maps with the same latent defect.
- Kept the wrapper off outbound-only params. They are constructed by the app and
  never decoded, so tolerance there would be tolerance for nothing.
- Made `WorkflowStatus` `ExpressibleByStringLiteral` so the ~20 informational
  status assignments did not have to change. Only a failure names a severity,
  which keeps the diff about the rule rather than about syntax.
- Separated `.canceled` from `.failure`. A canceled run is not a failed one, and
  C11 already established that the two must not read alike.
- Left five `alertMessage` sites as modals and wrote down why at the declaration:
  a backend that will not launch, will not restart, sent a frame the app cannot
  decode, or is blocked waiting for a reply that cannot be delivered. None of
  those belongs to a screen.
- Enforced both rules by reading the sources from a test. Neither "a failure
  belongs on its screen" nor "a disabled control states its reason" is
  expressible as a runtime assertion, and both had already been violated
  silently.
- Budgeted the surviving bare `.disabled(` sites per file with a justification
  each, rather than pretending `.constrained(by:)` fits a toolbar item. An
  exception that is written down is a decision; an exception that is not is the
  defect this phase exists to remove.
- Recorded that `@DefaultEmpty` must have no zero-argument initialiser. A default
  on `wrappedValue` silently changes every synthesised memberwise initialiser,
  and the error it produces points at the call site rather than the wrapper.

### 2026-08-02 — phase 8 implemented, redesign closed

- Closed the two carried verifications before writing any documentation, on the
  grounds that a completion criterion nobody has seen live is not met. Both
  found defects, which is the argument for the ordering.
- Made `syncDryRun` default to `true`. The Sync screen already claimed in a code
  comment that dry run was the default; the initialiser said otherwise, so
  reaching Run Sync from the sidebar offered a live run. C1's whole point is
  that the only way to see a plan is to start a run, so that run has to be
  reversible by default.
- Required a state file in `SourceEditorView` and in `ConfigValidation` rather
  than letting `config.validate` refuse the save. The backend stays the
  authority; the app simply stops offering a draft it knows will be rejected.
- Rendered a refused config write on the onboarding screen itself. `AppState`
  records the reason in fields only the Config screen reads, which made the one
  action on the onboarding screen fail silently.
- Fixed the restart race in `AgentProcess` by tearing down in `stop()` rather
  than depending on `Process.terminationHandler`, and guarded the handler on
  process identity so a late callback cannot delete the replacement. This is a
  backend-process fix, not a view fix, and it is the reason Finish Setup could
  never complete.
- Decoded `"playlists": null` as an empty list. The rule was already in
  `AGENTS.md`; the entry now names `playlists.config.read` specifically, because
  a rule stated abstractly did not stop one DTO from breaking it.
- Replaced `.help(_)`-only reasons on two disabled primaries with on-screen
  notes. A tooltip is not an adjacent stated reason.
- Added `.dev/ui`, a CGEvent click/type/key tool, alongside the existing
  screenshot harness, and recorded in `AGENTS.md` that SwiftUI text bindings
  commit on focus loss so an accessibility write must be followed by Tab.
- Did not run anything that writes against the user's real configuration. The
  C4 dry run was cancelled; onboarding wrote only into a disposable
  `XDG_CONFIG_HOME`.

### 2026-08-01 — phase 7 implemented

- Dropped the mockup's "Export snapshot…" button. `playlists.list` reports no
  snapshot file path and there is no export method, so the button would have
  written a file this app invented. The inspector states the omission, the same
  treatment C16 gives Home's missing panels.
- Made the YAML tab show `config.yaml` **as read from disk**, not a rendered
  preview of the draft. There is no method that canonicalises a config without
  writing it — `config.validate` returns `{"valid": true}` and `config.writeFile`
  returns canonical text only after saving — so a draft preview would be this app
  guessing at udl's YAML output. The callout says so and turns to a warning while
  the draft differs.
- Modelled the three `*bool` sync-policy fields as a tri-state `Picker`
  (`TriStateBool`). A `Toggle` would have written an explicit `false` into every
  previously-unset key the first time anyone saved. Verified by encoding: an
  unset field is absent from the JSON, not `false`.
- Gave C14 its own value (`AppState.ConfigConflict`) rather than a status string,
  and made overwrite a separately named call
  (`saveMainConfig(overwritingExternalChanges:)`) rather than a retry, because
  dropping the expected SHA is the whole difference between the two outcomes.
- Replaced `playlistStatusMessage` with `PlaylistStatus`, carrying a severity and
  a `preservedPreviousSnapshot` flag. C11 requires a failed or canceled refresh
  to read differently from a successful one, and a bare string rendered every
  outcome as the same blue callout.
- Made the source id read-only in the inline form. It names the state file and is
  what `--source` matches, so renaming in place orphans state rather than
  renaming it; Duplicate takes the next free `-copy` id instead.
- Extracted `MusicDuration` to the design system. Playlists reads the same
  `duration of t as text` field Rekordbox parses, and a second copy of that
  parser would have been a second chance to print `266,029` under "Time".
- Added a second note to "Missing locally": it is what udl saw when the snapshot
  was taken, not now. Without it the cache being a cache looks like a bug.
- Extended `BoundedContent` to the config screen after the C14 banner blew the
  window out — a `Form` reports its full ideal height exactly as a `Table` does,
  so the phase 4 rule is about unbounded intrinsic content, not about `Table`.
- Recorded that a `SidebarContext.select` closure must write through the
  projected binding. Assigning to the `@State` property directly writes into the
  view value the context was built from and the click silently does nothing;
  `RekordboxView` carries the same latent bug on its plan-target rows.
- Did not run a playlist refresh. It contacts Music.app and rewrites a snapshot,
  and none was requested; the confirmation dialog was opened to verify its
  wording and dismissed. Did not click Overwrite in the C14 conflict; the config
  file was backed up before the test and restored to its original SHA after.

### 2026-08-01 — phase 6 implemented

- Gave the Free DL stepper **four** steps rather than the mockup's three, because
  C7 is a statement about four RPCs with four run IDs and collapsing two of them
  would hide the seam the constraint exists to show.
- Dropped the mockup's "Unlimited" plan-limit switch. A Free DL job's
  `plan_limit: 0` is normalised to the defaults limit (50), so unlimited does not
  exist here at all — unlike the sync plan limit in C6. The field states this
  rather than offering a toggle that silently means 50.
- Made a streaming row read `Checking`, not `Blocked`, after a live run showed 26
  rows claiming to be blocked while their probes were still in flight. The rule is
  the *absence* of a skip reason: `recomputeSelectable` only writes one once the
  source plan is enumerated, and a genuinely blocked row always carries one.
- Dropped the mockup's Score and Artist columns from the plan and capture steps;
  neither exists in `freedl.plan.event`. The score appears on step 3, where
  `freedl.promotionPlan.build` actually reports it.
- Showed two Rekordbox apply preconditions as **unknown** rather than as ticks. The
  protocol reports no process state and no database state before a run reaches
  `CheckRekordboxClosed`, so a green tick there would be one this app cannot earn.
  They turn red only when a real refusal names them.
- Added `RekordboxObstacle`, constructed only from a backend refusal message, and
  `RekordboxApplyGate`, which derives the plan-bar pill, the primary label, the
  dry-run state and the help text together so they cannot disagree. This is what
  produces C10's *"Rekordbox is open — quit it to apply"*.
- Parsed the plan's `duration` into m:ss. It is `duration of t as text` from
  Music.app's AppleScript — seconds as a real in the system locale — and the first
  live capture rendered `266,029…` under a column headed Time.
- Split the Rekordbox status bar into changed rows and removals. `will_remove`
  counts Rekordbox content with no Music row, so it can never appear in the table;
  one combined "changes" number made the filter and the status bar disagree.
- Added `Theme.separatorStrong` (`--sep-2`) to the design system for the stepper's
  connector and unreached-step ring, which vanish against `--sunken` under `--sep`.
- Did not run capture, promotion build, promote apply, or a real Rekordbox apply.
  Each writes files or a database and none was requested; their surfaces were
  verified in the "Not run yet" and blocked states that C7 and C10 govern.

### 2026-08-01 — phase 5 implemented

- Dropped the mockup's Info tile rather than shipping a control that can only
  ever read 0: `methods_doctor.go` derives status from severity and spells
  `info` as `ok`, so info-severity checks are already counted as passed. The
  inspector states it.
- Derived Credentials' "Used by" column from `sources.capabilities` instead of
  inventing a workflow list, mirroring the adapter rules `internal/doctor` uses
  to decide which credential checks to emit. Where nothing uses a credential the
  cell says so.
- Extracted `DoctorFix` so Home and Doctor cannot route the same check to two
  screens, and deleted `HomeView.isCredentialCheck`. The routing is still a
  keyword match — `DoctorCheck` has no machine-readable target — but there is
  now exactly one of them, and a check with no recognisable target falls back to
  Doctor rather than guessing.
- Re-ranked `external_override` from `.ok` to `.warn`: a credential that works
  but is not the one this app manages is not a pass. `unhealthyCredentials`
  still counts only `.error`, so no badge changed.
- Fixed the override detection itself. The old test was
  `storage_source == "external_override"`, a value the backend never sends;
  `external_override` is a *health*, and the storage source is `env` or
  `spotdl_config`. C13 had therefore never rendered. It does now.
- Made the sidebar's contextual rows `Button`s. Phase 4 fixed rendering the
  section; the rows still swallowed every click, because `.onTapGesture` inside
  a `List(selection:)` loses to the list's own row hit-testing. Playlists
  carried the same latent bug.
- Added `BoundedContent` after finding that phase 4's `GeometryReader` bound
  over-reports by the status bar's 46pt safe-area inset, so anything stacked
  under a `Table` is drawn beneath the status bar. Neither `.layoutPriority`
  nor `.safeAreaInset` reclaims it, because a `Table` does not yield height to a
  sibling. Every table-hosting screen uses the container from here on.
- Corrected `Theme.sunken`. `underPageBackgroundColor` is #969696 under Aqua,
  which turned every card header and severity tile into a grey slab in light
  appearance. Phase 1's light captures had never included a tile.

### 2026-07-30 — phase 4 implemented

- Split `SyncView` into a router plus three surfaces, and made the plan surface
  outrank the run surface: while `ui.selectRows` is open the backend is stopped,
  so the plan is not a detour from the run, it *is* the run's current state.
- Removed `.selectRows` from the modal sheet entirely rather than leaving both
  paths alive. `AppState.modalPrompt` is the filter, so there is exactly one
  place a plan can be answered from.
- Decoded the plan prompt in `AppState` instead of the view, because the sidebar,
  the header counter, and the status bar all need the source ID, and decoding it
  three times would have been three chances to disagree.
- Made a plan prompt route the app to `.sync` on arrival: a docked surface nobody
  navigates to is worse than a sheet.
- Dropped the mockup's Artist and Time columns; `ui.selectRows` has no such
  fields. The plan inspector states the omission rather than leaving a reader to
  wonder.
- Kept the plan inspector's run options read-only. `SelectRowsResult` can carry
  only the order and the window, so editable plan-limit or dry-run controls on
  that screen would have been controls that do nothing.
- Modelled `askOnExisting` as a three-state policy because
  `ask_on_existing_set` makes "udl decides" a real state; the default reproduces
  the previously hardcoded `false`/`false` exactly.
- Added `SyncRunState.requestedSourceIDs`, without which "Source 2 of 4" and a
  never-reported queued source have no honest source of truth.
- Changed `SidebarView` to state a section's unavailable reason once instead of
  once per row, after four identical notes made the sidebar unreadable.
- Fixed two shell bugs this phase exposed. An unbounded `Table` in the detail
  column outgrows the window and takes the toolbar, the status bar and the sidebar
  with it; a `GeometryReader` giving the stack a definite height fixes it, and the
  same latent bug in `FreeDLView`/`PlaylistsView`/`RekordboxView` belongs to phases
  6 and 7. Separately, the sidebar's contextual slot had never worked: it published
  from `onAppear` and cleared unconditionally from `onDisappear`, and SwiftUI runs
  the outgoing screen's `onDisappear` after the incoming screen publishes. Phase 2's
  claim that Playlists proved the slot is corrected in place.
- Made a source that never reported read `Not run yet` rather than `Queued` once the
  run has ended, so the chip stops implying work that is still coming.

### 2026-07-30 — phases 1–3 implemented

- Added `Typography`, `Cards.swift`, and `EmptyStateView.swift` to the design system
  beyond the planned file list; each one exists to make a rule the plan already states
  enforceable (no hardcoded font sizes; one card/field vocabulary; three visually
  distinct kinds of empty).
- Used `.navigationTitle` + `.navigationSubtitle` for the toolbar's leading title
  instead of a custom toolbar item, after the custom item duplicated the window title.
- Made the inspector per screen with shared visibility in `ShellChrome`, rather than one
  shell-level inspector, because inspector content depends on per-screen state.
- Implemented the sidebar's pinned System section as a second `List` sharing the
  selection binding, since SwiftUI cannot pin a trailing section inside one `List`.
- Modelled `SidebarContext` as a published store plus a `.sidebarContext(_:)` modifier
  carrying a `select` closure; `PlaylistsView` is the first adopter and its former
  in-window definitions column is gone.
- Replaced the unexpected-EOF alert with the C15 recovery banner plus
  `AppState.notResumed`, and made in-flight workflow state resolve honestly instead of
  spinning on a run that no longer exists. `notResumed` survives a restart on purpose.
- Renamed `Destination` cases to the mockup labels and moved Settings out of the sidebar
  into a real `Settings` scene.
- Recorded that several later-phase constraints (C3–C6, C7–C14) received partial visual
  treatment during the phase 1 retint. They stay unchecked in the coverage table; each
  owning phase still has to finish and verify them.

### 2026-07-30

- Archived the completed native macOS frontend plan and implementation tracker to
  `plans/archive/`.
- Created this redesign plan and tracker.
- Decided the backend does not change: where the mockups and the agent protocol
  disagree, the protocol wins and the constraint is made visible instead.
- Decided to follow system light and dark with the system accent color, replacing
  the forced-dark `ControlRoomTheme`.
- Decided Home shows only protocol-backed values; the mockup's library statistics
  and run history are dropped rather than fabricated or stored app-side.
- Decided against adding a `sync.plan` preview RPC, which would have made
  `sync-plan.html` literally buildable; the docked prompt with explicit
  per-source lifecycle is the approved alternative.
- Decided on phased delivery, one pull request per phase, each leaving the
  application launchable.
