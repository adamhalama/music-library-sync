# September 12 isolated Free DL batch

These scripts only modify `~/Music/downloaded/sc-likes-today-09-12` and use
`~/dev/music-down/statefiles/freedl/upgrade-09-12` for originals, backups and evidence.

- `python3 .dev/freedl-0912/audit.py`: probe every actual media file, fully decode audio,
  compare audio bitrate, tags and artwork hashes with the original backup recorded by its successful promotion, and write
  `quality-audit.json` plus `quality-audit.md` in scratch. Bitrate is audio-only;
  container size including artwork is not used as a quality measurement.
- `preserve.py restore-tags BACKUP TARGET`: restore the entire original MP4 tag set
  including unknown iTunes freeform fields. Verify compressed audio and artwork hashes,
  all ffprobe-visible original tags, and decoding before replacement. Backup first.
- `preserve.py install-mp3 SOURCE EXISTING_AAC`: explicitly migrate a verified artist
  original to `.mp3`, stream-copying its encoded audio without lossy transcoding. Copy
  existing metadata/artwork, verify duration (within two seconds), compressed audio
  hash and decoding, back up the AAC, then install MP3 and remove old AAC. The caller
  must establish source identity/provenance; duration alone is not proof of identity.
- `status.py` incorporates these logged migrations so extension changes retain the
  original track identity and do not disappear from the batch count.

Requirements: Python 3, ffmpeg/ffprobe. MP4 tag preservation also uses Mutagen:

```sh
python3 -m pip install --target ~/dev/music-down/statefiles/freedl/upgrade-09-12/pythonlib mutagen==1.48.1
```

FFmpeg `-map_metadata` drops the original MP4 `WWWAUDIOFILE` freeform atom; ExifTool
`-TagsFromFile -all:all` also failed to preserve it. Mutagen transfers the entire MP4
`ilst` metadata including `purl`, freeform tags and cover artwork without re-encoding.
This is a batch maintenance dependency, not an added application dependency.

Every mutation writes `logs/<timestamp>-<action>/preservation-result.json` with
before/after probes, file hashes, encoded audio hash, backup and source information.
Filesystem creation time is retained on macOS, including when the extension changes.
The 2026-09-29 metadata repair also restored the four previously upgraded tracks'
creation times from their earliest backups.

The audit counts an upgrade only when a successful promotion/migration ledger entry
exists **and** the encoded audio differs from its backup. A metadata-only change
cannot inflate that count. `changed_audio` is reported separately, and the table
checks creation-time preservation explicitly.

Mutations first persist `transaction.json` with backup/target paths. A successfully
written `preservation-result.json` is the commit marker, and records the installed
file's actual creation time. An exception during installation or final evidence
writing restores the backup and removes the migrated output. A process/machine
crash can leave a prepared transaction without a commit marker; inspect its paths
and backup before resuming. Run isolated regression tests with:

```sh
python3 .dev/freedl-0912/test_quality.py
```

`promote.py CAPTURE_RUN --apply` automatically repairs each successfully replaced
row using its native backup/library paths, including successes in partially failed
runs. It checks Mutagen availability before applying, reports missing backups or
repair failures with a nonzero exit, and always closes its agent. Native promotion
hashes describe the pre-repair output; the separate preservation ledger records
the final metadata-restored file. Tag restoration uses the backup's creation time,
so it also corrects native promotion timestamp drift.

Background browser tools use explicitly selected Helium window/tab IDs and never
activate a window or synthesize keyboard/mouse input:

- `helium.py`: list tabs, inspect, navigate, inject or stop the gate helper. Tab lists
  redact URL queries/fragments because OAuth callbacks can contain credentials.
- `gaterush.py`: route the site's normal OAuth flow through an owned auxiliary tab
  and deliver only a verified provider callback to the gate.
- `download.py`: transfer an unlocked same-origin media response from Helium into
  its download directory without exporting browser cookies.
- `instagram.py`: perform an authorized profile follow and verify Following after
  reload. It refuses missing/ambiguous controls before acting.

Keep personal form configuration and action evidence outside the repository. A
browser timeout may occur after an action succeeded: inspect before retrying.
CAPTCHA detection pauses the userscript until explicit resume; leave human-owned
tabs untouched during verification. A real site follow must be observed before
confirming a gate step. Background tabs may stall; do not activate them to work
around that while the user is working.

`intake.py` creates fresh single-track capture runs from the original identity
plan, validates downloaded media, and stages names by remote ID. This avoids the
native state parser collapsing repeated spaces in browser-provided filenames.

`soundcloud.py` inspects an exact artist profile and track card without navigation.
Explicit follow/like/repost/comment actions verify the resulting state and never
retry uncertain writes. Comments additionally require `--allow-focus`: their lazy
UI requires scrolling and focusing the input, so do not use that flag while the
user is working. Default inspection preserves drafts and changes no browser UI.
