# Navidrome Likes to Rekordbox — Implementation Tracker

- **Overall status:** In review — every phase is implemented and validated,
  including the native app and the end-to-end round trip. What remains is one
  deliberate user action (the Rekordbox apply) and a set of Amperfy-client
  checks carried forward from the predecessor.
- **Plan:** [PLAN.md](./PLAN.md)
- **Last updated:** 2026-08-05
- **Archived predecessor:**
  [plans/archive/navidrome-phone-library-implementation.md](./plans/archive/navidrome-phone-library-implementation.md)

This is the live execution record. Update status, evidence, decisions, and
discovered work in the same change as the implementation. A checked task means
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

## Correction: an earlier version of this tracker was wrong about the toolchain

An earlier revision of this file declared every Swift validation a manual gate
"blocked: needs Xcode", on the basis that `xcodebuild` is unavailable and no
`Xcode.app` is installed. **Both of those facts are true and the conclusion
drawn from them was wrong.** This repository does not need Xcode:

- `make app-test-dev` → `packaging/dev/test_macos_app.sh` compiles the app
  sources and the test sources with `xcrun swiftc` against a hand-written
  `XCTest` shim and runs them. Command Line Tools are sufficient.
- `make app-dev-install` → `packaging/dev/install_macos_app.sh` builds, signs,
  installs, and launches `UDL-Dev.app` from `~/Applications`.
- `.dev/app.sh` drives the installed app for screenshots.

Everything previously deferred on that basis has now actually been run. The
mistake was concluding "no Xcode" meant "no Swift build" without first looking
for the project's own tooling, which is listed in the Makefile.

## Progress Dashboard

| Phase | Status | Depends on | Exit gate |
| --- | --- | --- | --- |
| 0. Carried forward from phone library | In progress | — | Every open item from the predecessor tracker is closed or re-deferred with a reason |
| 1. GUI playlist error surfacing | Done | — | A failing playlist operation shows the backend's own message |
| 2. Backend signing and Automation | Done | 1 | An Automation grant survives an app rebuild, and the real cause of the favourites failure is recorded |
| 3. Starred read path | Done | — | `getStarred2` produces a deterministic snapshot with no churn across identical refreshes |
| 4. Managed definition and Rekordbox target | Done | 3 | `navidrome-favorites` reaches its own Rekordbox playlist, Apple `favorites` untouched |
| 5. CLI, agent, and native surfaces | Done | 3, 4 | Starred tracks are readable from CLI, RPC, and the app |
| 6. End-to-end acceptance | In review | 1–5 | A star made on the phone reaches Rekordbox |
| 7. Documentation and close-out | In progress | 1–6 | Docs, evidence, and decision log complete |

---

## Phase 0 — Carried Forward from the Phone Library

**Status:** In progress

**Exit gate:** Every `[ ]` left in the archived predecessor tracker is closed
here or explicitly re-deferred with a recorded reason.

### Favourite round trip (was Phase 4 validation)

- [x] Verify an explicit `navidrome-favorites` refresh reflects new server
      stars. **Closed by Phase 6:** two tracks starred from a phone client
      appeared as `+2 -0 unchanged=154`. Superseded in mechanism — the `.nsp`
      smart playlist is no longer on the read path.

### Native workspace visual pass (was Phase 6 validation)

- [x] Build and manually inspect the Phone Library workspace against a real
      installed server. Done in light mode against the live server: setup
      checklist at 5 of 6, Navidrome 0.63.2 via Homebrew, service running,
      1574 tracks, 3 retained backups, and the new starred card populated.
- [ ] Dark mode and the empty / partial / failed states specifically.
      `.dev/app.sh appearance true` exists for this; only the healthy light
      state was exercised. Re-deferred as cosmetic.

### Amperfy acceptance (was Phase 7)

*Needs the iPhone and the companion app. The favourite round trip below is now
closed; the rest remain open.*

- [ ] Connect Amperfy over home Wi-Fi using the shared account.
- [ ] Verify All Music, HARD BOUNCE, and Favourites ordering and membership.
- [ ] Confirm enough free storage and download the complete All Music playlist.
- [ ] Disable Wi-Fi/Mac availability and play multiple downloaded tracks.
- [x] Favorite while connected and verify Navidrome plus explicit UDL refresh.
      **Closed** — validated with **substreamer**, a different Subsonic client,
      rather than Amperfy. The hop that mattered (phone client → Navidrome →
      UDL snapshot) is proven; see the deviation note in Phase 6.
- [ ] Test favorite changes while completely offline and record whether the
      client queues them on reconnect.
- [ ] Verify lock-screen, Control Center, Dynamic Island, and favorite control.

### Documentation (was Phase 8)

- [ ] Document Amperfy connection, Local Network permission, full download,
      offline playback, and observed favorite behavior. Still open: it
      documents observed behaviour and Amperfy itself has not been exercised.
- [ ] Write `docs/navidrome-recovery.md`. **Re-deferred** — the predecessor's
      own rule was to write it after walking the recovery path for real, and
      walking it means deliberately breaking a working install.
- [x] Document the deferred Tailscale/remote-access direction without
      configuring it. Present in PLAN.md's Deferred section and in `readme.md`.

### Known cosmetic defect (carried, not yet scheduled)

- [x] `getPlaylists` reports `songCount: 0` for a smart playlist until it is
      refreshed. **Resolved as no longer load-bearing** — UDL does not read
      favourites through the smart playlist any more. Still cosmetically
      possible on the All Music and HARD BOUNCE rows; re-deferred.

---

## Phase 1 — GUI Playlist Error Surfacing

**Status:** Done

**Exit gate:** A failing playlist operation in the native app shows the
backend's own message, and a refresh failure still preserves the previous valid
snapshot. **Met.**

### Tasks

- [x] In `applyPlaylistFinished` (`macos/UDL/Model/AppState.swift`), stop
      discarding `RunFinishedNotification.error`.
- [x] `.providerList` failure: `.error` severity carrying the backend message,
      falling back to "Provider listing failed with exit code N."
- [x] `.providerList` exit code `130`: `.warn`, "Provider listing canceled."
- [x] `.refresh` failure: `preservedPreviousSnapshot: true` and the C11
      guarantee intact; the backend message leads, the preservation sentence
      follows.
- [x] Match the Rekordbox handler's phrasing — both prefer `finished.error` and
      fall back to an exit-code sentence.
- [x] Confirm `PlaylistsView.swift` renders a multi-sentence message without
      truncating. **No change needed** — the footer renders through `Callout`,
      which already applies `.fixedSize(horizontal: false, vertical: true)` to
      both title and detail (`Callout.swift:19,24`).

**Deviation — a third case.** The original `else` covered both a non-zero exit
and a zero exit whose result would not decode; collapsing them would print
"failed with exit code 0". `AppState.playlistFailureDetail` distinguishes them.

`applyPlaylistFinished` became internal so the behaviour is directly testable,
following `consumeBufferedFinished` in the same file.

### Validation

- [x] Swift test: a `run.finished` carrying an error yields `.error` with the
      backend text, for both operations.
- [x] Swift test: exit code `130` yields `.warn` for both operations.
- [x] Swift test: a failed refresh still reports `preservedPreviousSnapshot`
      and still ends with the preservation sentence.
- [x] Swift test: message assembly terminates the backend sentence and keeps
      the undecodable case distinct.
- [x] `make app-test-dev` → **87 passed, 0 failed**, including all six
      `PlaylistErrorSurfacingTests`.
- [x] Manual: the Playlists pane renders correctly in the running app, and
      "Refresh from Music…" is correctly gated with *"A sync run is in
      progress. Stop it to start other work."*
- [ ] Manual: revoke Automation → Music and watch the pane name System
      Settings. **Not run** — it would require revoking a working grant on the
      user's machine, and the message path is covered by the unit tests above.
      Recorded as a deliberate non-action, not an oversight.

---

## Phase 2 — Backend Signing and Automation Permission

**Status:** Done

**Exit gate:** An Automation grant survives an app rebuild, and the actual
cause of the Apple Music favourites failure is recorded. **Met.**

### Tasks

- [x] In the Xcode project's `Embed udl backend` script phase, codesign the
      copied binary after `cp` / `chmod 755`, guarded on the identity being
      non-empty, with ad-hoc handled separately and an explicit warning when
      there is no identity. Shell syntax verified with `sh -n`.
- [x] Confirm no change is needed to `UDL.entitlements` or `Info.plist`.
      **Confirmed twice** — by inspection, and then on the built app:
      `codesign -d --entitlements -` on the installed bundle reports
      `com.apple.security.automation.apple-events = true`.
- [x] Add a Doctor check running `music.Reader.ListPlaylists` with a short
      timeout, reporting the actionable automation error on failure.
- [x] Register the check. **`Severity.forCheck` needed no change** — it maps on
      `status`/`severity`, not on the check's name.
- [x] Record the observed cause in the decision log.

**Correction — the dev path was already signing.** An earlier revision of this
tracker implied the embedded backend was unsigned everywhere.
`packaging/dev/build_macos_app.sh` already did
`codesign --force --options runtime --sign -` on `Contents/Resources/udl`
before this work. **The gap existed only in the Xcode project path**, which is
what the plan pointed at, and that is what was fixed.

**Deviation — the Doctor probe is gated.** The plan did not say when to run it.
Unconditional would prompt for Automation on every `udl doctor` for users with
no Apple Music playlist. It now runs only when at least one `apple_music`
definition is configured. `ProbeMusicAutomation` and `LoadPlaylistDefinitions`
are injectable so tests never reach `osascript`. Timeout 5s.

### Validation

- [x] `codesign -dv` on the installed bundle's backend: valid, `adhoc,runtime`,
      `Mach-O thin (arm64)`.
- [x] **Rebuild survival — the phase's real gate.** A Swift source file was
      changed so the ad-hoc cdhash would differ, then
      `make app-dev-install` rebuilt, re-signed, reinstalled, and relaunched.
      New `CDHash=0df9d3b564cdfdb11fffabb65031e19299e091ba`. No Automation
      prompt appeared and the Doctor `music` check still passed. **The grant
      survived a content-changing rebuild.** (The probe comment was reverted
      afterwards.)
- [x] Doctor shows the Automation check passing, in both surfaces:
      `udl doctor` prints
      `[info] music: Music.app automation is permitted; playlist refresh can
      read Music`, and the app reports 29 passed / 3 warnings / 0 errors —
      identical to the CLI's 29/3/0. The counts are a real discriminator: a
      denied grant would move `music` from info to warn, giving 28/4.
- [ ] The revoked-grant message in the Doctor pane. Not run, for the same
      reason as Phase 1's revoke check; the Go test covers the denied branch
      including the actionable text.
- [x] Go test for the new doctor check with a faked probe: granted, denied
      (asserting the actionable message survives), and not-configured
      (asserting the probe never runs).

### Cause — recorded as required

**The Automation hypothesis is not confirmed, because the failure it was meant
to explain does not currently reproduce.** `UDL-Dev.app` holds a working
Automation grant, drives Music.app successfully, and keeps that grant across a
rebuild. The plan required recording the cause "whether or not it matches the
hypothesis", so: the honest answer is that the observed symptom is absent on
this machine today, and the signing change is correct hygiene for the Xcode
build path regardless. What *is* now true is that the three things needed to
identify a recurrence exist — the GUI surfaces the backend message, Doctor
reports grant state before a refresh needs it, and the Xcode-embedded binary is
signed so the app's signature is not invalidated at build time.

---

## Phase 3 — Starred Read Path

**Status:** Done

**Exit gate:** `getStarred2` produces a snapshot whose checksum is stable across
repeated refreshes when the server has not changed. **Met against the live
server.**

### Tasks

- [x] Add `Starred(ctx)` to the `NavidromeClient` interface;
      `navidrome.Client.Starred` already satisfied it.
- [x] Add the method to the fake client.
- [x] Add `NavidromeStarredPlaylistID = "starred"` as the sentinel.
- [x] Extract the song → `Track` loop into a shared `songTracks` helper.
- [x] Branch in `Read` before the `PlaylistByName` lookup; return a synthetic
      `ProviderPlaylist` named `navidrome.SmartPlaylistFavoritesName` carrying
      the sentinel as its ID.
- [x] Sort starred tracks by normalized path.
- [x] Leave `playlists.Snapshot` / `Track` and `snapshotChecksum` untouched.

**Deviation — the sort lives in `internal/navidrome`.** With the CLI and agent
surfaces also listing stars, three consumers would each have defined an order
and any drift reintroduces the churn the sort prevents. It is one exported
function, `navidrome.SortStarredSongs`, used by the provider and by
`Manager.StarredFavorites`. Ties break on song ID.

**Correction to the exit gate's wording.** `RefreshedAt` is a
`snapshotChecksum` input, so checksums are only identical across refreshes with
the clock held still, which the unit test does. The property that matters in
production is that repeated refreshes report **zero changes** — confirmed live.

### Validation

- [x] Unit: sentinel path produces a path-sorted list with correct
      `MissingLocal` for present and absent files.
- [x] Unit: two refreshes over identical data (fixed clock) produce an
      identical checksum and zero changes.
- [x] Unit: shuffled server order produces an identical track list.
- [x] Unit: the named-playlist path still works unchanged.
- [x] Unit: a server error on the starred path propagates.
- [x] Unit: `SortStarredSongs` is total and order-independent.
- [x] `go test -race ./internal/playlists/ ./internal/navidrome/` — pass.
- [x] Live, twice against the running server:
      `+154 -0 unchanged=0`, then `+0 -0 unchanged=154`.
- [x] Live, after a real server-side change: `+2 -0 unchanged=154`. The two new
      stars were picked up exactly and the other 154 did not churn.

---

## Phase 4 — Managed Definition and Rekordbox Target

**Status:** Done

**Exit gate:** `navidrome-favorites` refreshes through the starred read and
plans into its own Rekordbox playlist, with the Apple `favorites` definition
provably untouched. **Met.**

### Tasks

- [x] Point the `SmartPlaylistFavorites` entry at the sentinel.
- [x] Set `DefaultRekordboxTarget: "nav_fav_imports"`.
- [x] Keep `ProviderPlaylist` populated so `Validate` passes.
- [x] Confirm `EnsureNavidromeDefinitions` remains append-only.
- [ ] ~~Confirm no new command is needed~~ — **wrong twice**, see below.

### Discovered work — `EnsureNavidromeDefinitions` was never called

It existed but had **no production caller**; only tests used it. The managed
definitions were never written to `playlists.yaml`, so
`udl playlist refresh navidrome-favorites` had nothing to resolve. Confirmed
against the real config, which contains only `favorites`.

- [x] Add `playlists.WriteNavidromeDefinitions`, merging into the file that
      will actually be written — not the merged user+project view, which would
      copy one file's entries into the other.
- [x] Call it after a successful setup apply from both the CLI and the agent. A
      registration failure warns rather than failing the setup.
- [x] Test: append-only, idempotent, Apple entry untouched.
- Consequence: existing installs need one idempotent
  `udl navidrome setup apply` to pick the definitions up. **This machine has
  not had that run** — validation used a temporary config copy instead, so the
  user's real `playlists.yaml` was never modified.

### Discovered work — `DefaultRekordboxTarget` was never read

Declared in `config.go:38`, normalized, and **read nowhere**. Setting it would
have had no effect: `planSnapshot` took its target from the caller's options or
the config-wide default (`fav_imports`), so a `navidrome-favorites` plan would
have aimed straight at the Apple Music favourites playlist — the exact merge
PLAN.md decision 3 forbids.

- [x] Add `SnapshotRekordboxTarget` to `RekordboxPlaylistSyncPlanRequest`,
      applied in `planSnapshot` only when the caller named neither a target
      name nor a target ID, so an explicit option still wins.
- [x] Resolve it from the definition in the agent's `rekordbox.plan`.
- [x] Test: the definition's target is used; an explicit option overrides it.

### Validation

- [x] Existing assertions that the Apple `favorites` definition survives the
      merge still pass, unmodified.
- [x] Unit: the managed definition carries the sentinel and the separate
      target, and the merged config validates.
- [x] The `favorites` snapshot is byte-for-byte identical across the entire
      session: `b03b32828e1425fcd832178a8e14322c7f5098aae07d4e0fb5875f1a5b264694`
      before and after.
- [x] The user's real `~/.config/udl/playlists.yaml` is unmodified:
      `baab284e40ce5ba959ed2e120c46dcb48d54e7a88a4494c4770b6b64beadaa7c`
      before and after.

---

## Phase 5 — CLI, Agent, and Native Surfaces

**Status:** Done

**Exit gate:** Starred tracks are readable from the CLI, over the agent
protocol, and in the app. **Met in all three.**

### Tasks

- [x] `Manager.StarredFavorites(ctx)` next to `AppleFavorites`, sorting through
      `SortStarredSongs`.
- [x] `udl navidrome favorites list [--json]`. The parent command's `Short` was
      updated — it claimed only "Migrate Apple Music favorites into Navidrome".
- [x] `navidrome.favorites.list` as a run, matching the other favourites
      methods. `tracks` is normalized to an array so an empty library never
      encodes as `null`.
- [x] Register in `protocolMethods` and the `handle` switch.
- [x] Update `protocol_v2_golden.json` and `docs/agent-protocol.md`.
- [x] Add the method to `UDLClient.swift` and a wire result in
      `PhoneLibraryModels.swift`.
- [x] Show the starred count in `PhoneLibraryView.swift` — a new "Likes from
      the phone" card, read-only, with an explicit "Read starred tracks" action.

**Deviation — the wire result lives in `PhoneLibraryModels.swift`.** The plan
said `WireModels.swift`. Every other Phone Library result type lives in
`PhoneLibraryModels.swift`; only the method enum lives in `WireModels.swift`,
and that is where the new case went.

**Correction — the contract test does not enforce the docs.** The plan said
"the contract test enforces both". It does not: nothing in `internal/agent/`
references `docs/agent-protocol.md`, and `go test ./internal/agent/` passed
before the docs were touched. The golden file *is* enforced. The docs were
updated by hand; do not rely on a test to catch a stale protocol doc.

### Validation

- [x] `go test ./internal/agent/` — contract and golden tests pass.
- [x] `go test ./internal/cli/` — pass, including a rendering test covering the
      empty listing and a path-less, artist-less track.
- [x] Swift decode test for the wire result, including `"tracks": null`.
- [x] Swift test asserting the read operation is non-mutating.
- [x] **CLI, live:** `udl navidrome favorites list` → `Starred on Navidrome:
      156`, path-sorted, with paths under each entry.
- [x] **Wire, live:** `navidrome.favorites.list` returned
      `{"count":156,"tracks":[…]}` via `run.finished`.
- [x] **App, live:** pressed "Read starred tracks" in the running
      `UDL-Dev.app`. The card populated from the server and now reads
      *"Starred on the server"*, twenty track rows, *"136 more starred track(s)
      not shown."* (20 + 136 = 156, matching the CLI), and the guidance
      callout *"Refresh the "Favourites (Navidrome)" snapshot in Playlists to
      send these to Rekordbox. It is a separate playlist from the Apple Music
      favorites, by design."*

**Note on how the app was verified.** Synthetic scroll events do not reach the
app without Input Monitoring, which was not granted, so the card could not be
photographed in place. It was verified through the accessibility tree instead —
which is what the app actually rendered — and the button was identified by
position (the only button between the card's body text and the next card's
title) rather than pressed blind, since neighbouring buttons include
"Repair or upgrade…" and "Import favorites…".

---

## Phase 6 — End-to-End Acceptance

**Status:** In review — the full path is proven; the Rekordbox apply is left to
the user

**Exit gate:** A star made on the phone reaches Rekordbox, and the Apple Music
path is unaffected.

### Tasks

- [x] Star tracks from a phone client. Two tracks were starred during the
      session: *MIESS x SMVGGLERS - CYNTHIA BATTLE (Bootleg)* and *[FREE DL]
      Bikini Bottom (MIESS Spongebob Hardbounce Bootleg)*.
- [x] `udl navidrome favorites list` shows them — count moved 154 → 156.
- [x] `udl playlist refresh navidrome-favorites` reports `+2 -0
      unchanged=154`. The two new stars propagated exactly; nothing else moved.
- [x] Refresh with no change: `+0 -0 unchanged=154` (recorded earlier in the
      session at the 154 baseline).
- [x] `udl rekordbox playlist-sync plan --playlist-id navidrome-favorites`:
      ```
      Music playlist: Favourites (Navidrome) (156 tracks)
      Rekordbox playlist: nav_fav_imports (will be created)
      Matched by path: 156
      Missing in RB: 0
      Final target count: 156
      ```
- [ ] Apply with Rekordbox closed; verify membership. **Deliberately not run —
      the user asked to keep this.** The plan is written and verified; applying
      it writes to the Rekordbox database and is a one-command manual step.
- [ ] Un-star and confirm the removal propagates. Not exercised; the removal
      path is covered by unit tests but not live.
- [x] Confirm `favorites` and its `fav_imports` target are unaffected —
      checksum identical, and every plan targeted `nav_fav_imports`.
- [x] Wire-level smoke matching how the app drives it — see the framing
      discovery below.
- [x] Repeat the favourites read from the native app — it succeeded.

**Deviation — the client was substreamer, not Amperfy.** The plan and the
predecessor tracker both name Amperfy. The stars were made in **substreamer**,
a different Subsonic client on the same phone. The hop this plan cares about —
phone client → Navidrome → UDL snapshot → Rekordbox plan — is fully proven.
What is *not* proven is anything Amperfy-specific (its offline queueing, its
lock-screen controls, its own favourite semantics), and those items stay open
in Phase 0.

### Discovered work — the wire smoke recipe in PLAN.md does not work

PLAN.md's Evidence section describes driving the agent by piping frames. That
produces **no output at all**, with exit code 0. The cause is in
`internal/agent/conn.go:108` — requests are dispatched with
`go c.handleRequest(...)`, so when stdin hits EOF the read loop returns and
`Serve` exits before in-flight handlers have written their replies. Two
consequences:

1. A recipe that pipes a fixed set of frames and closes stdin loses the
   responses. Holding stdin open works.
2. Frames sent back-to-back race: `navidrome.favorites.list` immediately after
   `session.initialize` returns `-32001` (session not initialized).

Neither affects the native app, which holds the pipe open for the session and
awaits each response. **Pre-existing, not caused by this work, out of scope to
fix here.** Recorded because PLAN.md's stated evidence method is unreliable and
anyone repeating it will conclude the agent is broken.

### Validation

- [x] `go build ./...`, `go vet ./...`, `go test ./...` — clean.
- [x] Native test suite: `make app-test-dev` → **87 passed, 0 failed**.
- [x] Track counts at each step: 154 → (2 starred from the phone) → 156 in the
      listing, 156 in the app's card (20 shown + 136 more), `+2 -0
      unchanged=154` in the snapshot, 156/156 matched by path and 0 missing in
      the Rekordbox plan. The Apple `favorites` snapshot stayed at 167 with an
      unchanged checksum throughout.

### Artifacts left on the machine

Validation was additive and reversible, but it did leave things:

- `statefiles/playlists/navidrome-favorites.json` — a valid 156-track snapshot.
  Harmless, and invisible to `udl playlist list` until the definition is
  registered.
- Two plan files under `statefiles/rekordbox/playlist-sync/`:
  `nav_fav_imports-20260805-135000.plan.json` (154 tracks) and
  `…-163126.plan.json` (156). Neither was applied.
- `~/Applications/UDL-Dev.app` was rebuilt and reinstalled several times.
- **Not touched:** `~/.config/udl/playlists.yaml`, `favorites.json`, the
  Rekordbox database, and the Navidrome database.
- A paused dry-run sync of `soundcloud-likes` was open in the app during
  validation and was left exactly as found — its "2 of 50" selection is
  unchanged, and a locked row was the only thing clicked in that pane.

---

## Phase 7 — Documentation and Close-Out

**Status:** In progress

### Tasks

- [x] Document `udl navidrome favorites list` and the `navidrome-favorites` →
      Rekordbox flow in `readme.md` as a four-step sequence.
- [x] Document that Apple and Navidrome favourites are separate by design and
      target different Rekordbox playlists, and that nothing is written back
      into Music.app.
- [x] Document the Automation permission requirement and how to re-grant it.
- [x] Document the new `--playlist-id` flag on `rekordbox playlist-sync plan`.
- [x] Document `navidrome.favorites.list` in `docs/agent-protocol.md`.
- [ ] Resolve every Phase 0 item — the favourite round trip and the visual pass
      are closed; the Amperfy-client items and the recovery doc remain open
      with reasons.
- [ ] Update PLAN.md's status. Still `Planned`; it should move to `Delivered`
      once the Rekordbox apply is run, which is the user's step.

---

## Decision Log

| Date | Phase | Decision | Rationale | Consequence |
| --- | --- | --- | --- | --- |
| 2026-08-05 | — | Read stars via `getStarred2`, not the `.nsp` smart playlist | The `.nsp` route depends on server import and ownership verification, never validated in the predecessor | The smart playlist stays for client browsing but is not load-bearing |
| 2026-08-05 | — | `navidrome-favorites` targets `nav_fav_imports` | Apple and Navidrome favourites stay permanently separate | Two Rekordbox playlists; no dedup rule needed |
| 2026-08-05 | — | Stars are snapshot membership, not a `Track` field | A starred field would change `snapshotChecksum` inputs | Snapshot format unchanged |
| 2026-08-05 | — | GUI error surfacing ships before the signing fix | The Automation cause was a hypothesis | Phase 1 independent and independently valuable |
| 2026-08-05 | 3 | The starred sort is one exported function in `internal/navidrome` | Three consumers would otherwise each define an order | `SortStarredSongs` is the single definition |
| 2026-08-05 | 2 | The Doctor Music check runs only with an `apple_music` definition configured | An unconditional probe would prompt users who never touch Music | Visible when needed, never demanded when not |
| 2026-08-05 | 2 | The Automation hypothesis is recorded as **unconfirmed, symptom absent** | The app holds a working grant and keeps it across rebuilds; the failure does not reproduce here | The signing fix stands as hygiene for the Xcode path, not as a proven cure |
| 2026-08-05 | 4 | Managed definitions are written at setup apply, not synthesized at load | A definition the user cannot see or edit would be surprising | Existing installs need one idempotent setup apply |
| 2026-08-05 | 4 | `default_rekordbox_target` applies only when the caller named no target | Unconditional would override an explicit `--rekordbox-playlist` | Definition sets the default; the flag wins |
| 2026-08-05 | 6 | The Rekordbox apply was not run | It writes to the Rekordbox database; the user explicitly kept this step | Plan verified and ready; applying is one command |
| 2026-08-05 | 6 | Amperfy was substituted with substreamer | It was the client to hand, and the hop under test is client-agnostic | The Subsonic round trip is proven; Amperfy-specific behaviour is not |
| 2026-08-05 | 1,2 | The revoke-Automation checks were not run | They would revoke a working grant on the user's machine; the denied branches are unit-tested | Two manual checks recorded as deliberate non-actions |
| 2026-08-05 | 0 | `docs/navidrome-recovery.md` re-deferred | It must be written after walking the recovery path, which means breaking a working install | Recovery undocumented; risk unchanged |
| 2026-08-05 | 0 | The stale `songCount: 0` defect re-deferred | No longer load-bearing now that favourites bypass the smart playlist | Cosmetic on two rows |

## Deviations from PLAN.md

| Deviation | Reason | Consequence |
| --- | --- | --- |
| The starred sort lives in `internal/navidrome` as `SortStarredSongs` | Three surfaces list stars; one definition prevents drift | Provider, CLI, and agent are guaranteed to agree |
| A third `.providerList` case for a zero exit with an undecodable result | The plan's two cases would print "failed with exit code 0" | `playlistFailureDetail` separates backend failure from decode failure |
| `applyPlaylistFinished` made internal | The behaviour is the deliverable and needed a direct test | Matches `consumeBufferedFinished`'s existing precedent |
| The Doctor probe is gated on an `apple_music` definition | The plan did not say when to run it | Recorded in the decision log |
| The Swift wire result went into `PhoneLibraryModels.swift` | Every other Phone Library result type lives there | The method case still went into `WireModels.swift` |
| **The plan's claim that the contract test enforces the protocol doc is false** | Nothing in `internal/agent/` references the doc | Docs updated by hand; no test guards them |
| **"No new command is needed" was wrong twice** | `EnsureNavidromeDefinitions` had no caller; `DefaultRekordboxTarget` was read nowhere | Both closed: `WriteNavidromeDefinitions` plus a setup-apply call, and `SnapshotRekordboxTarget` through the plan path |
| A `--playlist-id` flag was added to `rekordbox playlist-sync plan` | Phase 6's command assumed a flag that did not exist; snapshot planning was agent-only | The CLI can plan from a snapshot |
| "Checksum stable across repeated refreshes" is imprecise | `RefreshedAt` is a checksum input | Tested both ways: fixed-clock checksum equality, and zero reported changes live |
| The phone client was substreamer, not Amperfy | It was the client to hand | Subsonic round trip proven; Amperfy specifics still open |
| **An earlier revision of this tracker wrongly declared Swift validation impossible** | It concluded "no Xcode" meant "no Swift build" without checking the Makefile, which exposes a `swiftc`-based test runner and app installer | Everything so deferred has now been run: 87 Swift tests pass and the app was built, installed, and driven |

## Final Validation Evidence

- `go build ./...`, `go vet ./...`, `go test ./...` — clean.
- `make app-test-dev` — **87 Swift tests passed, 0 failed**, including 8 new.
- `make app-dev-install` — builds, signs, installs, launches; embedded backend
  signed `adhoc,runtime`; app entitlement `apple-events = true`.
- Automation grant survived a content-changing rebuild (new cdhash, no prompt,
  `music` check still passing).
- Doctor agrees across surfaces: CLI 29 info / 3 warn / 0 error; app 29 passed /
  3 warnings / 0 errors.
- Live round trip: 2 tracks starred from a phone client → `udl navidrome
  favorites list` 156 → app card 156 → snapshot `+2 -0 unchanged=154` →
  Rekordbox plan 156/156 matched, 0 missing, target `nav_fav_imports`.
- Apple Music path untouched: `favorites.json` `b03b3282…aa94` and
  `playlists.yaml` `baab284e…aa7c`, identical before and after.

**Outstanding:**

1. Apply the Rekordbox plan with Rekordbox closed and verify membership — the
   user's step, deliberately left.
2. Register the managed definitions on this machine with one idempotent
   `udl navidrome setup apply`.
3. The Amperfy-specific items in Phase 0, and `docs/navidrome-recovery.md`.
