// ==UserScript==
// @name         UDL Free DL gate autopilot
// @namespace    https://github.com/jaa/update-downloads
// @version      1.0.0
// @description  Clicks through SoundCloud free-download gates (Gaterush, Hypeddit, Droploud, MyPressKit) so `udl sync` with adapter.kind=scdl-freedl can capture the file unattended.
// @author       udl
// @match        *://gaterush.me/*
// @match        *://www.gaterush.me/*
// @match        *://hypeddit.com/*
// @match        *://www.hypeddit.com/*
// @match        *://droploud.com/*
// @match        *://www.droploud.com/*
// @match        *://mypresskit.info/*
// @match        *://www.mypresskit.info/*
// @run-at       document-idle
// @grant        none
// ==/UserScript==

//
// WHAT THIS DOES TO YOUR ACCOUNTS — read once, then forget about it.
//
// These gates are not paywalls; the artist is giving the file away. The price
// is a social action. Automating the clicks therefore automates the actions:
//
//   * SoundCloud steps are performed for real, by the gate's own backend, after
//     you authorise it once via OAuth. That means genuine follows, likes,
//     reposts and (where the gate asks for one) a comment from your account.
//     Reposts show up in your followers' feeds.
//   * Email steps submit CONFIG.email to the artist's list.
//   * Instagram/Spotify steps are honour-system: the gate opens the profile and
//     asks you to confirm you followed. This script opens the profile for real,
//     but with offsiteFollows:"auto" it also clicks confirm, which asserts a
//     follow that only happens if you actually did it in the opened tab. Set it
//     to "manual" to open the profile and leave the confirm to you.
//
// It will NOT click "Allow"/"Connect" on an OAuth consent screen. Granting an
// app access to your account is yours to approve; do it once per gate service
// and the script sails through afterwards.
//
// INSTALL: Tampermonkey dashboard -> Utilities -> Import from file, or open
// this file's file:// URL with "Allow access to file URLs" enabled for the
// extension. Press Escape on any gate page to abort the run.
//

(function () {
  "use strict";

  const CONFIG = {
    // Used for gates with an email step. Fill these in your installed copy
    // (Tampermonkey editor) only; this file is public. Left empty, email
    // gates stay on their form and nothing is submitted.
    email: "",
    name: "",
    // Left in the comment box when a gate requires a SoundCloud comment.
    comment: "🔥",
    // "auto"   - open each profile, then confirm automatically
    // "manual" - open each profile, leave the confirm click to you
    offsiteFollows: "auto",
    // Give up on a gate after this long and leave the page alone.
    budgetMs: 90000,
    // Poll interval for "has the step advanced yet".
    tickMs: 400,
    // Gates arm their confirm buttons on a timer; never click faster than this.
    minSettleMs: 1200,
  };

  /* ------------------------------ plumbing ------------------------------ */

  let aborted = false;
  const started = Date.now();
  const log = [];

  const sleep = (ms) => new Promise((r) => setTimeout(r, ms));
  const budgetLeft = () => CONFIG.budgetMs - (Date.now() - started);
  const visible = (el) =>
    !!el &&
    !el.disabled &&
    el.offsetParent !== null &&
    getComputedStyle(el).visibility !== "hidden";

  const textOf = (el) => (el.innerText || el.textContent || "").trim().toLowerCase();

  /** Poll `fn` until it returns something truthy, the budget runs out, or we abort. */
  async function waitFor(fn, timeoutMs = 15000, label = "") {
    const deadline = Date.now() + Math.min(timeoutMs, Math.max(0, budgetLeft()));
    while (Date.now() < deadline) {
      if (aborted) return null;
      let got;
      try {
        got = fn();
      } catch (e) {
        got = null;
      }
      if (got) return got;
      await sleep(CONFIG.tickMs);
    }
    if (label) note("timed out waiting for " + label);
    return null;
  }

  const clicked = new WeakSet();
  /** Click an element at most once, and only while it is actually clickable. */
  async function clickOnce(el, why) {
    if (!el || aborted || clicked.has(el) || !visible(el)) return false;
    clicked.add(el);
    note("click: " + why);
    el.scrollIntoView({ block: "center" });
    el.click();
    await sleep(CONFIG.minSettleMs);
    return true;
  }

  function setInput(el, value) {
    if (!el) return;
    // React/Next controlled inputs ignore a plain .value assignment.
    const proto = Object.getPrototypeOf(el);
    const desc = Object.getOwnPropertyDescriptor(proto, "value");
    if (desc && desc.set) desc.set.call(el, value);
    else el.value = value;
    el.dispatchEvent(new Event("input", { bubbles: true }));
    el.dispatchEvent(new Event("change", { bubbles: true }));
  }

  /* -------------------------------- HUD --------------------------------- */

  const hud = document.createElement("div");
  hud.style.cssText = [
    "position:fixed", "z-index:2147483647", "right:12px", "bottom:12px",
    "max-width:360px", "padding:10px 12px", "border-radius:10px",
    "font:12px/1.45 ui-monospace,SFMono-Regular,Menlo,monospace",
    "background:rgba(17,17,20,.93)", "color:#e8e8ea",
    "box-shadow:0 6px 28px rgba(0,0,0,.45)", "white-space:pre-wrap",
    "pointer-events:none",
  ].join(";");
  function note(line) {
    log.push(line);
    hud.textContent = "UDL gate autopilot — Esc aborts\n" + log.slice(-9).join("\n");
    if (!hud.isConnected && document.body) document.body.appendChild(hud);
    console.log("[udl-gate]", line);
  }
  addEventListener("keydown", function (e) {
    if (e.key === "Escape") {
      aborted = true;
      note("aborted by Escape");
    }
  });

  /* ------------------------------ adapters ------------------------------ */

  // Never click these, on any host: they cost money, change settings, or
  // navigate away from the gate.
  const DENY = /add to cart|buy|purchase|checkout|wishlist|cookie|subscribe|sign ?up|log ?in|sign ?in|create gate|charts|home|terms|dmca|privacy/i;
  const ADVANCE = /^(free download|download|continue|next|confirm|i followed|i have followed|verify|done|unlock|get (it|the file)|claim|submit|save)/i;

  /** Clickable controls inside `root`, in DOM order, that look like "advance". */
  function advanceCandidates(root) {
    return Array.prototype.slice
      .call(root.querySelectorAll('button,a[role="button"],[data-go],[data-confirm]'))
      .filter(visible)
      .filter(function (el) { return !clicked.has(el); })
      .filter(function (el) {
        const t = textOf(el);
        if (DENY.test(t)) return false;
        return ADVANCE.test(t) || el.hasAttribute("data-go") || el.hasAttribute("data-confirm");
      });
  }

  const adapters = {
    /* Gaterush renders one step at a time into #stepStage:
       email      -> #emailInput (+#nameInput) + [data-go]
       soundcloud -> optional #commentInput + .btn-soundcloud[data-go] -> OAuth popup
       follow     -> .follow-pair of [data-open] then [data-confirm] (armed on a timer)
       finish     -> #download gains .ready and stops being disabled */
    "gaterush.me": {
      detect: function () { return document.getElementById("stepStage"); },
      run: async function () {
        const stage = document.getElementById("stepStage");
        const download = document.getElementById("download");

        while (!aborted && budgetLeft() > 0) {
          if (download && visible(download) && download.classList.contains("ready")) {
            await clickOnce(download, "gaterush download");
            return "downloaded";
          }

          const email = stage.querySelector("#emailInput");
          if (email && !email.value) {
            setInput(stage.querySelector("#nameInput"), CONFIG.name);
            setInput(email, CONFIG.email);
            await clickOnce(stage.querySelector("[data-go]"), "email step");
            continue;
          }

          const comment = stage.querySelector("#commentInput");
          if (comment && !comment.value) setInput(comment, CONFIG.comment);

          // Follow lists: open the profile for real, then confirm.
          const open = stage.querySelector(".follow-pair [data-open]:not(.done)");
          if (open) {
            await clickOnce(open, "open " + (open.dataset.h || "profile"));
            if (CONFIG.offsiteFollows !== "auto") {
              note("offsiteFollows=manual — confirm it yourself");
              return "needs-user";
            }
            const pair = open.closest(".follow-pair");
            const confirm = await waitFor(function () {
              const c = pair && pair.querySelector("[data-confirm]");
              return c && !c.disabled ? c : null;
            }, 20000, "confirm to arm");
            await clickOnce(confirm, "confirm follow");
            continue;
          }

          const go = stage.querySelector("[data-go]");
          if (go && visible(go) && !clicked.has(go)) {
            const isOauth = go.classList.contains("btn-soundcloud");
            await clickOnce(go, isOauth ? "connect SoundCloud (OAuth popup)" : "continue");
            if (isOauth) {
              note("waiting for OAuth — approve it in the popup if asked");
              await waitFor(function () {
                return stage.querySelector("[data-go]") !== go ||
                  (download && download.classList.contains("ready"));
              }, 45000, "OAuth to complete");
            }
            continue;
          }

          await sleep(CONFIG.tickMs);
        }
        return "stalled";
      },
    },

    /* Hypeddit: #downloadProcess opens a carousel of .fangate-slider-content
       slides (.email/.sc/.ig, then .dw), each advanced by its .step_button_N. */
    "hypeddit.com": {
      detect: function () { return document.getElementById("downloadProcess"); },
      run: async function () {
        await clickOnce(document.getElementById("downloadProcess"), "start hypeddit gate");

        while (!aborted && budgetLeft() > 0) {
          const dl = document.getElementById("gateDownloadButton");
          if (visible(dl)) {
            await clickOnce(dl, "hypeddit download");
            return "downloaded";
          }

          const email = document.querySelector('#section-one input[type="email"], input[name="email"]');
          if (email && visible(email) && !email.value) {
            setInput(email, CONFIG.email);
            await clickOnce(document.getElementById("email_to_downloads_next"), "email step");
            continue;
          }

          const step = Array.prototype.slice
            .call(document.querySelectorAll('[class*="step_button_"]'))
            .find(function (el) {
              return visible(el) && !clicked.has(el) && !DENY.test(textOf(el));
            });
          if (step) {
            const oauth = /soundcloud|spotify|youtube/i.test(step.className);
            await clickOnce(step, "slide step" + (oauth ? " (OAuth popup)" : ""));
            if (oauth) {
              note("waiting for OAuth — approve it in the popup if asked");
              await sleep(4000);
            }
            continue;
          }

          await sleep(CONFIG.tickMs);
        }
        return "stalled";
      },
    },

    /* Droploud and MyPressKit are client-rendered SPAs with no stable step
       markup, so both are driven generically: press the entry CTA, then keep
       pressing whatever reads as "advance" while the page keeps changing. */
    "droploud.com": {
      detect: function () { return document.querySelector(".ds-free-dl"); },
      run: function () { return genericGate(".ds-free-dl"); },
    },
    "mypresskit.info": {
      detect: function () { return /\/gate\//.test(location.pathname); },
      run: function () { return genericGate(null); },
    },
  };

  async function genericGate(entrySelector) {
    if (entrySelector) {
      const entry = await waitFor(function () {
        const el = document.querySelector(entrySelector);
        return visible(el) ? el : null;
      }, 10000, "entry button");
      await clickOnce(entry, "open gate");
    }

    let idleTicks = 0;
    while (!aborted && budgetLeft() > 0) {
      const emails = Array.prototype.slice
        .call(document.querySelectorAll('input[type="email"]'))
        .filter(visible);
      for (const el of emails) if (!el.value) setInput(el, CONFIG.email);

      const next = advanceCandidates(document)[0];
      if (next) {
        idleTicks = 0;
        const label = textOf(next).slice(0, 40) || "(unlabelled)";
        await clickOnce(next, label);
        if (/download/.test(label)) return "downloaded";
        continue;
      }

      if (++idleTicks > 12) {
        note("no further step found — leaving the rest to you");
        return "stalled";
      }
      await sleep(CONFIG.tickMs);
    }
    return "stalled";
  }

  /* -------------------------------- main -------------------------------- */

  (async function main() {
    const host = location.hostname.replace(/^www\./, "");
    const adapter = adapters[host];
    if (!adapter) return;
    // Only engage on an actual gate page, not the site's other pages.
    if (!(await waitFor(adapter.detect, 8000))) return;

    note("gate detected on " + host);
    let outcome;
    try {
      outcome = await adapter.run();
    } catch (e) {
      note("error: " + (e && e.message));
      outcome = "error";
    }
    note("finished: " + outcome);
  })();
})();
