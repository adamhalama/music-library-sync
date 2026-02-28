package cli

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestNormalizePromoteKey(t *testing.T) {
	got := normalizePromoteKey("PICHI - BO FUNK [FREE DL]")
	if got != "pichi bo funk" {
		t.Fatalf("unexpected normalized key: %q", got)
	}
}

func TestNormalizePromoteTargetFormat(t *testing.T) {
	got, err := normalizePromoteTargetFormat("mp3-320")
	if err != nil {
		t.Fatalf("normalize target format: %v", err)
	}
	if got != promoteTargetMP3320 {
		t.Fatalf("expected %q, got %q", promoteTargetMP3320, got)
	}
}

func TestScorePromoteMatchPrefersExact(t *testing.T) {
	lib := promoteMediaFile{
		Key:       "pichi bo funk",
		TitleKey:  "pichi bo funk",
		ArtistKey: "pichi",
		Tokens:    []string{"pichi", "bo", "funk"},
	}
	free := promoteMediaFile{
		Key:       "pichi bo funk",
		TitleKey:  "pichi bo funk",
		ArtistKey: "pichi",
		Tokens:    []string{"pichi", "bo", "funk"},
	}
	if score := scorePromoteMatch(lib, free); score != 100 {
		t.Fatalf("expected exact match score 100, got %d", score)
	}
}

func TestBuildPromoteAssignmentsUsesUniqueMatches(t *testing.T) {
	library := []promoteMediaFile{
		{Rel: "a.m4a", Key: "track one", Tokens: []string{"track", "one"}},
		{Rel: "b.m4a", Key: "track two", Tokens: []string{"track", "two"}},
	}
	free := []promoteMediaFile{
		{Rel: "x.wav", Key: "track one", Tokens: []string{"track", "one"}},
		{Rel: "y.wav", Key: "track two", Tokens: []string{"track", "two"}},
	}
	plan := buildPromoteAssignments(library, free, 70, 8)
	if len(plan.Assignments) != 2 {
		t.Fatalf("expected 2 assignments, got %d", len(plan.Assignments))
	}
}

func TestBuildPromoteAssignmentsMarksAmbiguousLibraryMatches(t *testing.T) {
	library := []promoteMediaFile{
		{Rel: "a.m4a", Key: "track one", Tokens: []string{"track", "one"}},
	}
	free := []promoteMediaFile{
		{Rel: "x.wav", Key: "track one final", Tokens: []string{"track", "one", "final"}},
		{Rel: "y.wav", Key: "track one edit", Tokens: []string{"track", "one", "edit"}},
	}
	plan := buildPromoteAssignments(library, free, 60, 8)
	if len(plan.Assignments) != 0 {
		t.Fatalf("expected ambiguous candidates to skip assignment, got %d", len(plan.Assignments))
	}
	if len(plan.Ambiguous) != 1 {
		t.Fatalf("expected one ambiguous match, got %d", len(plan.Ambiguous))
	}
}

func TestProbePromoteAudioWithTimeout(t *testing.T) {
	origProbe := probeAudioFn
	probeAudioFn = func(ctx context.Context, path string) (promoteAudioProbe, error) {
		<-ctx.Done()
		return promoteAudioProbe{}, ctx.Err()
	}
	t.Cleanup(func() {
		probeAudioFn = origProbe
	})

	_, err := probePromoteAudioWithTimeout(context.Background(), "x.wav", 10*time.Millisecond)
	if err == nil {
		t.Fatalf("expected probe timeout error")
	}
}

func TestIsHighQualityLossySourceUsesEffectiveBitrateFallback(t *testing.T) {
	opts := promoteFreeDLOptions{
		MinAACKbps:  256,
		MinMP3Kbps:  320,
		MinOpusKbps: 192,
	}
	probe := promoteAudioProbe{
		Codec:            "aac",
		Bitrate:          0,
		FormatBitrate:    0,
		EffectiveBitrate: 256000,
	}
	if !isHighQualityLossySource(opts, probe) {
		t.Fatalf("expected effective bitrate to satisfy AAC quality threshold")
	}
}

func TestNormalizePromoteURLKey(t *testing.T) {
	got := normalizePromoteURLKey("https://soundcloud.com/PICHI/BOFUNK?utm_source=test#frag")
	if got != "https://soundcloud.com/PICHI/BOFUNK" {
		t.Fatalf("unexpected normalized URL key: %q", got)
	}
}

func TestPromoteFreeDLPreviewModePlansWithoutWriting(t *testing.T) {
	tmp := t.TempDir()
	freeDir := filepath.Join(tmp, "free")
	libraryDir := filepath.Join(tmp, "library")
	if err := os.MkdirAll(freeDir, 0o755); err != nil {
		t.Fatalf("mkdir free: %v", err)
	}
	if err := os.MkdirAll(libraryDir, 0o755); err != nil {
		t.Fatalf("mkdir library: %v", err)
	}
	freePath := filepath.Join(freeDir, "PICHI - BO FUNK [FREE DL].wav")
	libraryPath := filepath.Join(libraryDir, "PICHI - BO FUNK.m4a")
	if err := os.WriteFile(freePath, []byte("source"), 0o644); err != nil {
		t.Fatalf("write free file: %v", err)
	}
	if err := os.WriteFile(libraryPath, []byte("target"), 0o644); err != nil {
		t.Fatalf("write library file: %v", err)
	}

	origLookPath := lookPathFn
	origProbe := probeAudioFn
	origRun := runPromoteFFmpeg
	lookPathFn = func(bin string) (string, error) { return "/usr/bin/" + bin, nil }
	probeAudioFn = func(ctx context.Context, path string) (promoteAudioProbe, error) {
		switch strings.ToLower(filepath.Ext(path)) {
		case ".wav":
			return promoteAudioProbe{Codec: "pcm_s16le", Bitrate: 0}, nil
		default:
			return promoteAudioProbe{Codec: "aac", Bitrate: 192000}, nil
		}
	}
	runPromoteFFmpeg = func(ctx context.Context, opts promoteFreeDLOptions, assignment promoteAssignment, outputPath string, decision promoteDecision) error {
		t.Fatalf("did not expect ffmpeg invocation in preview mode")
		return nil
	}
	t.Cleanup(func() {
		lookPathFn = origLookPath
		probeAudioFn = origProbe
		runPromoteFFmpeg = origRun
	})

	stdout := &bytes.Buffer{}
	stderr := &bytes.Buffer{}
	app := &AppContext{
		Build: BuildInfo{Version: "test"},
		IO:    IOStreams{In: strings.NewReader(""), Out: stdout, ErrOut: stderr},
	}
	root := newRootCommand(app)
	root.SetArgs([]string{
		"promote-freedl",
		"--free-dl-dir", freeDir,
		"--library-dir", libraryDir,
		"--probe-timeout", "20ms",
	})

	if err := root.Execute(); err != nil {
		t.Fatalf("promote-freedl preview failed: %v", err)
	}
	if !strings.Contains(stdout.String(), "[plan]") {
		t.Fatalf("expected plan output, got: %s", stdout.String())
	}
}

func TestPromoteFreeDLApplyWritesToSeparateDirectory(t *testing.T) {
	tmp := t.TempDir()
	freeDir := filepath.Join(tmp, "free")
	libraryDir := filepath.Join(tmp, "library")
	writeDir := filepath.Join(tmp, "out")
	if err := os.MkdirAll(freeDir, 0o755); err != nil {
		t.Fatalf("mkdir free: %v", err)
	}
	if err := os.MkdirAll(libraryDir, 0o755); err != nil {
		t.Fatalf("mkdir library: %v", err)
	}
	freePath := filepath.Join(freeDir, "ninnidslvx - FUJI (MSTR3).wav")
	libraryPath := filepath.Join(libraryDir, "ninnidslvx - FUJI.m4a")
	if err := os.WriteFile(freePath, []byte("source"), 0o644); err != nil {
		t.Fatalf("write free file: %v", err)
	}
	if err := os.WriteFile(libraryPath, []byte("target"), 0o644); err != nil {
		t.Fatalf("write library file: %v", err)
	}

	origLookPath := lookPathFn
	origProbe := probeAudioFn
	origRun := runPromoteFFmpeg
	lookPathFn = func(bin string) (string, error) { return "/usr/bin/" + bin, nil }
	probeAudioFn = func(ctx context.Context, path string) (promoteAudioProbe, error) {
		switch strings.ToLower(filepath.Ext(path)) {
		case ".wav":
			return promoteAudioProbe{Codec: "pcm_s16le", Bitrate: 0}, nil
		default:
			return promoteAudioProbe{Codec: "aac", Bitrate: 192000}, nil
		}
	}
	runPromoteFFmpeg = func(ctx context.Context, opts promoteFreeDLOptions, assignment promoteAssignment, outputPath string, decision promoteDecision) error {
		if err := os.MkdirAll(filepath.Dir(outputPath), 0o755); err != nil {
			return err
		}
		return os.WriteFile(outputPath, []byte("upgraded"), 0o644)
	}
	t.Cleanup(func() {
		lookPathFn = origLookPath
		probeAudioFn = origProbe
		runPromoteFFmpeg = origRun
	})

	stdout := &bytes.Buffer{}
	stderr := &bytes.Buffer{}
	app := &AppContext{
		Build: BuildInfo{Version: "test"},
		IO:    IOStreams{In: strings.NewReader(""), Out: stdout, ErrOut: stderr},
	}
	root := newRootCommand(app)
	root.SetArgs([]string{
		"promote-freedl",
		"--free-dl-dir", freeDir,
		"--library-dir", libraryDir,
		"--write-dir", writeDir,
		"--apply",
		"--probe-timeout", "20ms",
	})

	if err := root.Execute(); err != nil {
		t.Fatalf("promote-freedl apply failed: %v", err)
	}
	outPath := filepath.Join(writeDir, "ninnidslvx - FUJI.m4a")
	if _, err := os.Stat(outPath); err != nil {
		t.Fatalf("expected output file at %s: %v", outPath, err)
	}
}

func TestPromoteFreeDLApplyTargetFormatMP3WritesMP3Output(t *testing.T) {
	tmp := t.TempDir()
	freeDir := filepath.Join(tmp, "free")
	libraryDir := filepath.Join(tmp, "library")
	writeDir := filepath.Join(tmp, "out")
	if err := os.MkdirAll(freeDir, 0o755); err != nil {
		t.Fatalf("mkdir free: %v", err)
	}
	if err := os.MkdirAll(libraryDir, 0o755); err != nil {
		t.Fatalf("mkdir library: %v", err)
	}
	freePath := filepath.Join(freeDir, "track one.wav")
	libraryPath := filepath.Join(libraryDir, "track one.m4a")
	if err := os.WriteFile(freePath, []byte("source"), 0o644); err != nil {
		t.Fatalf("write free file: %v", err)
	}
	if err := os.WriteFile(libraryPath, []byte("target"), 0o644); err != nil {
		t.Fatalf("write library file: %v", err)
	}

	origLookPath := lookPathFn
	origProbe := probeAudioFn
	origRun := runPromoteFFmpeg
	lookPathFn = func(bin string) (string, error) { return "/usr/bin/" + bin, nil }
	probeAudioFn = func(ctx context.Context, path string) (promoteAudioProbe, error) {
		switch strings.ToLower(filepath.Ext(path)) {
		case ".wav":
			return promoteAudioProbe{Codec: "pcm_s16le", Bitrate: 0}, nil
		default:
			return promoteAudioProbe{Codec: "aac", Bitrate: 192000}, nil
		}
	}
	runPromoteFFmpeg = func(ctx context.Context, opts promoteFreeDLOptions, assignment promoteAssignment, outputPath string, decision promoteDecision) error {
		if filepath.Ext(outputPath) != ".mp3" {
			t.Fatalf("expected mp3 output extension, got %s", outputPath)
		}
		if err := os.MkdirAll(filepath.Dir(outputPath), 0o755); err != nil {
			return err
		}
		return os.WriteFile(outputPath, []byte("upgraded"), 0o644)
	}
	t.Cleanup(func() {
		lookPathFn = origLookPath
		probeAudioFn = origProbe
		runPromoteFFmpeg = origRun
	})

	stdout := &bytes.Buffer{}
	stderr := &bytes.Buffer{}
	app := &AppContext{
		Build: BuildInfo{Version: "test"},
		IO:    IOStreams{In: strings.NewReader(""), Out: stdout, ErrOut: stderr},
	}
	root := newRootCommand(app)
	root.SetArgs([]string{
		"promote-freedl",
		"--free-dl-dir", freeDir,
		"--library-dir", libraryDir,
		"--write-dir", writeDir,
		"--target-format", "mp3-320",
		"--apply",
		"--probe-timeout", "20ms",
	})

	if err := root.Execute(); err != nil {
		t.Fatalf("promote-freedl target-format mp3-320 failed: %v", err)
	}
	outPath := filepath.Join(writeDir, "track one.mp3")
	if _, err := os.Stat(outPath); err != nil {
		t.Fatalf("expected output file at %s: %v", outPath, err)
	}
}

func TestPromoteFreeDLApplyTargetFormatWithoutWriteDirSkipsMismatchedExtensions(t *testing.T) {
	tmp := t.TempDir()
	freeDir := filepath.Join(tmp, "free")
	libraryDir := filepath.Join(tmp, "library")
	if err := os.MkdirAll(freeDir, 0o755); err != nil {
		t.Fatalf("mkdir free: %v", err)
	}
	if err := os.MkdirAll(libraryDir, 0o755); err != nil {
		t.Fatalf("mkdir library: %v", err)
	}
	freePath := filepath.Join(freeDir, "track one.wav")
	libraryPath := filepath.Join(libraryDir, "track one.m4a")
	if err := os.WriteFile(freePath, []byte("source"), 0o644); err != nil {
		t.Fatalf("write free file: %v", err)
	}
	if err := os.WriteFile(libraryPath, []byte("target"), 0o644); err != nil {
		t.Fatalf("write library file: %v", err)
	}

	origLookPath := lookPathFn
	origProbe := probeAudioFn
	origRun := runPromoteFFmpeg
	lookPathFn = func(bin string) (string, error) { return "/usr/bin/" + bin, nil }
	probeAudioFn = func(ctx context.Context, path string) (promoteAudioProbe, error) {
		return promoteAudioProbe{Codec: "pcm_s16le", Bitrate: 0}, nil
	}
	runPromoteFFmpeg = func(ctx context.Context, opts promoteFreeDLOptions, assignment promoteAssignment, outputPath string, decision promoteDecision) error {
		t.Fatalf("did not expect ffmpeg run when target ext policy mismatches")
		return nil
	}
	t.Cleanup(func() {
		lookPathFn = origLookPath
		probeAudioFn = origProbe
		runPromoteFFmpeg = origRun
	})

	stdout := &bytes.Buffer{}
	stderr := &bytes.Buffer{}
	app := &AppContext{
		Build: BuildInfo{Version: "test"},
		IO:    IOStreams{In: strings.NewReader(""), Out: stdout, ErrOut: stderr},
	}
	root := newRootCommand(app)
	root.SetArgs([]string{
		"promote-freedl",
		"--free-dl-dir", freeDir,
		"--library-dir", libraryDir,
		"--target-format", "mp3-320",
		"--apply",
		"--probe-timeout", "20ms",
	})

	err := root.Execute()
	if err != nil {
		t.Fatalf("promote-freedl should skip mismatched in-place target policy, got %v", err)
	}
	if !strings.Contains(stdout.String(), "summary") {
		t.Fatalf("expected summary output, got: %s", stdout.String())
	}
}

func TestPromoteFreeDLMatchesByTitleInsteadOfFilename(t *testing.T) {
	tmp := t.TempDir()
	freeDir := filepath.Join(tmp, "free")
	libraryDir := filepath.Join(tmp, "library")
	if err := os.MkdirAll(freeDir, 0o755); err != nil {
		t.Fatalf("mkdir free: %v", err)
	}
	if err := os.MkdirAll(libraryDir, 0o755); err != nil {
		t.Fatalf("mkdir library: %v", err)
	}
	freePath := filepath.Join(freeDir, "aaa-raw-export-123.wav")
	libraryPath := filepath.Join(libraryDir, "zzz-old-rip-789.m4a")
	if err := os.WriteFile(freePath, []byte("source"), 0o644); err != nil {
		t.Fatalf("write free file: %v", err)
	}
	if err := os.WriteFile(libraryPath, []byte("target"), 0o644); err != nil {
		t.Fatalf("write library file: %v", err)
	}

	origLookPath := lookPathFn
	origProbe := probeAudioFn
	origTags := probeTagsFn
	origRun := runPromoteFFmpeg
	lookPathFn = func(bin string) (string, error) { return "/usr/bin/" + bin, nil }
	probeAudioFn = func(ctx context.Context, path string) (promoteAudioProbe, error) {
		switch strings.ToLower(filepath.Ext(path)) {
		case ".wav":
			return promoteAudioProbe{Codec: "pcm_s16le", Bitrate: 0}, nil
		default:
			return promoteAudioProbe{Codec: "aac", Bitrate: 192000}, nil
		}
	}
	probeTagsFn = func(ctx context.Context, path string) (promoteTagProbe, error) {
		return promoteTagProbe{
			Title:  "PICHI - BO FUNK",
			Artist: "PICHI",
		}, nil
	}
	runPromoteFFmpeg = func(ctx context.Context, opts promoteFreeDLOptions, assignment promoteAssignment, outputPath string, decision promoteDecision) error {
		t.Fatalf("did not expect ffmpeg invocation in preview mode")
		return nil
	}
	t.Cleanup(func() {
		lookPathFn = origLookPath
		probeAudioFn = origProbe
		probeTagsFn = origTags
		runPromoteFFmpeg = origRun
	})

	stdout := &bytes.Buffer{}
	stderr := &bytes.Buffer{}
	app := &AppContext{
		Build: BuildInfo{Version: "test"},
		IO:    IOStreams{In: strings.NewReader(""), Out: stdout, ErrOut: stderr},
	}
	root := newRootCommand(app)
	root.SetArgs([]string{
		"promote-freedl",
		"--free-dl-dir", freeDir,
		"--library-dir", libraryDir,
		"--probe-timeout", "20ms",
	})

	if err := root.Execute(); err != nil {
		t.Fatalf("promote-freedl title-match preview failed: %v", err)
	}
	if !strings.Contains(stdout.String(), "[plan] zzz-old-rip-789.m4a <= aaa-raw-export-123.wav") {
		t.Fatalf("expected planned match via title metadata, got: %s", stdout.String())
	}
}

func TestPromoteFreeDLReportsAmbiguousMatchSkip(t *testing.T) {
	tmp := t.TempDir()
	freeDir := filepath.Join(tmp, "free")
	libraryDir := filepath.Join(tmp, "library")
	if err := os.MkdirAll(freeDir, 0o755); err != nil {
		t.Fatalf("mkdir free: %v", err)
	}
	if err := os.MkdirAll(libraryDir, 0o755); err != nil {
		t.Fatalf("mkdir library: %v", err)
	}
	if err := os.WriteFile(filepath.Join(freeDir, "alt-a.wav"), []byte("source"), 0o644); err != nil {
		t.Fatalf("write free A: %v", err)
	}
	if err := os.WriteFile(filepath.Join(freeDir, "alt-b.wav"), []byte("source"), 0o644); err != nil {
		t.Fatalf("write free B: %v", err)
	}
	if err := os.WriteFile(filepath.Join(libraryDir, "target.m4a"), []byte("target"), 0o644); err != nil {
		t.Fatalf("write library file: %v", err)
	}

	origLookPath := lookPathFn
	origProbe := probeAudioFn
	origTags := probeTagsFn
	origRun := runPromoteFFmpeg
	lookPathFn = func(bin string) (string, error) { return "/usr/bin/" + bin, nil }
	probeAudioFn = func(ctx context.Context, path string) (promoteAudioProbe, error) {
		switch strings.ToLower(filepath.Ext(path)) {
		case ".wav":
			return promoteAudioProbe{Codec: "pcm_s16le", EffectiveBitrate: 1411000}, nil
		default:
			return promoteAudioProbe{Codec: "aac", EffectiveBitrate: 192000}, nil
		}
	}
	probeTagsFn = func(ctx context.Context, path string) (promoteTagProbe, error) {
		return promoteTagProbe{
			Title:  "PICHI - BO FUNK",
			Artist: "PICHI",
		}, nil
	}
	runPromoteFFmpeg = func(ctx context.Context, opts promoteFreeDLOptions, assignment promoteAssignment, outputPath string, decision promoteDecision) error {
		t.Fatalf("did not expect ffmpeg invocation in preview mode")
		return nil
	}
	t.Cleanup(func() {
		lookPathFn = origLookPath
		probeAudioFn = origProbe
		probeTagsFn = origTags
		runPromoteFFmpeg = origRun
	})

	stdout := &bytes.Buffer{}
	stderr := &bytes.Buffer{}
	app := &AppContext{
		Build: BuildInfo{Version: "test"},
		IO:    IOStreams{In: strings.NewReader(""), Out: stdout, ErrOut: stderr},
	}
	root := newRootCommand(app)
	root.SetArgs([]string{
		"promote-freedl",
		"--free-dl-dir", freeDir,
		"--library-dir", libraryDir,
		"--probe-timeout", "20ms",
	})

	if err := root.Execute(); err != nil {
		t.Fatalf("promote-freedl ambiguous-match preview failed: %v", err)
	}
	if !strings.Contains(stdout.String(), "ambiguous-match top=") {
		t.Fatalf("expected ambiguous-match skip output, got: %s", stdout.String())
	}
}

func TestPromoteFreeDLApplyInPlaceAutoReplacesLibraryPath(t *testing.T) {
	tmp := t.TempDir()
	freeDir := filepath.Join(tmp, "free")
	libraryDir := filepath.Join(tmp, "library")
	if err := os.MkdirAll(freeDir, 0o755); err != nil {
		t.Fatalf("mkdir free: %v", err)
	}
	if err := os.MkdirAll(libraryDir, 0o755); err != nil {
		t.Fatalf("mkdir library: %v", err)
	}
	freePath := filepath.Join(freeDir, "track one.wav")
	libraryPath := filepath.Join(libraryDir, "track one.m4a")
	if err := os.WriteFile(freePath, []byte("source"), 0o644); err != nil {
		t.Fatalf("write free file: %v", err)
	}
	if err := os.WriteFile(libraryPath, []byte("target"), 0o644); err != nil {
		t.Fatalf("write library file: %v", err)
	}

	origLookPath := lookPathFn
	origProbe := probeAudioFn
	origTags := probeTagsFn
	origRun := runPromoteFFmpeg
	lookPathFn = func(bin string) (string, error) { return "/usr/bin/" + bin, nil }
	probeAudioFn = func(ctx context.Context, path string) (promoteAudioProbe, error) {
		switch strings.ToLower(filepath.Ext(path)) {
		case ".wav":
			return promoteAudioProbe{Codec: "pcm_s16le", Bitrate: 0}, nil
		default:
			return promoteAudioProbe{Codec: "aac", Bitrate: 192000}, nil
		}
	}
	probeTagsFn = func(ctx context.Context, path string) (promoteTagProbe, error) {
		return promoteTagProbe{}, nil
	}
	runPromoteFFmpeg = func(ctx context.Context, opts promoteFreeDLOptions, assignment promoteAssignment, outputPath string, decision promoteDecision) error {
		if err := os.MkdirAll(filepath.Dir(outputPath), 0o755); err != nil {
			return err
		}
		return os.WriteFile(outputPath, []byte("upgraded"), 0o644)
	}
	t.Cleanup(func() {
		lookPathFn = origLookPath
		probeAudioFn = origProbe
		probeTagsFn = origTags
		runPromoteFFmpeg = origRun
	})

	stdout := &bytes.Buffer{}
	stderr := &bytes.Buffer{}
	app := &AppContext{
		Build: BuildInfo{Version: "test"},
		IO:    IOStreams{In: strings.NewReader(""), Out: stdout, ErrOut: stderr},
	}
	root := newRootCommand(app)
	root.SetArgs([]string{
		"promote-freedl",
		"--free-dl-dir", freeDir,
		"--library-dir", libraryDir,
		"--target-format", "auto",
		"--apply",
		"--probe-timeout", "20ms",
	})

	if err := root.Execute(); err != nil {
		t.Fatalf("promote-freedl in-place apply failed: %v", err)
	}
	payload, err := os.ReadFile(libraryPath)
	if err != nil {
		t.Fatalf("read replaced file: %v", err)
	}
	if string(payload) != "upgraded" {
		t.Fatalf("expected in-place file replacement, got %q", string(payload))
	}
}
