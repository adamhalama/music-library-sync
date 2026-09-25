# Free DL gate userscripts

`udl sync` with `adapter.kind: scdl-freedl` opens each track's free-download
gate in a browser and waits for the finished file to appear in the browser's
download directory. The gate itself is a human step: a page that asks you to
follow/like/repost on SoundCloud, hand over an email, or follow an Instagram
account before it hands you the file.

`udl-freedl-gate-autopilot.user.js` performs those clicks, so a capture run can
go through unattended.

## Install (Tampermonkey, already present in Helium)

1. Open the Tampermonkey dashboard → **Utilities** → **Import from file**, and
   pick `udl-freedl-gate-autopilot.user.js`.
   (Alternatively enable *Allow access to file URLs* for the extension and open
   the file's `file://` URL — Tampermonkey then offers to install it.)
2. Open `CONFIG` at the top of the installed script and fill in `email` / `name`
   (they ship empty; never commit real values — the repo is public).
3. Visit one gate by hand first, so you can approve the SoundCloud OAuth consent
   screen once. The script deliberately does not click that button.

Press **Escape** on any gate page to abort a run. A small HUD in the bottom
right shows each click as it happens.

## Covered hosts

| host | how it is driven |
| --- | --- |
| `gaterush.me` | Exact: steps render into `#stepStage`; `#emailInput`/`#nameInput` + `[data-go]`, `.follow-pair` → `[data-open]` then `[data-confirm]`, `.btn-soundcloud` for OAuth, `#download` once it gains `.ready`. |
| `hypeddit.com` | Exact: `#downloadProcess` opens the `.fangate-slider-content` carousel; each slide advanced by its `.step_button_N`, finishing at `#gateDownloadButton`. |
| `droploud.com` | Generic: entry CTA `.ds-free-dl`, then text-matched advance buttons. |
| `mypresskit.info` | Generic: `/gate/` pages, text-matched advance buttons. |

Droploud and MyPressKit are client-rendered Next.js apps whose step markup is
not in the served HTML, so they get the generic driver rather than exact
selectors. The generic driver only presses controls whose label matches an
allow-list (`continue`, `next`, `confirm`, `download`, …) and never presses one
matching the deny-list (`add to cart`, `buy`, `checkout`, `subscribe`, …) —
Droploud in particular puts an **Add to cart** button next to the free download.

## What it does to your accounts

These gates are not paywalls; the artist is giving the file away and the price is
a social action. Automating the clicks automates the actions:

- **SoundCloud steps are real.** After you authorise the gate once via OAuth, its
  backend performs genuine follows, likes, reposts and comments from your
  account. Reposts appear in your followers' feeds.
- **Email steps** submit `CONFIG.email` to the artist's mailing list.
- **Instagram/Spotify steps are honour-system.** The gate opens the profile and
  asks you to confirm you followed. The script opens the profile for real, but
  with `offsiteFollows: "auto"` it also clicks confirm — which asserts a follow
  that only happened if you actually did it in the tab that opened. Set
  `offsiteFollows: "manual"` to open the profile and leave the confirm to you.

The script will not click **Allow**/**Connect** on an OAuth consent screen.
Granting an app access to your account is yours to approve; once granted per
service, later gates go through without it.

## Using it with a capture run

With the script installed, re-run the Free DL capture and the gates complete on
their own:

```sh
python3 .dev/freedl-0912/plan.py 50          # refresh the plan
python3 .dev/freedl-0912/capture.py <plan.json> [remote_id ...]
python3 .dev/freedl-0912/promote.py <capture-run-id> --apply
python3 .dev/freedl-0912/status.py           # what upgraded, what is left
```

If a gate is completed out of band, the file just sits in `~/Downloads`;
`udl promote-freedl --free-dl-dir ~/Downloads --library-dir <dir>` picks it up
without another capture run.
