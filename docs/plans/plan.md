# Plan — SoundCloud Free DL upgrade of 2026-09-12 likes batch

## Goal
Upgrade the 46 tracks downloaded into the library on 2026-09-12 (staged in the
scratch directory `~/Music/downloaded/sc-likes-today-09-12`, oldest member
`Nom Nom [Free DL] - Birthday Special.m4a`) to their SoundCloud free-download
originals wherever one exists.

The scratch directory is a plain copy of today's 46 additions to
`~/Music/downloaded/sc-likes` (separate inodes, not hardlinks), so every write
here is isolated from the real library.

## How the Free DL flow works in this repo
Two composable halves, both reachable from the CLI:

1. **Capture** — a `soundcloud` source with `adapter.kind: scdl-freedl`
   (`internal/engine/soundcloud_freedl_*.go`). For each enumerated track it
   probes the SoundCloud page for a free-download / purchase URL. Supported
   gates (currently Hypeddit only) are opened in a browser
   (`open -a Helium` by default, override with `UDL_FREEDL_BROWSER_APP`) and the
   flow watches the browser download directory (`~/Downloads`, override with
   `UDL_FREEDL_BROWSER_DOWNLOAD_DIR`) until a completed media file appears. The
   file is tagged, artwork-embedded, and moved into the source `target_dir`.
   Unsupported hosts / no free DL are skipped. Browser launch/wait failures are
   appended to `<state_dir>/<source-id>.freedl-stuck.jsonl`.
2. **Promotion** — `udl promote-freedl --free-dl-dir <captured> --library-dir
   <library>`. Matches captured files to library files by embedded metadata
   (title/artist/source URL) with filename stem as fallback, scores the match
   (`--min-match-score`, `--ambiguity-gap`), compares effective bitrate from
   `ffprobe`, and only replaces when the free DL is actually better. In-place
   replacement (no `--write-dir`) preserves library paths; originals are copied
   to a backup dir first. Default is preview-only; `--apply` writes.

The TUI/GUI `SoundCloud Free DL` workflow (`internal/freedl`) wraps exactly these
two halves around a `freedl.yaml` job. Plan classification comes from the job's
*own* state/archive files (`soundcloud-free-dl.sync.scdl` /
`soundcloud-free-dl.archive.txt`), not the main sync's, so tracks already
stream-ripped by the ordinary `soundcloud-likes` sync still show as upgradable.

## Chosen interface: CLI
Autonomous operation rules out the TUI/GUI screens. The CLI gives the same two
halves with explicit paths and no interactive selection step:

- a dedicated scratch config (`docs/plans/`-adjacent, under
  `~/dev/music-down/statefiles/freedl/upgrade-09-12/`) with one `scdl-freedl`
  source whose `target_dir` is the capture buffer;
- `udl sync --source <id>` to capture;
- `udl promote-freedl --library-dir ~/Music/downloaded/sc-likes-today-09-12` to
  preview, then `--apply` to replace.

Nothing in the real `udl.yaml` or `freedl.yaml` is touched.

## Steps
1. Probe-only pass to learn which of the 46 expose a usable free download.
2. Capture the supported ones into the buffer (driving the Hypeddit gates).
3. `promote-freedl` preview, review the match scores and quality deltas.
4. `promote-freedl --apply` in place against the scratch directory.
5. Report what upgraded, what was skipped and why.

## Constraints
- Never write to `~/Music/downloaded/sc-likes` in this task.
- Plan limit must cover exactly today's batch; the 46 tracks are the newest
  likes, so enumerate 46 and stop.
- When something breaks, fix it (code on this branch:
  `chore/freedl-upgrade-09-12`, branched from `feature/swiftui-gui`, which is 8
  commits ahead of `master` and contains all the Free DL work) and resume.
