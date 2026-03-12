# UDL TUI Guide

`udl tui` launches an interactive Bubble Tea interface for the main workflows:
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

## Sync Workflow

### Source selection

- `j/k` or up/down: move between configured enabled sources
- `space`: toggle source enabled for this run

### Sync option panel

- `p`: toggle plan mode
- `[` / `]`: decrement/increment plan limit
- `l`: type plan limit directly (`0` = unlimited)
- `u`: toggle unlimited plan limit
- `d`: toggle dry-run
- `a`: toggle `ask_on_existing` override (`inherit` / `on`)
- `g`: toggle `scan_gaps`
- `f`: toggle `no_preflight`
- `t`: type timeout override (Go duration, for example `10m`, `90s`, `1h`)
- `enter`: start run

Validation parity with CLI is enforced before run start:
- `plan` cannot be combined with `scan_gaps`
- `plan` cannot be combined with `ask_on_existing`
- `plan` cannot be combined with `no_preflight`

### Plan mode track selector (`--plan`)

For supported sources (currently `adapter.kind=scdl`), sync enters an interactive selector before download:

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

### Runtime prompts (sync interaction parity)

The TUI now handles sync prompts via in-app dialogs:
- confirm prompts (yes/no, default on enter)
- input prompts (including masked input for sensitive prompts such as Deezer ARL)

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
