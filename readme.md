### `update-downloads/README.md`

# update-downloads

Point of this script is to have an easy and convinient way to always have a local library that is synced with my SounCloud and Spotify likes/playlists.
Running the script will update the local library in an easy way.

One Bash script that:
1. Enters a directory,
2. Runs the given downloader,
3. Uses `spotdl sync` to only grab new tracks,
4. Streams output to your terminal.

It currently runs:
- `scdl` for SoundCloud likes
- `spotdl` for a few Spotify playlists (can use `.sync.spotdl` state files)

## Requirements
- [spotDL](https://github.com/spotDL/spotify-downloader) (MIT)
- [scdl](https://github.com/scdl-org/scdl) (GPL-2.0)
- macOS/Linux shell with `bash`

> Note: Service Terms of Service apply. This repo just automates **your own** use of those tools.

## Install
Make the script executable and symlink it onto your `PATH`:

```bash
chmod +x bin/update-downloads

# user-local
mkdir -p "$HOME/bin"
ln -sfn "$PWD/bin/update-downloads" "$HOME/bin/update-downloads"

# or system-wide (macOS)
/usr/bin/sudo ln -sfn "$PWD/bin/update-downloads" /usr/local/bin/update-downloads
```


Configure

    Edit playlist URLs and download directories in the script.

    State files default to: ~/dev/music-down/statefiles/

    .gitignore ideas for your clone:

    statefiles/
    *.sync.spotdl
    archive.txt
    *.log

Run

update-downloads

It will process each source, printing progress and a summary at the end.
License

MIT — see LICENSE.