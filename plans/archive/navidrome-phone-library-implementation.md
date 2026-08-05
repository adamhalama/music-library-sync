# Navidrome + Amperfy Phone Library — Implementation Tracker

- **Overall status:** Archived — Phases 1–6 complete; Phase 7 (phone acceptance)
  and Phase 8 (docs) unfinished and carried forward
- **Plan:** [navidrome-phone-library-plan.md](./navidrome-phone-library-plan.md)
- **Last updated:** 2026-08-05
- **Successor:** [../../IMPLEMENTATION.md](../../IMPLEMENTATION.md) — every `[ ]`
  left in this file is restated there as Phase 0. Do not tick boxes here; this
  file is history.
- **Archived predecessor:**
  [reliable-unified-sync-queue-implementation.md](./reliable-unified-sync-queue-implementation.md)

This is the live execution record for the Navidrome + Amperfy initiative. Update
status, evidence, decisions, and discovered work in the same change as the
implementation. A checked task means the behavior is implemented **and**
proportionally validated.

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
| 1. Config and service foundation | In review | — | Managed config and LaunchAgent are safe, testable, and cannot overwrite an unowned install |
| 2. Navidrome API and credentials | In review | 1 | Token-authenticated status, catalog, playlist, star, scan, and backup operations pass contract tests |
| 3. Smart playlists and providers | In review | 2 | All Music, derived Hard Bounce, and Navidrome Favourites produce ordered snapshots |
| 4. Favorite migration | In review | 2, 3 | Checksummed plan/apply preserves Apple Music and reaches exact Navidrome parity |
| 5. CLI, agent, and post-sync integration | In review | 1–4 | Public commands/RPCs work and successful downloads request a non-fatal scan |
| 6. Native Phone Library workspace | In review | 5 | The complete setup and migration workflow is usable without hidden state or silent disabling |
| 7. Real-server and Amperfy acceptance | In progress (phone pending) | 1–6 | Isolated and full-library verification passes, including complete offline phone playback |
| 8. Documentation and close-out | In progress | 1–7 | User/recovery docs, validation evidence, and decision log are complete |

## Cross-Cutting Definition of Done

- [x] Preserve unrelated dirty worktree changes and keep each phase reviewable.
- [x] Add/update tests with every behavior change.
- [x] Keep passwords out of YAML, plans, logs, command arguments, RPC frames,
      diagnostics, fixtures, and screenshots.
- [x] Use atomic writes for managed config, playlist, manifest, and snapshot files.
- [x] Refuse to overwrite files or services not marked as UDL-owned.
- [x] Keep Swift wire-inbound collections tolerant of Go `null`.
- [x] State the reason next to every disabled native control.
- [x] Route workflow failures through Phone Library state, not session alerts.
- [x] Preserve cache-first playlist behavior and the last valid snapshot.
- [x] Keep optional Navidrome failures from changing normal download results.
- [x] Record exact validation commands and material manual evidence.
- [x] Record every deviation from [navidrome-phone-library-plan.md](./navidrome-phone-library-plan.md) below.

---

## Phase 1 — Config and Service Foundation

**Status:** In review

**Exit gate:** UDL can plan and apply an owned native Navidrome service without
changing music files or overwriting an unrelated installation.

Package layout as built: `internal/navidrome/{config,fsutil,deps,render,setup,service}.go`.

### Feature configuration

- [x] Add a versioned standalone `navidrome.yaml` loader, normalizer, validator,
      path resolver, canonical marshaler, and atomic saver.
      (`config.go`: `Load`, `normalize`, `Validate`, `Resolve`, `Marshal`, `Save`)
- [x] Add defaults for music, data, cache, logs, playlists, backups, LAN port,
      scan schedule, and username.
- [x] Keep the password outside the config model.
      (`TestConfigModelHasNoPasswordField`)
- [x] Validate absolute/expanded paths, port range, non-overlapping managed
      directories, supported platform, and ownership markers.
- [x] Add project/user/environment config discovery consistent with other UDL
      feature configs (`UDL_NAVIDROME_CONFIG`, `udl.navidrome.yaml`,
      `~/.config/udl/navidrome.yaml`).

### Dependency and service management

- [x] Detect Homebrew and Navidrome without mutating the system.
      (`DependencyChecker.Status`; also probes both Homebrew prefixes because a
      GUI-launched backend has a minimal `PATH`)
- [x] Require and parse Navidrome version 0.63.2 or newer.
- [x] Add an explicitly confirmed `brew install navidrome` ensure operation.
      (`Ensure(ctx, confirmed)` refuses and runs nothing when unconfirmed)
- [x] Render `navidrome.toml` with creation-time ordering, playlist import,
      favorites, ratings, downloads, real paths, redacted logs, disabled external
      services/insights, and scheduled backups.
- [x] Render an owned `com.jaa.udl.navidrome.plist` using the resolved binary and
      config paths.
- [x] Build a checksummed setup plan covering every file/service mutation.
- [x] Revalidate the plan before apply and use atomic replacement for owned files.
- [x] Implement start, stop, restart, PID, log-tail, and local/LAN URL discovery.
      HTTP health moved to Phase 2 — see the decision log.
- [x] Refuse apply when an unowned config, LaunchAgent, or conflicting listener
      is detected.

### Validation

- [x] Unit-test defaults, validation, path expansion, version parsing, ownership,
      plan checksums, stale-plan rejection, and rendered TOML/plist content.
- [x] Verify dry-run/setup-plan performs no writes or service changes.
      (`TestPlanIsApplicableAndWritesNothing` walks the whole temp HOME before
      and after planning and asserts the tree is byte-identical)
- [x] Verify generated configuration contains no secret fields.

### Phase evidence

- Commands: `go test ./internal/navidrome/`, `go build ./...`, `go vet ./internal/navidrome/`
- Manual checks: none yet; a real `brew install navidrome` and service apply
  belong to Phase 7.
- Notes:
  - `Validate` refuses on non-darwin, so the package's tests are macOS-only by
    construction. That matches the plan's macOS-only scope.
  - Secret-leak assertions that scan a rendered artifact for the substrings
    `password`/`secret` must not be named after those words: the temp `HOME`
    contains the Go test name, so `TestPlanContainsNoSecrets` failed against its
    own path. Renamed to `…OmitsCredentialFields`.
  - `plannedFile` pins `existing_sha256`, so replaying an already-applied plan
    is refused with a regenerate hint rather than silently re-writing.

---

## Phase 2 — Navidrome API and Credentials

**Status:** In review

**Exit gate:** UDL can securely authenticate and perform every server operation
needed by later phases against a deterministic fake server.

Built as `internal/auth/navidrome_password.go`, `internal/navidrome/client.go`,
`internal/navidrome/backup.go`, with `fakeserver_test.go` as the deterministic
server double.

### Keychain and authentication

- [x] Add Navidrome password presence/save/replace/clear operations to the
      existing credential layer (`auth.SaveNavidromePassword`,
      `RemoveNavidromePassword`, `HasNavidromePassword`,
      `InspectNavidromePassword`, `CredentialKindNavidromePassword`).
- [x] Keep server URL and username in feature config and password only in
      Keychain.
- [x] Implement salted Subsonic token authentication with fresh salt per request.
      (`TestAuthParamsUseAFreshSaltPerRequest`)
- [x] Redact query authentication, authorization data, and responses that could
      contain secrets. (`RedactURL`, `redactError`)

### API client

- [x] Implement ping/version/status and actionable compatibility errors.
- [x] Implement library enumeration with stable IDs, genres, creation dates,
      relative paths, and configured real paths.
- [x] Implement playlist list/read, starred reads, star/unstar, scan start/status,
      and backup creation.
- [x] Normalize macOS paths consistently with Apple Music and Rekordbox matching.
- [x] Bound response bodies, timeouts, retries, and diagnostic output.

### Validation

- [x] Test authentication construction without exposing plaintext passwords.
- [x] Test successful, unauthorized, incompatible, malformed, timeout, canceled,
      and partial-response behavior.
- [x] Test real-path normalization, Unicode, symlinks, missing paths, duplicate
      IDs, and multi-valued genres. Symlink resolution is deliberately *not*
      performed — see the decision log.
- [x] Add secret-leak regression tests across logs and RPC errors.
      (`TestRequestsNeverPutThePasswordOnTheWire`,
      `TestTimeoutIsReportedWithoutLeakingTheURL`,
      `TestWrongPasswordIsUnauthorized`)

### Phase evidence

- Commands: `go test -race ./internal/navidrome/ ./internal/auth/`
- Manual checks: none; every assertion runs against `fakeserver_test.go`, which
  enforces real salted-token authentication, so an unauthenticated or
  password-leaking client fails there.
- Notes:
  - Library enumeration walks `search3.view` with an empty query in pages of
    500. Duplicate IDs across pages (library changing mid-walk) keep the first
    occurrence so the result stays self-consistent.
  - Backup creation runs `navidrome backup --configfile …` rather than an API
    call: Navidrome exposes no Subsonic backup endpoint. `CreateBackup` treats
    "command succeeded but no new file appeared" as a failure, because favorite
    migration is only safe if the backup genuinely exists.
  - Retries cover transport errors and 5xx only, max 3 attempts. Unauthorized
    and incompatible-server are final and are asserted not to retry.

---

## Phase 3 — Smart Playlists and Providers

**Status:** In review

**Exit gate:** Managed smart playlists are deterministic, server-owned by the
shared account, and readable through checksummed UDL snapshots.

Built as `internal/navidrome/smartplaylists.go` and
`internal/playlists/navidrome_provider.go`.

### Smart playlist generation

- [x] Implement atomic `.nsp` generation under the configured relative
      `PlaylistsPath`.
- [x] Generate `All Music` with no item limit and `-dateadded,filepath` sorting.
      `limit` is `omitempty`, so an unbounded playlist emits no limit key at all.
- [x] Generate `Favourites` with `loved = true` and the same sorting.
- [x] Read the selected Apple Music `HARD BOUNCE` playlist explicitly and match
      its local paths to the Navidrome catalog. (`DeriveHardBounceGenres` takes
      the source tracks as an argument; nothing reads Apple Music implicitly.)
- [x] Derive, case-fold/deduplicate, preview, approve, and persist the explicit
      genre allowlist.
- [x] Report unmatched and genre-less source tracks without silently broadening
      the rule. An empty allowlist is a hard refusal, not a match-everything
      fallback.
- [x] Generate `HARD BOUNCE` using exact allowlisted genres and deterministic
      sorting.
- [x] Trigger/observe a scan and verify playlist ownership belongs to the shared
      first-admin account. (`RefreshManagedPlaylists`, `VerifyPlaylistOwnership`)

### Standalone playlist provider

- [x] Add `ProviderNavidrome` validation and provider construction.
- [x] Map Navidrome tracks into the existing snapshot shape without breaking old
      Apple Music snapshots/checksums. The `Snapshot`/`Track` structs are
      untouched; only the provider allowlist widened.
- [x] Add `navidrome-all`, `navidrome-hard-bounce`, and
      `navidrome-favorites` definitions while preserving the existing Apple
      Music `favorites` entry. (`EnsureNavidromeDefinitions` only appends.)
- [x] Keep provider discovery/refresh explicit and opening cache-only.
- [x] Verify ordered membership survives API-to-snapshot conversion.

### Validation

- [x] Test playlist JSON, escaping, genre variants, empty allowlists, missing
      genres, no-limit behavior, duplicate tracks, and filepath tie-breaking.
      Tie-breaking itself is the server's job — the assertion is that
      `-dateadded,filepath` reaches the `.nsp` verbatim; real ordering is a
      Phase 7 check.
- [x] Test provider list/read, missing-local reporting, snapshot compatibility,
      failed refresh preservation, and cancellation.

### Phase evidence

- Commands: `go test ./internal/navidrome/ ./internal/playlists/`
- Manual checks: none yet; whether Navidrome accepts the exact rule and sort
  syntax can only be settled against a real server (Phase 7).
- Notes:
  - **Open risk.** Two `.nsp` details are taken from PLAN.md on faith and are
    unverified against a real Navidrome: the multi-field sort string
    `-dateadded,filepath`, and the "match everything" rule for All Music
    (`contains: {filepath: ""}`). Both are single-line changes if the real
    server disagrees; Phase 7 must check them first, before anything else.
  - `Service` grew a `ProviderFactory` so a definition's own `provider` selects
    the reader. The old `Provider` field still wins when set, which is what
    every existing test uses, so no existing behavior moved.
  - Managed `.nsp` files carry the ownership marker inside their `comment`
    field, so the same refuse-to-overwrite rule as the config files applies and
    the marker is visible from the server UI too.

---

## Phase 4 — Favorite Migration

**Status:** In review

**Exit gate:** A revalidated plan imports every exact local Apple favorite,
preserves Apple Music, and restores Navidrome pre-state on interrupted apply.

Built as `internal/navidrome/favorites.go`, plus
`music.Reader.ListFavoriteTracks` in `internal/rekordbox/music/music.go`.

### Planning

- [x] Extend the Apple Music reader only as needed to enumerate favorited local
      tracks and canonical paths without mutation. (`ListFavoriteTracks` —
      read-only AppleScript; it tries `favorited` and falls back to `loved`,
      because Music.app renamed the property.)
- [x] Scope candidates to the configured music root and report outside-library
      favorites separately.
- [x] Classify exact-path, already-starred, missing, ambiguous, and diagnostic
      metadata-only matches.
- [x] Include source/server fingerprints, ordered rows, counts, and checksum in
      the immutable plan.
- [x] Make blockers and excluded rows explicit in CLI, RPC, and native models.
      Go-side model done here; the CLI/RPC/native surfaces land in Phases 5–6.

### Apply and recovery

- [x] Reject modified, stale, mismatched-user, or mismatched-library plans.
- [x] Create and verify a Navidrome database backup before changing stars. A
      zero-byte backup file is rejected: it looks like protection but is not.
- [x] Apply exact matches idempotently and record which stars were newly added.
- [x] Verify final exact-path/count parity.
- [x] On failure, unstar only newly added rows and verify compensation.
- [x] Preserve/report the backup and recovery command if compensation fails.
- [x] Never mutate Apple Music favorites or its existing standalone definition.

### Validation

- [x] Test exact migration, already-starred rows, outside-root tracks, missing
      tracks, ambiguity, stale checksums, backup failure, partial API failure,
      compensation, cancellation, retry, and idempotency.
- [ ] Verify an explicit `navidrome-favorites` refresh reflects new server
      stars. Needs a real Amperfy/server round trip — deferred to Phase 7.

### Phase evidence

- Commands: `go test -race ./internal/navidrome/`
- Manual checks: none; `ListFavoriteTracks` runs real AppleScript and is
  verified in Phase 7, since Music automation cannot be faked meaningfully.
- Notes:
  - Apple Music is read-only by construction: `ApplyFavoritePlan` takes
    `[]AppleFavorite` by value and returns nothing addressed to Music. A test
    pins that signature so adding an Apple writer breaks the build.
  - Compensation runs on a detached 30s context, because the failure that
    triggers it is often a cancellation — rolling back on the already-canceled
    context would leave the stars it just created in place.
  - The source fingerprint hashes persistent ID + normalized path, and the
    server fingerprint hashes ID + path + starred state. Either changing
    between plan and apply is a hard refusal with a regenerate hint.

---

## Phase 5 — CLI, Agent, and Post-Sync Integration

**Status:** In review

**Exit gate:** Every public command and RPC has stable wire behavior, and normal
sync remains successful when Navidrome is absent or unhealthy.

Built as `internal/navidrome/manager.go` (the shared composition layer),
`internal/cli/navidrome.go`, `internal/agent/methods_navidrome.go`, and
`internal/app/navidrome_postsync.go`.

### CLI and agent

- [x] Add the `udl navidrome` command tree defined in the high-level plan, plus
      `playlists derive-genres`, `playlists save-genres`, and `backup list`,
      which the workflow needs and the plan did not enumerate.
- [x] Add explicit config-path selection/environment override
      (`--navidrome-config`, `UDL_NAVIDROME_CONFIG`).
- [x] Add protocol-v2 methods for config, dependencies, status, setup,
      playlists, favorite import, and backup — 14 methods, plus
      `navidrome.service.control` for start/stop/restart.
- [x] Use normal run IDs, cancellation, progress, terminal frames, and exit-code
      mapping for long operations. Every mutating or network-bound method goes
      through `StartRun`; `navidrome.status` and the config methods are
      synchronous because they are cheap reads.
- [x] Update initialize capabilities, method lists, fixtures, protocol docs, and
      CLI help/readme. (`testdata/protocol_v2_golden.json`,
      `docs/agent-protocol.md`, `readme.md`)
- [x] **Added:** `credentials.save`/`clear`/`list` learned
      `navidrome_password`, so there stays exactly one code path that writes a
      secret to Keychain.

### Download integration

- [x] After a successful mutating sync, request a Navidrome scan when the feature
      is enabled and authenticated.
- [x] Treat unavailable server, rejected auth, and scan errors as visible
      warnings without changing the download exit code or state writes.
- [x] Do not request scans for dry runs, canceled runs, or no-change runs.
      Plan-only runs and failed runs are skipped too.
- [x] Coalesce multi-source completion into one scan request. The hook runs once
      per `SyncUseCase.Run`, after the result is final.

### Validation

- [x] Add CLI command/flag/output tests and agent method tests.
- [x] Update protocol-v2 golden fixtures and null-collection coverage.
      (`TestNavidromeStatusFrameToleratesNoCredentials` asserts `problems` and
      `managed_playlists` encode as arrays, not `null`.)
- [x] Test post-sync scan success, coalescing, disabled feature, missing
      credentials, server failure, canceled sync, and dry run.

### Phase evidence

- Commands: `go build ./...`, `go vet ./...`, `go test ./...`
- Manual checks: none; every command is covered by a CLI test, and none of them
  can reach a real server in test.
- Notes:
  - **`RequestScanBestEffort` checks `enabled` before touching credentials.**
    The first version built the full manager first, which meant every `udl sync`
    on a machine that has never configured Navidrome would shell out to
    `security` and read the Keychain. It now probes with `SkipCredentials`.
  - The post-sync hook runs on a detached 15s context. A sync can finish
    successfully while the caller's context is already canceled (TUI teardown),
    and inheriting that would make the scan fail every time.
  - `listCredentials` skips nil inspectors. Adding a fourth inspector panicked
    every existing test that supplied a partial `CredentialOperations`; the
    tolerant loop is the fix, not filling in four fields at every call site.

---

## Phase 6 — Native Phone Library Workspace

**Status:** In review

**Exit gate:** A user can complete setup, playlist creation, favorite migration,
and Amperfy connection from one truthful native workflow.

Built as `macos/UDL/Features/PhoneLibrary/{PhoneLibraryModels,PhoneLibraryView}.swift`
and `macos/UDL/Model/AppState+PhoneLibrary.swift`, with
`macos/UDLTests/PhoneLibraryTests.swift`.

### Wire and state

- [x] Add Swift DTOs for every new RPC result/plan with default-empty collection
      decoding. (`@DefaultEmpty` on every inbound collection;
      `testEveryPhoneLibraryResultDecodesWithAllCollectionsNull` decodes all six
      result shapes with every array as JSON `null`.)
- [x] Add Phone Library workflow state, operation state, cancellation, recovery,
      and reconnect behavior.
- [x] Never replay install, setup apply, favorite apply, or service mutations
      after backend restart. (`PhoneLibraryOperation.isMutating`,
      `interruptPhoneLibrary()`, and the `notResumed` route.)

### Workspace

- [x] Add a Phone Library destination and sidebar glyph/status badge. The badge
      counts remaining setup steps and stays absent until status has actually
      been read, so it never shows an invented number.
- [x] Present dependency/version state and a confirmed Install/Repair action.
- [x] Present service status and Start/Stop/Restart/Open Local Server actions.
- [x] Present library path, track count, scan status, log state, and backup state.
- [x] Add account URL/username/password form with an empty masked secret field.
      The field is never preloaded with the stored secret — that is the bug
      where a pasted replacement silently appends to the old value.
- [x] Present setup plan contents and blockers before apply. "Show file
      contents…" shows the exact bytes apply would write.
- [x] Present derived genre values and unmatched/genre-less counts before save.
- [x] Present favorite migration rows, blockers, counts, checksum, backup, and
      parity result, including the recovery command when compensation failed.
- [x] Present hostname/IP connection strings, copy action, Amperfy guidance,
      Local Network permission guidance, and the approximate storage note. The
      size is derived from the live track count rather than hardcoded at 9.4 GiB
      — see the decision log.
- [x] Keep failures inline and explain every disabled action beside the control.

### Validation

- [x] Add wire decoding, state transition, secret-absence, constraint, and
      plan-presentation tests. (14 new tests; 79 Swift tests pass in total.)
- [x] Update source-rule and alert allowlist budgets deliberately — **no update
      was needed**. `PhoneLibraryView.swift` uses `.constrained(by:)` at every
      disable site and adds no `alertMessage` assignment, so both existing
      budgets in `SourceRuleTests` still pass unchanged. That is the intended
      outcome, not an oversight.
- [ ] Build and manually inspect light/dark, empty, partial, running, failed, and
      complete states. The app builds and launches, but a meaningful visual pass
      needs a real installed server; deferred to Phase 7.

### Phase evidence

- Commands: `make app-dev`, `make app-test-dev` (79 passed, 0 failed)
- Manual checks: the app builds and signs; screen states beyond "nothing
  installed yet" need a real Navidrome, so they belong to Phase 7.
- Notes:
  - `AppState`'s Phone Library properties are plain `@Published var`, not
    `private(set)`: Swift scopes `private(set)` to the declaring file and the
    workflow lives in `AppState+PhoneLibrary.swift`. `client`,
    `consumeBufferedFinished`, and a `decodeWire` helper widened for the same
    reason.
  - The setup and favorite plans follow the `RekordboxPlanPresentation` rule:
    the checksummed `JSONValue` is kept verbatim and sent back untouched, and
    every field on screen is a *read* of it. A Swift round trip would drop
    `omitempty` fields and break the checksum.
  - The "Connect Amperfy" step is never marked done. No backend state can
    observe whether a phone connected, so claiming it would be a lie; the
    checklist stops at 5 of 6 and names the last step as current.

---

## Phase 7 — Real-Server and Amperfy Acceptance

**Status:** In progress — server side complete, phone side pending

The isolated harness and the full real-library rollout are done and passing.
Only the Amperfy items remain, and they need the iPhone.

**Exit gate:** The isolated server and real library pass non-mutation, ordering,
favorite, full-download, and offline-playback acceptance.

### Automated real-server acceptance

- [x] Add an opt-in test using `UDL_NAVIDROME_ACCEPTANCE_BINARY` and temporary
      music/data/config/backup directories on a random loopback port.
      (`internal/navidrome/acceptance_test.go`)
- [x] Use copied fixtures with controlled birth times, equal-time ties, genres,
      Unicode paths, and favorites. Fixtures are generated with `ffmpeg` and
      their APFS birth times set with `SetFile -d`.
- [x] Verify scan counts, `dateadded` ordering, filepath tie-breaking, smart
      playlist refresh, stars, backup, restart, and exact cleanup.
- [x] Verify source file hashes, sizes, birth times, and modification times are
      identical before and after the server run.
      (`birthtime_darwin_test.go` reads `Stat_t.Birthtimespec` directly.)
- [x] **Actually run it.** Executed against Homebrew Navidrome 0.63.2 and
      passing. It found four real defects: the hidden playlists directory, the
      wrong backup subcommand, `ND_DEVAUTOCREATEADMINPASSWORD` set on the test
      process instead of the child, and the `dateadded` semantics above.

### Real library rollout

*Rolled out on this machine with explicit go-ahead.*

- [x] Capture a pre-scan manifest for all supported audio files. 1,593 files
      (1,574 audio + 19 non-audio) with SHA-256, size, mtime, and APFS birth
      time.
- [x] Apply the owned service setup and create/connect the first admin account.
      Service `com.jaa.udl.navidrome` is running with `RunAtLoad`; account `jaa`
      created through `/auth/createAdmin` with a generated 28-character password
      written straight to Keychain and never printed.
- [x] Scan the complete library and reconcile expected, indexed, unsupported,
      and failed files. 1,574 of 1,574 audio files indexed; nothing unsupported
      or failed.
- [x] Compare the post-scan manifest and stop on any source-file mutation.
      **0 differences across all 1,593 files** — every byte, size, mtime and
      birth time identical, re-verified after the favorite import.
- [x] Derive/approve Hard Bounce genres and verify known ordering examples.
      436 of 436 Apple Music tracks matched by real path, 0 unmatched, 0
      genre-less, 0 outside the library. Approved `Hard Bounce`, `HARD BOUNCE`,
      `bounce` → 510 tracks. Ordering examples confirm the `dateadded`
      deviation below rather than contradicting it.
- [x] Plan/apply favorite migration and record exact parity evidence.
      151 Apple favorites → 151 matched exactly by path, 0 missing, 0 ambiguous,
      0 outside library, 0 metadata-only. Applied after a verified backup;
      `Favourites (Navidrome)` reports 151 and Apple Music still reports 151.

### Amperfy acceptance

*Every item below needs the iPhone and the companion app.*

- [ ] Connect Amperfy over home Wi-Fi using the shared account.
- [ ] Verify All Music, HARD BOUNCE, and Favourites ordering and membership.
- [ ] Confirm enough free storage and download the complete All Music playlist.
- [ ] Disable Wi-Fi/Mac availability and play multiple downloaded tracks.
- [ ] Favorite/unfavorite while connected and verify Navidrome plus explicit UDL
      refresh.
- [ ] Test favorite changes while completely offline and record whether Amperfy
      queues them on reconnect.
- [ ] Verify lock-screen, Control Center, Dynamic Island, and favorite control;
      record any client limitation without starting custom-app work.

### Phase evidence

- Commands: `go test ./internal/navidrome/ -run Acceptance` — **skips**, as
  designed, until `UDL_NAVIDROME_ACCEPTANCE_BINARY` is set.
- Manual checks: none.
- Notes:
  - Both `.nsp` details flagged as unverified in Phase 3 are now **confirmed
    working**: `contains: {filepath: ""}` matches the whole library, and the
    multi-field sort `-dateadded,filepath` is parsed and applied. The exact
    genre allowlist (`is: {genre: …}`) matched exactly the two intended tracks
    and did not broaden.
  - What was *not* right is what `dateadded` means. See the deviation above.
  - `getPlaylists` reports `songCount: 0` for a smart playlist until it is
    refreshed; the real membership comes from `getPlaylist`. The native
    workspace shows the `getPlaylists` count, so a freshly imported playlist can
    read "0 tracks" for a moment. Cosmetic, not yet addressed.
  - The acceptance test creates the first admin through
    `ND_DEVAUTOCREATEADMINPASSWORD`, which Navidrome always names `admin`
    regardless of configuration. The variable must be on the server child's
    environment — setting it on the test process silently does nothing, which is
    how the first run failed.

---

## Phase 8 — Documentation and Close-Out

**Status:** In progress

**Exit gate:** Installation, operation, recovery, security boundaries, and final
validation are documented and every tracker item is resolved.

- [x] Document Homebrew install/repair, managed paths, LaunchAgent behavior,
      account creation, Keychain storage, and local URLs. (`readme.md`
      `navidrome` command section, `docs/agent-protocol.md`)
- [ ] Document Amperfy connection, Local Network permission, full download,
      playlist refresh, offline playback, and observed favorite behavior. The
      in-app guidance exists; the standalone doc waits on observed behavior
      from Phase 7 rather than describing untested steps.
- [ ] Document backups, restore while stopped, favorite compensation, service
      logs, port conflicts, uninstall, and unowned-install refusal. Wants its
      own `docs/navidrome-recovery.md`, written after the recovery path has
      been walked once for real.
- [x] State that v1 is trusted-LAN HTTP only and must not be port-forwarded.
      (`readme.md`, and inline in the Phone Library workspace)
- [ ] Document deferred Tailscale/remote-access direction without configuring it.
- [x] Run and record `go test ./...`, `go vet ./...`, targeted race tests,
      protocol tests, native tests, and builds. Opt-in acceptance skips — see
      Final Validation Evidence.
- [ ] Update [navidrome-phone-library-plan.md](./navidrome-phone-library-plan.md) status and this dashboard only after every exit
      gate is satisfied. PLAN.md deliberately still reads `Planned`.

### Phase evidence

- Commands:
- Manual checks:
- Notes:

---

## Decision and Deviation Log

Record decisions that refine or deviate from the high-level plan. Do not edit
the plan silently after implementation starts.

| Date | Phase | Decision or deviation | Reason | Follow-up |
| --- | --- | --- | --- | --- |
| 2026-08-04 | Planning | Use native APFS creation time instead of normalizing modification times | Navidrome natively reads birth time on macOS; this avoids mutating 1,574 files | Verify with isolated and full-library manifests |
| 2026-08-04 | Planning | Keep Apple Music `favorites` and `navidrome-favorites` permanently separate | Preserve the current Apple workflow and avoid an irreversible provider cutover | Downstream Rekordbox mapping remains an explicit later choice |
| 2026-08-04 | Planning | Derive an explicit genre allowlist from the current HARD BOUNCE playlist | Reproduces current intent without a permanently broad `Hard*` rule | Preview and approve the derived values before writing `.nsp` |
| 2026-08-04 | 1 | HTTP health lives in Phase 2, not Phase 1 | Health means an authenticated Subsonic ping; the credential and client layer that makes it meaningful is Phase 2. Phase 1 reports launchd state, PID, and last exit code | `ServiceStatus` gains a health field once the client exists |
| 2026-08-04 | 1 | Setup plan carries the full rendered file contents | Lets the native workspace show exactly what apply will write, and lets apply verify content against its own checksum without re-rendering | Keep every new plan field non-`omitempty`-safe per the Rekordbox checksum lesson |
| 2026-08-04 | 1 | Ownership marker is a literal comment string in each managed file | A marker inside the file survives copies and moves, unlike an external manifest, and is trivially checkable before any overwrite | — |
| 2026-08-04 | 2 | Paths are NFC-folded, not symlink-resolved | Apple Music returns NFD filenames while Navidrome reports what it read from disk, so folding is required for matching. Resolving symlinks would make a path depend on filesystem state at read time and break plan revalidation | Record real-library match counts in Phase 7 |
| 2026-08-04 | 2 | Database backup runs the `navidrome backup` CLI, not an API call | Navidrome exposes no Subsonic backup endpoint; the CLI is the supported path and writes into the managed `Backup.Path` | Verify against the real binary in Phase 7 |
| 2026-08-04 | 2 | `UDL_NAVIDROME_PASSWORD` exists as an env override | Matches the existing Deezer/SoundCloud credential pattern and keeps headless/CI use possible without Keychain prompts | The GUI and TUI must always write to Keychain instead |
| 2026-08-04 | 7 | **`dateadded` is import time, not APFS creation time — PLAN.md decisions 2 and 6 do not hold as written** | Probed Navidrome 0.63.2 directly: `media_file.birth_time` *is* populated correctly from APFS, but no smart-playlist criteria field exposes it. `dateadded` resolves to `media_file.created_at`, which is when the scanner first saw the file. A file with a 2019 birth time added today gets today's `created_at` | **Open — needs a product decision.** On first scan the whole back catalogue gets one timestamp, so day-one order is scan order, not creation order. See "Ordering deviation" below |
| 2026-08-04 | 7 | Default `PlaylistsPath` changed from `.udl/playlists` to `udl-playlists` | Navidrome's scanner skips hidden files and folders. With the dot-directory from PLAN.md, all three `.nsp` files were walked past in silence and no playlist was ever imported | `Validate` now refuses any hidden segment and says why |
| 2026-08-04 | 7 | UDL validates its own smart-playlist sort fields | Navidrome silently ignores an unrecognised sort field and returns the playlist in arbitrary order rather than erroring. `birthtime`, `birth_time`, `created_at`, `recentlyadded`, `filemodified` all behaved identically ascending and descending | `ManagedSortFields` + `ValidateSort`; `MarshalSmartPlaylist` refuses to write a playlist Navidrome would misorder |
| 2026-08-04 | 7 | Backup runs `navidrome backup create`, not `navidrome backup` | `navidrome backup` alone is a parent command that prints help and exits 0. The "reported success but no new backup appeared" guard is what caught it | — |
| 2026-08-04 | 7 | LAN address comes from `scutil --get LocalHostName`, not `os.Hostname` | `os.Hostname` returned the DHCP-assigned `MacBookPro.webspeed.dk`, which a phone on the same Wi-Fi cannot resolve. The Bonjour name is `jaa-mpb.local` | — |
| 2026-08-04 | 7 | **Genre allowlist deduplicates exactly, not case-insensitively** | Navidrome matches `is: {genre: …}` case-sensitively — verified: a rule for `bounce` returned only the lowercase-tagged track. The old case-folding collapsed `Bounce` and `bounce` into one spelling; in the real library the losing spelling covered 155 tracks | `NormalizeGenres` now dedupes exactly; each spelling gets its own clause |
| 2026-08-04 | 7 | `ListFavoriteTracks` AppleScript renamed its `matched` variable | `matched` is a Music.app term (smart-playlist "matched"); using it as a variable failed with `-10003 Access not allowed`. Also confirmed `loved` no longer exists in this Music version and `favorited` is the working property | — |
| 2026-08-04 | 7 | `RefreshManagedPlaylists` reads each imported playlist once | `getPlaylists` reports `songCount: 0` for a smart playlist Navidrome has not evaluated yet; only reading it forces evaluation. Without this the workspace showed a full 1,574-track playlist as empty | — |
| 2026-08-04 | 7 | Date Added is reconciled from `birth_time` via `udl navidrome dates reconcile` | The only way PLAN.md decisions 2 and 6 can hold: Navidrome exposes no birth-time sort field. Probed to survive quick/full rescans and touched-file re-reads, and to be safe while live | Re-run after importing older files; considered wiring it into the post-sync scan but that would take a backup on every sync |
| 2026-08-05 | 7 | **ffmpeg is a hard dependency, and the launchd service could not find it** | Amperfy failed to play or download 923 of 1,574 tracks with `Internal Server Error: invalid argument`. The server log gave the real cause: `exec: "ffmpeg": executable file not found in $PATH`. launchd does not read a login shell, so the service ran with the bare default PATH and never saw Homebrew. Only the 651 mp3s, which stream raw, worked | Fixed in two places: `FFmpegPath` is pinned by absolute path in the managed TOML, and the LaunchAgent now sets `EnvironmentVariables/PATH`. `DependencyChecker` reports ffmpeg, `Ensure` installs it, and setup warns when it is missing |
| 2026-08-05 | 7 | `Status` reports managed files that are out of date | The ffmpeg fix was invisible on an existing install: status showed everything healthy while the running service still used the old config. Status now re-plans and flags any file whose action is `replace` | This is what makes a shipped config change discoverable rather than silent |
| 2026-08-05 | 7 | `Start` retries `launchctl bootstrap` through the teardown window | The real restart failed with `Bootstrap failed: 5: Input/output error` and left the service down until it was started by hand — launchd still held the job just booted out. Retried with a short backoff; non-transient failures are still surfaced immediately | — |
| 2026-08-04 | 7 | Trance excluded from the approved allowlist | One mis-tagged track in the Apple `HARD BOUNCE` playlist carries genre `Trance`; approving it would pull in 40 unrelated Trance tracks library-wide | Reversible: re-run `save-genres` with `--genre Trance` to include it |

## Ordering Deviation — needs a decision

PLAN.md decision 6 says managed playlists sort by "newest creation time first".
Verified against Navidrome 0.63.2, that is not achievable with a smart playlist:

- `media_file.birth_time` holds the correct APFS creation time.
- `media_file.created_at` holds the time the scanner first saw the file.
- `dateadded` — the only date-added criteria field Navidrome honours — reads
  `created_at`. No criteria field reads `birth_time`.

So `-dateadded,filepath` means **newest added to Navidrome first**. Going
forward that is exactly right: each newly downloaded track lands at the top.
But the initial full scan stamps all ~1,574 existing files with one timestamp,
so on day one `All Music` is ordered by scan order, not by when tracks were
actually added.

**Resolved** by option 2, after measuring all three:

1. *Accept it* — rejected; day-one order would be meaningless for the whole
   existing library.
2. *Seed `created_at` from `birth_time`* — **taken.** Implemented as
   `ReconcileDateAdded` / `udl navidrome dates reconcile`. Verified it survives
   quick rescans, full rescans and re-reads of touched files, runs safely while
   the server is live, is idempotent, and takes a backup first.
3. *Sort by `datemodified`* — rejected on evidence. In the real library,
   birth-order and mtime-order agree in only 10 of 400 sampled files: birth
   times reach back to 2014 while mtimes start in 2019 because of later tag
   edits.

Applied to the real library: 1,574 rows updated, and `All Music` now runs
2026-08-03 → 2014-02-18, strictly monotonic against real APFS birth times.

This also fixes the client side. A Subsonic client's own "Date Added" sort reads
the `created` field, which is the same `created_at` column, so it was wrong for
the same reason. One server-side fix corrects both.

**Caveat:** newly discovered files still get discovery time, which for a fresh
download is within minutes of birth time. Re-run the reconcile after importing a
batch of older files.

## Final Validation Evidence

- Go unit/integration: `go test ./...` — pass
- Go race: `go test -race ./internal/...` — pass
- Go vet: `go vet ./...` — clean
- Agent protocol: `go test ./internal/agent/` — pass, golden inventory updated
  with the 14 `navidrome.*` methods and a `navidrome` group
- Native tests: `make app-test-dev` — 79 passed, 0 failed
- Native build: `make app-dev` — builds and ad-hoc signs
- Isolated Navidrome acceptance: **passes** against a real Navidrome 0.63.2 —
  `UDL_NAVIDROME_ACCEPTANCE_BINARY=/opt/homebrew/bin/navidrome go test ./internal/navidrome/`.
  Covers scan, indexing at real paths, playlist import and ownership, exact
  genre matching, monotonic sort, star round trip, backup, restart persistence,
  and a byte/size/mtime/birth-time manifest that is identical before and after.
- Full-library manifest comparison: **0 differences** across 1,593 files
  (SHA-256, size, mtime, APFS birth time), checked after scan and again after
  the favorite import
- Favorite parity: **exact** — 151 Apple favorites → 151 Navidrome stars, Apple
  Music unchanged at 151
- Amperfy full-download/offline playback: **not run** — needs the phone
- Lock-screen/Dynamic Island: **not run** — needs the phone
