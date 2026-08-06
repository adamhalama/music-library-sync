# `udl triage` — Remote Metadata Triage From the Phone

- **Status:** Planned
- **Target:** Open a page on the phone, hear a track, tap a genre. The file's
  tag changes on the Mac, Navidrome picks it up, and Rekordbox reads the right
  genre on import.
- **Last updated:** 2026-08-06
- **Implementation tracker:** [IMPLEMENTATION.md](./IMPLEMENTATION.md)
- **Predecessor:**
  [plans/archive/navidrome-likes-to-rekordbox-plan.md](./plans/archive/navidrome-likes-to-rekordbox-plan.md)
- **Wider context:**
  [plans/holy-grail-music-loop.md](./plans/holy-grail-music-loop.md) — where this
  sits in the full SoundCloud-like → CDJ-USB loop

## Purpose

Genre is the weak link in the library. A 400-file sample of `~/Music/downloaded`
shows four spellings of one genre (`Hard Bounce` / `HARD BOUNCE` / `Hardbounce` /
`hardbounce`), labels and artists sitting in the genre field (`240 SOUND`,
`bladee` — about 9% of the sample), and no curated set of genres anywhere.

Navidrome's `.nsp` genre rule is case-sensitive
(`internal/navidrome/config.go:563`), which is why `hard_bounce_genres` has to be
an allowlist of *spellings* rather than a list of genres. That workaround is the
symptom; the uncurated tags are the disease.

Everything downstream inherits it: the Navidrome smart playlists, any
genre-gated Rekordbox export, and browsing in Amperfy. Fixing genre **at the file
tag** — before Rekordbox first imports a track — fixes all of them at once,
because the tag is the one representation Navidrome and Rekordbox both read.

The triage work itself is phone-shaped. It happens in batches of thirty, away
from the desk, and it needs audio preview to make the call. Navidrome already
serves the library to the phone over LAN and tailnet. But **Navidrome cannot
write tags** — it reads them on scan and that is the whole API. So the decision
has to travel back to UDL on the Mac, and that round trip is what this initiative
builds.

## Scope

- **Editable:** genre (tap-to-pick from a canonical taxonomy), title, artist.
- **Queue:** derived, not tracked — a track is queued when its genre tag is empty
  **or** not an exact member of the taxonomy — minus a persisted skip list.
- **Transport:** one listener on `0.0.0.0`, mirroring Navidrome
  (`internal/navidrome/config.go:120-147`). LAN at home, tailnet away, one
  mechanism.
- **No genre suggestions in v1.** Manual picker. Ship the seam, not the
  classifier.
- Audio preview proxied from the local Navidrome.

## Current State

The pieces that already exist, and the seam each one stops at:

| Capability | Where | Stops at |
| --- | --- | --- |
| Library catalog with real paths | `internal/navidrome/client.go:394` `Songs()` | Read-only; no tag write path exists in Navidrome at all |
| Real disk path per song | `render.go:58` `DefaultReportRealPath = true`, `client.go:625` `NormalizePath` | Direct join to disk; no mapping layer needed |
| Genre normalization | `internal/navidrome/config.go:568` `NormalizeGenres` | Dedupes exactly, not case-folded — deliberately, and load-bearing |
| ffmpeg tag writing | `internal/engine/soundcloud_freedl_metadata.go:210` | Unexported, coupled to SoundCloud scraping, **cannot clear a field** |
| Birthtime-preserving rewrite | `internal/freedl/service.go:837` `applyReplacement` | The only correct ordering in the repo; not reusable as-is |
| Keychain credentials | `internal/auth/navidrome_password.go` | Template exists; no triage token |
| HTTP serving | — | **Nothing.** No server anywhere in production code |

## Findings

Verified against the repo and the real library, not assumed. Each one changed the
design.

1. **`Song.Path` is already the real disk path.** Navidrome runs with
   `[Subsonic] DefaultReportRealPath = true` and `NormalizePath` NFC-folds it.
   Navidrome song → file is a direct join.
2. **`<audio src>` cannot send an `Authorization` header.** Bearer-only auth
   makes preview impossible without putting a token in a URL. **Use a session
   cookie**, minted from a bearer token; keep bearer for `curl` and tests.
3. **A Subsonic salted token never expires and has no scope.** `AuthParams()`
   returns `t = md5(password+salt)`, good for `deletePlaylist.view` as much as
   `stream.view`. Handing it to a browser is disqualifying — **proxy the
   stream.** CORS is *not* the reason: a plain `<audio src>` media load is a
   no-CORS request and works cross-origin today.
4. **A saved genre will reappear in the queue** unless the server keeps a local
   overlay, because the catalog comes from Navidrome and Navidrome only knows
   what it last scanned. Day-one correctness, not polish.
5. **WAV is 12 of 1,574 files** (911 m4a, 651 mp3, 12 wav). ffmpeg writes WAV
   genre into the LIST/INFO `IGNR` chunk, which round-trips unreliably. Not a
   blocker at 0.8% — mark those `taggable: false` and refuse with 422.
6. **No existing genre tag contains `;`, `/`, or `,`.** Navidrome's default
   `GenreSeparators` would split such a tag into several genres, none matching
   the taxonomy, trapping the track in the queue forever. Prevented entirely by
   one taxonomy validation rule — no `render.go` change, no full rescan.
7. **The real hazard in `ReplaceFileSafely`** (`internal/fileops/replace.go:56`)
   is not the `.udl.bak` sidecar — Navidrome never indexes that extension — but
   the microsecond window where the real path does not exist. A scan landing
   there deletes the row; re-adding mints a new ID and a fresh `created_at`.
   Mitigated free by never scanning while a save is in flight.

## Product Decisions

1. **The tag is the source of truth.** Not a UDL database, not Navidrome, not
   Rekordbox. Edits made inside Rekordbox are not propagated back.
2. **The queue is derived, not tracked.** No "new arrivals" state to drift out of
   sync; backfilling the existing 1,574 files and handling tomorrow's downloads
   are the same code path.
3. **Taxonomy membership is exact and case-sensitive**, because `NormalizeGenres`
   dedupes exactly and the `.nsp` rule is case-sensitive. A file tagged
   `hard bounce` against taxonomy `Hard Bounce` stays queued. That is correct —
   the casing drift silently breaks smart playlists — but the UI must say *why*
   so it does not read as a bug.
4. **One genre per track in v1.** Save replaces all existing genres. Multi-genre
   editing complicates the picker, the queue predicate, and the separator problem
   at once.
5. **The SoundCloud tagger is not refactored in this initiative.** It is in the
   sync hot path and its skip-empty semantics are load-bearing; folding it onto
   an empty-means-clear tagger would silently blank titles whenever a scrape
   returns partial metadata. Separate PR, own tests.

## Architecture

### `internal/tags` (new, shared)

Exists because triage must *clear* a field and the existing tagger structurally
cannot.

```go
Tags { Title, Artist, Album, Genre, Comment, Format string }
Edit { Title, Artist, Genre *string }   // nil = leave, "" = clear, else set
Read(ctx, path string, timeout time.Duration) (Tags, error)
Write(ctx, path string, edit Edit, opts WriteOptions) error
```

`Write` copies `applyReplacement`'s ordering verbatim: `ReadCreationTime` →
sibling temp → ffmpeg → **`RestoreCreationTime` before the rename** →
`ReplaceFileSafely`. `-map 0 -codec copy` carries the `comment` tag through
untouched, which matters because the SoundCloud permalink there scores 100 in
promotion matching (`internal/cli/promote_freedl.go:664`).

### `internal/triage` (new)

Standalone feature config mirroring `internal/navidrome/config.go`, plus
taxonomy, catalog, queue, skiplist, save, server, handlers, auth, stream, and an
embedded `web/` asset tree.

The taxonomy lives in its **own file** (`~/.config/udl/genres.yaml`, embedded
default) — not in `navidrome.yaml`, whose `HardBounceGenres` is machine-derived
and overwritten wholesale by `navidrome playlists save-genres`. Genres are
grouped, because a flat 50-entry picker is a scroll-hunt on a phone.

### HTTP surface

Stdlib `net/http` only — Go 1.22's `ServeMux` handles
`POST /api/tracks/{id}/save` patterns, and `go.mod` has no web dependency to add
one to. Track id is `hex(sha256(NormalizePath(path)))[:16]`, not the Navidrome
ID: Navidrome IDs churn (finding 7), the path is the real write key, and an
opaque id makes path traversal structurally impossible rather than
defended-against.

Session cookie for the browser, bearer for scripts. Pairing by 6-digit code
rather than QR — there is no QR dependency and a 43-character token is unusable
to type.

### Write path

Under a process-global save mutex: resolve id from the snapshot only → check
`expect_mtime_ns` (409 on mismatch, guarding against concurrent `udl sync`) →
validate genre (422) → refuse untaggable containers (422) → hold the rescan
debouncer → write via `internal/tags` → confirm the tag landed by re-probing →
record the overlay → arm a 45s rescan debounce → **respond immediately without
waiting for the scan**.

## Phasing

- **P0 — no HTTP at all.** `internal/tags` plus triage config/taxonomy/catalog/
  queue/skiplist and `udl triage list|set|skip`. Proves the queue derivation,
  the taxonomy semantics and the write path end to end, entirely in Go. If
  `tags.Write` mangles a container, find that out from a CLI you can re-run, not
  from a phone.
- **P1 — minimal serve.** Pairing, cookie session, queue, taxonomy, save,
  proxied stream. Genre picker only. This is the shippable slice.
- **P2 — text cleanup, skip UI, installability.**
- **P3 — background hygiene.** Debounced rescan, `ReconcileDateAdded`, overlay
  persistence, advisory lockfile, session persistence.
- **P4 — surfaces and classifier.** Agent JSON-RPC methods, then a
  `GenreSuggester` behind the `suggestions` field shipped empty since P1.

## Risks

| Risk | Severity | Mitigation |
| --- | --- | --- |
| Saved genre reappears in the queue | High — breaks the core loop | In-memory + persisted overlay over the Navidrome snapshot |
| Bearer-only auth cannot feed `<audio>` | High — blocks preview | Cookie session, bearer as bootstrap |
| Subsonic token leaked to the browser | High if direct-streamed | Proxy through UDL; `Stream()` stays in the navidrome package |
| `Client.HTTP`'s 60s timeout kills streams | Medium | Dedicated no-timeout client; never set `WriteTimeout` |
| Refactoring the SC tagger blanks tags | Medium | Explicitly out of scope; separate PR |
| Concurrent `udl sync` / `promote-freedl` | Medium | `expect_mtime_ns` precondition now, advisory lockfile in P3 |
| WAV genre does not round-trip | Low — 0.8% of library | Probe format, refuse with 422 `untaggable` |
| Taxonomy entry containing a separator char | Low — none exist today | Reject `;`, `/`, `,` in taxonomy validation |
| Missing-path window during replace | Low — microseconds | Save mutex + rescan debounce; reconcile as safety net |
| Plaintext HTTP on `0.0.0.0` | Low, accepted | Same posture as Navidrome today; document, do not discover |

## Deferred

- Multi-genre editing, artwork, album/year, bulk apply.
- Service worker / offline. A triage tool is useless without the Mac reachable,
  and a stale cache on a phone you cannot attach devtools to is a nasty trap.
- launchd-managed always-on serving.
- Pinning Navidrome's `GenreSeparators` — it rewrites the ownership-marked
  `navidrome.toml` and forces a full rescan, so it carries its own blast radius.
- The genre classifier itself. The `suggestions` field ships empty from P1 so
  adding it later needs no client change.
