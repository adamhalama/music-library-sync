# `udl agent` Protocol

Protocol version 2 is JSON-RPC 2.0 over newline-delimited JSON. Each stdin or
stdout line is one complete JSON object. Protocol stdout never contains logs,
prompts, progress bars, or adapter output. Raw adapter output is retained only
in bounded runner tails and persisted failure diagnostics; it is not duplicated
onto agent stderr.

## Framing and compatibility

- The maximum encoded frame is 8 MiB. This is comfortably above current
  worst-case plan payloads while bounding memory use.
- Request IDs may be JSON strings or numbers. `null`, arrays, and objects are
  invalid IDs. Server-originated IDs use opaque `s-<number>` strings.
- Unknown object fields are ignored. Additive fields are compatible within
  protocol version 2.
- A breaking field, method, or semantic change requires a new negotiated
  protocol version.
- Responses may arrive out of order. Callers correlate them by ID.
- A second request with an ID that is still active is rejected with `-32003`.
  A duplicate or unknown response ID is ignored after the first response wins.
- EOF or a write failure closes the session and fails every pending call.

## Errors

Standard JSON-RPC codes are used for parse error (`-32700`), invalid request
(`-32600`), method not found (`-32601`), invalid params (`-32602`), and
internal error (`-32603`).

Application codes are:

| Code | Meaning |
| --- | --- |
| `-32001` | Session not initialized |
| `-32002` | Run not found |
| `-32003` | Conflicting run or request |
| `-32004` | Operation canceled |
| `-32005` | Frame exceeds the 8 MiB limit |
| `-32006` | Client disconnected |

Error messages are actionable but never include credential values. Error
`data`, logs, protocol fixtures, and diagnostics must redact Deezer ARLs,
Spotify secrets, SoundCloud client IDs, tokens, and authorization headers.

## Lifecycle

The first application request is `session.initialize`, which negotiates
protocol version 2 and returns the supported method inventory. Long-running
methods return a `runId`; every accepted run emits exactly one `run.finished`
notification. `session.shutdown` cancels active runs and closes cleanly.

Server-to-client UI requests use `ui.confirm`, `ui.input`, and
`ui.selectRows`. Cancellation answers a pending UI request as canceled before
canceling its run context, preventing a worker from remaining blocked on the
interaction reply.

## Session and method inventory

`session.initialize` takes `{"protocol_version":2}`. Its result contains
`protocol_version`, `build`, the complete `methods` array, `working_dir`,
`config_paths`, `feature_config_paths`, and transport `capabilities`.
`session.shutdown` takes no params and returns `{"shutdown":true}` after all
active runs have been canceled.

Protocol v2 exposes 37 methods:

| Group | Methods |
| --- | --- |
| Session/runs | `session.initialize`, `session.shutdown`, `run.cancel` |
| Sync | `sync.start`, `sync.cancel` |
| Config | `config.load`, `config.validate`, `config.readFile`, `config.writeFile` |
| Doctor | `doctor.run` |
| Credentials | `credentials.list`, `credentials.save`, `credentials.clear` |
| Startup/sources | `startup.onboardingState`, `startup.attention`, `sources.capabilities` |
| Playlists | `playlists.list`, `playlists.providerList`, `playlists.show`, `playlists.refresh`, `playlists.saveDefinition`, `playlists.config.read`, `playlists.config.write` |
| Free DL | `freedl.config.read`, `freedl.config.write`, `freedl.plan.start`, `freedl.capture.start`, `freedl.promotionPlan.build`, `freedl.promote.apply` |
| Rekordbox | `rekordbox.config.read`, `rekordbox.config.write`, `rekordbox.deps.status`, `rekordbox.deps.ensure`, `rekordbox.deps.reset`, `rekordbox.inspect`, `rekordbox.plan`, `rekordbox.apply` |
| Navidrome | `navidrome.config.read`, `navidrome.config.write`, `navidrome.deps.status`, `navidrome.deps.ensure`, `navidrome.status`, `navidrome.service.control`, `navidrome.setup.plan`, `navidrome.setup.apply`, `navidrome.playlists.refresh`, `navidrome.playlists.deriveGenres`, `navidrome.playlists.saveGenres`, `navidrome.favorites.plan`, `navidrome.favorites.apply`, `navidrome.favorites.list`, `navidrome.backup.create` |

## Method contracts

All field names are snake_case. Params shown as `{}` take no fields. Methods
marked **run** return `{"run_id":"…"}` immediately and terminate through
`run.finished`.

| Method | Params | Result |
| --- | --- | --- |
| `run.cancel`, `sync.cancel` | `run_id` | `canceled` |
| `sync.start` | source IDs, dry-run, timeout, plan/window/order, gap/preflight, prompt and track-status options | **run** |
| `config.load` | `{}` | merged `config` |
| `config.validate` | `config` | `valid` |
| `config.readFile` | optional scoped `path` | scoped `path`, parsed `config`, original `content`, `content_sha256` |
| `config.writeFile` | scoped `path`, `config`, optional `expected_content_sha256` | scoped `path`, saved `config`, canonical YAML `content`, `content_sha256` |
| `doctor.run` | `{}` | structured `checks`, `effective_path`, resolved dependencies, `exit_code` |
| `credentials.list` | `{}` | status metadata in `credentials`; never values |
| `credentials.save` | `kind` plus `value`, or Spotify `client_id` and `client_secret` | `saved`, `kind` |
| `credentials.clear` | `kind` | `cleared`, `kind` |
| `startup.onboardingState` | `{}` | `needed`, frontend-neutral onboarding `state` |
| `startup.attention` | `{}` | `status` (`ready`, `attention`, or `blocked`) and optional `attention` |
| `sources.capabilities` | `{}` | plan/window/order support and defaults per source |
| `playlists.list` | `{}` | configured definitions plus cache-only snapshot status |
| `playlists.show` | `playlist_id` | definition and validated cached snapshot |
| `playlists.providerList` | optional `provider` | **run** returning provider playlists |
| `playlists.refresh` | `playlist_id` | **run** returning snapshot, changes, and path |
| `playlists.saveDefinition` | `definition` | path, definition, and `created` |
| `playlists.config.read` | `{}` | write path, merged config, content |
| `playlists.config.write` | `config` | write path, saved config, canonical content |
| `freedl.config.read` | `{}` | write path, merged config, content |
| `freedl.config.write` | `config` | write path, saved config, canonical content |
| `freedl.plan.start` | `job_id`, optional plan limit, playlist ID, and selection overrides | **run**, streaming `freedl.planEvent` |
| `freedl.capture.start` | capture `plan`, optional selected remote IDs | **run** returning sync result |
| `freedl.promotionPlan.build` | job ID, capture run ID, optional target format | **run** returning promotion plan |
| `freedl.promote.apply` | promotion `plan` | **run** returning promotion result |
| `rekordbox.config.read` | `{}` | resolved write-path metadata, merged config, content |
| `rekordbox.config.write` | `config` | resolved path, saved config, canonical content |
| `rekordbox.deps.status` | `{}` | runtime `status`, `exit_code` |
| `rekordbox.deps.ensure`, `rekordbox.deps.reset`, `rekordbox.inspect` | `{}` | **run** |
| `rekordbox.plan` | optional job/mapping, Music/Rekordbox selectors, runtime settings, or cached playlist ID | **run** returning the signed plan and path |
| `rekordbox.apply` | signed `plan`, optional runtime/backup overrides, `dry_run` | **run** returning apply result |
| `navidrome.config.read` | `{}` | write path, merged config, content |
| `navidrome.config.write` | `config` | write path, saved config, canonical content |
| `navidrome.deps.status` | `{}` | Homebrew/Navidrome availability, version, and problems; never mutates |
| `navidrome.deps.ensure` | `confirm` (must be `true`) | **run** returning the Homebrew install result |
| `navidrome.status` | `{}` | dependency, service, account, library, playlist, and backup state |
| `navidrome.service.control` | `action` (`start`, `stop`, `restart`) | **run** returning the resulting service status |
| `navidrome.setup.plan` | `{}` | **run** returning the checksummed setup plan |
| `navidrome.setup.apply` | signed `plan` | **run** returning written files, directories, and service action |
| `navidrome.playlists.refresh` | `{}` | **run** returning generated `.nsp` files, scan state, and warnings |
| `navidrome.playlists.deriveGenres` | `{}` | **run** returning the previewed HARD BOUNCE allowlist and unmatched counts |
| `navidrome.playlists.saveGenres` | `genres` (non-empty) | **run** persisting the allowlist and rewriting the playlists |
| `navidrome.favorites.plan` | `{}` | **run** returning the checksummed favorite migration plan |
| `navidrome.favorites.apply` | signed `plan` | **run** returning backup path, newly starred IDs, and parity |
| `navidrome.favorites.list` | `{}` | **run** returning `count` and the server's starred `tracks`, path-sorted |
| `navidrome.backup.create` | `{}` | **run** returning the created database backup |

The Navidrome account password is never carried by any of these methods. It
lives only in macOS Keychain and is written through `credentials.save`.
`navidrome.setup.apply` and `navidrome.favorites.apply` reject a plan whose
checksum does not verify, so a stale plan is refused rather than replayed.

`navidrome.favorites.list` is the read side of the return path — a like made on
the phone reaches UDL through it. It reads the server's stars directly and never
touches Apple Music, so it is the one favourites method that works without a
Music automation grant. Its ordering matches the `navidrome-favorites` snapshot,
so a listing and a refreshed snapshot can be compared line for line.

Config write methods intentionally perform canonical rewrites; comments and
the caller's original formatting are not preserved. Main, playlist, Free DL,
and Rekordbox config writes are restricted to paths discovered for the
initialized session, and symbolic-link config targets are rejected.
For the main config editor, clients should return the `content_sha256` from
`config.readFile` as `expected_content_sha256`; a mismatch returns a run
conflict without overwriting an external edit.

Playlist list/show never contact Music.app. Provider discovery and refresh are
explicit cancellable runs, and a failed or canceled refresh preserves the last
valid snapshot.

Free DL capture ignores client-supplied buffer/log roots and derives them from
the configured job. Promotion apply rebuilds the authoritative plan and merges
selection state only. Rekordbox apply validates plan checksum and completeness
before runtime or backup overrides; invalid plans include structured blocker
rows in error `data`.

## Notifications and UI requests

| Message | Direction | Payload |
| --- | --- | --- |
| `sync.event` | server → client | Lossless `run_id`, lifecycle/outcome/failure engine `event`, and full source run-state snapshot |
| `sync.progress` | server → client | Latest-only `run_id`, `source_id`, structured progress snapshot, and optional fully resolved changed row |
| `freedl.planEvent` | server → client | `run_id`, event kind/stage/status/detail, optional row/plan/error and counters |
| `run.finished` | server → client | `run_id`, optional `result` or `error`, and `exit_code` |
| `ui.confirm` | server → client request | run/source context, prompt, default |
| `ui.input` | server → client request | run/source context, prompt, `mask` |
| `ui.selectRows` | server → client request | run/source context, source details, rows, order/window |

Exit codes preserve the CLI contract: success `0`, runtime failure `1`,
invalid usage `2`, invalid config `3`, missing dependency `4`, partial success
`5`, and interruption `130`.

`track_progress` engine events are coalesced into `sync.progress` at a minimum
100 ms interval per run. The newest pending percentage replaces older pending
percentages. Pending progress is flushed before lifecycle/outcome/terminal
notifications and emitter shutdown without bypassing that 100 ms clock, while
`sync.event` and `run.finished` are never coalesced. Clients should buffer only
the newest `sync.progress` frame and keep lossless buffering for every other
notification.
