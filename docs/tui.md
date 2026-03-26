# UDL TUI Guide

`udl tui` launches an interactive Bubble Tea interface for the main workflows:
- `Get Started`
- `Credentials`
- `Check System`
- `Run Sync`
- `Advanced Config`

The TUI is additive. Existing CLI commands (`udl sync`, `udl doctor`, etc.) remain unchanged.

The current TUI uses a shared shell:
- home screen with outcome-focused navigation
- shared titlebar, badges, command summary, shortcuts, body panel, and footer
- inline plan-selection screen for interactive sync
- in-app modal prompts for confirmations/input
- compact shell fallback when terminal width is below `110` columns

## Launch

```bash
udl tui
```

Optional debug mode:

```bash
udl tui --debug-messages
```

## Navigation

- Home screen: `j/k` or up/down to move, `enter` to open workflow
- Global: `esc` returns to the landing screen when the active workflow allows back navigation and no in-workflow prompt is open
- Home screen only: `q` or `ctrl+c` quits
- Shell layout:
  - width `>= 110`: left sidebar + main panel shell
  - width `< 110`: compact top navigation strip + main panel shell

Inside active workflows, the sidebar/top navigation is informational in this stage. Workflow switching still happens by returning to the landing screen with `esc`.

## Get Started Workflow

`Get Started` is the first-run path.

- If runtime config is missing, invalid, or has zero sources, `udl tui` opens this workflow automatically.
- The workflow asks for:
  - music folder root
  - state folder
  - one starter source
- If the source needs auth, the wizard can save SoundCloud, Deezer, and Spotify credentials into macOS Keychain before the first sync.
- SoundCloud is the default and recommended path for v1.
- Spotify is available but shown as beta/advanced and does not store secrets in YAML.
- Saving the starter config immediately runs `doctor` and shows one clear next step.

## Credentials Workflow

- `Credentials` is the persistent auth/status board.
- It shows SoundCloud client ID, Deezer ARL, and Spotify app credentials with:
  - health state (`missing`, `available`, `external override`, `needs refresh`)
  - storage source
  - affected workflows
  - one obvious action (`save`, `update`, `refresh`, or `clear`)
- Managed values are stored in macOS Keychain, not in `udl.yaml`.
- Environment variables and `~/.spotdl/config.json` still work as compatibility sources, but they are treated as external.

## Run Sync Workflow

`Run Sync` opens the interactive sync path.

### Source selection

- sources render in the shell sidebar once the workflow is opened
- `j/k` or up/down: move between configured enabled sources
- `space`: toggle source enabled for this run

### Interactive sync option panel

- `[` / `]`: decrement/increment plan limit
- `l`: type plan limit directly (`0` = unlimited)
- `u`: toggle unlimited plan limit
- `d`: toggle dry-run
- `t`: type timeout override (Go duration, for example `10m`, `90s`, `1h`)
- `p`: collapse/expand the activity panel
- `enter`: start run

`interactive sync` always runs the existing `udl sync --plan` path. Standard sync-only flags such as `ask_on_existing`, `scan_gaps`, and `no_preflight` are intentionally hidden here.

### Interactive track selector (`--plan`)

For supported sources (currently `adapter.kind=scdl`), interactive sync enters an interactive selector before download:

- `j/k` or up/down: move
- `space`: toggle current row
- `a`: select all toggleable rows
- `n`: clear all toggleable rows
- `enter`: confirm selection and continue run
- `q`/`esc`: cancel selection and interrupt run

Selector header includes source config context:
- source id/type/adapter
- target directory
- state file
- source URL
- current plan limit and run mode

The selector is rendered inline in the main content area instead of opening a separate overlay.

Interactive sync now keeps the shell shortcuts/footer active during selection:
- the track table stays inline in the main body
- the footer shows selected, pending, skipped, and progress stub counts before the run
- the activity panel is expanded by default on wide layouts and collapsed by default on compact layouts

### Runtime prompts (sync interaction parity)

The TUI now handles sync prompts via in-app dialogs:
- confirm prompts (yes/no, default on enter)
- input prompts (including masked input for sensitive prompts such as Deezer ARL)

These prompts render as shell modals rather than full-screen prompt pages.

### Runtime output

During active sync runs, the TUI renders:
- a shell-native `Run` section with the current source headline, current track progress, and overall run progress
- per-source summary rows with lifecycle, planned count, done/skipped/failed totals, and latest track/outcome on wide layouts
- a shell-native `Activity` section with recent outcomes (`[done]`, `[skip]`, `[fail]`) plus source/sync summaries and pinned failure diagnostics

The activity panel is expanded by default on wide layouts and collapsed by default on compact layouts.

### Cancellation

During an active sync run:
- `x` or `ctrl+c`: request cancellation

Cancellation also works while waiting in plan selection or prompt dialogs.

## Check System Workflow

### Doctor

- `Check System` runs `doctor` automatically when opened
- the shell renders:
  - `Summary` with total checks and error/warn/info counts
  - `Checks` with severity-ordered checklist rows
  - `Next Step` guidance when warnings or errors are present
- when auth is missing or stale, press `c` to jump straight into `Credentials`
- `esc` can be used to return to the landing screen immediately

## Advanced Config Workflow

`Advanced Config` opens the shell-native config editor for a single config file.

### Target behavior

- if `--config` is set, the editor targets that file
- otherwise it targets the user config path (`$XDG_CONFIG_HOME/udl/config.yaml` or `~/.config/udl/config.yaml`)
- it edits one concrete file, not the merged runtime config view
- saves rewrite canonical YAML and ensure `defaults.state_dir` exists

### Flow

- `Target`: inspect the target file and recover from invalid YAML
- `Defaults`: edit global defaults such as `state_dir`, `archive_file`, threads, and timeout
- `Sources`: add, duplicate, delete, reorder, enable, and edit sources
- `Review`: inspect human-readable summary, validation issues, and canonical YAML
- `Save`: write the config and show next-step guidance

### Controls

- `j/k`: move inside the active list/form
- `tab` / `shift+tab`: switch panes or toggle preview depending on step
- `enter`: edit/apply/advance
- `a`: add source
- `d`: duplicate source
- `D`: delete source
- `[` / `]`: reorder source
- `p`: toggle canonical YAML preview in review/save
- `s`: save from review or save step
- `esc`: back, or prompt to discard unsaved changes

## Current Limitations

- `promote-freedl`, `version`, `validate`, and low-level `init` are not exposed on the public TUI home screen.
- `sync` output style controls (`progress`, `preflight-summary`, `track-status`) are not currently configurable from TUI.
- Workflow sidebar/top-nav switching is not active yet; use `esc` to return to the landing screen first.
- The config editor rewrites canonical YAML; it does not preserve hand-written comments or original layout.
