/* Shared mock data for the udl GUI design concepts.
   Mirrors internal/engine.PlanRow + planSourceDetails so the mockups
   exercise the real shape of the data, not invented fields.

   PlanRow.Status ∈ { already_downloaded, missing_new, missing_known_gap }
   Toggleable is false for already_downloaded rows.
*/
(function (global) {
  const NEW = "missing_new";
  const GAP = "missing_known_gap";
  const HAVE = "already_downloaded";

  function row(i, title, artist, status, remoteId, dur, added) {
    return {
      index: i,
      title: title,
      artist: artist,
      status: status,
      remoteId: remoteId,
      remoteURL:
        String(remoteId).length > 9
          ? "https://api.soundcloud.com/tracks/" + remoteId
          : "https://open.spotify.com/track/" + remoteId,
      duration: dur,
      addedAt: added,
      toggleable: status !== HAVE,
      selectedByDefault: status === NEW,
    };
  }

  const sources = [
    {
      id: "sc-favorites",
      label: "SoundCloud Likes",
      type: "soundcloud",
      adapter: "scdl",
      url: "https://soundcloud.com/jaa/likes",
      targetDir: "~/Music/SoundCloud/Favorites",
      stateFile: "~/.local/state/udl/sc-favorites.json",
      supportsWindow: false,
      enabled: true,
    },
    {
      id: "sc-dnb-crate",
      label: "DnB Crate",
      type: "soundcloud",
      adapter: "scdl",
      url: "https://soundcloud.com/jaa/sets/dnb-crate",
      targetDir: "~/Music/SoundCloud/DnB Crate",
      stateFile: "~/.local/state/udl/sc-dnb-crate.json",
      supportsWindow: false,
      enabled: true,
    },
    {
      id: "sp-liked",
      label: "Spotify Liked Songs",
      type: "spotify",
      adapter: "deemix",
      url: "https://open.spotify.com/collection/tracks",
      targetDir: "~/Music/Spotify/Liked",
      stateFile: "~/.local/state/udl/sp-liked.json",
      supportsWindow: true,
      enabled: true,
    },
    {
      id: "sp-warmup",
      label: "Warmup Set",
      type: "spotify",
      adapter: "deemix",
      url: "https://open.spotify.com/playlist/3xTQ8bR2vKcMd1nQ",
      targetDir: "~/Music/Spotify/Warmup",
      stateFile: "~/.local/state/udl/sp-warmup.json",
      supportsWindow: true,
      enabled: false,
    },
  ];

  const rows = {
    "sc-favorites": [
      row(1, "Troglodyte (VIP)", "Culture Shock", NEW, "1874553921", "5:12", "2h ago"),
      row(2, "Air I Breathe", "Sub Focus & Wilkinson", NEW, "1874120884", "3:48", "5h ago"),
      row(3, "Aces", "Halogenix", GAP, "1802119043", "4:26", "3d ago"),
      row(4, "Melodies (2024 Remaster)", "Alix Perez", HAVE, "1799001233", "6:03", "4d ago"),
      row(5, "Sleepless Nights", "Monrroe", NEW, "1873998210", "4:11", "6h ago"),
      row(6, "Dead Limit", "Noisia & The Upbeats", HAVE, "1651220984", "5:38", "1w ago"),
      row(7, "Voyager", "Kanine", NEW, "1873441220", "3:59", "9h ago"),
      row(8, "Feel It (Bou Remix)", "Bou", NEW, "1872210044", "4:33", "11h ago"),
      row(9, "Nowhere Fast", "Skeptical", GAP, "1790032118", "5:20", "5d ago"),
      row(10, "Wobbler", "Break", HAVE, "1602233910", "4:47", "2w ago"),
      row(11, "Solitude", "Hybrid Minds", NEW, "1871009552", "4:02", "14h ago"),
      row(12, "Departure Lounge", "LSB", NEW, "1870551204", "6:41", "1d ago"),
      row(13, "Circles", "Workforce", HAVE, "1588102394", "5:09", "3w ago"),
      row(14, "Kites", "Etherwood", NEW, "1869940037", "4:55", "1d ago"),
      row(15, "Iron Galaxy", "Dossa & Locuzzed", NEW, "1869112288", "3:41", "2d ago"),
      row(16, "Blue Hour", "Ownglow", GAP, "1755400112", "4:18", "1w ago"),
      row(17, "Trust In Me", "Serum & Bladerunner", HAVE, "1544029183", "5:52", "1mo ago"),
      row(18, "Last Train Home", "Technimatic", NEW, "1868441093", "6:12", "2d ago"),
    ],
    "sc-dnb-crate": [
      row(1, "Pointe Blank", "Enei", NEW, "1873001234", "5:01", "1d ago"),
      row(2, "Overdrive", "Mefjus", NEW, "1872884410", "4:22", "1d ago"),
      row(3, "Skanka", "DLR & Mako", HAVE, "1610334422", "5:44", "2w ago"),
      row(4, "Gemini", "Klax", NEW, "1871220983", "4:09", "3d ago"),
      row(5, "Rain Dance", "Ivy Lab", GAP, "1745009911", "3:57", "1w ago"),
      row(6, "Redshift", "Buunshin", NEW, "1870001199", "4:44", "3d ago"),
      row(7, "Nightfall", "Signal", HAVE, "1599220477", "5:15", "3w ago"),
      row(8, "Elastic", "Molecular", NEW, "1869554021", "4:31", "4d ago"),
    ],
    "sp-liked": [
      row(1, "Midnight City", "M83", NEW, "3gVhsZ5HlV1", "4:03", "2026-07-29"),
      row(2, "Windowlicker", "Aphex Twin", NEW, "5kQb4Xz2Pm0", "6:07", "2026-07-29"),
      row(3, "Innerbloom", "RÜFÜS DU SOL", HAVE, "2nLtzopw4rP", "9:38", "2026-07-24"),
      row(4, "Opus", "Eric Prydz", NEW, "6vX2mQpLd8T", "9:04", "2026-07-28"),
      row(5, "Teardrop", "Massive Attack", HAVE, "67Hna0dtVSj", "5:29", "2026-07-20"),
      row(6, "Kerala", "Bonobo", NEW, "1nT4ZQnEpx9", "3:54", "2026-07-27"),
      row(7, "Avril 14th", "Aphex Twin", GAP, "0yPTPnQ92kL", "2:05", "2026-07-19"),
      row(8, "Xtal", "Aphex Twin", NEW, "4qVpZ3nRmDs", "4:51", "2026-07-27"),
      row(9, "Nightcall", "Kavinsky", NEW, "0U0ldCRmgCq", "4:18", "2026-07-26"),
      row(10, "Emerald Rush", "Jon Hopkins", HAVE, "3sQ0mnRpLKc", "6:06", "2026-07-15"),
      row(11, "Digital Love", "Daft Punk", NEW, "2VEZx7NWsZ1", "4:58", "2026-07-25"),
      row(12, "An Ending (Ascent)", "Brian Eno", GAP, "7HZ0mQ2sVKm", "4:24", "2026-07-11"),
    ],
    "sp-warmup": [
      row(1, "Sun Models", "ODESZA", NEW, "5aVpQ2mLd0X", "4:32", "2026-07-30"),
      row(2, "Coastline", "Hollow Coves", NEW, "1kQ9mZpR3Ts", "3:47", "2026-07-30"),
      row(3, "Little Bit", "Lykke Li", HAVE, "6nX0qLpZ2Vm", "4:15", "2026-07-22"),
      row(4, "Sunset Lover", "Petit Biscuit", NEW, "0pLm2QvZ9Rd", "3:57", "2026-07-29"),
      row(5, "Golden Hour", "Kygo", GAP, "4mVpQ0nZLd2", "3:31", "2026-07-18"),
      row(6, "Ocean Drive", "Duke Dumont", NEW, "2sQpLm9ZVd0", "4:02", "2026-07-28"),
      row(7, "Weightless", "Marconi Union", HAVE, "3nZpQ2mLdV0", "8:09", "2026-07-12"),
      row(8, "Cola", "CamelPhat", NEW, "7VpQ0nZmLd2", "6:31", "2026-07-27"),
      row(9, "Breathe", "Télépopmusik", NEW, "1ZpQ2mLdV0n", "4:41", "2026-07-26"),
      row(10, "Silhouette", "Aquilo", GAP, "5QpLm2ZVd0n", "4:12", "2026-07-14"),
    ],
  };

  const statusMeta = {
    missing_new: { label: "New", short: "new", hint: "Not in state file, not on disk" },
    missing_known_gap: {
      label: "Known gap",
      short: "gap",
      hint: "Previously seen but never downloaded successfully",
    },
    already_downloaded: {
      label: "Already have",
      short: "have",
      hint: "Recorded in state file — locked, cannot be selected",
    },
  };

  function counts(list) {
    return list.reduce(
      function (acc, r) {
        acc[r.status] = (acc[r.status] || 0) + 1;
        return acc;
      },
      { missing_new: 0, missing_known_gap: 0, already_downloaded: 0 }
    );
  }

  global.UDL = {
    sources: sources,
    rows: rows,
    statusMeta: statusMeta,
    counts: counts,
    STATUS: { NEW: NEW, GAP: GAP, HAVE: HAVE },
  };
})(window);
