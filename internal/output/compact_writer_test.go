package output

import (
	"bytes"
	"strings"
	"testing"
)

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

func TestCompactLogWriterStructuredTrackEventsDriveResults(t *testing.T) {
	buf := &bytes.Buffer{}
	writer := NewCompactLogWriterWithOptions(buf, CompactLogOptions{Interactive: false})

	writer.ObserveEvent(Event{
		Event:    EventSourcePreflight,
		SourceID: "soundcloud-likes",
		Details: map[string]any{
			"planned_download_count": 1,
		},
	})
	writer.ObserveEvent(Event{
		Event:    EventTrackStarted,
		SourceID: "soundcloud-likes",
		Details: map[string]any{
			"track_name": "Structured Song",
			"index":      1,
			"total":      1,
		},
	})
	writer.ObserveEvent(Event{
		Event:    EventTrackProgress,
		SourceID: "soundcloud-likes",
		Details: map[string]any{
			"track_name": "Structured Song",
			"index":      1,
			"total":      1,
			"percent":    67.5,
		},
	})
	writer.ObserveEvent(Event{
		Event:    EventTrackDone,
		SourceID: "soundcloud-likes",
		Details: map[string]any{
			"track_name": "Structured Song",
			"index":      1,
			"total":      1,
		},
	})

	payload := strings.Join([]string{
		"[download] Downloading item 1 of 1",
		"[download] Destination: /tmp/Structured Song.m4a",
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
	if !strings.Contains(out, "[done] Structured Song") {
		t.Fatalf("expected structured done line, got: %s", out)
	}
	if strings.Contains(out, "Downloading item 1 of 1") || strings.Contains(out, "Destination: /tmp/Structured Song.m4a") {
		t.Fatalf("expected raw adapter chatter to be suppressed, got: %s", out)
	}
}

func TestCompactLogWriterInteractiveRendersStructuredProgress(t *testing.T) {
	buf := &bytes.Buffer{}
	writer := NewCompactLogWriterWithOptions(buf, CompactLogOptions{Interactive: true})

	writer.ObserveEvent(Event{
		Event:    EventSourcePreflight,
		SourceID: "soundcloud-likes",
		Details: map[string]any{
			"planned_download_count": 1,
		},
	})
	writer.ObserveEvent(Event{
		Event:    EventTrackStarted,
		SourceID: "soundcloud-likes",
		Details: map[string]any{
			"track_name": "Structured Song",
			"index":      1,
			"total":      4,
		},
	})
	writer.ObserveEvent(Event{
		Event:    EventTrackProgress,
		SourceID: "soundcloud-likes",
		Details: map[string]any{
			"track_name": "Structured Song",
			"index":      1,
			"total":      4,
			"percent":    67.5,
		},
	})
	writer.ObserveEvent(Event{
		Event:    EventTrackDone,
		SourceID: "soundcloud-likes",
		Details: map[string]any{
			"track_name": "Structured Song",
			"index":      1,
			"total":      4,
		},
	})

	if err := writer.Flush(); err != nil {
		t.Fatalf("flush: %v", err)
	}

	out := buf.String()
	if !strings.Contains(out, "Structured Song") || !strings.Contains(out, "67.5%") {
		t.Fatalf("expected live structured track progress, got: %s", out)
	}
	if !strings.Contains(out, "[overall]") || !strings.Contains(out, "(1/1)") {
		t.Fatalf("expected overall progress to use planned total, got: %s", out)
	}
	if !strings.Contains(out, "all planned tracks complete (1/1)") {
		t.Fatalf("expected idle completion line after done, got: %s", out)
	}
}

func TestCompactLogWriterShowsPreflightSummaryByDefault(t *testing.T) {
	buf := &bytes.Buffer{}
	writer := NewCompactLogWriterWithOptions(buf, CompactLogOptions{Interactive: false})

	payload := "[spotify-deemix] preflight: remote=19 known=4 gaps=15 known_gaps=0 first_existing=1 planned=15 mode=break\n"
	if _, err := writer.Write([]byte(payload)); err != nil {
		t.Fatalf("write: %v", err)
	}
	if err := writer.Flush(); err != nil {
		t.Fatalf("flush: %v", err)
	}

	out := buf.String()
	if !strings.Contains(out, "preflight: remote=19 known=4 gaps=15") {
		t.Fatalf("expected preflight summary line to be preserved, got: %s", out)
	}
}

func TestCompactLogWriterCanSuppressPreflightSummary(t *testing.T) {
	buf := &bytes.Buffer{}
	writer := NewCompactLogWriterWithOptions(buf, CompactLogOptions{
		Interactive:      false,
		PreflightSummary: CompactPreflightNever,
	})

	payload := "[spotify-deemix] preflight: remote=19 known=4 gaps=15 known_gaps=0 first_existing=1 planned=15 mode=break\n"
	if _, err := writer.Write([]byte(payload)); err != nil {
		t.Fatalf("write: %v", err)
	}
	if err := writer.Flush(); err != nil {
		t.Fatalf("flush: %v", err)
	}

	if strings.Contains(buf.String(), "preflight: remote=19") {
		t.Fatalf("expected preflight summary to be suppressed, got: %s", buf.String())
	}
}

func TestCompactLogWriterTrackStatusCountModeUsesStructuredOutcomes(t *testing.T) {
	buf := &bytes.Buffer{}
	writer := NewCompactLogWriterWithOptions(buf, CompactLogOptions{
		Interactive: false,
		TrackStatus: CompactTrackStatusCount,
	})

	writer.ObserveEvent(Event{
		Event:    EventSourcePreflight,
		SourceID: "spotify-source",
		Details: map[string]any{
			"planned_download_count": 2,
		},
	})
	writer.ObserveEvent(Event{
		Event:    EventTrackDone,
		SourceID: "spotify-source",
		Details: map[string]any{
			"track_name": "Regent - Permean",
			"index":      1,
			"total":      2,
		},
	})
	writer.ObserveEvent(Event{
		Event:    EventTrackDone,
		SourceID: "spotify-source",
		Details: map[string]any{
			"track_name": "Regent - Encoder",
			"index":      2,
			"total":      2,
		},
	})

	out := buf.String()
	if !strings.Contains(out, "[done] track 1/2") || !strings.Contains(out, "[done] track 2/2") {
		t.Fatalf("expected count-style structured outcomes, got: %s", out)
	}
	if strings.Contains(out, "Regent - Permean") || strings.Contains(out, "Regent - Encoder") {
		t.Fatalf("did not expect track names in count mode, got: %s", out)
	}
}

func TestCompactLogWriterSuppressesBreakOnExistingTracebackButKeepsStructuredStopMessage(t *testing.T) {
	buf := &bytes.Buffer{}
	writer := NewCompactLogWriterWithOptions(buf, CompactLogOptions{Interactive: false})

	payload := strings.Join([]string{
		"[download] 2210531636: PICHI - BO FUNK [FREE DL] has already been recorded in the archive",
		"Traceback (most recent call last):",
		"File \"/opt/homebrew/bin/scdl\", line 7, in <module>",
		"yt_dlp.utils.ExistingVideoReached: Encountered a video that is already in the archive, stopping due to --break-on-existing",
		"[soundcloud-likes] stopped at first existing track (break_on_existing)",
	}, "\n") + "\n"

	if _, err := writer.Write([]byte(payload)); err != nil {
		t.Fatalf("write: %v", err)
	}
	if err := writer.Flush(); err != nil {
		t.Fatalf("flush: %v", err)
	}

	out := buf.String()
	if !strings.Contains(out, "stopped at first existing track (break_on_existing)") {
		t.Fatalf("expected structured stop message, got: %s", out)
	}
	if strings.Contains(out, "Traceback") || strings.Contains(out, "ExistingVideoReached") {
		t.Fatalf("expected traceback chatter to be suppressed, got: %s", out)
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
		t.Fatalf("expected structured retry guidance line to be preserved, got: %s", out)
	}
	if strings.Contains(out, "Traceback") ||
		strings.Contains(out, "HTTP Error for GET to https://api.spotify.com/") ||
		strings.Contains(out, "SpotifyException: http status: 403") ||
		strings.Contains(out, "An error occurred") {
		t.Fatalf("expected spotdl traceback noise to be suppressed, got: %s", out)
	}
}

func TestCompactLogWriterKeepsNonStructuredSkipLineWhileSuppressingDeemixStackNoise(t *testing.T) {
	buf := &bytes.Buffer{}
	writer := NewCompactLogWriterWithOptions(buf, CompactLogOptions{Interactive: false})

	payload := strings.Join([]string{
		"[spotify-deemix] [skip] 27375CASeros2Z5JaxJ8j0 (Unknown Track) (unavailable-on-deezer)",
		"GWAPIError: Track unavailable on Deezer",
		"at GW.api_call (/snapshot/cli/dist/main.cjs)",
	}, "\n") + "\n"
	if _, err := writer.Write([]byte(payload)); err != nil {
		t.Fatalf("write: %v", err)
	}
	if err := writer.Flush(); err != nil {
		t.Fatalf("flush: %v", err)
	}

	out := buf.String()
	if !strings.Contains(out, "[skip] 27375CASeros2Z5JaxJ8j0 (Unknown Track) (unavailable-on-deezer)") {
		t.Fatalf("expected non-structured skip line to remain visible, got: %s", out)
	}
	if strings.Contains(out, "GWAPIError") || strings.Contains(out, "at GW.api_call") {
		t.Fatalf("expected raw deemix stack noise to be suppressed, got: %s", out)
	}
}
