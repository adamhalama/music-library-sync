/* Mock data shaped after the real udl types:
   config.Source, engine.PlanRow, doctor.Check, freedl.Job,
   playlists.Snapshot/Track, rekordbox/playlistsync.Plan/PlanRow. */
(function (g) {
  const NEW = "missing_new", GAP = "missing_known_gap", HAVE = "already_downloaded";

  function pr(i, title, artist, status, id, dur, added) {
    return { index: i, title, artist, status, remoteId: id, duration: dur, addedAt: added,
      toggleable: status !== HAVE, selectedByDefault: status === NEW,
      remoteURL: String(id).length > 9 ? "https://api.soundcloud.com/tracks/" + id : "https://open.spotify.com/track/" + id };
  }

  const sources = [
    { id:"soundcloud-likes", label:"SoundCloud Likes", type:"soundcloud", adapter:"scdl", enabled:true,
      url:"https://soundcloud.com/janxadam/likes", targetDir:"~/Music/downloaded/sc-likes",
      stateFile:"soundcloud-likes.sync.scdl", supportsWindow:false,
      sync:{ breakOnExisting:true, askOnExisting:true, localIndexCache:false }, extraArgs:["-f"] },
    { id:"sc-dnb-crate", label:"DnB Crate", type:"soundcloud", adapter:"scdl", enabled:true,
      url:"https://soundcloud.com/janxadam/sets/dnb-crate", targetDir:"~/Music/downloaded/dnb-crate",
      stateFile:"sc-dnb-crate.sync.scdl", supportsWindow:false,
      sync:{ breakOnExisting:true, askOnExisting:false, localIndexCache:true }, extraArgs:[] },
    { id:"spotify-liked", label:"Spotify Liked Songs", type:"spotify", adapter:"deemix", enabled:true,
      url:"https://open.spotify.com/collection/tracks", targetDir:"~/Music/downloaded/spotify-liked",
      stateFile:"spotify-liked.sync.deemix", supportsWindow:true,
      sync:{ breakOnExisting:false, askOnExisting:false, localIndexCache:true }, extraArgs:[] },
    { id:"spotify-warmup", label:"Warmup Set", type:"spotify", adapter:"deemix", enabled:false,
      url:"https://open.spotify.com/playlist/3xTQ8bR2vKcMd1nQ", targetDir:"~/Music/downloaded/warmup",
      stateFile:"spotify-warmup.sync.deemix", supportsWindow:true,
      sync:{ breakOnExisting:false, askOnExisting:false, localIndexCache:false }, extraArgs:[] },
  ];

  const rows = {
    "soundcloud-likes":[
      pr(1,"Troglodyte (VIP)","Culture Shock",NEW,"1874553921","5:12","2h ago"),
      pr(2,"Air I Breathe","Sub Focus & Wilkinson",NEW,"1874120884","3:48","5h ago"),
      pr(3,"Aces","Halogenix",GAP,"1802119043","4:26","3d ago"),
      pr(4,"Melodies (2024 Remaster)","Alix Perez",HAVE,"1799001233","6:03","4d ago"),
      pr(5,"Sleepless Nights","Monrroe",NEW,"1873998210","4:11","6h ago"),
      pr(6,"Dead Limit","Noisia & The Upbeats",HAVE,"1651220984","5:38","1w ago"),
      pr(7,"Voyager","Kanine",NEW,"1873441220","3:59","9h ago"),
      pr(8,"Feel It (Bou Remix)","Bou",NEW,"1872210044","4:33","11h ago"),
      pr(9,"Nowhere Fast","Skeptical",GAP,"1790032118","5:20","5d ago"),
      pr(10,"Wobbler","Break",HAVE,"1602233910","4:47","2w ago"),
      pr(11,"Solitude","Hybrid Minds",NEW,"1871009552","4:02","14h ago"),
      pr(12,"Departure Lounge","LSB",NEW,"1870551204","6:41","1d ago"),
      pr(13,"Circles","Workforce",HAVE,"1588102394","5:09","3w ago"),
      pr(14,"Kites","Etherwood",NEW,"1869940037","4:55","1d ago"),
      pr(15,"Iron Galaxy","Dossa & Locuzzed",NEW,"1869112288","3:41","2d ago"),
      pr(16,"Blue Hour","Ownglow",GAP,"1755400112","4:18","1w ago"),
      pr(17,"Trust In Me","Serum & Bladerunner",HAVE,"1544029183","5:52","1mo ago"),
      pr(18,"Last Train Home","Technimatic",NEW,"1868441093","6:12","2d ago"),
    ],
    "sc-dnb-crate":[
      pr(1,"Pointe Blank","Enei",NEW,"1873001234","5:01","1d ago"),
      pr(2,"Overdrive","Mefjus",NEW,"1872884410","4:22","1d ago"),
      pr(3,"Skanka","DLR & Mako",HAVE,"1610334422","5:44","2w ago"),
      pr(4,"Gemini","Klax",NEW,"1871220983","4:09","3d ago"),
      pr(5,"Rain Dance","Ivy Lab",GAP,"1745009911","3:57","1w ago"),
      pr(6,"Redshift","Buunshin",NEW,"1870001199","4:44","3d ago"),
      pr(7,"Nightfall","Signal",HAVE,"1599220477","5:15","3w ago"),
      pr(8,"Elastic","Molecular",NEW,"1869554021","4:31","4d ago"),
    ],
    "spotify-liked":[
      pr(1,"Midnight City","M83",NEW,"3gVhsZ5HlV1","4:03","2026-07-29"),
      pr(2,"Windowlicker","Aphex Twin",NEW,"5kQb4Xz2Pm0","6:07","2026-07-29"),
      pr(3,"Innerbloom","RÜFÜS DU SOL",HAVE,"2nLtzopw4rP","9:38","2026-07-24"),
      pr(4,"Opus","Eric Prydz",NEW,"6vX2mQpLd8T","9:04","2026-07-28"),
      pr(5,"Teardrop","Massive Attack",HAVE,"67Hna0dtVSj","5:29","2026-07-20"),
      pr(6,"Kerala","Bonobo",NEW,"1nT4ZQnEpx9","3:54","2026-07-27"),
      pr(7,"Avril 14th","Aphex Twin",GAP,"0yPTPnQ92kL","2:05","2026-07-19"),
      pr(8,"Xtal","Aphex Twin",NEW,"4qVpZ3nRmDs","4:51","2026-07-27"),
      pr(9,"Nightcall","Kavinsky",NEW,"0U0ldCRmgCq","4:18","2026-07-26"),
      pr(10,"Emerald Rush","Jon Hopkins",HAVE,"3sQ0mnRpLKc","6:06","2026-07-15"),
      pr(11,"Digital Love","Daft Punk",NEW,"2VEZx7NWsZ1","4:58","2026-07-25"),
      pr(12,"An Ending (Ascent)","Brian Eno",GAP,"7HZ0mQ2sVKm","4:24","2026-07-11"),
    ],
    "spotify-warmup":[
      pr(1,"Sun Models","ODESZA",NEW,"5aVpQ2mLd0X","4:32","2026-07-30"),
      pr(2,"Coastline","Hollow Coves",NEW,"1kQ9mZpR3Ts","3:47","2026-07-30"),
      pr(3,"Little Bit","Lykke Li",HAVE,"6nX0qLpZ2Vm","4:15","2026-07-22"),
      pr(4,"Sunset Lover","Petit Biscuit",NEW,"0pLm2QvZ9Rd","3:57","2026-07-29"),
      pr(5,"Golden Hour","Kygo",GAP,"4mVpQ0nZLd2","3:31","2026-07-18"),
    ],
  };

  const statusMeta = {
    missing_new:{label:"New",short:"new",hint:"Not in the state file and not on disk"},
    missing_known_gap:{label:"Known gap",short:"gap",hint:"Seen in an earlier run but never downloaded successfully"},
    already_downloaded:{label:"Already have",short:"have",hint:"Recorded in the state file — locked, cannot be selected"},
  };

  /* doctor.Check — severity + name + message */
  const checks = [
    {sev:"err",  name:"auth",       msg:"Deezer ARL is missing",                          detail:"Spotify + deemix sources cannot run without it. Stored in macOS Keychain.", fix:"credentials"},
    {sev:"warn", name:"dependency", msg:"scdl 2.7.1 is older than the tested 2.9.0",       detail:"Free DL quality detection may misreport bitrate on older scdl builds.", fix:"upgrade"},
    {sev:"warn", name:"rekordbox",  msg:"Rekordbox appears to be running",                 detail:"Planning is allowed, applying is refused while the database is open.", fix:"quit-rb"},
    {sev:"ok",   name:"config",     msg:"Config parsed and validated",                     detail:"~/.config/udl/config.yaml · 4 sources, 3 enabled"},
    {sev:"ok",   name:"dependency", msg:"yt-dlp 2026.06.11 is compatible",                 detail:"/opt/homebrew/bin/yt-dlp"},
    {sev:"ok",   name:"dependency", msg:"python@3.12 runtime ready",                       detail:"pyrekordbox 0.4.3 in udl's private environment"},
    {sev:"ok",   name:"auth",       msg:"SoundCloud client ID is available",               detail:"macOS Keychain"},
    {sev:"ok",   name:"auth",       msg:"Spotify app credentials are available",           detail:"macOS Keychain (user-owned app)"},
    {sev:"ok",   name:"filesystem", msg:"All 3 enabled target directories are writable",   detail:"~/Music/downloaded/*"},
    {sev:"ok",   name:"filesystem", msg:"State directory is writable",                     detail:"~/dev/music-down/statefiles"},
    {sev:"info", name:"filesystem", msg:"Warmup Set target directory will be created on first sync", detail:"~/Music/downloaded/warmup"},
    {sev:"info", name:"security",   msg:"No secrets found in config.yaml",                 detail:"All managed values live in macOS Keychain"},
  ];

  /* credentials board */
  const creds = [
    {id:"sc", label:"SoundCloud client ID", state:"available", store:"macOS Keychain",
     affects:["Run Sync (SoundCloud)","SoundCloud Free DL"], value:"a3e0…9f21", action:"Update", env:"SCDL_CLIENT_ID"},
    {id:"arl", label:"Deezer ARL", state:"missing", store:"—",
     affects:["Run Sync (Spotify + deemix)"], value:"", action:"Save", env:"UDL_DEEMIX_ARL"},
    {id:"sp", label:"Spotify app credentials", state:"external", store:"~/.spotdl/config.json",
     affects:["Run Sync (Spotify + deemix)"], value:"client_id + secret", action:"Move to Keychain",
     env:"UDL_SPOTIFY_CLIENT_ID / _SECRET"},
    {id:"arl-old", label:"Deezer ARL (previous)", state:"stale", store:"macOS Keychain",
     affects:["Run Sync (Spotify + deemix)"], value:"expired 12 Jul 2026", action:"Clear", env:"—"},
  ];
  const credState = {
    available:{pill:"ok",  label:"Available"},
    missing:  {pill:"err", label:"Missing"},
    external: {pill:"warn",label:"External override"},
    stale:    {pill:"warn",label:"Needs refresh"},
  };

  /* freedl.Job + capture/promotion rows */
  const freedlJobs = [
    {id:"likes-upgrade", enabled:true, sourceURL:"https://soundcloud.com/janxadam/likes",
     libraryDir:"~/Music/downloaded/sc-likes", bufferDir:"~/Music/.udl-freedl/buffer",
     backupDir:"~/Music/.udl-freedl/backup", logDir:"~/Music/.udl-freedl/logs",
     planLimit:50, targetFormat:"auto", downloadOrder:"oldest_first", minMatchScore:72, ambiguityGap:8, applyPromotions:true},
    {id:"dnb-crate-upgrade", enabled:true, sourceURL:"https://soundcloud.com/janxadam/sets/dnb-crate",
     libraryDir:"~/Music/downloaded/dnb-crate", bufferDir:"~/Music/.udl-freedl/buffer",
     backupDir:"~/Music/.udl-freedl/backup", logDir:"~/Music/.udl-freedl/logs",
     planLimit:25, targetFormat:"flac", downloadOrder:"newest_first", minMatchScore:80, ambiguityGap:6, applyPromotions:false},
    {id:"archive-2024", enabled:false, sourceURL:"https://soundcloud.com/janxadam/sets/archive-2024",
     libraryDir:"~/Music/downloaded/archive", bufferDir:"~/Music/.udl-freedl/buffer",
     backupDir:"~/Music/.udl-freedl/backup", logDir:"~/Music/.udl-freedl/logs",
     planLimit:50, targetFormat:"auto", downloadOrder:"oldest_first", minMatchScore:72, ambiguityGap:8, applyPromotions:false},
  ];
  /* freeDL: local quality vs available Free DL quality */
  const freedlRows = [
    {i:1, title:"Troglodyte (VIP)", artist:"Culture Shock", local:"mp3 128", localKbps:128, avail:"wav", score:98, state:"upgrade"},
    {i:2, title:"Aces", artist:"Halogenix", local:"mp3 192", localKbps:192, avail:"flac", score:96, state:"upgrade"},
    {i:3, title:"Air I Breathe", artist:"Sub Focus & Wilkinson", local:"mp3 320", localKbps:320, avail:"mp3 320", score:94, state:"same"},
    {i:4, title:"Sleepless Nights", artist:"Monrroe", local:"mp3 128", localKbps:128, avail:"—", score:91, state:"nofree"},
    {i:5, title:"Voyager", artist:"Kanine", local:"m4a 256", localKbps:256, avail:"wav", score:88, state:"upgrade"},
    {i:6, title:"Departure Lounge", artist:"LSB", local:"mp3 192", localKbps:192, avail:"flac", score:74, state:"upgrade"},
    {i:7, title:"Kites", artist:"Etherwood", local:"—", localKbps:0, avail:"flac", score:69, state:"nomatch"},
    {i:8, title:"Iron Galaxy", artist:"Dossa & Locuzzed", local:"mp3 320", localKbps:320, avail:"wav", score:93, state:"upgrade"},
    {i:9, title:"Solitude", artist:"Hybrid Minds", local:"mp3 128", localKbps:128, avail:"mp3 320", score:86, state:"upgrade"},
    {i:10,title:"Last Train Home", artist:"Technimatic", local:"flac", localKbps:1411, avail:"flac", score:97, state:"same"},
  ];
  const freedlState = {
    upgrade:{pill:"ok",   label:"Upgrade available"},
    same:   {pill:"",     label:"No gain"},
    nofree: {pill:"",     label:"No free DL"},
    nomatch:{pill:"warn", label:"Below match score"},
  };

  /* playlists.Snapshot */
  const playlists = [
    {id:"friday-warmup", name:"Friday Warmup", provider:"apple_music", providerPlaylist:"Friday Warmup",
     refreshed:"2026-07-30 09:14", tracks:42, missingLocal:2, checksum:"9f2c…41ab",
     freedlJob:"likes-upgrade", rbTarget:"UDL/Warmup"},
    {id:"peak-time", name:"Peak Time", provider:"apple_music", providerPlaylist:"Peak Time",
     refreshed:"2026-07-28 22:03", tracks:68, missingLocal:0, checksum:"3b71…c204",
     freedlJob:"dnb-crate-upgrade", rbTarget:"UDL/Peak Time"},
    {id:"closers", name:"Closers", provider:"apple_music", providerPlaylist:"Closers",
     refreshed:"2026-07-12 18:40", tracks:19, missingLocal:5, checksum:"08de…7f13",
     freedlJob:"", rbTarget:"UDL/Closers"},
    {id:"new-finds", name:"New Finds", provider:"apple_music", providerPlaylist:"New Finds",
     refreshed:"never", tracks:0, missingLocal:0, checksum:"—", freedlJob:"", rbTarget:""},
  ];
  const playlistTracks = [
    {index:1, artist:"Culture Shock", title:"Troglodyte (VIP)", album:"Troglodyte", duration:"5:12", path:"~/Music/downloaded/sc-likes/Culture Shock - Troglodyte (VIP).wav", missing:false},
    {index:2, artist:"Monrroe", title:"Sleepless Nights", album:"Sleepless Nights EP", duration:"4:11", path:"~/Music/downloaded/sc-likes/Monrroe - Sleepless Nights.mp3", missing:false},
    {index:3, artist:"Kanine", title:"Voyager", album:"Voyager", duration:"3:59", path:"~/Music/downloaded/sc-likes/Kanine - Voyager.wav", missing:false},
    {index:4, artist:"Ownglow", title:"Blue Hour", album:"Blue Hour", duration:"4:18", path:"", missing:true},
    {index:5, artist:"LSB", title:"Departure Lounge", album:"Content", duration:"6:41", path:"~/Music/downloaded/sc-likes/LSB - Departure Lounge.flac", missing:false},
    {index:6, artist:"Technimatic", title:"Last Train Home", album:"Better Perspective", duration:"6:12", path:"~/Music/downloaded/sc-likes/Technimatic - Last Train Home.flac", missing:false},
    {index:7, artist:"Skeptical", title:"Nowhere Fast", album:"Nowhere Fast", duration:"5:20", path:"", missing:true},
    {index:8, artist:"Etherwood", title:"Kites", album:"In Stillness", duration:"4:55", path:"~/Music/downloaded/sc-likes/Etherwood - Kites.mp3", missing:false},
  ];

  /* rekordbox playlistsync.PlanRow — match_status + action */
  const rbRows = [
    {i:1, artist:"Culture Shock", title:"Troglodyte (VIP)", dur:"5:12", match:"matched_path", action:"keep",  path:"~/Music/downloaded/sc-likes/Culture Shock - Troglodyte (VIP).wav", rbId:"18422"},
    {i:2, artist:"Monrroe", title:"Sleepless Nights", dur:"4:11", match:"matched_path", action:"add",   path:"~/Music/downloaded/sc-likes/Monrroe - Sleepless Nights.mp3", rbId:"18455"},
    {i:3, artist:"Kanine", title:"Voyager", dur:"3:59", match:"matched_path", action:"move",  path:"~/Music/downloaded/sc-likes/Kanine - Voyager.wav", rbId:"18401"},
    {i:4, artist:"Ownglow", title:"Blue Hour", dur:"4:18", match:"missing", action:"blocked", path:"~/Music/downloaded/sc-likes/Ownglow - Blue Hour.mp3", rbId:""},
    {i:5, artist:"LSB", title:"Departure Lounge", dur:"6:41", match:"matched_path", action:"keep", path:"~/Music/downloaded/sc-likes/LSB - Departure Lounge.flac", rbId:"18377"},
    {i:6, artist:"Technimatic", title:"Last Train Home", dur:"6:12", match:"ambiguous_path", action:"blocked", path:"~/Music/downloaded/sc-likes/Technimatic - Last Train Home.flac", rbId:"18390, 18512"},
    {i:7, artist:"Etherwood", title:"Kites", dur:"4:55", match:"matched_path", action:"add", path:"~/Music/downloaded/sc-likes/Etherwood - Kites.mp3", rbId:"18499"},
    {i:8, artist:"Skeptical", title:"Nowhere Fast", dur:"5:20", match:"duplicate_path", action:"blocked", path:"~/Music/downloaded/sc-likes/Skeptical - Nowhere Fast.mp3", rbId:"18466"},
    {i:9, artist:"Bou", title:"Feel It (Bou Remix)", dur:"4:33", match:"matched_path", action:"add", path:"~/Music/downloaded/sc-likes/Bou - Feel It.mp3", rbId:"18521"},
    {i:10,artist:"Enei", title:"Pointe Blank", dur:"5:01", match:"matched_path", action:"keep", path:"~/Music/downloaded/dnb-crate/Enei - Pointe Blank.wav", rbId:"18300"},
  ];
  const rbMatch = {
    matched_path:{pill:"ok", label:"Matched"},
    missing:{pill:"err", label:"Missing in Rekordbox"},
    ambiguous_path:{pill:"err", label:"Ambiguous path"},
    duplicate_path:{pill:"err", label:"Duplicate in playlist"},
  };
  const rbAction = {keep:"Keep", add:"Add", move:"Move", remove:"Remove", blocked:"—"};

  /* live run state for the sync-run view */
  const runSources = [
    {id:"soundcloud-likes", label:"SoundCloud Likes", state:"done",    planned:11, done:10, skipped:0, failed:1, latest:"Last Train Home — Technimatic"},
    {id:"sc-dnb-crate",     label:"DnB Crate",        state:"running", planned:5,  done:2,  skipped:0, failed:0, latest:"Gemini — Klax"},
    {id:"spotify-liked",    label:"Spotify Liked",    state:"pending", planned:8,  done:0,  skipped:0, failed:0, latest:""},
  ];
  const activity = [
    {k:"fail", t:"14:22:41", s:"soundcloud-likes", m:"Last Train Home — Technimatic", d:"scdl exit 1: HTTP 403 on media URL"},
    {k:"done", t:"14:22:12", s:"soundcloud-likes", m:"Kites — Etherwood"},
    {k:"done", t:"14:21:48", s:"soundcloud-likes", m:"Departure Lounge — LSB"},
    {k:"skip", t:"14:21:30", s:"soundcloud-likes", m:"Circles — Workforce", d:"already in state file"},
    {k:"done", t:"14:21:02", s:"soundcloud-likes", m:"Solitude — Hybrid Minds"},
    {k:"done", t:"14:20:35", s:"soundcloud-likes", m:"Feel It (Bou Remix) — Bou"},
    {k:"done", t:"14:20:01", s:"soundcloud-likes", m:"Voyager — Kanine"},
    {k:"done", t:"14:19:22", s:"soundcloud-likes", m:"Sleepless Nights — Monrroe"},
  ];

  function counts(list){
    return list.reduce((a,r)=>{a[r.status]=(a[r.status]||0)+1;return a},
      {missing_new:0,missing_known_gap:0,already_downloaded:0});
  }

  g.UDL = { STATUS:{NEW,GAP,HAVE}, sources, rows, statusMeta, counts, checks, creds, credState,
    freedlJobs, freedlRows, freedlState, playlists, playlistTracks, rbRows, rbMatch, rbAction,
    runSources, activity };
})(window);
