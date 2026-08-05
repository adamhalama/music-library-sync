# Navidrome Likes to Rekordbox, and a Diagnosable GUI

- **Status:** Planned
- **Target:** Likes made on the phone reach Rekordbox, and the native app
  reports why a playlist operation failed
- **Last updated:** 2026-08-05
- **Implementation tracker:** [IMPLEMENTATION.md](./IMPLEMENTATION.md)
- **Predecessor:**
  [plans/archive/navidrome-phone-library-plan.md](./plans/archive/navidrome-phone-library-plan.md)

## Purpose

Two problems, discovered together while working on `feature/swiftui-gui`. They
are unrelated in mechanism but coupled in practice: the second one is why the
first is hard to observe.

**Likes only flow one way.** The phone library initiative delivered a complete,
hardened Apple Music → Navidrome star import. It never delivered the return
path. A track favourited on the phone in Amperfy is canonical in Navidrome and
stops there — it never becomes a UDL snapshot and never reaches Rekordbox. That
was a deliberate v1 scope cut, recorded in the predecessor plan. This plan lifts
it, one way only.

**The GUI cannot report playlist failures.** Every other feature in the native
app surfaces the backend's error text. The playlist handler alone discards it,
and downgrades a hard failure to a warning that says nothing happened. The
backend already produces an actionable message; the UI throws it away. The
result is a class of failure that is invisible by construction.

## Current State

The pieces that already exist, and the exact seam each one stops at:

| Capability | Where | Stops at |
| --- | --- | --- |
| Apple Music → Navidrome star import | `internal/navidrome/favorites.go` | Complete: checksummed plan, mandatory DB backup, parity verify, compensating un-star |
| Subsonic `getStarred2` / `star` / `unstar` | `internal/navidrome/client.go` | `Starred` is called from exactly one place — the parity check inside `ApplyFavoritePlan` |
| `Favourites (Navidrome)` smart playlist | `internal/navidrome/smartplaylists.go` | `.nsp` rule `{"is": {"loved": true}}`; depends on the server importing the file and on `VerifyPlaylistOwnership` |
| Navidrome playlist provider | `internal/playlists/navidrome_provider.go` | `NavidromeClient` deliberately omits the star methods |
| Snapshot → Rekordbox pipeline | `internal/app/rekordbox_playlist_sync.go` | Works today for any snapshot; needs no change |
| GUI error surfacing | `macos/UDL/Model/AppState.swift` | Sync, Free DL, Rekordbox and Phone Library surface `finished.error`; playlists does not |

So the Rekordbox half of the delivery is already built. What is missing is a read
path that turns Navidrome stars into a snapshot.

## Evidence

Driving `bin/udl agent --working-dir /Users/jaa` over the wire, exactly as the
app does:

- `playlists.list` returns the `favorites` definition **and** its snapshot.
- `playlists.providerList {"provider":"apple_music"}` returns `Favourites`
  (`70C641CA78BB0F3C`, 167 tracks) among six playlists.

Config discovery, the home working directory, and the AppleScript reader are all
healthy when the agent is spawned from a terminal. The differentiator is *who
spawns it*: AppleEvents from the child `udl` process are attributed to the
responsible process, `UDL.app`, which needs its own Automation → Music grant.
The Xcode embed phase copies the backend into the bundle without signing it,
so grants keyed on the bundle's code signature do not survive a rebuild.

This is a strong hypothesis, not a proven cause — it cannot be confirmed until
the GUI stops swallowing the error. That ordering drives the plan.

## Product Decisions

1. **Likes flow one way: Navidrome → UDL → Rekordbox.** Bidirectional
   Apple ↔ Navidrome favourite synchronisation stays deferred. It needs a
   conflict policy and a write path into Music.app, and this repo's Music
   integration is deliberately read-only.
2. **Read stars directly, not through the smart playlist.** Use `getStarred2`.
   The `.nsp` route works only once the server has imported the file and the
   playlist passes ownership verification, and that round trip is the one link
   the predecessor tracker never validated. The smart playlist stays in place
   for Amperfy's own browsing; it is no longer load-bearing for UDL.
3. **Apple and Navidrome favourites stay permanently separate.** Carried
   forward unchanged from the predecessor plan. `navidrome-favorites` gets its
   own Rekordbox target; the existing `favorites` → `fav_imports` flow is not
   touched, not merged, and not migrated.
4. **The snapshot format does not change.** Stars are represented as
   *membership* of the `navidrome-favorites` snapshot, not as a field on a
   track. Adding a starred field would change the checksum inputs and
   invalidate every existing `apple_music` snapshot.
5. **Refreshing stars is an explicit user action.** Post-sync stays scan-only.
   A star refresh is not a side effect of downloading music.
6. **A failure must name its cause.** The GUI shows the backend's message
   verbatim. A refresh that fails still preserves the previous valid snapshot —
   that guarantee is unchanged — but the message says why it failed rather than
   implying nothing happened.
7. **Fix observability before fixing the cause.** The signing change is the
   likely fix for the Automation failure, but shipping it first would mean
   guessing. Error surfacing lands first so the next failure identifies itself.

## Scope

**In scope**

- A direct starred read on the Navidrome provider, producing a deterministic
  snapshot that the existing Rekordbox pipeline consumes unchanged.
- A `navidrome-favorites` definition pointed at that read, with its own
  Rekordbox target.
- CLI, agent-protocol, and native-app surfaces for reading starred tracks.
- Playlist error surfacing in the native app.
- Signing the embedded backend so Automation grants survive rebuilds.
- A Doctor check that reports Music automation state before a refresh needs it.

**Out of scope**

- Bidirectional favourite synchronisation, and any write into Music.app.
- Merging Apple and Navidrome favourites into one Rekordbox playlist.
- Ratings, play counts, album and artist starring.
- Changes to `internal/navidrome/favorites.go` — the Apple → Navidrome import
  is finished and is not being revisited.
- Changes to `playlists.Snapshot` / `Track` shape or checksum inputs.
- Automatic star refresh during sync.

## Exit Criteria

This plan is done when:

- a track starred in Amperfy appears in `udl navidrome favorites list`, becomes
  a `navidrome-favorites` snapshot after an explicit refresh, and reaches
  Rekordbox through `rekordbox playlist-sync` against its own target;
- repeated refreshes with no server-side change produce no snapshot churn;
- the Apple Music `favorites` definition and its `fav_imports` target are
  byte-for-byte unaffected;
- a failing playlist operation in the native app shows the backend's own
  message, and a revoked Automation permission is identifiable from the UI
  without reading logs;
- an Automation grant survives an app rebuild;
- the carried-forward Phase 7 and 8 items from the predecessor tracker are
  either closed or explicitly re-deferred with a reason; and
- [IMPLEMENTATION.md](./IMPLEMENTATION.md) holds the validation evidence and
  every deviation from this plan.

## Risks

- **The Automation hypothesis may be wrong.** Mitigated by ordering: error
  surfacing lands first and is independently valuable. If the real cause turns
  out to be different, the signing change is still correct hygiene, and the
  tracker records the actual cause.
- **`getStarred2` has no defined ordering.** An unstable order would produce a
  new checksum on every refresh and endless phantom Rekordbox diffs. Mitigated
  by sorting on a stable key before building the snapshot.
- **Verification needs real hardware.** The end-to-end path needs the iPhone,
  Amperfy, a running server, and a closed Rekordbox. Unit tests cover the
  logic; the round trip is a manual gate and is tracked as one.

## Deferred

- Bidirectional Apple Music ↔ Navidrome favourite synchronisation.
- Writing anything back into Music.app.
- Unioning Apple and Navidrome favourites into a single Rekordbox playlist.
- Importing ratings, play counts, or cloud-only tracks.
- Album and artist starring.
- Internet exposure, reverse proxies, HTTPS, and Tailscale.
