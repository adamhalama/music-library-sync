# UDL TUI Guide

`udl tui` launches an interactive Bubble Tea interface for the main workflows:
- `interactive sync`
- `sync`
- `doctor`
- `validate`
- `config editor`
- `init`

The TUI is additive. Existing CLI commands (`udl sync`, `udl doctor`, etc.) remain unchanged.

The current TUI uses a shared shell:
- landing screen with workflow navigation
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

- Landing screen: `j/k` or up/down to move, `enter` to open workflow
- Global: `esc` returns to the landing screen when the active workflow allows back navigation and no in-workflow prompt is open
- Landing screen only: `q` or `ctrl+c` quits
- Shell layout:
  - width `>= 110`: left sidebar + main panel shell
  - width `< 110`: compact top navigation strip + main panel shell

Inside active workflows, the sidebar/top navigation is informational in this stage. Workflow switching still happens by returning to the landing screen with `esc`.

## Interactive Sync Workflow

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

## Sync Workflow

### Source selection

- sources render in the shell sidebar once the workflow is opened
- `j/k` or up/down: move between configured enabled sources
- `space`: toggle source enabled for this run

### Sync option panel

- `d`: toggle dry-run
- `a`: toggle `ask_on_existing` override (`inherit` / `on`)
- `g`: toggle `scan_gaps`
- `f`: toggle `no_preflight`
- `t`: type timeout override (Go duration, for example `10m`, `90s`, `1h`)
- `p`: collapse/expand the activity panel
- `enter`: start run

This workflow is the streamlined non-plan path and no longer exposes `--plan` controls.

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

## Doctor / Validate Workflows

### Doctor

- `doctor` auto-runs when opened
- the shell renders:
  - `Summary` with total checks and error/warn/info counts
  - `Checks` with severity-ordered checklist rows
  - `Next Step` guidance when warnings or errors are present
- `esc` can be used to return to the landing screen immediately

### Validate

- `validate` auto-runs when opened
- the shell renders:
  - `Status` with valid/invalid outcome
  - `Context` with config path/search-path information and source counts when config loaded
  - `Details` with wrapped load or validation errors
- `esc` can be used to return to the landing screen immediately

## Init Workflow

`init` is now a guided workflow rather than auto-running on open.

### Init intro

- opening `init` shows:
  - `Plan` summary of what `udl init` will create
  - `Paths` for config and state directory targets
  - `Actions` with `enter` to start and `esc` to go back
- if the target config already exists, the intro screen calls that out before the run starts

### Init run and result states

- `enter`: start init
- while init is running, back navigation is disabled
- after completion, the shell renders result panels for success, cancel, or failure
- success includes a next-step reminder to review the config and run `udl validate`

### Init prompts

- overwrite confirmation still renders as an in-app modal
- prompt controls:
  - `y` / `n`: answer confirm prompts
  - `enter`: accept default
  - `esc` / `q`: cancel prompt

Init prompts render as shell modals.

## Config Editor Workflow

`config editor` is a shell-native guided editor for a single config file.

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

- `promote-freedl` and `version` are not exposed as TUI workflows.
- `sync` output style controls (`progress`, `preflight-summary`, `track-status`) are not currently configurable from TUI.
- Workflow sidebar/top-nav switching is not active yet; use `esc` to return to the landing screen first.
- The config editor rewrites canonical YAML; it does not preserve hand-written comments or original layout.
