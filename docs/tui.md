# UDL TUI Guide

`udl tui` launches an interactive Bubble Tea interface for the main workflows:
- `interactive sync`
- `sync`
- `doctor`
- `validate`
- `init`

The TUI is additive. Existing CLI commands (`udl sync`, `udl doctor`, etc.) remain unchanged.

## Launch

```bash
udl tui
```

Optional debug mode:

```bash
udl tui --debug-messages
```

## Navigation

- Menu: `j/k` or up/down to move, `enter` to open workflow
- Global: `esc` returns to menu when no active in-workflow prompt is open
- Menu-only: `q` or `ctrl+c` quits

## Interactive Sync Workflow

### Source selection

- `j/k` or up/down: move between configured enabled sources
- `space`: toggle source enabled for this run

### Interactive sync option panel

- `[` / `]`: decrement/increment plan limit
- `l`: type plan limit directly (`0` = unlimited)
- `u`: toggle unlimited plan limit
- `d`: toggle dry-run
- `t`: type timeout override (Go duration, for example `10m`, `90s`, `1h`)
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

## Sync Workflow

### Source selection

- `j/k` or up/down: move between configured enabled sources
- `space`: toggle source enabled for this run

### Sync option panel

- `d`: toggle dry-run
- `a`: toggle `ask_on_existing` override (`inherit` / `on`)
- `g`: toggle `scan_gaps`
- `f`: toggle `no_preflight`
- `t`: type timeout override (Go duration, for example `10m`, `90s`, `1h`)
- `enter`: start run

This workflow is the streamlined non-plan path and no longer exposes `--plan` controls.

### Runtime prompts (sync interaction parity)

The TUI now handles sync prompts via in-app dialogs:
- confirm prompts (yes/no, default on enter)
- input prompts (including masked input for sensitive prompts such as Deezer ARL)

### Runtime output

During active sync runs, the TUI renders:
- a compact live progress panel for the current track and overall run progress
- a short compact activity history (`[done]`, `[skip]`, `[fail]`, plus source/sync summaries)

This mirrors the streamlined default `udl sync` experience more closely than the earlier raw event-log-only view.

### Cancellation

During an active sync run:
- `x` or `ctrl+c`: request cancellation

Cancellation also works while waiting in plan selection or prompt dialogs.

## Doctor / Validate Workflows

- `doctor` runs checks and shows sorted check output
- `validate` runs config validation and shows result

These remain lightweight result views in the current TUI pass.

## Init Workflow

`init` runs through the TUI and now supports overwrite confirmation in-app when config already exists.

Prompt controls:
- `y` / `n`: answer confirm prompts
- `enter`: accept default
- `esc` / `q`: cancel prompt

## Current Limitations

- `promote-freedl` and `version` are not exposed as TUI workflows.
- `sync` output style controls (`progress`, `preflight-summary`, `track-status`) are not currently configurable from TUI.
- `doctor` and `validate` are non-guided views (run + output).
