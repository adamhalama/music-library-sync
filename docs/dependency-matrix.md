# Adapter Dependency Matrix (`scdl` / `yt-dlp`)

`udl` currently uses a strict external dependency contract for SoundCloud adapters.

## Scope

- Enforced tools:
  - `scdl` for `adapter.kind: scdl`
  - `yt-dlp` for both `adapter.kind: scdl` and `adapter.kind: scdl-freedl`
- Enforcement point: `udl doctor`
- Transitional tool: `spotdl` remains supported but is outside strict matrix enforcement in this phase.

## Supported matrix

| Tool | Supported range | Notes |
|---|---|---|
| `scdl` | `>= 3.0.0` and `< 4.0.0` | Required only for `adapter.kind: scdl`; must support `--yt-dlp-args` passthrough. |
| `yt-dlp` | `>= 2024.1.0` and `< 2027.0.0` | Used for SoundCloud preflight and `scdl-freedl` execution. |

Known-bad versions can be blocked explicitly in doctor matrix rules as regressions are discovered.

## Upgrade behavior

1. Upgrade external tools (`brew upgrade scdl yt-dlp`, pip, or distro tooling).
2. Run `udl doctor` and verify matrix compatibility before running `udl sync`.
3. If doctor reports out-of-range or blocked version, either:
   - downgrade to a supported version, or
   - update `udl` when matrix support is expanded in a release.

## Rollback behavior

1. Reinstall supported tool versions.
2. Re-run `udl doctor` to confirm compatibility.
3. Resume `udl sync`.

## Homebrew behavior

- Homebrew formula installs `udl` and depends on external formulas for:
  - `scdl`
  - `yt-dlp`
- `spotdl` is not required as a Homebrew formula dependency in this phase.

## Future direction

The roadmap tracks a future "maximum shielding" mode: bundled per-platform adapter toolchains with pinned versions, release asset signing, and explicit rollback mechanics.
