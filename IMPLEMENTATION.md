# `udl triage` — Implementation Tracker

- **Overall status:** Not started
- **Plan:** [PLAN.md](./PLAN.md)
- **Last updated:** 2026-08-06
- **Archived predecessor:**
  [plans/archive/navidrome-likes-to-rekordbox-implementation.md](./plans/archive/navidrome-likes-to-rekordbox-implementation.md)

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

Do not check a task that is only coded. Add newly discovered work explicitly; do
not silently broaden an existing checkbox.

## Phase Overview

| # | Phase | Status | Depends on | Exit gate |
| --- | --- | --- | --- | --- |
| 0 | Carried forward from Navidrome likes | In progress | — | Every open predecessor item closed or explicitly re-deferred |
| 1 | Document restructure | Done | — | Root plan and tracker describe `udl triage`; archive describes nothing as current |
| 2 | `internal/tags` | Not started | — | A genre can be set, cleared, and left alone on mp3 and m4a without losing `comment` or birthtime |
| 3 | Triage core + CLI (P0) | Not started | 2 | `udl triage list/set/skip` works end to end with no HTTP surface |
| 4 | Minimal serve (P1) | Not started | 3 | Genre triage from the phone, over LAN and tailnet |
| 5 | Text cleanup + installability (P2) | Not started | 4 | Title/artist editing and a home-screen icon |
| 6 | Background hygiene (P3) | Not started | 4 | Rescan, dates, overlay persistence, concurrency safety |
| 7 | Surfaces + classifier (P4) | Not started | 4 | Agent RPC and non-empty `suggestions` |

---

## Phase 0 — Carried Forward from Navidrome Likes to Rekordbox

**Status:** In progress

**Exit gate:** Every `[ ]` left in the archived predecessor tracker is closed
here or explicitly re-deferred with a recorded reason.

This gates nothing in Phases 2–7. It is bookkeeping that must not evaporate
because the root documents changed subject.

### The one deliberate user action

- [ ] Apply the `navidrome-favorites` → `nav_fav_imports` plan with Rekordbox
      closed, and verify membership. **Deliberately not run — the user asked to
      keep this.** The plan is written and verified; applying it writes to the
      Rekordbox database and is a one-command manual step.
- [ ] Un-star a track and confirm the removal propagates. Not exercised live;
      the removal path is covered by unit tests.
- [ ] Move the archived plan's status to `Delivered` once the apply is run.

### Amperfy acceptance

*Needs the iPhone and the companion app. The favourite round trip is already
closed — validated with **substreamer**, a different Subsonic client.*

- [ ] Connect Amperfy over home Wi-Fi using the shared account.
- [ ] Verify All Music, HARD BOUNCE, and Favourites ordering and membership.
- [ ] Confirm enough free storage and download the complete All Music playlist.
- [ ] Disable Wi-Fi/Mac availability and play multiple downloaded tracks.
- [ ] Test favorite changes while completely offline and record whether the
      client queues them on reconnect.
- [ ] Verify lock-screen, Control Center, Dynamic Island, and favorite control.

### Documentation

- [ ] Document Amperfy connection, Local Network permission, full download,
      offline playback, and observed favorite behavior. Open because it
      documents *observed* behaviour and Amperfy itself has not been exercised.
- [ ] Write `docs/navidrome-recovery.md`. **Re-deferred** — the rule was to write
      it after walking the recovery path for real, and walking it means
      deliberately breaking a working install.

### Re-deferred as cosmetic

- [ ] Dark mode and the empty / partial / failed states of the Phone Library
      workspace. `.dev/app.sh appearance true` exists for this.
- [ ] The revoked-grant messages in the Doctor pane and the playlist pane. Both
      would require revoking a working Automation grant on the user's machine;
      the Go tests cover the denied branches including the actionable text.
      Recorded as deliberate non-actions, not oversights.

---

## Phase 1 — Document Restructure

**Status:** Done

**Exit gate:** The root `PLAN.md` and `IMPLEMENTATION.md` describe `udl triage`,
the archive describes nothing as current, and no relative link dangles.

- [x] `git mv` the likes-to-Rekordbox plan and tracker into `plans/archive/`
      with rename detection intact, so `git log --follow` still resolves them.
- [x] Rewrite both archived headers: status becomes superseded/archived, and
      each points forward to the current root documents so neither claims to be
      the source of truth.
- [x] Fix relative links inside the moved files — sibling archive links lose
      their `plans/archive/` prefix; root links gain `../../`.
- [x] Write the new root `PLAN.md`.
- [x] Write this tracker, carrying the predecessor's open items into Phase 0.
- [x] Update `plans/archive/README.md`: add the two new entries and remove the
      trailing paragraphs describing likes-to-Rekordbox as the current work.
- [x] Verify no dangling links across all plan documents.

### Evidence

Renames tracked, so `git log --follow` resolves once committed:

```
$ git status --porcelain
RM IMPLEMENTATION.md -> plans/archive/navidrome-likes-to-rekordbox-implementation.md
RM PLAN.md -> plans/archive/navidrome-likes-to-rekordbox-plan.md
 M plans/archive/README.md
?? IMPLEMENTATION.md
?? PLAN.md
?? plans/holy-grail-music-loop.md
```

Every relative link in `PLAN.md`, `IMPLEMENTATION.md`,
`plans/holy-grail-music-loop.md`, and all of `plans/archive/*.md` resolves to an
existing file. No inbound references to `PLAN.md` / `IMPLEMENTATION.md` exist in
`readme.md`, `AGENTS.md`, `Makefile`, or `.github/`, so nothing else needed
updating.

**Phase 1 status → Done** once the remaining Phase 0 bookkeeping is unaffected;
the restructure itself is complete.

---

## Phase 2 — `internal/tags`

**Status:** Not started

**Exit gate:** A genre can be set, cleared, and left alone on both mp3 and m4a,
with `comment` and APFS birthtime provably intact afterwards.

- [ ] `Tags` / `Edit` / `Read` / `Write` with the three-state pointer semantics
      (`nil` = leave, `""` = clear, else set).
- [ ] `Write` copies `internal/freedl/service.go:837` `applyReplacement`
      ordering exactly: `ReadCreationTime` → sibling temp → ffmpeg →
      `RestoreCreationTime` **before** the rename → `ReplaceFileSafely`.
- [ ] Temp removed on every error path.
- [ ] Real `exec.CommandContext` timeout. The existing tagger has none; a hang
      on a corrupt file would wedge an HTTP handler forever.
- [ ] `-id3v2_version 3` for mp3 — Rekordbox reads 2.3 more reliably than
      ffmpeg's 2.4 default. **Confirm against the real library before locking
      this in.**
- [ ] `CommandRunner` seam so argv is assertable without ffmpeg installed.
- [ ] Table tests on exact argv: set genre, clear genre (`-metadata genre=`
      present and empty), leave genre (flag absent entirely).
- [ ] Test that `comment` never appears in argv.
- [ ] Round-trip test that `comment` survives a write.
- [ ] Birthtime preservation test on darwin, shaped like
      `internal/fileops/creation_time_darwin_test.go`.
- [ ] Integration test gated on `exec.LookPath("ffmpeg")` and `!testing.Short()`:
      synthesize a 1s silent MP3 via `-f lavfi -i anullsrc`, write, re-read.
      **This is the only test that catches "ffmpeg silently didn't write it".**

### Evidence

_(record commands and output here)_

---

## Phase 3 — Triage Core and CLI (P0)

**Status:** Not started

**Exit gate:** `udl triage list|set|skip` works end to end against the real
library with no HTTP surface at all.

### Config and taxonomy

- [ ] `internal/triage/config.go` mirroring `internal/navidrome/config.go`:
      `ConfigVersion`, `EnvConfigPath = "UDL_TRIAGE_CONFIG"`, pointer-field
      `fileConfig` + `apply`, precedence explicit-only → user → project,
      `Validate` accumulating **all** problems into one `; `-joined error with
      dotted paths, `Save` via `navidrome.AtomicWrite`.
- [ ] Secret-grep test cloned from `internal/navidrome/config_test.go:31`.
- [ ] `taxonomy.go` with grouped genres and an embedded default so zero setup
      works.
- [ ] Taxonomy `Validate` rejects empties, case-insensitive duplicates, and any
      genre containing `;`, `/`, or `,`.

### Catalog, queue, state

- [ ] `catalog.go` reading the full catalog via `navidrome.Client.Songs()`,
      filtered to paths under the resolved music dir.
- [ ] Overlay: `map[normalizedPath]overlayEntry` under `RWMutex`, applied over
      every snapshot, dropped when a fresh snapshot agrees or after a 6h TTL.
- [ ] `queue.go` as a **pure** `Derive(catalog, taxonomy, skips, overlay)`.
- [ ] `skiplist.go` at `<state_dir>/triage/skiplist.json`, NFC-normalized paths,
      whole-file rewrite under a mutex via `navidrome.AtomicWrite`.
- [ ] Prune skip entries whose path no longer exists — a renamed track returns
      to the queue, which is the safe failure direction.
- [ ] `save.go` orchestrating `tags.Write` + overlay + skip removal.

### CLI

- [ ] `internal/cli/triage.go` with `list`, `set`, `skip`/`unskip`,
      `config path|show`.
- [ ] One `root.AddCommand` line and a `--triage-config` persistent flag.
- [ ] **Do not** copy `agent.go`'s `os.Chdir` + deferred restore. Resolve paths
      absolutely instead.
- [ ] Exit codes: `InvalidConfig` for config/taxonomy, `MissingDependency` for
      absent ffmpeg/ffprobe or unreachable Navidrome, `RuntimeFailure` otherwise.

### Tests

- [ ] Queue derivation table test: empty genre; non-canonical; canonical;
      **case-mismatched stays queued**; multi-genre with one canonical; skipped
      excluded; overlay masks a stale catalog value; path outside music dir
      excluded.
- [ ] Skip list and overlay round-trip in `t.TempDir()`; concurrent access under
      `-race`; missing-path pruning.
- [ ] `--help` walk test registering the whole `triage` subtree, shaped like
      `internal/cli/navidrome_test.go:22`.
- [ ] `--triage-config`-honoured test, shaped like `:47`.

### Manual validation

- [ ] `udl triage list --limit 20` surfaces the known offenders — `240 SOUND`,
      `bladee`, and the four spellings of Hard Bounce.
- [ ] `udl triage set --path <m4a> --genre "Hard Bounce"`, then `ffprobe`:
      genre changed, `comment` preserved, `stat -f %B` unchanged.
- [ ] Same on one of the 12 WAVs — expect a clean refusal, not a silent no-op.

### Evidence

_(record commands and output here)_

---

## Phase 4 — Minimal Serve (P1)

**Status:** Not started

**Exit gate:** Genre triage works from the phone over both LAN and tailnet, and
a saved track leaves the queue without waiting for a Navidrome scan.

### Auth

- [ ] `internal/auth/triage_token.go` mirroring `navidrome_password.go`;
      Keychain service `udl.triage`, account `token`; env override
      `UDL_TRIAGE_TOKEN`.
- [ ] `crypto/subtle.ConstantTimeCompare` for token comparison.
- [ ] 6-digit pairing code: single-use, 5-minute expiry, rate-limited to 5
      attempts, constant-time compared.
- [ ] Session cookie `HttpOnly; SameSite=Lax; Path=/`, 30-day max age.
- [ ] `udl triage token set|show|rotate`.

### Server

- [ ] `udl triage serve` printing LAN and tailnet URLs via
      `navidrome.LocalHostNames()` — it already handles the DHCP-FQDN trap that
      would otherwise print a hostname the phone cannot resolve.
- [ ] Routes: `/api/session`, `/api/taxonomy`, `/api/queue`,
      `/api/tracks/{id}/save`, `/api/tracks/{id}/skip`, `/api/stream/{id}`,
      `/api/refresh`, `/api/health`.
- [ ] Track id = `hex(sha256(NormalizePath(path)))[:16]`, resolved to a path
      **only** from the server's own snapshot.
- [ ] `suggestions` field present and always empty — the classifier seam.
- [ ] `expect_mtime_ns` optimistic concurrency → 409 on mismatch.
- [ ] Save under a process-global mutex, with `context.WithoutCancel` so the
      phone locking its screen cannot kill ffmpeg mid-write.
- [ ] Re-probe after write and fail loudly if the genre did not land.
- [ ] Respond without waiting for the rescan.

### Streaming

- [ ] `Stream()` added to `internal/navidrome/client.go` so the credential never
      leaves that package. No retries — retry-on-5xx is wrong for a
      partial-content body.
- [ ] **Dedicated HTTP client**, not `Client.HTTP` — its 60s whole-request
      timeout would guillotine any longer stream.
- [ ] **No `WriteTimeout`** on the `http.Server`. `ReadHeaderTimeout` and
      `IdleTimeout` only.
- [ ] Proxy `maxBitRate=128&format=mp3`; a 60 MB WAV preview over cellular is
      unusable.
- [ ] `?raw=1` → `http.ServeContent` from disk, giving free Range handling and a
      test path needing no fake upstream.

### PWA

- [ ] `web/` tree embedded with `//go:embed`; vanilla JS, no build step.
- [ ] One card per track; genre chips in a 2-column grid with sticky group
      headers in the thumb zone; ~56px targets.
- [ ] Tap a chip → save → auto-advance. One tap per track is the entire point.
- [ ] Optimistic advance with undo toast; 409/422 brings the card back with the
      error.
- [ ] Case-mismatch explained in the UI so it does not read as a bug.
- [ ] `app.js?v=<buildhash>` with strong `ETag`; `index.html` `no-cache`.

### Tests

- [ ] `httptest` over the real mux with an injected fake catalog and a fake
      upstream shaped like `internal/navidrome/fakeserver_test.go`.
- [ ] 401 without a cookie on every `/api/*`.
- [ ] Pairing code single-use, expiring, rate-limited.
- [ ] `Range: bytes=10-19` → 206 with correct `Content-Range` and body slice.
- [ ] `Set-Cookie` carries `HttpOnly` and `SameSite`.
- [ ] Wrong `expect_mtime_ns` → 409; off-taxonomy genre → 422; unknown id → 404.
- [ ] An arbitrary path string as `{id}` cannot reach the filesystem.

### Manual validation

- [ ] Pair from the phone, triage five tracks, confirm each leaves the queue
      **without** waiting for a scan — this is what proves the overlay.
- [ ] Repeat from the tailnet URL with Wi-Fi off.
- [ ] After the rescan settles: `udl navidrome status` shows the new genres and
      Date Added has not moved.

### Evidence

_(record commands and output here)_

---

## Phase 5 — Text Cleanup and Installability (P2)

**Status:** Not started

**Exit gate:** Title and artist can be cleaned from the phone, and the app
installs to the home screen.

- [ ] Strip regexes in Go (`[FREE DL]`, `(OUT NOW)`, `*FREE DOWNLOAD*`, trailing
      cruft, collapsed whitespace), unit-tested, shared with `udl triage list`.
- [ ] Server computes `suggested_title` / `suggested_artist`.
- [ ] One-tap **Clean** button per field showing an accept/reject diff.
- [ ] Skip / unskip from the UI.
- [ ] `manifest.webmanifest`, `apple-touch-icon`,
      `apple-mobile-web-app-capable`, `viewport-fit=cover`.
- [ ] **No service worker** — a stale cache on a device you cannot attach
      devtools to means shipping a fix the phone ignores.

---

## Phase 6 — Background Hygiene (P3)

**Status:** Not started

**Exit gate:** Repeated triage sessions leave Date Added, the catalog, and
concurrent `udl sync` runs all intact.

- [ ] Debounced rescan: `StartScan(ctx, false)` → poll `ScanStatus` until idle
      (cap ~10 min) → refresh snapshot. Re-arm rather than scan if a save is in
      flight.
- [ ] `ReconcileDateAdded` after a rescan settles. Expected to be a no-op
      because `RestoreCreationTime` keeps `birth_time` correct — **verify that
      empirically** rather than assuming.
- [ ] Overlay persisted to `<state_dir>/triage/overlay.json` so a restart
      mid-session does not resurrect tracks just triaged.
- [ ] Advisory lockfile against concurrent `udl sync` / `promote-freedl`.
- [ ] Session persistence, if re-pairing after a restart proves annoying.

---

## Phase 7 — Surfaces and Classifier (P4)

**Status:** Not started

**Exit gate:** Triage is reachable from the native app, and suggestions appear
without a client change.

- [ ] Agent RPC `triage.status`, `triage.serve.start|stop`. Remember this is
      **three** edits: `protocolMethods` (`internal/agent/server.go:70-119`),
      the dispatch switch (`:155`), and
      `internal/agent/testdata/protocol_v2_golden.json`, which
      `protocol_contract_test.go:12` `reflect.DeepEqual`s.
- [ ] `GenreSuggester` behind the `suggestions` field shipped empty since
      Phase 4. Cheap signals only: SoundCloud's genre tag, and
      nearest-neighbour on artist against already-classified tracks.
- [ ] Native app screen, if the PWA proves it is worth one.

---

## Decision Log

Record choices made during implementation here, including ones later found
wrong — the predecessor's candour about being "wrong twice" was worth more than
a clean-looking record.

- **2026-08-06 — Taxonomy lives in its own file, not `navidrome.yaml`.**
  `Playlists.HardBounceGenres` there is machine-derived and overwritten wholesale
  by `navidrome playlists save-genres`. A hand-curated list next to a
  machine-overwritten one invites exactly one bad day.
- **2026-08-06 — Proxy the audio stream rather than pointing the browser at
  Navidrome.** Not for CORS (a plain `<audio src>` media load is a no-CORS
  request and works cross-origin today) but because `AuthParams()` yields a
  never-expiring, unscoped Subsonic credential.
- **2026-08-06 — Session cookie, not bearer-only.** `<audio src>` cannot carry
  an `Authorization` header, and putting the token in a URL leaks it into
  history and the home-screen shortcut.
- **2026-08-06 — The SoundCloud tagger is not refactored here.** Its skip-empty
  semantics are load-bearing on the sync hot path.
- **2026-08-06 — WAV downgraded from blocker to 422.** Measured at 12 of 1,574
  files.
- **2026-08-06 — Genre separators handled by taxonomy validation, not by pinning
  Navidrome's config.** No existing genre tag contains `;`, `/`, or `,`, so
  prevention is free; pinning `GenreSeparators` would rewrite the
  ownership-marked `navidrome.toml` and force a full rescan.
