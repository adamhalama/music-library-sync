# Native Free DL preservation proof — 2026-10-10

Metadata restoration now runs in Go before installation through the app's
`freedl.promote.apply` RPC, the TUI service, and `udl promote-freedl`. Python
restoration is not invoked by these routes. FFmpeg/ffprobe remain the existing
media dependencies; no Python or Mutagen runtime dependency is added to the app.

- [Full local verification report](https://qs3a3srv17.ufs.sh/f/xzO3BGKm3RAf8yIlmWIJCKLWU76aHzQbqViwlekdXMIJNsjv)
- [Measured hash and creation-time evidence](https://qs3a3srv17.ufs.sh/f/xzO3BGKm3RAfXhT5ZirRXmBu9rgdev0TxVhU7ZWIbaQsqMJY)
- [Checked-in evidence snapshot](native-freedl-preservation.json)

The fixtures are synthetic and live only in temporary directories. Mutagen is
used by tests to generate opaque metadata and independently compare the complete
tag sets. No real-library writes or browser/social actions were performed.

| Direct acceptance case | Result |
| --- | --- |
| App RPC: MP4 WWWAUDIOFILE, binary freeforms, repeated values and cover art | Pass; complete tag equality and upgraded encoded audio |
| App RPC: MP3 private UFID, custom TXXX, comments and APIC art | Pass; complete ID3 equality |
| App RPC: unknown WAV iXML metadata | Pass; original chunk retained with upgraded PCM |
| App RPC: forced installation failure | Pass; library and durable original backup exact, failed row visible, stage cleaned |
| Legacy CLI: complete MP4 tags/artwork | Pass; no Python repair |
| Cross-container export with opaque tags | Refused; original and existing output untouched |

APFS creation time was exactly `1600000000.123456` before and after the MP4
upgrade. Native result hashes match the final installed file and encoded audio.
Each successful row carries `preservation` verification and `promoted_sha256`;
the original `sha256` field retains its backup-checksum meaning.

Go tests additionally cover fast-start MP4 and co64 offsets, opaque APE/ID3v1,
BWF/RIFF metadata, malformed/fragmented containers, metadata verification
failure, concurrent library edits, cancellation before installation, and partial
results that claim preservation only for successful rows. Maintenance tests
verify that native success avoids a second Python rewrite and hash drift fails
without silently repairing an external edit.

Exact validation commands:

```sh
go test ./...
go vet ./...
go test -race ./...
go test ./internal/mediametadata ./internal/freedl -run 'Test(MP4|ID3|WAV|NativePromotion)' -v -count=1
go build -o /tmp/udl-native-preservation ./cmd/udl
UDL_FREEDL_TEST_BINARY=/tmp/udl-native-preservation UDL_FREEDL_PROOF_JSON=/tmp/udl-native-preservation-proof/native-evidence.json /tmp/udl-freedl-general-tests/bin/python .dev/freedl-0912/test_native_promotion.py
UDL_FREEDL_TEST_BINARY=/tmp/udl-native-preservation /tmp/udl-freedl-general-tests/bin/python .dev/freedl-0912/test_media.py
/tmp/udl-freedl-general-tests/bin/python .dev/freedl-0912/test_context.py
/tmp/udl-freedl-general-tests/bin/python .dev/freedl-0912/test_quality.py
git diff --check
```

Go tests, vet and race passed; direct native acceptance passed 6/6 without skips,
existing media acceptance passed 3/3, context tests passed 19/19, and quality/
wrapper tests passed 13/13. The hosted report records the earlier 11-test quality
suite; two wrapper regression cases were added afterward and passed separately.

Local `make app-test-dev` was attempted but failed before test execution because
the installed Command Line Tools/macOS 27 SDK cannot load
`SwiftUIMacros.StateMacro`. Full app compilation and Swift tests use the PR's
pinned Xcode CI. This document does not claim successful local Swift execution.

Native preservation supports ordinary MP4/M4A, MP3/ADTS tags and RIFF/WAV.
Cross-container exports and unsupported MP4/WAV layouts fail before install
instead of silently dropping opaque metadata. Manual migration/repair remains
available through the maintenance tools.
