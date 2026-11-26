# udl-rbl-analyse

A database-free audio analyser that writes Rekordbox ANLZ files and a JSON result.
This separate program is GPL-2.0-or-later (see LICENSE). It links the GPL crates
from RBxport 1.10.0 at d0ff75b378ec85be891d9f3067ada1e82bad4cae; its decode
pipeline is adapted from RBxport's src-tauri/src/analysis.rs
(Copyright (c) 2026 Chris Le). The MIT udl CLI
executes it as a subprocess and does not link these crates.

Build with Rust 1.89 or newer:

```sh
make analyser
# or, from this directory:
cargo build --release --locked
cargo test --locked
```

```sh
udl-rbl-analyse --audio track.mp3 --mode rbxport --out /tmp/analysis \
  --key-notation classic --existing /path/to/previous/analysis
```

`--existing` is optional. `--out` must not contain ANLZ0000 siblings already.
Modes select RBxport's Rbxport or Rekordbox preset, with its default Analysis
Settings (70–180 BPM, high precision). Both use RBxport's analyser, not the
proprietary Rekordbox application. All stereo frames feed RBxport's wave creator;
tempo/key use the first 30 minutes and the grid extends through longer audio.
Existing cue and other preserved sections follow RBxport's ANLZ encoder.
Audio tags and databases are never written.

Success prints one JSON object; failure exits nonzero with a JSON error on stderr.
The helper is built separately and is not included in udl release packages.
