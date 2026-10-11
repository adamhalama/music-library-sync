# Free DL final review proof — 2026-10-11

The final GPT-6.1 Sol review found three blockers: the maintenance planner used
the wrong event name; its RPC driver discarded notifications arriving before
the start response; and native MP4 promotion could discard top-level Adobe XMP
metadata while claiming complete preservation. All three are fixed. The review
also checked a bounded Spotify correction: metadata lookup or cache-publication
failure stops deemix before invocation, preventing unprimed downloads and state
writes. The final review found no remaining blockers after these changes.

No live library, browser or social actions were performed. The acceptance suite
uses synthetic files and temporary configurations. Mutagen is an independent
test oracle, not an app runtime dependency.

| Verification | Result |
| --- | --- |
| Maintenance RPC and new-plan/provenance acceptance | 3 tests pass |
| Direct native app RPC/CLI promotion | 7 tests pass, no skips |
| Existing media/migration acceptance | 3 tests pass |
| Context/provenance | 19 tests pass |
| Quality/rollback/wrapper | 13 tests pass |
| Spotify lookup/cache guard | Both failure cases pass; no subprocess or state mutation |
| Go tests, vet and race | Pass |
| Diff whitespace check | Pass |

The maintenance acceptance sends the actual `freedl.planEvent` done notification
and `run.finished` **before** the start response. The CLI still completes and
saves connection provenance. Explicit notification callbacks are not replayed
twice.

The new native acceptance adds an Adobe XMP UUID atom to valid AAC/M4A. Promotion
returns a failed row with no preservation claim, retains the exact original
library file and backup, and removes the stage. Unsupported MP4 top-level,
movie-level and track metadata is refused before installation; track-container
parsing has a depth bound. Ordinary supported opaque metadata still preserves
complete independent tag equality, artwork and encoded audio. MP4 creation time
remains exactly `1600000000.123456` before and after the successful promotion.

The [measured evidence](freedl-final-review.json) records native results and
hashes. The historical [native preservation proof](native-freedl-preservation.md)
remains available with its original six-test snapshot.

Exact commands run:

```sh
python3 .dev/freedl-0912/test_agent.py
python3 .dev/freedl-0912/test_context.py
/tmp/udl-freedl-general-tests/bin/python .dev/freedl-0912/test_quality.py
go test ./internal/engine -run TestSpotifyDeemixRefusesExecutionWithoutPrimedMetadata -count=1
go test ./internal/engine -count=1
go test ./internal/mediametadata ./internal/freedl -count=1
go test ./...
go vet ./...
go test -race ./...
go build -o /tmp/udl-pr24-final-proof/udl ./cmd/udl
UDL_FREEDL_TEST_BINARY=/tmp/udl-pr24-final-proof/udl UDL_FREEDL_PROOF_JSON=/tmp/udl-pr24-final-proof/native-evidence.json /tmp/udl-freedl-general-tests/bin/python .dev/freedl-0912/test_native_promotion.py
UDL_FREEDL_TEST_BINARY=/tmp/udl-pr24-final-proof/udl /tmp/udl-freedl-general-tests/bin/python .dev/freedl-0912/test_media.py
git diff --check
```

The notification-order regression and both Spotify guard cases failed on the
previous implementation and passed after correction. The reviewer independently
reproduced XMP loss before the MP4 guard and verified safe refusal afterward.
Full macOS app builds/Swift execution are verified by pinned-Xcode PR CI; no new
successful local Swift execution is claimed.
