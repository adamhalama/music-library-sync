# Reusable Free DL maintenance

`python .dev/freedl.py` exposes `plan`, `capture`, `intake`, `promote`, `preserve`,
`status`, and `audit` for any configured SoundCloud Free DL job. The implementation
remains in `.dev/freedl-0912` beside the historical batch evidence; its job IDs,
checkout, library, buffer, backup and log roots are no longer fixed to that batch.
The normal app, TUI and native promotion commands now preserve metadata in the
shared Go backend before installing upgraded audio. These maintenance commands
reuse that protection and also provide manual repair, migration and audit tools.

## Setup

Use Python 3.10+, ffmpeg/ffprobe and the current Go backend (`make build`).
Native promotion needs no Python or Mutagen. The optional manual `preserve`
command, legacy-backend repair and independent test oracle require Mutagen:

```sh
python3 -m venv /tmp/udl-freedl-maintenance
/tmp/udl-freedl-maintenance/bin/pip install mutagen==1.48.1
source /tmp/udl-freedl-maintenance/bin/activate
python .dev/freedl.py --help
```

Create/choose a Free DL job in the app first. Its library must already exist.
Its library, buffer, backup and log directories must be distinct, nonoverlapping
roots; they can be on separate volumes. These commands deliberately reject
filesystem roots, missing metadata, relative job paths, traversal IDs, duplicate
remote IDs, symlink escapes, and saved/configured identity or storage conflicts.

## New plan and capture

```sh
python .dev/freedl.py plan --job my-likes --limit 50
```

This returns JSON with `job_id`, `run_id`, the saved `plan` path, and row counts.
`--limit` is optional: without it, the job's configured limit applies.
`--playlist-id ID` uses an existing standalone playlist snapshot.

Every command supports `--plan /absolute/path/capture-plan.json`; a run directory
containing that file also works. The plan supplies its job ID, library, buffer,
backup, log roots and exact track identities. You do not repeat these directories
or rely on a shared scratch-directory layout. Explicit `--job` must agree with
that plan.

For a separate configuration, supply `-c /path/config.yaml` and
`--freedl-config /path/freedl.yaml` on the initial plan command. `--working-dir`
selects backend config discovery; `--udl` selects another backend binary.
By default, these tools use this checkout's `bin/udl`, the user's
`~/.config/udl/config.yaml`, and the home directory for backend discovery, so
running them inside the checkout does not silently select its sample `udl.yaml`.

New plans save those connection settings in `maintenance-context.json` beside
the native plan. Subsequent capture/promotion commands reuse them; explicit flags
override saved connection settings. The default binary is resolved from the
current checkout so relocating the code does not retain an old checkout path.
No tokens or cookies are copied into this context.

After inspecting the plan and authorizing its gate interactions:

```sh
python .dev/freedl.py capture --plan /path/capture-plan.json --remote-id 123456789
```

Repeat `--remote-id` to capture multiple selectable tracks. Omitting it uses the
plan's selected rows. Unknown/unselectable IDs fail before capture. CAPTCHA,
OAuth and social actions retain their normal explicit interaction requirements;
the headless wrapper refuses unexpected backend UI prompts and closes its agent.
The [gate helper](../.dev/userscripts/README.md) and existing URL/tab-based browser
helpers remain available. Browser actions need observed identity/provenance.

## Browser intake

For an already verified, completed browser download:

```sh
python .dev/freedl.py intake --plan /path/capture-plan.json \
  --remote-id 123456789 --source /path/browser-download.wav
```

Intake derives the exact title, gate URL and local track from that saved remote
ID. Optional `--title` and `--gate-url` are assertions against the plan, not
identity overrides. `--expected-bytes` and `--expected-sha256` add download
assertions. Source audio must fully decode and be within two seconds of the local
track's duration; duration alone does not establish provenance.

Each intake creates a fresh run, returns its ID and staged paths as JSON, and
copies the original plan/connection context with its selected row. Files are
staged as `REMOTE_ID.ext`; the original browser filename remains in evidence,
including repeated spaces. Other selected rows are cleared. Use the returned
run's `capture-plan.json` for promotion. Original media is copied, never moved.

## Promotion and preservation

```sh
python .dev/freedl.py promote --plan /path/new-run/capture-plan.json
python .dev/freedl.py promote --plan /path/new-run/capture-plan.json --apply
```

Preview prints the native promotion plan, including paths, actions and qualities,
without changing the library. Native preview can write planning evidence in the
job log directory. Target format comes from the configured job unless overridden
with `--target-format auto|aac-256|mp3-320|wav`.

Apply verifies the current configured job against the saved plan. The native
backend restores opaque container tags from the immutable original backup onto
the staged upgraded audio, verifies tag bytes, encoded audio, artwork and full
decode, restores creation time and checks for concurrent library changes before
replacement. A failed check leaves the original unchanged and retains its backup.

The result ledger includes each successful row's `preservation` report and
`promoted_sha256` for the final installed file; `sha256` continues to identify its
original backup. Maintenance promotion checks the final native file hash and
avoids a second restoration. With an older backend that does not advertise
`native_metadata_preservation`, it checks Mutagen before applying and retains the
legacy Python repair path. Failed native or repair rows return nonzero.

Native protection covers MP4/M4A metadata (including unknown iTunes freeforms and
artwork), MP3/ADTS ID3 and APE tags, and RIFF/WAV metadata including opaque chunks.
The older `udl promote-freedl` command uses the same verification for in-place and
separate-directory outputs. Cross-container outputs are refused before
installation, because opaque metadata cannot be promised intact. Keep the library
container for native upgrades; use the explicit manual migration workflow below
when changing a track's container. Fragmented/open-ended MP4, metadata outside
MP4's supported movie-level `udta`/`meta` atoms (including Adobe XMP UUID atoms
and track-level metadata), and non-RIFF WAV layouts are refused rather than
silently dropping metadata. Refused files retain their original bytes and backup.

Manual repairs use the same saved promotion/capture context and exact
backup/target association. Both commands preview unless `--apply` is present:

```sh
python .dev/freedl.py preserve --plan /path/promotion-plan.json \
  restore-tags /path/original-backup.m4a /path/library-track.m4a --apply
python .dev/freedl.py preserve --plan /path/capture-plan.json \
  install-mp3 /path/buffer/RUN/downloads/123456789.mp3 /path/library-track.m4a \
  --remote-id 123456789 --apply
```

MP3 migration requires the exact saved remote ID, planned local AAC path, and
native capture-state mapping to the staged source. Intake evidence, when present,
must also match its SHA-256. Metadata must survive the container change; unknown
MP4 fields that cannot be represented in ID3 cause validation to refuse the
migration. Compressed MP3 audio is copied without lossy re-encoding.

Preservation checks full MP4 tags (including unknown iTunes freeform fields),
encoded audio hashes, artwork hashes, original ffprobe tags, and full decode
before replacement. It creates a verified backup and durable prepared transaction,
installs atomically, and restores the backup on in-process failure. macOS creation
time is retained and verified. A process/machine crash can leave `transaction.json`
without its `preservation-result.json` commit marker; inspect its paths and backup.
The Python command remains available for repairing earlier promotions and explicit
MP3 migration. Normal app/TUI/native promotions now provide their own preservation
and do not execute this Python repair step.

## Status and audit

```sh
python .dev/freedl.py status --plan /path/capture-plan.json
python .dev/freedl.py audit --plan /path/capture-plan.json
```

Status combines all saved plans for the job, including single-track intake runs,
and follows explicit extension-migration records. JSON distinguishes upgraded,
pending (including captures still present), and unavailable tracks. Status treats
a successful promotion ledger as completed work; the quality audit separately
requires changed encoded audio to count an upgrade.

Audit scans nested library folders, keeps exact relative file identities including
extensions, uses explicit successful-promotion backups, fully decodes audio, and
compares metadata, artwork and creation times. It writes `quality-audit.json` and
`.md` under the job's log root; `--output /path/report-prefix` overrides that
location. It exits nonzero on decode/metadata/artwork/timestamp failure. Audio
bitrate is measured from the audio stream, never the artwork-inflated container.
Missing original backups fail visibly. Migration baselines follow the original
native backup when a track was promoted before its extension changed.

Existing September 12 JSON plans work with `--plan`; all 13 saved contexts were
validated read-only during this change. Older runs have no connection sidecar:
supply their original `-c` / `--freedl-config` when capturing/promoting. No library,
private evidence or backup is copied into Git. Git preserves tooling and portable
proof, not personal media or rollback state.

## Validation

```sh
python .dev/freedl-0912/test_agent.py
python .dev/freedl-0912/test_context.py
python .dev/freedl-0912/test_quality.py
go build -o /tmp/udl-freedl-validation ./cmd/udl
UDL_FREEDL_TEST_BINARY=/tmp/udl-freedl-validation python .dev/freedl-0912/test_media.py
UDL_FREEDL_TEST_BINARY=/tmp/udl-freedl-validation python .dev/freedl-0912/test_native_promotion.py
```

The media tests require ffmpeg/ffprobe and Mutagen. They generate synthetic tracks
and configs in temporary directories, exercise two independent jobs through the
real backend, preview/apply preservation, verify unknown tags/artwork/encoded
audio/creation time, and check MP3 migration plus audit/status identity. The
`freedl-tools` CI workflow runs these tests on Linux and macOS. No live capture or
real library mutations are needed for validation.

Direct native acceptance drives `freedl.promote.apply` (the app's RPC) and the
legacy CLI without invoking Python repair. It independently checks complete
MP4/ID3 tags, artwork and unknown WAV chunks; exact backups; upgraded encoded
audio; creation time; final native hashes; install failure; and refused lossy
metadata conversion. Fixture generation and the independent tag oracle use
Mutagen only in tests.

The maintenance RPC tests run a synthetic backend that sends plan completion
and `run.finished` before the start response. They verify notification replay,
the actual `freedl.planEvent` method and saved connection provenance without
network access. Engine regressions also refuse deemix invocation when metadata
lookup or cache publication fails, preserving Spotify state for a retry.
