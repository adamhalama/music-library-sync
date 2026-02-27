# Rekordbox Ordering Implementation Checklist

## 1. CLI wiring
- Add command constructors in `/Users/jaa/dev/utils/update-downloads/internal/cli/root.go`.
- Add tests mirroring style in `/Users/jaa/dev/utils/update-downloads/internal/cli/sync_test.go`.

## 2. Core packages
- Create `internal/rekordbox/order` (planner + matcher + policy resolution).
- Create `internal/rekordbox/bridge` (python subprocess protocol).

## 3. Safety
- Enforce process-closed precondition.
- Enforce backup-before-write.
- Enforce scope/row precondition checks.

## 4. Data contracts
- Implement plan JSON schema and checksum.
- Implement `show` rendering from saved plan file.

## 5. Tests
- Unit tests for matching/policy/planner.
- CLI behavior tests for prompts and `--no-input`.
- Bridge contract tests.
- Manual acceptance runbook on test DB copy.

## 6. Docs/readme follow-up
- Update `/Users/jaa/dev/utils/update-downloads/readme.md` command surface only after command exists.
- Keep feature marked experimental until end-to-end validation on backup DB succeeds.
