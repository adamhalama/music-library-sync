// Package mediametadata preserves opaque library tags before promoted audio is installed.
// It deliberately copies container metadata rather than translating it through ffmpeg.
package mediametadata

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

const maxMetadata = 64 << 20

type Report struct {
	MetadataVerified bool   `json:"metadata_verified"`
	AudioSHA256      string `json:"audio_sha256"`
	ArtworkVerified  bool   `json:"artwork_verified"`
	DecodeVerified   bool   `json:"decode_verified"`
	FileSHA256       string `json:"file_sha256"`
}

// PreserveAndVerify restores the original library metadata onto a staged output.
// It never writes original. Failed stages must not be installed by the caller.
func PreserveAndVerify(ctx context.Context, original, stage string) (Report, error) {
	var report Report
	before, err := streamHashes(ctx, stage, "a")
	if err != nil {
		return report, fmt.Errorf("hash staged audio: %w", err)
	}
	ext := strings.ToLower(filepath.Ext(original))
	outputExt := strings.ToLower(filepath.Ext(stage))
	sameMP4 := (ext == ".m4a" || ext == ".mp4") && (outputExt == ".m4a" || outputExt == ".mp4")
	if ext != outputExt && !sameMP4 {
		return report, fmt.Errorf("complete metadata preservation requires the same container (%s)", ext)
	}
	switch ext {
	case ".m4a", ".mp4":
		err = preserveMP4(original, stage)
	case ".mp3", ".aac":
		err = preserveID3(original, stage)
	case ".wav":
		err = preserveWAV(original, stage)
	default:
		err = fmt.Errorf("complete metadata preservation unsupported for %s", ext)
	}
	if err != nil {
		return report, fmt.Errorf("preserve library metadata: %w", err)
	}
	after, err := streamHashes(ctx, stage, "a")
	if err != nil {
		return report, err
	}
	if !equalStrings(before, after) {
		return report, fmt.Errorf("metadata restoration changed encoded audio")
	}
	originalArt, err := streamHashes(ctx, original, "v")
	if err != nil {
		return report, err
	}
	stagedArt, err := streamHashes(ctx, stage, "v")
	if err != nil {
		return report, err
	}
	if !equalStrings(originalArt, stagedArt) {
		return report, fmt.Errorf("library artwork differs after restoration")
	}
	if _, err := command(ctx, "ffmpeg", "-v", "error", "-xerror", "-i", stage, "-map", "0:a", "-f", "null", "-"); err != nil {
		return report, fmt.Errorf("promoted audio decode: %w", err)
	}
	report = Report{MetadataVerified: true, AudioSHA256: strings.Join(after, ","), ArtworkVerified: true, DecodeVerified: true}
	report.FileSHA256, err = FileSHA256(stage)
	if err != nil {
		return Report{}, err
	}
	return report, nil
}

func equalStrings(a, b []string) bool { return strings.Join(a, "\x00") == strings.Join(b, "\x00") }
func command(ctx context.Context, bin string, args ...string) ([]byte, error) {
	out, err := exec.CommandContext(ctx, bin, args...).CombinedOutput()
	if err != nil {
		return nil, fmt.Errorf("%s: %w: %s", bin, err, strings.TrimSpace(string(out)))
	}
	return out, nil
}
func streamHashes(ctx context.Context, path, kind string) ([]string, error) {
	out, err := command(ctx, "ffprobe", "-v", "error", "-select_streams", kind, "-show_entries", "stream=index", "-of", "json", path)
	if err != nil {
		return nil, err
	}
	var probe struct {
		Streams []struct {
			Index int `json:"index"`
		} `json:"streams"`
	}
	if err := json.Unmarshal(out, &probe); err != nil {
		return nil, err
	}
	if kind == "a" && len(probe.Streams) == 0 {
		return nil, fmt.Errorf("no audio stream in staged media")
	}
	hashes := make([]string, 0, len(probe.Streams))
	for _, stream := range probe.Streams {
		out, err = command(ctx, "ffmpeg", "-v", "error", "-i", path, "-map", fmt.Sprintf("0:%d", stream.Index), "-c", "copy", "-f", "hash", "-hash", "sha256", "-")
		if err != nil {
			return nil, err
		}
		hash := strings.TrimPrefix(strings.TrimSpace(string(out)), "SHA256=")
		decoded, err := hex.DecodeString(hash)
		if err != nil || len(decoded) != sha256.Size {
			return nil, fmt.Errorf("invalid encoded stream hash")
		}
		hashes = append(hashes, hash)
	}
	return hashes, nil
}

// FileSHA256 is used to revalidate immutable inputs immediately before install.
func FileSHA256(path string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer f.Close()
	h := sha256.New()
	if _, err = io.Copy(h, f); err != nil {
		return "", err
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}
