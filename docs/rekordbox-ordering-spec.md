# Rekordbox Date Added Ordering Spec (SoundCloud v1)

## 1. Goal
- Reorder Rekordbox `Date Added` by origin ordering (SoundCloud v1).
- Modify only `djmdContent.created_at` for scoped matched rows.
- Never implement hidden side effects.

## 2. Non-Goals
- No Spotify ordering in v1.
- No direct edit of `DateCreated`/`ReleaseDate`.
- No auto-import into Rekordbox.

## 3. CLI Contract
- `udl rekordbox order plan --source <id> [flags]`
- `udl rekordbox order apply --plan-file <path> [flags]`
- `udl rekordbox order show --plan-file <path> [flags]`

## 4. Behavioral Policy (Locked)
- If day-preserving exact ordering is impossible, ask only when this case occurs.
- If no conflict, run without asking.
- `--no-input` handling:
  - `--day-policy preserve` => abort with warning.
  - `--day-policy allow` => proceed.
  - `--day-policy ask` => abort (cannot prompt).

## 5. Safety Invariants
- Apply blocked while Rekordbox process is running.
- `apply` performs preflight revalidation of planned rows.
- Prompt user before write and auto-create timestamped DB backup.
- Single transaction apply.
- Abort on scope mismatch.

## 6. Public Interface Additions
- New commands and flags:
  - `plan`: `--source`, `--rekordbox-db-dir`, `--python-bin`, `--out`, `--day-policy`, `--min-match-ratio`, `--max-unmatched`
  - `apply`: `--plan-file`, `--wait-timeout`, `--wait-interval`, `--backup-dir`, `--force`, `--allow-drift`, `--day-policy`
  - `show`: `--plan-file`
- New env vars:
  - `UDL_REKORDBOX_DB_DIR`
  - `UDL_REKORDBOX_PYTHON_BIN`

## 7. Planning Algorithm
1. Resolve source from config (`soundcloud` only).
2. Enumerate origin list using existing SoundCloud preflight path.
3. Query scoped RB rows from target directory.
4. Match exact title first; normalized fallback second.
5. Compute proposed `created_at` timeline with minimal impact.
6. Preserve day when feasible; mark conflict rows otherwise.
7. Emit warnings and machine-readable plan artifact.

## 8. Apply Algorithm
1. Validate checksum and schema of plan file.
2. Wait for Rekordbox process to stop (polling + timeout).
3. Re-read target rows; enforce preconditions (`content_id`, `old_created_at`, path/title).
4. Prompt for backup confirmation, then write backup file.
5. Apply only changed `created_at` values.
6. Commit and verify post-state.

## 9. Data Contract: Plan File JSON
Top-level fields:
- `version`
- `generated_at`
- `source_id`
- `source_type`
- `source_url`
- `target_dir`
- `rekordbox_db_path`
- `day_policy_requested`
- `day_policy_effective`
- `requires_day_change`
- `summary`:
  - `origin_total`
  - `rb_total`
  - `matched`
  - `unmatched_origin`
  - `unmatched_rb`
  - `will_change`
  - `unchanged`
- `rows[]`:
  - `content_id`
  - `title`
  - `folder_path`
  - `origin_index`
  - `matched_by`
  - `old_created_at`
  - `new_created_at`
  - `day_changed`
- `warnings[]`
- `checksum_sha256`

Canonical checksum generation:
1. Build a checksum input object from the plan object with `checksum_sha256` excluded.
2. Serialize to canonical JSON: UTF-8, lexicographically sorted keys, no insignificant whitespace, deterministic array order.
3. Use deterministic row ordering before serialization: `origin_index` ascending, then `content_id` ascending.
4. Hash canonical bytes with SHA-256.
5. Store lower-case hex digest in `checksum_sha256`.

Verification:
1. Parse plan JSON.
2. Save and clear/remove `checksum_sha256`.
3. Recompute canonical digest with the same rules.
4. Compare exact hex string and abort on mismatch.

Example schema shape:

```json
{
  "version": "1",
  "generated_at": "2026-02-27T16:12:30Z",
  "source_id": "sc-likes",
  "source_type": "soundcloud",
  "source_url": "https://soundcloud.com/you/likes",
  "target_dir": "/Users/jaa/Music/downloaded/sc-likes",
  "rekordbox_db_path": "/Users/jaa/Library/Pioneer/rekordbox/master.db",
  "day_policy_requested": "ask",
  "day_policy_effective": "preserve",
  "requires_day_change": false,
  "summary": {
    "origin_total": 800,
    "rb_total": 790,
    "matched": 780,
    "unmatched_origin": 20,
    "unmatched_rb": 10,
    "will_change": 400,
    "unchanged": 380
  },
  "rows": [
    {
      "content_id": "14502831",
      "title": "Track Name",
      "folder_path": "/Users/jaa/Music/downloaded/sc-likes/Track Name.m4a",
      "origin_index": 0,
      "matched_by": "exact_title",
      "old_created_at": "2026-02-24T21:10:05+00:00",
      "new_created_at": "2026-02-24T21:10:09.900000+00:00",
      "day_changed": false
    }
  ],
  "warnings": [],
  "checksum_sha256": "ab12..."
}
```

## 10. Process Detection
- macOS: check both `rekordbox` and `rekordboxAgent` processes.
- Linux/Windows: best-effort process-name checks (`rekordbox`) and clear warning when detection is partial.
- Fallback behavior when detection is uncertain: require explicit user confirmation (`--force`) before write.
- Poll defaults: interval `2s`, timeout `10m`.

## 11. Python Bridge Constraints
- Use `pyrekordbox` helper subprocess.
- Read/write operations allowed:
  - `inspect_content` (read scoped rows)
  - `apply_created_at` (update only `created_at`)
- Important serialization caveat:
  - non-zero microseconds required to preserve second precision in stored `created_at`.

## 12. Exit Codes
Reuse existing:
- `0`
- `1`
- `2`
- `3`
- `4`
- `130`

## 13. Acceptance Criteria
- `plan` is fully read-only.
- `apply` changes only planned row IDs and only `created_at`.
- Backup exists before first write.
- Conflict prompt appears only when conflict exists.
- Non-interactive behavior deterministic.

## 14. Examples
1. Standard plan:

   ```bash
   udl rekordbox order plan --source sc-likes --out /tmp/sc-likes.plan.json
   ```

2. Show plan summary:

   ```bash
   udl rekordbox order show --plan-file /tmp/sc-likes.plan.json
   ```

3. Standard apply (interactive):

   ```bash
   udl rekordbox order apply --plan-file /tmp/sc-likes.plan.json
   ```

4. Plan in JSON output mode:

   ```bash
   udl --json rekordbox order plan --source sc-likes --out /tmp/sc-likes.plan.json
   ```

5. Apply with explicit no-input + allow policy:

   ```bash
   udl rekordbox order apply --plan-file /tmp/sc-likes.plan.json --no-input --day-policy allow --force
   ```

6. No-input with preserve policy and conflict (expected abort):

   ```bash
   udl rekordbox order apply --plan-file /tmp/sc-likes.plan.json --no-input --day-policy preserve
   ```

7. No-input with ask policy (expected abort because prompt disabled):

   ```bash
   udl rekordbox order apply --plan-file /tmp/sc-likes.plan.json --no-input --day-policy ask
   ```

8. Plan with stricter matching thresholds:

   ```bash
   udl rekordbox order plan --source sc-likes --min-match-ratio 0.95 --max-unmatched 5 --out /tmp/sc-likes.plan.json
   ```

9. Apply with custom wait settings:

   ```bash
   udl rekordbox order apply --plan-file /tmp/sc-likes.plan.json --wait-interval 2s --wait-timeout 10m
   ```

10. Plan with explicit DB/Python overrides:

   ```bash
   udl rekordbox order plan --source sc-likes --rekordbox-db-dir /Users/jaa/Library/Pioneer/rekordbox --python-bin /Users/jaa/.venvs/udl-rb/bin/python3 --out /tmp/sc-likes.plan.json
   ```
