/* udl shell — sidebar navigation, toasts, modals, inspector + drawer wiring.
   Every view page calls Shell.init({page, contextual}). */
(function (global) {
  const NAV = [
    {
      title: "Workflows",
      items: [
        { id: "home", label: "Home", href: "home.html", g: "⌂" },
        { id: "sync", label: "Run Sync", href: "sync-plan.html", g: "⇣", badge: "26" },
        { id: "freedl", label: "SoundCloud Free DL", href: "freedl.html", g: "✦", badge: "3" },
        { id: "rekordbox", label: "Rekordbox Sync", href: "rekordbox.html", g: "▤", badge: "2", kind: "warn" },
        { id: "playlists", label: "Playlists", href: "playlists.html", g: "♫", badge: "4" },
      ],
    },
  ];
  const SYSTEM = {
    title: "System",
    items: [
      { id: "doctor", label: "Check System", href: "doctor.html", g: "✚", badge: "2", kind: "warn" },
      { id: "credentials", label: "Credentials", href: "credentials.html", g: "⚿", badge: "1", kind: "err" },
      { id: "config", label: "Advanced Config", href: "config.html", g: "⚙" },
      { id: "settings", label: "Settings", href: "settings.html", g: "◉" },
    ],
  };

  const ALIAS = { "sync-plan": "sync", "sync-run": "sync" };

  function itemHTML(it, active) {
    const badge = it.badge ? `<span class="badge ${it.kind || ""}">${it.badge}</span>` : "<span></span>";
    return `<a class="sb-item ${active === it.id ? "on" : ""}" href="${it.href}">
      <span class="g">${it.g}</span><span class="nm">${it.label}</span>${badge}</a>`;
  }

  const Shell = {
    /* opts: {page, contextual:{title, html}, note} */
    init(opts) {
      const active = ALIAS[opts.page] || opts.page;
      const sb = document.getElementById("sidebar");
      if (sb) {
        let html = NAV.map(
          (sec) => `<div class="sb-head">${sec.title}</div>` + sec.items.map((i) => itemHTML(i, active)).join("")
        ).join("");
        if (opts.contextual) {
          html += `<div class="sb-head">${opts.contextual.title}</div><div id="sbContext">${opts.contextual.html || ""}</div>`;
        }
        html += `<div class="sb-foot"><div class="sb-head">${SYSTEM.title}</div>` +
          SYSTEM.items.map((i) => itemHTML(i, active)).join("") +
          (opts.note ? `<div class="sb-note">${opts.note}</div>` : "") +
          `</div>`;
        sb.innerHTML = html;
      }

      // toolbar: inspector toggle
      const insBtn = document.querySelector("[data-inspector-toggle]");
      const ins = document.querySelector(".inspector");
      if (insBtn && ins) {
        insBtn.addEventListener("click", () => {
          ins.classList.toggle("hidden");
          insBtn.classList.toggle("on");
        });
      }
      // toolbar: sidebar toggle
      const sbBtn = document.querySelector("[data-sidebar-toggle]");
      if (sbBtn && sb) sbBtn.addEventListener("click", () => sb.classList.toggle("hidden"));

      // swift mapping drawer
      const drawer = document.getElementById("drawer");
      const dBtn = document.querySelector("[data-drawer-open]");
      if (drawer && dBtn) {
        dBtn.addEventListener("click", () => drawer.classList.add("open"));
        drawer.querySelector("[data-drawer-close]")?.addEventListener("click", () => drawer.classList.remove("open"));
      }

      // generic switches: <div class="switch" role="switch" data-toggle>
      document.querySelectorAll(".switch[data-toggle]").forEach((sw) => {
        sw.addEventListener("click", () => {
          const next = sw.getAttribute("aria-checked") !== "true";
          sw.setAttribute("aria-checked", next);
          sw.dispatchEvent(new CustomEvent("switched", { detail: next, bubbles: true }));
        });
      });

      // generic segmented controls: <div class="seg" data-seg>
      document.querySelectorAll(".seg[data-seg]").forEach((seg) => {
        seg.addEventListener("click", (e) => {
          const b = e.target.closest("button");
          if (!b) return;
          [...seg.children].forEach((x) => x.setAttribute("aria-pressed", x === b));
          seg.dispatchEvent(new CustomEvent("picked", { detail: b.dataset.v, bubbles: true }));
        });
      });

      document.addEventListener("keydown", (e) => {
        if (e.key === "Escape") {
          document.querySelectorAll(".backdrop.open").forEach((b) => b.classList.remove("open"));
          drawer?.classList.remove("open");
        }
      });
    },

    toast(html, ms) {
      let t = document.getElementById("toast");
      if (!t) {
        t = document.createElement("div");
        t.id = "toast";
        t.className = "toast";
        document.body.appendChild(t);
      }
      t.innerHTML = html;
      t.classList.add("show");
      clearTimeout(Shell._tid);
      Shell._tid = setTimeout(() => t.classList.remove("show"), ms || 2800);
    },

    open(id) { document.getElementById(id)?.classList.add("open"); },
    close(id) { document.getElementById(id)?.classList.remove("open"); },

    /* wire every [data-close] inside a backdrop to dismiss it */
    wireDismiss() {
      document.querySelectorAll(".backdrop").forEach((bd) => {
        bd.addEventListener("click", (e) => {
          if (e.target === bd || e.target.closest("[data-close]")) bd.classList.remove("open");
        });
      });
    },
  };

  global.Shell = Shell;
})(window);
