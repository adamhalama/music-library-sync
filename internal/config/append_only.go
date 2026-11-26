package config

import "fmt"

// ValidateAppendOnlySource restricts the adapter's write surface. Arbitrary scdl
// or yt-dlp arguments can redirect files or execute commands outside staging.
func ValidateAppendOnlySource(source Source) error {
	if source.Type != SourceTypeSoundCloud || source.Adapter.Kind != "scdl" {
		return fmt.Errorf("sync.append_only requires soundcloud with adapter.kind=scdl; other adapters, including scdl-freedl, cannot guarantee append-only writes")
	}
	for _, arg := range source.Adapter.ExtraArgs {
		switch arg {
		case "-f", "-t", "-r", "-C", "--no-playlist-folder", "--onlymp3", "--no-original", "--only-original", "--original-metadata", "--no-album-tag", "--original-art", "--opus", "--flac", "--hide-progress", "--hidewarnings":
		default:
			return fmt.Errorf("sync.append_only cannot safely use adapter.extra_args %q; remove custom paths, templates, overwrite flags, and yt-dlp arguments", arg)
		}
	}
	return nil
}
