package output

import (
	"bytes"
	"strings"
	"testing"
)

func TestCompactLogWriterPersistsOnlyTrackResults(t *testing.T) {
	buf := &bytes.Buffer{}
	writer := NewCompactLogWriterWithOptions(buf, CompactLogOptions{Interactive: false})

	payload := strings.Join([]string{
		"[download] Downloading item 1 of 2",
		"[download] Destination: /tmp/ninnidslvx - FUJI.m4a",
		"[info] Writing video thumbnail t500x500 to: /tmp/ninnidslvx - FUJI.jpg",
		"[download]   3.8% of ~  19.32KiB at    3.84KiB/s ETA Unknown (frag 0/26)",
		"[download]   5.2% of ~  19.32KiB at    3.84KiB/s ETA Unknown (frag 1/26)",
		"[download] 100% of    5.00MiB in 00:00:02 at 1.98MiB/s",
		"[download] Downloading item 2 of 2",
		"[download] /tmp/Aether.m4a has already been downloaded",
		"[download] 100% of    5.00MiB in 00:00:01 at 1.98MiB/s",
	}, "\n") + "\n"

	if _, err := writer.Write([]byte(payload)); err != nil {
		t.Fatalf("write: %v", err)
	}
	if err := writer.Flush(); err != nil {
		t.Fatalf("flush: %v", err)
	}

	out := buf.String()
	if !strings.Contains(out, "[done] ninnidslvx - FUJI (thumb)") {
		t.Fatalf("expected downloaded track line, got: %s", out)
	}
	if !strings.Contains(out, "[skip] Aether (already-present)") {
		t.Fatalf("expected already-present track line, got: %s", out)
	}
	if strings.Contains(out, "frag 0/26") || strings.Contains(out, "Downloading item") || strings.Contains(out, "Destination") {
		t.Fatalf("expected noisy lines to be suppressed, got: %s", out)
	}
}

func TestCompactLogWriterFlushesWarningLineWithoutTrailingNewline(t *testing.T) {
	buf := &bytes.Buffer{}
	writer := NewCompactLogWriterWithOptions(buf, CompactLogOptions{Interactive: false})

	if _, err := writer.Write([]byte("Warning: network is slow")); err != nil {
		t.Fatalf("write: %v", err)
	}
	if err := writer.Flush(); err != nil {
		t.Fatalf("flush: %v", err)
	}
	if !strings.Contains(buf.String(), "Warning: network is slow") {
		t.Fatalf("expected buffered line to flush, got: %s", buf.String())
	}
}

func TestCompactLogWriterInteractiveRendersProgressBar(t *testing.T) {
	buf := &bytes.Buffer{}
	writer := NewCompactLogWriterWithOptions(buf, CompactLogOptions{Interactive: true})

	payload := strings.Join([]string{
		"[download] Downloading item 1 of 1",
		"[download] Destination: /tmp/Track One.m4a",
		"[download]  25.0% of ~   2.51MiB at    6.88KiB/s ETA Unknown (frag 1/26)",
		"[download] 100% of    5.00MiB in 00:00:02 at 1.98MiB/s",
	}, "\n") + "\n"

	if _, err := writer.Write([]byte(payload)); err != nil {
		t.Fatalf("write: %v", err)
	}
	if err := writer.Flush(); err != nil {
		t.Fatalf("flush: %v", err)
	}

	out := buf.String()
	if !strings.Contains(out, "25.0%") || !strings.Contains(out, "[####") {
		t.Fatalf("expected progress bar and percent in output, got: %s", out)
	}
	if !strings.Contains(out, "[overall]") {
		t.Fatalf("expected global progress line in output, got: %s", out)
	}
	if !strings.Contains(out, "[done] Track One") {
		t.Fatalf("expected final done line, got: %s", out)
	}
}

func TestCompactLogWriterUsesPreflightPlannedCountForGlobalProgress(t *testing.T) {
	buf := &bytes.Buffer{}
	writer := NewCompactLogWriterWithOptions(buf, CompactLogOptions{Interactive: true})

	payload := strings.Join([]string{
		"[soundcloud-clean-test] preflight: remote=1041 known=12 gaps=1029 known_gaps=3 first_existing=4 planned=3 mode=break",
		"[download] Downloading item 1 of 4",
		"[download] Destination: /tmp/PICHI - BO FUNK [FREE DL].m4a",
		"[download]  25.0% of ~   2.51MiB at    6.88KiB/s ETA Unknown (frag 1/26)",
	}, "\n") + "\n"

	if _, err := writer.Write([]byte(payload)); err != nil {
		t.Fatalf("write: %v", err)
	}
	if err := writer.Flush(); err != nil {
		t.Fatalf("flush: %v", err)
	}

	out := buf.String()
	if !strings.Contains(out, "[overall]") {
		t.Fatalf("expected overall live line, got: %s", out)
	}
	if !strings.Contains(out, "8.3%") {
		t.Fatalf("expected global progress to use planned=3 denominator (8.3%%), got: %s", out)
	}
}

func TestCompactLogWriterKeepsGlobalBarVisibleBetweenTracks(t *testing.T) {
	buf := &bytes.Buffer{}
	writer := NewCompactLogWriterWithOptions(buf, CompactLogOptions{Interactive: true})

	payload := strings.Join([]string{
		"[soundcloud-clean-test] preflight: remote=1041 known=12 gaps=1029 known_gaps=3 first_existing=4 planned=3 mode=break",
		"[download] Downloading item 1 of 4",
		"[download] Destination: /tmp/PICHI - BO FUNK [FREE DL].m4a",
		"[download] 100% of    5.00MiB in 00:00:02 at 1.98MiB/s",
		"[download] Downloading item 2 of 4",
	}, "\n") + "\n"

	if _, err := writer.Write([]byte(payload)); err != nil {
		t.Fatalf("write: %v", err)
	}
	if err := writer.Flush(); err != nil {
		t.Fatalf("flush: %v", err)
	}

	out := buf.String()
	if !strings.Contains(out, "waiting for next track (1/3 done)") {
		t.Fatalf("expected idle live status between tracks, got: %s", out)
	}
}

func TestCompactLogWriterSuppressesBreakOnExistingTraceback(t *testing.T) {
	buf := &bytes.Buffer{}
	writer := NewCompactLogWriterWithOptions(buf, CompactLogOptions{Interactive: false})

	payload := strings.Join([]string{
		"[download] 2210531636: PICHI - BO FUNK [FREE DL] has already been recorded in the archive",
		"Traceback (most recent call last):",
		"File \"/opt/homebrew/bin/scdl\", line 7, in <module>",
		"yt_dlp.utils.ExistingVideoReached: Encountered a video that is already in the archive, stopping due to --break-on-existing",
	}, "\n") + "\n"

	if _, err := writer.Write([]byte(payload)); err != nil {
		t.Fatalf("write: %v", err)
	}
	if err := writer.Flush(); err != nil {
		t.Fatalf("flush: %v", err)
	}

	out := buf.String()
	if !strings.Contains(out, "[stop] reached existing track in archive (break_on_existing)") {
		t.Fatalf("expected compact stop message, got: %s", out)
	}
	if strings.Contains(out, "Traceback") || strings.Contains(out, "ExistingVideoReached") {
		t.Fatalf("expected traceback to be suppressed, got: %s", out)
	}
}

func TestCompactLogWriterSuppressesSpotDLSpotifyTracebackNoise(t *testing.T) {
	buf := &bytes.Buffer{}
	writer := NewCompactLogWriterWithOptions(buf, CompactLogOptions{Interactive: false})

	payload := strings.Join([]string{
		"Processing query: https://open.spotify.com/playlist/test",
		"HTTP Error for GET to https://api.spotify.com/v1/playlists/test/items with Params: {'limit': 100} returned 401 due to Valid user authentication required",
		"An error occurred",
		"╭───────────────────── Traceback (most recent call last) ──────────────────────╮",
		"│ /Users/jaa/.venvs/udl-spotdl/lib/python3.11/site-packages/spotipy/client.py: │",
		"╰──────────────────────────────────────────────────────────────────────────────╯",
		"SpotifyException: http status: 403, code: -1 - https://api.spotify.com/v1/playlists/test/tracks: Forbidden, reason: None",
		"[spotify-source] spotify login required; retrying once with --user-auth (browser login enabled)",
	}, "\n") + "\n"

	if _, err := writer.Write([]byte(payload)); err != nil {
		t.Fatalf("write: %v", err)
	}
	if err := writer.Flush(); err != nil {
		t.Fatalf("flush: %v", err)
	}

	out := buf.String()
	if !strings.Contains(out, "spotify login required; retrying once with --user-auth") {
		t.Fatalf("expected compact retry guidance line to be preserved, got: %s", out)
	}
	if strings.Contains(out, "Traceback") ||
		strings.Contains(out, "HTTP Error for GET to https://api.spotify.com/") ||
		strings.Contains(out, "SpotifyException: http status: 403") ||
		strings.Contains(out, "An error occurred") {
		t.Fatalf("expected spotdl traceback noise to be suppressed, got: %s", out)
	}
}

func TestCompactLogWriterFormatsSpotDLAsDoneAndSkip(t *testing.T) {
	buf := &bytes.Buffer{}
	writer := NewCompactLogWriterWithOptions(buf, CompactLogOptions{Interactive: false})

	payload := strings.Join([]string{
		"Processing query: https://open.spotify.com/playlist/test",
		"Found 3 songs in roove (Playlist)",
		"Nothing to delete...",
		"Downloaded \"Regent - Permean\": https://music.youtube.com/watch?v=3GguPIsWJdE",
		"LookupError: No results found for song: Missing Song",
		"Downloaded \"Regent - Encoder\": https://music.youtube.com/watch?v=87HZwS_3soQ",
		"https://open.spotify.com/track/abc - LookupError: No results found for song: Missing Song",
		"Saved archive with 2 urls to spotify-sandbox.archive.txt",
	}, "\n") + "\n"

	if _, err := writer.Write([]byte(payload)); err != nil {
		t.Fatalf("write: %v", err)
	}
	if err := writer.Flush(); err != nil {
		t.Fatalf("flush: %v", err)
	}

	out := buf.String()
	if !strings.Contains(out, "[done] Regent - Permean") {
		t.Fatalf("expected first spotdl completion line, got: %s", out)
	}
	if !strings.Contains(out, "[skip] Missing Song (no-match)") {
		t.Fatalf("expected spotdl lookup miss line, got: %s", out)
	}
	if !strings.Contains(out, "[done] Regent - Encoder") {
		t.Fatalf("expected second spotdl completion line, got: %s", out)
	}
	if strings.Contains(out, "Processing query:") ||
		strings.Contains(out, "Found 3 songs in") ||
		strings.Contains(out, "Nothing to delete...") ||
		strings.Contains(out, "Saved archive with") ||
		strings.Contains(out, "open.spotify.com/track/abc") ||
		strings.Contains(out, "Downloaded \"Regent - Permean\"") {
		t.Fatalf("expected raw spotdl chatter to be suppressed, got: %s", out)
	}
}

func TestCompactLogWriterSpotDLInteractiveShowsTrackAndGlobalProgress(t *testing.T) {
	buf := &bytes.Buffer{}
	writer := NewCompactLogWriterWithOptions(buf, CompactLogOptions{Interactive: true})

	payload := strings.Join([]string{
		"Found 2 songs in roove (Playlist)",
		"Downloaded \"Track One\": https://music.youtube.com/watch?v=one",
	}, "\n") + "\n"

	if _, err := writer.Write([]byte(payload)); err != nil {
		t.Fatalf("write: %v", err)
	}
	if err := writer.Flush(); err != nil {
		t.Fatalf("flush: %v", err)
	}

	out := buf.String()
	if !strings.Contains(out, "[track 1/2] Track One") {
		t.Fatalf("expected per-track live line for spotdl download, got: %s", out)
	}
	if !strings.Contains(out, "[overall]") || !strings.Contains(out, "50.0%") {
		t.Fatalf("expected global progress for spotdl download, got: %s", out)
	}
	if !strings.Contains(out, "waiting for next track (1/2 done)") {
		t.Fatalf("expected idle status after first spotdl completion, got: %s", out)
	}
}

func TestCompactLogWriterSuppressesWrappedSpotDLErrorFragments(t *testing.T) {
	buf := &bytes.Buffer{}
	writer := NewCompactLogWriterWithOptions(buf, CompactLogOptions{Interactive: false})

	payload := strings.Join([]string{
		"Found 2 songs in roove (Playlist)",
		"AudioProviderError: YT-DLP download error -",
		"https://www.youtube.com/watch?v=9YglTIS4XD4",
		"https://open.spotify.com/track/3dJcnE6fhRx4kKGmr68LJo - LookupError: No results",
		"found for song: Bours? - Silent Clubstep",
		"YT-DLP download error - https://www.youtube.com/watch?v=9YglTIS4XD4",
	}, "\n") + "\n"

	if _, err := writer.Write([]byte(payload)); err != nil {
		t.Fatalf("write: %v", err)
	}
	if err := writer.Flush(); err != nil {
		t.Fatalf("flush: %v", err)
	}

	out := buf.String()
	if !strings.Contains(out, "[skip] track 1/2 (download-error)") {
		t.Fatalf("expected compact placeholder for wrapped audio provider failure, got: %s", out)
	}
	if strings.Contains(out, "found for song:") ||
		strings.Contains(out, "https://www.youtube.com/watch?v=") ||
		strings.Contains(out, "YT-DLP download error - https://") {
		t.Fatalf("expected wrapped error fragments to be suppressed, got: %s", out)
	}
}
