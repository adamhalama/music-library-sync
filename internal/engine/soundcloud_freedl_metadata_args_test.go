package engine

import (
	"slices"
	"strings"
	"testing"
)

// ffmpeg reads options positionally: an option placed between two -i flags is
// taken as an input option for the second input. Declaring -map/-codec before
// the artwork input made every artwork embed fail with
// "Option map (set input stream mapping) cannot be applied to input url".
func TestSoundCloudMetadataFFmpegArgsDeclareAllInputsBeforeOutputOptions(t *testing.T) {
	metadata := soundCloudFreeDownloadMetadata{
		Title:         "RED ROOM",
		Artist:        "Absrismusic",
		Genre:         "Techno",
		SoundCloudURL: "https://soundcloud.com/absrismusic/red-room",
	}
	args := soundCloudMetadataFFmpegArgs("/tmp/in.wav", "/tmp/out.wav", metadata, "/tmp/art.jpg")

	lastInput := slices.Index(args, "-i")
	for i, arg := range args {
		if arg == "-i" {
			lastInput = i
		}
	}
	firstOutputOption := -1
	for i, arg := range args {
		if arg == "-map" || arg == "-codec" || strings.HasPrefix(arg, "-c:") || strings.HasPrefix(arg, "-disposition") {
			firstOutputOption = i
			break
		}
	}
	if firstOutputOption < 0 {
		t.Fatalf("no output option found in %v", args)
	}
	if firstOutputOption < lastInput {
		t.Fatalf("output option %q at %d precedes input at %d: %v", args[firstOutputOption], firstOutputOption, lastInput, args)
	}

	if got := strings.Count(strings.Join(args, " "), "-i "); got != 2 {
		t.Fatalf("expected 2 inputs, got %d: %v", got, args)
	}
	if !slices.Contains(args, "attached_pic") {
		t.Fatalf("artwork run should mark the cover as attached_pic: %v", args)
	}
	if args[len(args)-1] != "/tmp/out.wav" {
		t.Fatalf("output path must come last, got %q", args[len(args)-1])
	}
}

func TestSoundCloudMetadataFFmpegArgsWithoutArtworkStillMapsAndCopies(t *testing.T) {
	args := soundCloudMetadataFFmpegArgs("/tmp/in.m4a", "/tmp/out.m4a", soundCloudFreeDownloadMetadata{Title: "t"}, "")
	if got := strings.Count(strings.Join(args, " "), "-i "); got != 1 {
		t.Fatalf("expected a single input, got %d: %v", got, args)
	}
	for _, unwanted := range []string{"1:0", "attached_pic", "mjpeg"} {
		if slices.Contains(args, unwanted) {
			t.Fatalf("artwork-only option %q leaked into a no-artwork run: %v", unwanted, args)
		}
	}
	if !slices.Contains(args, "-map") || !slices.Contains(args, "-codec") {
		t.Fatalf("expected -map and -codec in %v", args)
	}
}
