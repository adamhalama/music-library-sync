// ==UserScript==
// @name         UDL Free DL gate autopilot
// @namespace    https://github.com/jaa/update-downloads
// @version      1.2.0
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
//   * Offsite steps open the actual profile. Confirmation requires explicit
//     evidence from the person/automation that performed the follow. Opening a
//     tab alone never counts as completing a social action.
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

  // Reinjection replaces the old runner instead of issuing duplicate actions.
  if (window.udlGate && (window.udlGate.isPaused?.() || ["paused", "needs-captcha"].includes(window.udlGate.state.status))) {
    // Reinjection must not restart automation while a person owns the form.
    return;
  }
  if (window.udlGate) window.udlGate.stop();
  const CONFIG = {
    // Used for gates with an email step. Fill these in your installed copy
    // (Tampermonkey editor) only; this file is public. Left empty, email
    // gates stay on their form and nothing is submitted.
    backgroundOnly: false,
    email: "",
    name: "",
    // Left in the comment box when a gate requires a SoundCloud comment.
    comment: "🔥",
    // Ten minutes allows OAuth, email verification and offsite actions.
    budgetMs: 600000,
    // Poll interval for "has the step advanced yet".
    tickMs: 400,
    // Gates arm their confirm buttons on a timer; never click faster than this.
    minSettleMs: 1200,
  };
  Object.assign(CONFIG, window.UDL_GATE_CONFIG || {});

  /* ------------------------------ plumbing ------------------------------ */

  let aborted = false;
  let paused = false;
  let pauseStarted = 0;
  let pausedMs = 0;
  const started = Date.now();
  const log = [];

  const sleep = (ms) => new Promise((r) => setTimeout(r, ms));
  const budgetLeft = () => CONFIG.budgetMs - (Date.now() - started - pausedMs - (paused ? Date.now() - pauseStarted : 0));
  function finishBackgroundEntryAnimations() {
    if (!CONFIG.backgroundOnly || !document.getAnimations) return;
    const host = location.hostname.replace(/^www\./, "");
    for (const animation of document.getAnimations()) {
      if (animation.playState === "finished" || animation.playState === "idle") continue;
      const effect = animation.effect, target = effect?.target;
      if (!target?.matches || target.closest('[hidden],[aria-hidden="true"],.leaving,.upcomming-slide,.move-left')) continue;
      let knownEntry = false;
      if (host === "gaterush.me") {
        knownEntry = (target.matches(".card") && !!target.querySelector("#stepStage")) ||
          target.matches("#stepStage > .step.entering");
      } else if (host === "hypeddit.com") {
        // Upcoming cards remain untouched, even if their animations are finite.
        knownEntry = target.matches(".fangate-slider-content.current-slide:not(.upcomming-slide):not(.move-left),#myCarousel.downloadProcess.move-bottom-now");
      } else if (host === "droploud.com") {
        knownEntry = target.matches(".dtr-stage.is-vis,.dtr-stage.is-vis > .dtr-card-pane");
      }
      if (!knownEntry) continue;
      const timing = effect.getComputedTiming();
      if (!Number.isFinite(timing.endTime) || !Number.isFinite(timing.iterations)) continue;
      const frames = effect.getKeyframes();
      const opacity = frames.filter(frame => frame.opacity !== undefined).map(frame => Number(frame.opacity));
      // Finish only a fade IN. Exit slides, spinners and unrelated transitions
      // must retain their provider-controlled timing and completion semantics.
      if (opacity.length < 2 || opacity[0] !== 0 || opacity.at(-1) !== 1) continue;
      try { animation.finish(); } catch { /* detached/replaced animation */ }
    }
  }
  const visible = (el) => {
    finishBackgroundEntryAnimations();
    if (!el || el.disabled || el.getAttribute("aria-disabled") === "true" ||
        el.closest('[hidden],[aria-hidden="true"],.slick-slide:not(.slick-active),.owl-item:not(.active),.fangate-slider-content.upcomming-slide,.fangate-slider-content.move-left')) return false;
    if (!el.getClientRects().length) return false;
    for (let node = el; node && node.nodeType === 1; node = node.parentElement) {
      const style = getComputedStyle(node);
      if (style.visibility === "hidden" || style.display === "none" || style.opacity === "0") return false;
    }
    const rect = el.getBoundingClientRect();
    // Carousel slides can have offsetParent while translated outside viewport.
    return rect.right > 0 && rect.left < innerWidth;
  };

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
  const evidence = new Map();
  const state = { status: "starting", host: location.hostname, pending: null, log };
  const api = window.udlGate = {
    state,
    isPaused() { return paused; },
    pause() { beginPause("paused"); note("paused; call resume() when finished"); },
    resume() {
      if (aborted) return false;
      if (captchaPending()) { beginPause("needs-captcha"); return false; }
      if (paused) pausedMs += Date.now() - pauseStarted;
      paused = false;
      state.status = "running";
      note("resumed explicitly");
      return true;
    },
    stop() { aborted = true; state.status = "aborted"; removeEventListener("keydown", onKeyDown); hud.remove(); },
    // Supply the exact profile URL plus a description of observed success.
    // Caller must first perform and verify the actual follow in the profile tab.
    confirmFollow(url, observed) { api.confirmAction(url, "follow", observed); },
    confirmAction(url, action, observed) {
      if (!url || !action || !observed || !String(observed).trim()) throw new Error("Observed action evidence required");
      evidence.set(action + " " + new URL(url, location.href).href, String(observed));
    },
    configure(values) { Object.assign(CONFIG, values); },
  };
  function beginPause(status) {
    if (!paused) pauseStarted = Date.now();
    paused = true;
    state.status = status;
  }
  function captchaPending() {
    const providers = [
      { widget: '.g-recaptcha,[data-sitekey][data-callback],iframe[src*="recaptcha"][src*="anchor"]', response: '[name="g-recaptcha-response"]' },
      { widget: '.h-captcha,iframe[src*="hcaptcha.com"]', response: '[name="h-captcha-response"]' },
    ];
    return providers.some(provider => {
      const shown = [...document.querySelectorAll(provider.widget)].some(visible);
      const solved = [...document.querySelectorAll(provider.response)].some(el => el.value?.trim());
      return shown && !solved;
    });
  }
  function canProceed() {
    if (paused || aborted) return false;
    if (captchaPending()) {
      beginPause("needs-captcha");
      note("CAPTCHA visible; paused until explicit resume()");
      return false;
    }
    return true;
  }
  function followKey(open) {
    return open.dataset.url || open.href || open.dataset.open || open.dataset.h || "";
  }
  function hasFollowEvidence(open) {
    const key = followKey(open);
    return key && evidence.has("follow " + new URL(key, location.href).href);
  }
  /** Click an element at most once, and only while it is actually clickable. */
  async function clickOnce(el, why) {
    if (!el || aborted || !canProceed() || clicked.has(el) || !visible(el)) return false;
    clicked.add(el);
    note("click: " + why);
    el.scrollIntoView({ block: "center" });
    // Public gate social handlers call window.open synchronously. In quiet
    // mode record the destination for an external background-tab driver while
    // preserving the provider's normal click bookkeeping. Never claim success.
    const originalOpen = window.open;
    if (CONFIG.backgroundOnly) window.open = (url) => {
      state.openRequests = [...(state.openRequests || []), String(url)];
      return { closed: false, focus() {}, close() {} };
    };
    try { el.click(); } finally { window.open = originalOpen; }
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
    if (aborted) return;
    log.push(line);
    hud.textContent = "UDL gate autopilot — Esc aborts\n" + log.slice(-9).join("\n");
    if (!hud.isConnected && document.body) document.body.appendChild(hud);
    console.log("[udl-gate]", line);
  }
  function onKeyDown(e) {
    if (e.key === "Escape") {
      api.stop();
    }
  }
  addEventListener("keydown", onKeyDown);

  /* ------------------------------ adapters ------------------------------ */

  // Never click these, on any host: they cost money, change settings, or
  // navigate away from the gate.
  const DENY = /add to cart|buy|purchase|checkout|wishlist|cookie|subscribe|sign ?up|log ?in|sign ?in|create gate|charts|home|terms|dmca|privacy/i;
  const ADVANCE = /^(free download|download|continue|next|unlock|get (it|the file)|claim|submit|save)/i;
  const SOCIAL_CONFIRM = /i (have )?followed|confirm|verify|done/i;
  const fileLink = (el) => el && el.tagName === "A" &&
    (el.hasAttribute("download") || /\.(wav|mp3|flac|aiff?|m4a|zip)(?:[?#]|$)/i.test(el.href));
  function currentHypedditSlide() {
    const slides = [...document.querySelectorAll(".fangate-slider-content")].filter(visible);
    return slides.find(el => el.classList.contains('current-slide')) || slides[0] || null;
  }
  // Exposed read-only helpers let a driver inspect the current rendered step.
  api.inspect = () => ({ ...state, currentSlide: currentHypedditSlide()?.className || null });

  /** Clickable controls inside `root`, in DOM order, that look like "advance". */
  function advanceCandidates(root) {
    return Array.prototype.slice
      .call(root.querySelectorAll('button,a[href],a[role="button"],[data-go]'))
      .filter(visible)
      .filter(function (el) { return !clicked.has(el); })
      .filter(function (el) {
        const t = textOf(el);
        if (DENY.test(t) || SOCIAL_CONFIRM.test(t)) return false;
        return ADVANCE.test(t) || el.hasAttribute("data-go");
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
          if (!canProceed()) { await sleep(CONFIG.tickMs); continue; }
          if (download && visible(download) && download.classList.contains("ready")) {
            await clickOnce(download, "gaterush download");
            return "download-requested";
          }

          const email = stage.querySelector("#emailInput");
          if (email && visible(email)) {
            if (!email.value && !CONFIG.email) { state.status = "needs-email"; await sleep(CONFIG.tickMs); continue; }
            setInput(stage.querySelector("#nameInput"), CONFIG.name);
            if (!email.value) setInput(email, CONFIG.email);
            await clickOnce(stage.querySelector("[data-go]"), "email step");
            await sleep(CONFIG.tickMs);
            continue;
          }

          const comment = stage.querySelector("#commentInput");
          if (comment && !comment.value) setInput(comment, CONFIG.comment);

          // Opening a profile is not evidence that its follow succeeded.
          const pair = [...stage.querySelectorAll(".follow-pair")]
            .find(el => visible(el) && !el.querySelector("[data-confirm].done"));
          const listButton = stage.querySelector(".list-btn:not(.done)");
          const open = pair ? pair.querySelector("[data-open]") : listButton;
          if (open) {
            // Gaterush adds aria-label when a list button becomes confirmation.
            // On reinjection it may already be armed: never click that as "open".
            if (!listButton || pair || !listButton.hasAttribute("aria-label")) {
              await clickOnce(open, "open " + (open.dataset.h || "profile"));
            }
            state.pending = { kind: "follow", profile: followKey(open) };
            if (!hasFollowEvidence(open)) {
              state.status = "needs-follow";
              await sleep(CONFIG.tickMs);
              continue;
            }
            const confirm = await waitFor(() => {
              const c = pair ? pair.querySelector("[data-confirm]") : listButton;
              return visible(c) ? c : null;
            }, 20000, "confirm to arm");
            // List layout reuses the opened button as its confirmation control.
            if (confirm === listButton) clicked.delete(confirm);
            await clickOnce(confirm, "confirm verified follow");
            state.pending = null;
            state.status = "running";
            continue;
          }

          const go = stage.querySelector("[data-go]");
          if (go && visible(go) && !clicked.has(go)) {
            const isOauth = go.classList.contains("btn-soundcloud");
            if (isOauth && CONFIG.backgroundOnly) {
              state.status = "needs-oauth";
              state.pending = { kind: "oauth", label: textOf(go) };
              await sleep(CONFIG.tickMs);
              continue;
            }
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
          if (!canProceed()) { await sleep(CONFIG.tickMs); continue; }
          const slide = currentHypedditSlide();
          const dl = document.getElementById("gateDownloadButton");
          if (visible(dl) && (!slide || slide.contains(dl))) {
            await clickOnce(dl, "hypeddit download");
            return "download-requested";
          }

          const email = slide && slide.querySelector('#email_address,input[type="email"],input[name="email"]');
          if (email && visible(email)) {
            if (!email.value && !CONFIG.email) { state.status = "needs-email"; await sleep(CONFIG.tickMs); continue; }
            if (!email.value) setInput(email, CONFIG.email);
            const name = slide.querySelector("#email_name");
            if (name && !name.value && !CONFIG.name) { state.status = "needs-name"; await sleep(CONFIG.tickMs); continue; }
            if (name && !name.value) setInput(name, CONFIG.name);
            await clickOnce(slide.querySelector("#email_to_downloads_next"), "email step");
            await sleep(CONFIG.tickMs);
            continue;
          }

          // Current Hypeddit gates often use no-API SoundCloud links. Clicking
          // them only opens SoundCloud, so wait for each actual action's evidence.
          const social = slide ? [...slide.querySelectorAll("a[data-url]")]
            .filter(el => visible(el) && (el.dataset.step || /instagram/.test(el.className))) : [];
          const pending = social.find(el => !evidence.has(
            (el.dataset.step || "follow") + " " + new URL(el.dataset.url, location.href).href));
          if (pending) {
            await clickOnce(pending, "open " + (pending.dataset.step || "follow") + " action");
            state.status = "needs-social-action";
            state.pending = { kind: pending.dataset.step || "follow", profile: pending.dataset.url };
            await sleep(CONFIG.tickMs);
            continue;
          }
          // Marked-as-opened is a page condition, not our evidence of completion.
          for (const control of social) await clickOnce(control, "open verified action");
          state.pending = null;
          const step = Array.prototype.slice
            .call(slide ? slide.querySelectorAll('[class*="step_button_"],button,a.hype-btn') : [])
            .find(function (el) {
              return visible(el) && !clicked.has(el) && !DENY.test(textOf(el)) && !el.dataset.url;
            });
          if (step) {
            if (SOCIAL_CONFIRM.test(textOf(step)) && social.length === 0) {
              state.status = "needs-follow";
              state.pending = { kind: "follow", step: textOf(step) };
              await sleep(CONFIG.tickMs);
              continue;
            }
            const oauth = /soundcloud|spotify|youtube/i.test(step.className);
            if (oauth && CONFIG.backgroundOnly) {
              state.status = "needs-oauth";
              state.pending = { kind: "oauth", label: textOf(step) };
              await sleep(CONFIG.tickMs);
              continue;
            }
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

    "droploud.com": {
      detect: () => document.querySelector(".dtr-root,.ds-free-dl"),
      run: droploudGate,
    },
    "mypresskit.info": {
      detect: function () { return /\/gate\//.test(location.pathname); },
      run: myPressKitGate,
    },
  };

  // Next embeds exact public gate data in Flight string chunks. Read data only;
  // never evaluate scripts or infer account URLs from button display names.
  function flightData(key) {
    const chunks = (window.__next_f || []).filter(x => typeof x[1] === "string").map(x => x[1]);
    for (const script of document.scripts) {
      const match = script.textContent.match(/self\.__next_f\.push\((\[.*\])\)/s);
      if (match) { try { const row = JSON.parse(match[1]); if (typeof row[1] === "string") chunks.push(row[1]); } catch {} }
    }
    const text = chunks.join("");
    const marker = JSON.stringify(key) + ":";
    const offset = text.indexOf(marker);
    if (offset < 0) return null;
    const start = text.indexOf("{", offset + marker.length);
    let depth = 0, quoted = false, escaped = false;
    for (let i = start; i < text.length; i++) {
      const ch = text[i];
      if (quoted) { if (escaped) escaped = false; else if (ch === "\\") escaped = true; else if (ch === '"') quoted = false; continue; }
      if (ch === '"') quoted = true;
      else if (ch === "{") depth++;
      else if (ch === "}" && --depth === 0) { try { return JSON.parse(text.slice(start, i + 1)); } catch { return null; } }
    }
    return null;
  }

  async function droploudGate() {
    while (!aborted && budgetLeft() > 0) {
      if (!canProceed()) { await sleep(CONFIG.tickMs); continue; }
      const pane = document.querySelector(".dtr-stage.is-vis .dtr-card-pane");
      if (!pane) { await sleep(CONFIG.tickMs); continue; }
      const heading = textOf(pane.querySelector(".dtr-card-title") || pane);
      const entry = pane.querySelector(".ds-free-dl");
      if (visible(entry)) {
        const final = /drop unlocked/.test(heading);
        await clickOnce(entry, final ? "droploud final download" : "open droploud gate");
        if (final) return "download-requested";
        await sleep(CONFIG.tickMs);
        continue;
      }
      const data = flightData("gateData");
      const step = data?.steps?.find(x => x.label.trim().toLowerCase() === heading);
      const buttons = [...pane.querySelectorAll(".dtr-open-grid button")];
      if (step && buttons.length && !step.type.startsWith("soundcloud_")) {
        const urls = step.urls?.length ? step.urls : step.url ? [step.url] : [];
        const action = step.type.split("_").slice(1).join("_");
        if (urls.length !== buttons.length) {
          state.status = "needs-interaction";
          state.pending = { kind: "unmatched-provider-controls", step: step.id };
        } else {
          const pending = urls.filter(url => !evidence.has(action + " " + new URL(url, location.href).href));
          state.status = pending.length ? "needs-social-action" : "running";
          state.pending = pending.length ? { kind: action, profile: pending[0], profiles: pending, step: step.id } : null;
          // Normal handlers arm the confirmation only after every link is clicked.
          // In background mode they queue destinations instead of creating popups.
          for (const button of buttons) await clickOnce(button, "open droploud social link");
          if (!pending.length) await clickOnce(pane.querySelector(".dtr-confirm-btn"), "confirm verified droploud actions");
        }
      } else {
        state.status = step?.type.startsWith("soundcloud_") ? "needs-oauth" : "needs-interaction";
        state.pending = { kind: state.status, step: step?.id || null, label: heading,
          requirements: data?.steps?.filter(x => x.type.startsWith("soundcloud_")) || [] };
      }
      await sleep(CONFIG.tickMs);
    }
    return "stalled";
  }

  async function myPressKitGate() {
    while (!aborted && budgetLeft() > 0) {
      if (!canProceed()) { await sleep(CONFIG.tickMs); continue; }
      const gate = flightData("gate");
      const controls = [...document.querySelectorAll("main button")].filter(visible);
      const download = controls.find(el => textOf(el) === "download");
      if (download) {
        await clickOnce(download, "mypresskit final download");
        return "download-requested";
      }
      if (!gate?.id || !Array.isArray(gate.gateSteps)) {
        state.status = "needs-interaction";
        state.pending = { kind: "missing-public-gate-data" };
        await sleep(CONFIG.tickMs);
        continue;
      }
      const combo = controls.find(el => /^(follow.*|support) on soundcloud$/.test(textOf(el)));
      if (combo) {
        const comment = document.querySelector('textarea[placeholder="Write your comment…"]');
        if (comment && !comment.value.trim()) setInput(comment, CONFIG.comment);
        if (comment && !comment.value.trim()) {
          state.status = "needs-comment";
          state.pending = { kind: "comment", profile: gate.trackUrl };
        } else {
          const url = new URL("/api/download-gates/oauth/soundcloud/start", location.origin);
          url.searchParams.set("gate", gate.id);
          url.searchParams.set("combo", "1");
          if (comment) url.searchParams.set("comment", comment.value.trim());
          state.status = "needs-oauth";
          // Normal provider fallback omits popup=1 and works in an owned tab.
          state.pending = { kind: "oauth", profile: url.href,
            requirements: gate.gateSteps.filter(step => step.type.startsWith("soundcloud-")) };
          if (!CONFIG.backgroundOnly) await clickOnce(combo, "mypresskit SoundCloud OAuth");
        }
        await sleep(CONFIG.tickMs);
        continue;
      }
      const instagram = controls.find(el => textOf(el) === "follow on instagram");
      if (instagram) {
        const steps = gate.gateSteps.filter(step => step.type === "instagram-follow");
        // Multiple indistinguishable controls need provider-specific mapping.
        if (steps.length !== 1 || !steps[0].targetUrl) {
          state.status = "needs-interaction";
          state.pending = { kind: "unmatched-instagram-control" };
        } else {
          const url = new URL(steps[0].targetUrl, location.href).href;
          if (!evidence.has("follow " + url)) {
            state.status = "needs-social-action";
            state.pending = { kind: "follow", profile: url, profiles: [url] };
          } else {
            // This click records completion immediately: only allow AFTER proof.
            await clickOnce(instagram, "confirm verified mypresskit Instagram follow");
            state.status = "running";
            state.pending = null;
          }
        }
      } else {
        state.status = "needs-interaction";
        state.pending = { kind: "waiting-for-provider-verification" };
      }
      await sleep(CONFIG.tickMs);
    }
    return "stalled";
  }

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
      if (!canProceed()) { await sleep(CONFIG.tickMs); continue; }
      const emails = Array.prototype.slice
        .call(document.querySelectorAll('input[type="email"]'))
        .filter(visible);
      if (emails.some(el => !el.value) && !CONFIG.email) {
        state.status = "needs-email";
        await sleep(CONFIG.tickMs);
        continue;
      }
      for (const el of emails) if (!el.value) setInput(el, CONFIG.email);

      // Unknown gates may label their confirmation "Next". If social controls
      // are still present, do not infer completion from that generic wording.
      const social = [...document.querySelectorAll('button,a[role="button"],a[href]')]
        .find(el => visible(el) && /\bfollow(?:ed)?\b|\brepost\b|\blike\b|\bcomment\b/i.test(textOf(el)));
      if (social) {
        state.status = "needs-social-action";
        state.pending = { kind: "inspect-provider-step", label: textOf(social) };
        await sleep(CONFIG.tickMs);
        continue;
      }
      const next = advanceCandidates(document)[0];
      if (next) {
        idleTicks = 0;
        const label = textOf(next).slice(0, 40) || "(unlabelled)";
        await clickOnce(next, label);
        if (fileLink(next)) return "download-requested";
        continue;
      }

      if (++idleTicks === 12) {
        state.status = "needs-interaction";
        note("waiting for login, social action or page update");
      }
      await sleep(CONFIG.tickMs);
    }
    return "stalled";
  }

  /* -------------------------------- main -------------------------------- */

  (async function main() {
    const host = location.hostname.replace(/^www\./, "");
    const adapter = adapters[host];
    if (!adapter) { state.status = "unsupported-host"; return; }
    // Only engage on an actual gate page, not the site's other pages.
    if (!(await waitFor(adapter.detect, 8000))) { state.status = "not-a-gate"; return; }

    state.status = "running";
    note("gate detected on " + host);
    let outcome;
    try {
      outcome = await adapter.run();
    } catch (e) {
      note("error: " + (e && e.message));
      outcome = "error";
    }
    state.status = aborted ? "aborted" : outcome;
    note("finished: " + state.status);
  })();
})();
