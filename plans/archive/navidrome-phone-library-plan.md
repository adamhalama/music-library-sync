# Navidrome + Amperfy Phone Library

- **Status:** Archived — code complete, phone/docs acceptance carried forward
- **Target:** A local-first phone library backed by Navidrome, managed by UDL,
  and consumed by Amperfy on iPhone
- **Last updated:** 2026-08-05
- **Implementation tracker:** [navidrome-phone-library-implementation.md](./navidrome-phone-library-implementation.md)
- **Successor:** [../../PLAN.md](../../PLAN.md) — the unfinished Phase 7 and
  Phase 8 items from this initiative are carried forward there as Phase 0.
- **Predecessor:**
  [reliable-unified-sync-queue-plan.md](./reliable-unified-sync-queue-plan.md)

## Purpose

Make `~/Music/downloaded` available on iPhone without depending on Apple Music's
playlist ordering. Navidrome becomes the server-side library and favorite store,
Amperfy provides the native iPhone player and offline cache, and UDL manages the
server lifecycle, smart playlists, Apple Music favorite migration, and explicit
snapshot reads.

The first release is local-first: the Mac serves the library on trusted home
Wi-Fi, while Amperfy downloads the complete library for offline use. Remote
access is deliberately deferred, but the server layout must remain suitable for
adding Tailscale later without migrating library state.

## Product Decisions

1. **Run Navidrome natively on macOS.** Install the Homebrew package and launch
   it through an UDL-managed LaunchAgent. Do not use Docker, because native
   access preserves APFS birth/creation time.
2. **Creation time is Date Added.** Keep `RecentlyAddedByModTime = false`. UDL
   does not rewrite audio contents, tags, creation times, or modification times.
3. **Use one shared user.** The first Navidrome admin account is also the account
   used by Amperfy and UDL, so favorites, ratings, play counts, and personalized
   smart playlists refer to the same user.
4. **Keep Apple Music and Navidrome favorites separate.** The existing stable
   Apple Music `favorites` definition remains. Add a separate
   `navidrome-favorites` definition and migrate Apple favorites one way without
   deleting or changing the source favorites.
5. **Derive Hard Bounce genres once.** Match the current Apple Music
   `HARD BOUNCE` playlist to the Navidrome catalog by real path, preview its
   distinct embedded genres, and store an explicit allowlist. Future membership
   remains dynamic against that allowlist.
6. **Sort deterministically.** Managed playlists sort by
   `-dateadded,filepath`: newest creation time first, then relative path for
   files sharing a timestamp.
7. **The full phone library is explicit.** Create an `All Music` smart playlist
   for Amperfy's offline download/update action. The current library is about
   1,574 tracks and 9.4 GiB before client cache overhead.
8. **Installation is explicit.** Status and doctor checks never mutate the
   system. A clearly labeled, confirmed action may run `brew install navidrome`.
9. **Snapshots remain cache-first.** Opening a saved UDL playlist never contacts
   Apple Music or Navidrome. Provider access happens only through explicit
   discovery, refresh, migration, or setup actions.
10. **Home Wi-Fi only in v1.** Never configure router port forwarding or public
    exposure. Plain HTTP is permitted only on the trusted LAN; Tailscale/remote
    HTTPS is future work.

## Target Experience

The native app gains a **Phone Library** workspace that guides the user through:

1. detecting or explicitly installing Navidrome;
2. previewing and applying the managed server configuration;
3. creating the first admin account at `http://localhost:4533`;
4. saving the same account password to macOS Keychain;
5. scanning and verifying the local music library;
6. deriving and approving the Hard Bounce genre allowlist;
7. creating `All Music`, `HARD BOUNCE`, and `Favourites` smart playlists;
8. previewing and applying the Apple Music favorite migration; and
9. copying the LAN address into Amperfy and downloading `All Music`.

The workspace always distinguishes dependency, service, account, scan,
playlist, migration, and phone-connection state. A disabled action states its
reason beside the control, and a failure stays on the screen that caused it.

## Architecture

### Managed service

UDL owns a versioned `navidrome.yaml` feature config, a rendered
`navidrome.toml`, and `com.jaa.udl.navidrome.plist`. Defaults are:

- music root: `~/Music/downloaded`;
- operational data: `~/Library/Application Support/UDL/Navidrome`;
- managed playlist path: `.udl/playlists` relative to the music root;
- port `4533`, bound for LAN access;
- favorites, star ratings, downloads, playlist import, log redaction, and real
  path reporting enabled;
- external metadata services and anonymous insights disabled; and
- daily database backups with seven retained copies.

UDL requires Navidrome 0.63.2 or newer. It may manage only files carrying its
own ownership marker. If an unrelated Navidrome service/configuration exists,
setup stops and offers an explicit future adoption path rather than overwriting
it.

After a successful UDL download run, the backend requests a best-effort
Navidrome scan. Scan failure produces a warning and never changes the download
result.

### Authentication and API

The server URL and username are ordinary feature configuration. The password is
stored only in macOS Keychain. UDL uses salted Subsonic token authentication and
never emits a plaintext password into YAML, logs, command arguments, RPC
results, or diagnostic tails.

The Navidrome provider lists playlists, reads ordered playlist membership and
real paths, reads stars, starts scans, and changes stars. It feeds the existing
checksummed standalone snapshot layer so downstream Free DL and Rekordbox flows
can consume Navidrome playlists without learning the API.

### Managed smart playlists

UDL atomically writes three `.nsp` definitions:

- `navidrome-all` / **All Music**: every track;
- `navidrome-hard-bounce` / **HARD BOUNCE**: any genre in the approved explicit
  allowlist; and
- `navidrome-favorites` / **Favourites (Navidrome)**: `loved = true`.

All use `-dateadded,filepath` with no arbitrary item limit. Navidrome assigns
imported smart playlists to the first admin user. UDL validates ownership before
accepting personalized playlist results.

Hard Bounce derivation reads the Apple Music playlist only when explicitly
requested. Apple paths are matched to Navidrome real paths; genre values come
from the server's parsed file metadata. Missing paths and genre-less tracks are
reported and never cause the rule to broaden implicitly.

### Favorite migration

Favorite migration uses a checksummed plan. It scopes Apple favorites to local
files under the configured music root, matches canonical real paths, and reports
matched, already-starred, outside-library, missing, and ambiguous rows.
Metadata-only matches are diagnostic and never automatically applied.

Apply revalidates both libraries, creates a Navidrome database backup, stars
exact matches, and verifies final parity. If an API failure interrupts apply,
UDL compensates by un-starring only tracks newly changed by that attempt and
reports the backup path if compensation cannot restore the pre-state. Apple
Music is read-only throughout.

Future Amperfy favorites are canonical in Navidrome. UDL sees them after an
explicit refresh of `navidrome-favorites`; v1 does not synchronize changes back
to Apple Music.

## Public Interfaces

CLI additions:

- `udl navidrome config path|show`
- `udl navidrome deps status|ensure`
- `udl navidrome status`
- `udl navidrome setup plan|apply --plan-file <path>`
- `udl navidrome playlists refresh`
- `udl navidrome favorites import plan|apply --plan-file <path>`
- `udl navidrome backup create`

The agent exposes matching protocol-v2 methods for configuration, dependency
status/repair, server status, setup planning/application, playlist refresh,
favorite planning/application, and backup creation. New wire-inbound
collections remain tolerant of Go `null`.

Standalone playlist config accepts `provider: navidrome`. The existing Apple
Music `favorites` entry remains unchanged; setup adds separate Navidrome
definitions.

## Safety and Compatibility

- Preserve all unrelated dirty worktree changes.
- Never overwrite an unowned Navidrome configuration or LaunchAgent.
- Setup/apply and favorite/apply revalidate checksummed plans before mutation.
- Write config, playlist, and manifest files atomically.
- Back up the Navidrome database before favorite mutation and on schedule.
- Never make general sync fail because the optional phone server is unavailable.
- Preserve old standalone snapshots and config files; add provider support
  without invalidating their checksums.
- Keep Apple Music automation explicit and read-only for this feature.
- Do not add Navidrome as an unconditional Homebrew dependency of every UDL
  installation.

## Completion Criteria

The initiative is complete when:

- the native Homebrew Navidrome service starts through UDL and survives login;
- its full scan indexes the expected local tracks without changing audio size,
  birth time, or modification time;
- API and Amperfy order `All Music` and `HARD BOUNCE` newest-first by APFS
  creation time with deterministic ties;
- the derived Hard Bounce genre set is previewed, approved, persisted, and used
  dynamically;
- Apple favorites import with exact path parity while Apple Music remains
  unchanged;
- a favorite made in Amperfy appears in Navidrome and an explicitly refreshed
  UDL snapshot;
- Amperfy downloads the approximately 9.4 GiB `All Music` playlist and plays it
  with Wi-Fi disabled;
- lock-screen/Dynamic Island playback works and favorite-control behavior is
  recorded;
- service, provider, migration, protocol, Swift, and opt-in real-server tests
  pass; and
- [navidrome-phone-library-implementation.md](./navidrome-phone-library-implementation.md) contains the final validation
  evidence and every deviation from this plan.

## Deferred

- Internet exposure, reverse proxies, HTTPS, and Tailscale.
- A custom or forked iPhone application.
- Bidirectional Apple Music/Navidrome favorite synchronization.
- Importing Apple Music ratings, play counts, or cloud-only tracks.
- Rewriting audio tags or filesystem timestamps.
- Automatically changing existing Rekordbox mappings from Apple Music to
  Navidrome.
