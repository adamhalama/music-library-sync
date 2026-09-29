# Free DL gate userscript

`udl-freedl-gate-autopilot.user.js` advances artist-provided download gates in
Helium. It fills configured email/name fields, starts the gate's SoundCloud
OAuth workflow, opens required social profiles, and requests the final file.
It waits for evidence of offsite actions; opening a profile is never treated as
following, liking, commenting or reposting.

## Install and run

Import the script through Tampermonkey → Utilities → Import from file. Set
`email` and `name` in the **installed** copy only. Never put personal values or
credentials into the repository. Existing browser logins remain in Helium.
Press Escape to stop. Each run has a ten-minute budget; the corner HUD records
its actions. OAuth consent is left to the user or a separately authorized
browser driver.

A browser driver can inject the file's contents into an existing Helium gate
tab. Before injection, set `window.UDL_GATE_CONFIG = { email, name }` using local
values. Reinjection stops the previous runner so two loops cannot click the
same page. Runtime configuration is also available through
`window.udlGate.configure({ email, name })`.

Inspect `window.udlGate.inspect()` or `window.udlGate.state` for progress:

| Status | Meaning |
| --- | --- |
| `running` | Advancing the current gate. |
| `needs-email` / `needs-name` | Fill the field or configure the missing value; the loop resumes. |
| `needs-follow` | Perform and verify the follow identified by `state.pending.profile`. |
| `needs-social-action` | Perform and verify the action identified by `state.pending.kind` and `.profile`. |
| `needs-interaction` | Generic gate has no recognized next control; inspect login, CAPTCHA or provider UI. |
| `download-requested` | Final download control was clicked; filesystem capture must still verify the file. |
| `stalled` / `error` / `aborted` | Budget exhausted, script error, or deliberate stop. |

After actually performing an action in the opened profile tab, the driver can
report observed success back in the gate tab:

```js
// Use the exact profile URL from state.pending; do not fabricate evidence.
window.udlGate.confirmFollow(profileURL, "Following button observed");
window.udlGate.confirmAction(trackURL, "like", "Unlike button observed");
window.udlGate.confirmAction(trackURL, "repost", "Undo repost button observed");
window.udlGate.confirmAction(trackURL, "comment", "Submitted comment visible");
```

Evidence is held only in the current page's memory. The script does not inspect
cross-origin tabs itself. A human can also complete the gate controls directly;
the runner observes subsequent page changes. `window.udlGate.stop()` stops it.

## Host behavior

| Host | Adapter |
| --- | --- |
| `gaterush.me` | Email, SoundCloud OAuth, both follow-list layouts, final ready download button. Follow confirmations require reported evidence. |
| `hypeddit.com` | Only the current carousel slide; text email/name fields; no-API SoundCloud actions; final download slide. |
| `droploud.com` | Current `.dtr-stage` card, exact public Flight gate URLs, all-account evidence before confirmation, explicit unlocked download card. |
| `mypresskit.info` | Native combined SoundCloud OAuth, comment textarea, verified-only Instagram confirmation, exact enabled Download. |

Hypeddit's upcoming and completed slides can still have layout boxes, so
`offsetParent` alone does not indicate the current step. Its SoundCloud
no-API buttons merely open a popup and remove `.undone`; neither event proves
an action happened. Required actions are confirmed independently by URL and
action, so a verified like cannot also satisfy a required repost or comment.

Generic adapters do not automatically press follow confirmations or purchase
controls. An entry button labelled Download does not count as a finished
download. Only a final provider download button or direct file link yields
`download-requested`; success belongs to the file capture and quality check.

## Capture and promotion

```sh
python3 .dev/freedl-0912/plan.py 50
python3 .dev/freedl-0912/capture.py <plan.json> [remote_id ...]
python3 .dev/freedl-0912/promote.py <capture-run-id> --apply
python3 .dev/freedl-0912/status.py
```

Files completed outside a capture run remain in Helium's download directory
and can be scanned with `udl promote-freedl --free-dl-dir <downloads> --library-dir
<isolated-upgrade-directory>`.

## Regression tests

Install the DOM test dependency outside the checkout, then run the tests:

```sh
npm install --prefix /tmp/udl-userscript-tests jsdom@29 --no-audit --no-fund
NODE_PATH=/tmp/udl-userscript-tests/node_modules node .dev/userscripts/udl-freedl-gate-autopilot.test.cjs
node --check .dev/userscripts/udl-freedl-gate-autopilot.user.js
```

Fixtures cover hidden carousel slides, missing/runtime email configuration,
both Gaterush follow layouts, armed confirmations after reinjection, separate
like/repost evidence, and entry download buttons. They simulate DOM behavior;
real downloads still require browser and filesystem verification.

## Background-only operation

Set `window.UDL_GATE_CONFIG = { backgroundOnly: true }` before injection (or
use `udlGate.configure`). Synchronous social-link popup requests are recorded
in `state.openRequests` instead of opening windows. An external driver performs
those actions in existing background tabs, then supplies actual evidence.
Droploud exposes every outstanding account in `state.pending.profiles`; one
confirmed account never satisfies a multi-account step. URLs come from the
page's public `gateData`, not guessed display names. SoundCloud/OAuth steps
report `needs-oauth` and their requirements for the browser driver.

Background mode suppresses popup calls made synchronously by recognized click
handlers. It does not manage asynchronous provider code or unknown navigation;
the driver should stop the runner before any unsupported workflow.

MyPressKit's Instagram button immediately records gate completion, so it is
never clicked before independently verified following. Its combined SoundCloud
step fills the configured comment and, in background mode, reports the exact
native OAuth fallback URL in `state.pending.profile`. The driver can review
scopes and navigate an owned background tab; the script does not impersonate
OAuth completion. Completed steps render checkmarked text and are not clicked
again. Only an enabled button whose exact label is Download requests the file.

Hidden tabs may pause finite CSS entry animations at opacity zero. Background
mode finishes fade-in animations only on known gate cards/current slides;
infinite spinners, exit animations, unrelated elements and upcoming Hypeddit
slides stay untouched. This restores visibility without advancing future steps.

Inject into the **page's main JavaScript world**. Helium's Apple Events
`execute javascript` can run in an isolated world: its `window.open` replacement
cannot intercept handlers in the page world. A driver can execute a script
node using the page's existing CSP nonce (see `.dev/freedl-0912/helium.py`'s
main-world helper). Runtime configuration must be set in that same world.
