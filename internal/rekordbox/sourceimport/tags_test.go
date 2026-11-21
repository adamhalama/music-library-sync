package sourceimport

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

const ffprobeDeemixMP3 = `{
  "streams": [
    {"codec_type": "audio", "sample_rate": "44100", "bit_rate": "128000", "bits_per_sample": 0},
    {"codec_type": "video", "bits_per_raw_sample": "8"}
  ],
  "format": {
    "duration": "422.796712",
    "size": "6981834",
    "bit_rate": "132107",
    "tags": {
      "title": "Abroad",
      "artist": "Len Faki/REGAL",
      "album": "The End",
      "track": "3/9",
      "disc": "1",
      "genre": "Electro",
      "publisher": "Figure",
      "date": "2016-05-16"
    }
  }
}`

func TestParseFFProbeJSONMatchesRekordboxColumns(t *testing.T) {
	tags, err := ParseFFProbeJSON([]byte(ffprobeDeemixMP3), "/x/Len Faki - Abroad.mp3")
	if err != nil {
		t.Fatal(err)
	}
	want := Tags{
		Title: "Abroad", Artist: "Len Faki/REGAL", Album: "The End", Genre: "Electro", Label: "Figure",
		Year: 2016, TrackNo: 3, DiscNo: 1, DurationSec: 422, BitRateKbps: 128, SampleRate: 44100,
		BitDepth: 16, FileSize: 6981834, Source: TagSourceFFProbe,
	}
	if tags != want {
		t.Fatalf("got %+v\nwant %+v", tags, want)
	}
}

func TestParseFFProbeJSONFallsBackToFilenameForMissingTitle(t *testing.T) {
	tags, err := ParseFFProbeJSON([]byte(`{"format":{"duration":"10.5","tags":{}}}`), "/x/Fluke - Zion.mp3")
	if err != nil {
		t.Fatal(err)
	}
	if tags.Title != "Zion" || tags.Artist != "Fluke" || tags.DurationSec != 10 {
		t.Fatalf("got %+v", tags)
	}
}

func TestTagsFromFilenameWithoutSeparator(t *testing.T) {
	tags := TagsFromFilename("/x/Just A Title.mp3")
	if tags.Title != "Just A Title" || tags.Artist != "" || tags.Source != TagSourceFilename {
		t.Fatalf("got %+v", tags)
	}
}

func TestFFProbeTagReaderUsesRunnerAndStatSize(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "Fluke - Zion.mp3")
	if err := os.WriteFile(path, []byte("12345"), 0o644); err != nil {
		t.Fatal(err)
	}
	reader := FFProbeTagReader{Run: func(_ context.Context, _ string, got string) ([]byte, error) {
		if got != path {
			t.Fatalf("ffprobe got %q", got)
		}
		return []byte(`{"format":{"tags":{"title":"Zion","artist":"Fluke"}}}`), nil
	}}
	tags, err := reader.ReadTags(context.Background(), path)
	if err != nil {
		t.Fatal(err)
	}
	if tags.FileSize != 5 || tags.Title != "Zion" {
		t.Fatalf("got %+v", tags)
	}
}
