# Navidrome Likes to Rekordbox — Implementation Tracker

- **Overall status:** Not started
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

## Progress Dashboard

| Phase | Status | Depends on | Exit gate |
| --- | --- | --- | --- |
| 0. Carried forward from phone library | In progress | — | Every open item from the predecessor tracker is closed or re-deferred with a reason |
| 1. GUI playlist error surfacing | Not started | — | A failing playlist operation shows the backend's own message |
| 2. Backend signing and Automation | Not started | 1 | An Automation grant survives an app rebuild, and the real cause of the favourites failure is recorded |
| 3. Starred read path | Not started | — | `getStarred2` produces a deterministic snapshot with no churn across identical refreshes |
| 4. Managed definition and Rekordbox target | Not started | 3 | `navidrome-favorites` reaches its own Rekordbox playlist, Apple `favorites` untouched |
| 5. CLI, agent, and native surfaces | Not started | 3, 4 | Starred tracks are readable from CLI, RPC, and the app |
| 6. End-to-end acceptance | Not started | 1–5 | A star made in Amperfy reaches Rekordbox |
| 7. Documentation and close-out | Not started | 1–6 | Docs, evidence, and decision log complete |

---

## Phase 0 — Carried Forward from the Phone Library

**Status:** In progress

**Exit gate:** Every `[ ]` left in the archived predecessor tracker is closed
here or explicitly re-deferred with a recorded reason.

These items were open when
[plans/archive/navidrome-phone-library-implementation.md](./plans/archive/navidrome-phone-library-implementation.md)
was archived. They are restated verbatim in intent so nothing is lost. Several
are closed as a side effect of the phases below — where that is true, it is
noted.

### Favourite round trip (was Phase 4 validation)

- [ ] Verify an explicit `navidrome-favorites` refresh reflects new server
      stars. *Superseded by Phase 6 below, which validates the same thing
      through the direct starred read rather than the `.nsp` smart playlist.*

### Native workspace visual pass (was Phase 6 validation)

- [ ] Build and manually inspect light/dark, empty, partial, running, failed,
      and complete Phone Library states against a real installed server.

### Amperfy acceptance (was Phase 7)

*Every item below needs the iPhone and the companion app.*

- [ ] Connect Amperfy over home Wi-Fi using the shared account.
- [ ] Verify All Music, HARD BOUNCE, and Favourites ordering and membership.
- [ ] Confirm enough free storage and download the complete All Music playlist.
- [ ] Disable Wi-Fi/Mac availability and play multiple downloaded tracks.
- [ ] Favorite/unfavorite while connected and verify Navidrome plus explicit UDL
      refresh. *Overlaps Phase 6.*
- [ ] Test favorite changes while completely offline and record whether Amperfy
      queues them on reconnect.
- [ ] Verify lock-screen, Control Center, Dynamic Island, and favorite control;
      record any client limitation without starting custom-app work.

### Documentation (was Phase 8)

- [ ] Document Amperfy connection, Local Network permission, full download,
      playlist refresh, offline playback, and observed favorite behavior.
- [ ] Write `docs/navidrome-recovery.md`: backups, restore while stopped,
      favorite compensation, service logs, port conflicts, uninstall, and
      unowned-install refusal. Write it after walking the recovery path once
      for real.
- [ ] Document the deferred Tailscale/remote-access direction without
      configuring it.

### Known cosmetic defect (carried, not yet scheduled)

- [ ] `getPlaylists` reports `songCount: 0` for a smart playlist until it is
      refreshed; the native workspace shows that count, so a freshly imported
      playlist can briefly read "0 tracks". Decide whether to read through to
      `getPlaylist` or to label the count as stale.

### Phase evidence

- Commands:
- Manual checks:
- Notes:

---

## Phase 1 — GUI Playlist Error Surfacing

**Status:** Not started

**Exit gate:** A failing playlist operation in the native app shows the
backend's own message, and a refresh failure still preserves the previous valid
snapshot.

Landing first is deliberate. Until this ships, the Automation hypothesis in
PLAN.md cannot be confirmed or refuted.

### Tasks

- [ ] In `applyPlaylistFinished` (`macos/UDL/Model/AppState.swift`, ~line 1509),
      stop discarding `RunFinishedNotification.error`.
- [ ] `.providerList` failure: replace the `.warn` "Provider listing ended
      without changing any snapshot." with `severity: .error` and
      `finished.error ?? "Provider listing failed with exit code N."`.
- [ ] `.providerList` exit code `130`: keep `.warn`, "Provider listing
      canceled.", matching how `.refresh` already treats 130.
- [ ] `.refresh` failure: keep `preservedPreviousSnapshot: true` and the C11
      guarantee intact, but prepend the backend message before "The previous
      valid snapshot was preserved."
- [ ] Match the phrasing already used by the Rekordbox handler so all features
      read consistently.
- [ ] Confirm `PlaylistsView.swift` renders a multi-sentence message without
      truncating; add `.fixedSize(horizontal: false, vertical: true)` if it
      uses a single-line `Text`.

### Validation

- [ ] Swift test: a `run.finished` carrying an error yields a `.error`
      `PlaylistStatus` whose message contains the backend text, for both
      operations.
- [ ] Swift test: exit code `130` yields `.warn` and does not read as a failure.
- [ ] Swift test: a failed refresh still reports `preservedPreviousSnapshot`.
- [ ] Manual: revoke Automation → Music, refresh from the app, confirm the
      pane names System Settings.

### Phase evidence

- Commands:
- Manual checks:
- Notes:

---

## Phase 2 — Backend Signing and Automation Permission

**Status:** Not started

**Exit gate:** An Automation grant survives an app rebuild, and the actual cause
of the Apple Music favourites failure is recorded — whether or not it matches
the hypothesis.

### Tasks

- [ ] In the `Embed udl backend` script phase
      (`macos/UDL.xcodeproj/project.pbxproj`), codesign the copied binary with
      `EXPANDED_CODE_SIGN_IDENTITY` and `--options runtime` after the existing
      `cp` / `chmod 755`. Guard on the identity being non-empty.
- [ ] Confirm no change is needed to `UDL.entitlements`
      (`com.apple.security.automation.apple-events`) or to
      `Info.plist` (`NSAppleEventsUsageDescription`) — both are already correct.
- [ ] Add a Doctor check that runs `music.Reader.ListPlaylists` with a short
      timeout and reports `actionableMusicAutomationError`
      (`internal/rekordbox/music/music.go`) on failure, so permission state is
      visible before a refresh needs it.
- [ ] Register the check with the existing doctor check list and confirm
      `Severity.forCheck` maps it correctly in `AppState.swift`.
- [ ] Record the observed cause in the decision log, including the case where
      the hypothesis is wrong.

### Validation

- [ ] `codesign -dv "$APP/Contents/Resources/udl"` reports a valid signature.
- [ ] Grant Automation, rebuild, relaunch: the grant still holds.
- [ ] Doctor pane shows the Automation check passing, and shows the actionable
      message when the grant is revoked.
- [ ] Go test for the new doctor check with a faked `RunAppleScript`.

### Phase evidence

- Commands:
- Manual checks:
- Notes:

---

## Phase 3 — Starred Read Path

**Status:** Not started

**Exit gate:** `getStarred2` produces a snapshot whose checksum is stable across
repeated refreshes when the server has not changed.

### Tasks

- [ ] Add `Starred(ctx) ([]navidrome.Song, error)` to the `NavidromeClient`
      interface in `internal/playlists/navidrome_provider.go`.
      `navidrome.Client.Starred` already satisfies it.
- [ ] Add the method to the fake client in `navidrome_provider_test.go`.
- [ ] Add `NavidromeStarredPlaylistID = "starred"` as the sentinel.
- [ ] Extract the existing song → `Track` loop in `Read` (the `os.Stat` /
      `MissingLocal` block) into a shared helper so both paths use it.
- [ ] Branch in `Read` before the `PlaylistByName` lookup: on the sentinel, call
      `Starred` and return a synthetic `ProviderPlaylist` named
      `navidrome.SmartPlaylistFavoritesName`.
- [ ] Sort the starred tracks by normalized path before building the snapshot.
      `getStarred2` defines no order; without this the checksum changes on every
      refresh and Rekordbox sees phantom diffs.
- [ ] Leave `playlists.Snapshot` / `Track` and `snapshotChecksum` inputs
      untouched — see PLAN.md decision 4.

### Validation

- [ ] Unit test: sentinel path produces a path-sorted track list with correct
      `MissingLocal` for present and absent files.
- [ ] Unit test: two refreshes over identical fake data produce an identical
      checksum and zero reported changes.
- [ ] Unit test: a server returning starred songs in shuffled order still
      produces the same snapshot.
- [ ] Unit test: the named-playlist path still works unchanged.
- [ ] `go test -race ./internal/playlists/ ./internal/navidrome/`

### Phase evidence

- Commands:
- Manual checks:
- Notes:

---

## Phase 4 — Managed Definition and Rekordbox Target

**Status:** Not started

**Exit gate:** `navidrome-favorites` refreshes through the starred read and
plans into its own Rekordbox playlist, with the Apple `favorites` definition
provably untouched.

### Tasks

- [ ] Point the `SmartPlaylistFavorites` entry in `NavidromeDefinitions()` at
      `ProviderPlaylistID: NavidromeStarredPlaylistID`.
- [ ] Set `DefaultRekordboxTarget: "nav_fav_imports"` — a separate playlist, per
      PLAN.md decision 3.
- [ ] Keep `ProviderPlaylist` populated so `Validate` still passes and the name
      reads well in the UI.
- [ ] Confirm `EnsureNavidromeDefinitions` remains append-only.
- [ ] Confirm no new command is needed: `udl playlist refresh
      navidrome-favorites` and `udl rekordbox playlist-sync plan --playlist-id
      navidrome-favorites` should work through the existing pipeline.

### Validation

- [ ] Existing assertions that the Apple `favorites` definition survives the
      merge still pass, unmodified.
- [ ] Unit test: the managed definition carries the sentinel and the separate
      Rekordbox target.
- [ ] Snapshot checksum for `favorites` is byte-for-byte identical before and
      after the branch.

### Phase evidence

- Commands:
- Manual checks:
- Notes:

---

## Phase 5 — CLI, Agent, and Native Surfaces

**Status:** Not started

**Exit gate:** Starred tracks are readable from the CLI, over the agent
protocol, and in the app.

### Tasks

- [ ] Add `Manager.StarredFavorites(ctx)` in `internal/navidrome/manager.go`,
      next to `AppleFavorites`.
- [ ] Add `udl navidrome favorites list [--json]` to
      `newNavidromeFavoritesCommand` in `internal/cli/navidrome.go`, reusing the
      rendering style of `printFavoritePlan`.
- [ ] Add `navidrome.favorites.list` to `internal/agent/methods_navidrome.go`.
- [ ] Register it in the `protocolMethods` array and the `handle` switch in
      `internal/agent/server.go`.
- [ ] Update `internal/agent/testdata/protocol_v2_golden.json` and the Navidrome
      block in `docs/agent-protocol.md` in the same change — the contract test
      enforces both.
- [ ] Add the method to `UDLClient.swift` and a wire result in `WireModels.swift`.
- [ ] Show the starred count in `PhoneLibraryView.swift`. No new screen: the
      `navidrome-favorites` definition appears in the existing Playlists pane
      once Phase 4 writes it into the config.

### Validation

- [ ] `go test ./internal/agent/` — contract and golden tests pass.
- [ ] `go test ./internal/cli/`
- [ ] Swift decode test for the new wire result.
- [ ] Manual: `udl navidrome favorites list` against a running server.

### Phase evidence

- Commands:
- Manual checks:
- Notes:

---

## Phase 6 — End-to-End Acceptance

**Status:** Not started

**Exit gate:** A star made in Amperfy reaches Rekordbox, and the Apple Music
path is unaffected.

*Needs the iPhone, a running server, and a closed Rekordbox.*

### Tasks

- [ ] Star a track in Amperfy.
- [ ] `udl navidrome favorites list` shows it.
- [ ] `udl playlist refresh navidrome-favorites` reports `+1`.
- [ ] Refresh again with no change: reports zero changes, same checksum.
- [ ] `udl rekordbox playlist-sync plan --playlist-id navidrome-favorites` shows
      the add against `nav_fav_imports`.
- [ ] Apply with Rekordbox closed; verify membership in Rekordbox.
- [ ] Un-star in Amperfy, refresh, confirm the removal propagates.
- [ ] Confirm `udl playlist refresh favorites` and its `fav_imports` target are
      unaffected.
- [ ] Wire-level smoke matching how the app drives it: `session.initialize` then
      `playlists.refresh navidrome-favorites` piped into
      `bin/udl agent --working-dir ~`; read the `run.finished` payload.
- [ ] Repeat the favourites refresh from the native app and confirm it succeeds
      or names its failure.

### Validation

- [ ] `go test ./...` and `go vet ./...`
- [ ] Native test suite passes.
- [ ] Record track counts at each step so the numbers can be re-checked.

### Phase evidence

- Commands:
- Manual checks:
- Notes:

---

## Phase 7 — Documentation and Close-Out

**Status:** Not started

**Exit gate:** Docs, evidence, and the decision log are complete, and every
Phase 0 item is closed or re-deferred.

### Tasks

- [ ] Document `udl navidrome favorites list` and the `navidrome-favorites`
      → Rekordbox flow in `readme.md`.
- [ ] Document that Apple and Navidrome favourites are separate by design, and
      that they target different Rekordbox playlists.
- [ ] Document the Automation permission requirement for the native app and how
      to re-grant it.
- [ ] Resolve every Phase 0 item: close it or re-defer it with a reason in the
      decision log.
- [ ] Update PLAN.md status and this dashboard only after every exit gate is
      satisfied.

### Phase evidence

- Commands:
- Manual checks:
- Notes:

---

## Decision Log

| Date | Phase | Decision | Rationale | Consequence |
| --- | --- | --- | --- | --- |
| 2026-08-05 | — | Read stars via `getStarred2`, not via the `Favourites (Navidrome)` `.nsp` smart playlist | The `.nsp` route depends on the server having imported the file and on ownership verification passing; that round trip was never validated in the predecessor initiative | The smart playlist stays for Amperfy browsing but is no longer load-bearing for UDL |
| 2026-08-05 | — | `navidrome-favorites` targets `nav_fav_imports`, not `fav_imports` | Carried forward from the predecessor decision that Apple and Navidrome favourites stay permanently separate | Two Rekordbox playlists; no dedup or precedence rule needed |
| 2026-08-05 | — | Stars are snapshot membership, not a `Track` field | A starred field would change `snapshotChecksum` inputs and invalidate every existing `apple_music` snapshot | Snapshot format is unchanged across this initiative |
| 2026-08-05 | — | GUI error surfacing ships before the signing fix | The Automation cause is a hypothesis; shipping the fix first would be guessing | Phase 1 has no dependency on Phase 2 and is independently valuable |
| 2026-08-05 | — | Sort starred tracks by normalized path | `getStarred2` defines no ordering; an unstable order would churn the checksum on every refresh | Deterministic snapshots and no phantom Rekordbox diffs |

## Deviations from PLAN.md

*Record every deviation here, with the reason and its consequence.*

## Final Validation Evidence

*Filled in at close-out.*
