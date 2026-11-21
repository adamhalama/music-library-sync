package sourceimport

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
)

const (
	TagSourceFFProbe  = "ffprobe"
	TagSourceFilename = "filename"
)

// Tags is the metadata written onto a new Rekordbox collection row. It mirrors
// the columns Rekordbox (and RBxport) fill when they import a file themselves.
type Tags struct {
	Title       string `json:"title"`
	Artist      string `json:"artist,omitempty"`
	Album       string `json:"album,omitempty"`
	Genre       string `json:"genre,omitempty"`
	Label       string `json:"label,omitempty"`
	Comment     string `json:"comment,omitempty"`
	Year        int    `json:"year,omitempty"`
	TrackNo     int    `json:"track_no,omitempty"`
	DiscNo      int    `json:"disc_no,omitempty"`
	DurationSec int    `json:"duration_sec,omitempty"`
	BitRateKbps int    `json:"bit_rate_kbps,omitempty"`
	SampleRate  int    `json:"sample_rate,omitempty"`
	BitDepth    int    `json:"bit_depth,omitempty"`
	FileSize    int64  `json:"file_size,omitempty"`
	Source      string `json:"source"`
}

// TagReader reads Tags from an audio file.
type TagReader interface {
	ReadTags(ctx context.Context, path string) (Tags, error)
}

// FFProbeTagReader reads tags and stream properties with ffprobe and falls back
// to "Artist - Title" parsed from the file name when ffprobe is unavailable.
type FFProbeTagReader struct {
	// Binary overrides the ffprobe executable; empty means look it up on PATH.
	Binary string
	// Run is a test seam returning ffprobe's JSON for a path.
	Run func(ctx context.Context, binary string, path string) ([]byte, error)
}

func (r FFProbeTagReader) ReadTags(ctx context.Context, path string) (Tags, error) {
	info, err := os.Stat(path)
	if err != nil {
		return Tags{}, fmt.Errorf("stat %s: %w", path, err)
	}
	binary := strings.TrimSpace(r.Binary)
	if binary == "" && r.Run == nil {
		found, lookErr := exec.LookPath("ffprobe")
		if lookErr != nil {
			tags := TagsFromFilename(path)
			tags.FileSize = info.Size()
			return tags, nil
		}
		binary = found
	}
	run := r.Run
	if run == nil {
		run = runFFProbe
	}
	payload, err := run(ctx, binary, path)
	if err != nil {
		return Tags{}, err
	}
	tags, err := ParseFFProbeJSON(payload, path)
	if err != nil {
		return Tags{}, err
	}
	if tags.FileSize == 0 {
		tags.FileSize = info.Size()
	}
	return tags, nil
}

func runFFProbe(ctx context.Context, binary string, path string) ([]byte, error) {
	cmd := exec.CommandContext(ctx, binary,
		"-v", "error",
		"-show_entries", "format=duration,bit_rate,size:format_tags:stream=codec_type,sample_rate,bit_rate,bits_per_sample,bits_per_raw_sample",
		"-of", "json",
		path,
	)
	out, err := cmd.Output()
	if err != nil {
		detail := ""
		if exitErr, ok := err.(*exec.ExitError); ok {
			detail = strings.TrimSpace(string(exitErr.Stderr))
		}
		return nil, fmt.Errorf("ffprobe %s: %w %s", path, err, detail)
	}
	return out, nil
}

type ffprobeOutput struct {
	Streams []struct {
		CodecType        string `json:"codec_type"`
		SampleRate       string `json:"sample_rate"`
		BitRate          string `json:"bit_rate"`
		BitsPerSample    int    `json:"bits_per_sample"`
		BitsPerRawSample string `json:"bits_per_raw_sample"`
	} `json:"streams"`
	Format struct {
		Duration string            `json:"duration"`
		BitRate  string            `json:"bit_rate"`
		Size     string            `json:"size"`
		Tags     map[string]string `json:"tags"`
	} `json:"format"`
}

// ParseFFProbeJSON turns ffprobe's JSON into Tags. Missing title/artist fall
// back to the file name so a row never lands in the collection untitled.
func ParseFFProbeJSON(payload []byte, path string) (Tags, error) {
	var out ffprobeOutput
	if err := json.Unmarshal(payload, &out); err != nil {
		return Tags{}, fmt.Errorf("decode ffprobe output for %s: %w", path, err)
	}
	tag := func(keys ...string) string {
		for _, key := range keys {
			for k, v := range out.Format.Tags {
				if strings.EqualFold(k, key) && strings.TrimSpace(v) != "" {
					return strings.TrimSpace(v)
				}
			}
		}
		return ""
	}
	tags := Tags{
		Title:   tag("title"),
		Artist:  tag("artist"),
		Album:   tag("album"),
		Genre:   tag("genre"),
		Label:   tag("publisher", "label", "organization"),
		Comment: tag("comment"),
		Year:    leadingInt(tag("date", "year", "TDRC", "TYER")),
		TrackNo: leadingInt(tag("track")),
		DiscNo:  leadingInt(tag("disc")),
		Source:  TagSourceFFProbe,
	}
	if duration, err := strconv.ParseFloat(strings.TrimSpace(out.Format.Duration), 64); err == nil && duration > 0 {
		// Rekordbox stores whole seconds and truncates.
		tags.DurationSec = int(math.Floor(duration))
	}
	if size, err := strconv.ParseInt(strings.TrimSpace(out.Format.Size), 10, 64); err == nil {
		tags.FileSize = size
	}
	formatBitRate := atoiOrZero(out.Format.BitRate)
	for _, stream := range out.Streams {
		if stream.CodecType != "audio" {
			continue
		}
		tags.SampleRate = atoiOrZero(stream.SampleRate)
		// The stream bit rate is the nominal CBR value Rekordbox shows (128,
		// 320); the container rate includes tag and artwork overhead.
		if streamBitRate := atoiOrZero(stream.BitRate); streamBitRate > 0 {
			tags.BitRateKbps = int(math.Round(float64(streamBitRate) / 1000))
		}
		tags.BitDepth = stream.BitsPerSample
		if tags.BitDepth == 0 {
			tags.BitDepth = atoiOrZero(stream.BitsPerRawSample)
		}
		break
	}
	if tags.BitRateKbps == 0 && formatBitRate > 0 {
		tags.BitRateKbps = int(math.Round(float64(formatBitRate) / 1000))
	}
	if tags.BitDepth == 0 {
		// Rekordbox records 16 for lossy files that carry no sample depth.
		tags.BitDepth = 16
	}
	fallback := TagsFromFilename(path)
	if tags.Title == "" {
		tags.Title = fallback.Title
	}
	if tags.Artist == "" {
		tags.Artist = fallback.Artist
	}
	return tags, nil
}

// TagsFromFilename parses "Artist - Title.ext". Without a separator the whole
// stem is the title.
func TagsFromFilename(path string) Tags {
	stem := strings.TrimSuffix(filepath.Base(path), filepath.Ext(path))
	tags := Tags{Title: strings.TrimSpace(stem), Source: TagSourceFilename, BitDepth: 16}
	if artist, title, ok := strings.Cut(stem, " - "); ok && strings.TrimSpace(artist) != "" && strings.TrimSpace(title) != "" {
		tags.Artist = strings.TrimSpace(artist)
		tags.Title = strings.TrimSpace(title)
	}
	return tags
}

func leadingInt(raw string) int {
	raw = strings.TrimSpace(raw)
	end := 0
	for end < len(raw) && raw[end] >= '0' && raw[end] <= '9' {
		end++
	}
	if end == 0 {
		return 0
	}
	value, err := strconv.Atoi(raw[:end])
	if err != nil {
		return 0
	}
	return value
}

func atoiOrZero(raw string) int {
	value, err := strconv.Atoi(strings.TrimSpace(raw))
	if err != nil {
		return 0
	}
	return value
}
