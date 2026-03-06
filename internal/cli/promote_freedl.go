package cli

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"
	"unicode"

	"github.com/jaa/update-downloads/internal/config"
	"github.com/jaa/update-downloads/internal/exitcode"
	"github.com/jaa/update-downloads/internal/fileops"
	"github.com/spf13/cobra"
)

const (
	promoteCodecAAC = "aac"
	promoteCodecMP3 = "mp3"
	promoteCodecWAV = "wav"
)

const (
	promoteTargetAuto   = "auto"
	promoteTargetWAV    = "wav"
	promoteTargetMP3320 = "mp3-320"
	promoteTargetAAC256 = "aac-256"
)

var (
	lookPathFn       = exec.LookPath
	probeAudioFn     = probePromoteAudio
	probeTagsFn      = probePromoteTags
	runPromoteFFmpeg = runPromoteFFmpegCommand
)

type promoteFreeDLOptions struct {
	FreeDLDir     string
	LibraryDir    string
	WriteDir      string
	TargetFormat  string
	ProbeTimeout  time.Duration
	Apply         bool
	Overwrite     bool
	MinMatchScore int
	MP3Bitrate    string
	AACBitrate    string
	MinMP3Kbps    int
	MinAACKbps    int
	MinOpusKbps   int
	ReplaceLimit  int
	AmbiguityGap  int
}

type promoteMediaFile struct {
	Path         string
	Rel          string
	Ext          string
	MatchName    string
	Title        string
	Artist       string
	Comment      string
	Key          string
	TitleKey     string
	ArtistKey    string
	SourceURLKey string
	Tokens       []string
}

type promoteAudioProbe struct {
	Codec            string
	Bitrate          int
	FormatBitrate    int
	EffectiveBitrate int
}

type promotePairCandidate struct {
	LibraryIdx int
	FreeDLIdx  int
	Score      int
}

type promoteAssignment struct {
	Library promoteMediaFile
	FreeDL  promoteMediaFile
	Score   int
}

type promoteAmbiguousMatch struct {
	Library     promoteMediaFile
	Best        promoteMediaFile
	Alternative promoteMediaFile
	BestScore   int
	AltScore    int
}

type promoteAssignmentPlan struct {
	Assignments []promoteAssignment
	Ambiguous   []promoteAmbiguousMatch
}

type promoteTagProbe struct {
	Title   string
	Artist  string
	Comment string
}

type promoteActionMode string

const (
	promoteActionSkip      promoteActionMode = "skip"
	promoteActionCopyAudio promoteActionMode = "copy-audio"
	promoteActionEncodeAAC promoteActionMode = "encode-aac"
	promoteActionEncodeMP3 promoteActionMode = "encode-mp3"
	promoteActionEncodeWAV promoteActionMode = "encode-wav"
)

type promoteDecision struct {
	Mode   promoteActionMode
	Reason string
}

type promoteTargetPolicy struct {
	DesiredCodec   string
	OutputExt      string
	LosslessAction promoteActionMode
}

func newPromoteFreeDLCommand(app *AppContext) *cobra.Command {
	opts := promoteFreeDLOptions{
		MinMatchScore: 72,
		MP3Bitrate:    "320k",
		AACBitrate:    "256k",
		MinMP3Kbps:    320,
		MinAACKbps:    256,
		MinOpusKbps:   192,
		TargetFormat:  promoteTargetAuto,
		ProbeTimeout:  2 * time.Second,
		AmbiguityGap:  8,
	}

	cmd := &cobra.Command{
		Use:   "promote-freedl",
		Short: "Match free-DL captures to a library and promote higher-quality audio",
		Long: "Preview or apply a separate post-processing flow that matches tracks from a free-download folder " +
			"to a target library folder and replaces target audio with higher-quality sources.",
		RunE: func(cmd *cobra.Command, args []string) error {
			if strings.TrimSpace(opts.FreeDLDir) == "" {
				return withExitCode(exitcode.InvalidUsage, fmt.Errorf("--free-dl-dir is required"))
			}
			if strings.TrimSpace(opts.LibraryDir) == "" {
				return withExitCode(exitcode.InvalidUsage, fmt.Errorf("--library-dir is required"))
			}
			if opts.MinMatchScore < 0 || opts.MinMatchScore > 100 {
				return withExitCode(exitcode.InvalidUsage, fmt.Errorf("--min-match-score must be between 0 and 100"))
			}
			if opts.ReplaceLimit < 0 {
				return withExitCode(exitcode.InvalidUsage, fmt.Errorf("--replace-limit must be >= 0"))
			}
			if opts.AmbiguityGap < 0 {
				return withExitCode(exitcode.InvalidUsage, fmt.Errorf("--ambiguity-gap must be >= 0"))
			}
			if opts.ProbeTimeout <= 0 {
				return withExitCode(exitcode.InvalidUsage, fmt.Errorf("--probe-timeout must be > 0"))
			}
			targetFormat, err := normalizePromoteTargetFormat(opts.TargetFormat)
			if err != nil {
				return withExitCode(exitcode.InvalidUsage, err)
			}
			opts.TargetFormat = targetFormat

			freeDLDir, err := config.ExpandPath(opts.FreeDLDir)
			if err != nil {
				return withExitCode(exitcode.InvalidUsage, fmt.Errorf("resolve --free-dl-dir: %w", err))
			}
			libraryDir, err := config.ExpandPath(opts.LibraryDir)
			if err != nil {
				return withExitCode(exitcode.InvalidUsage, fmt.Errorf("resolve --library-dir: %w", err))
			}
			writeDir := ""
			if strings.TrimSpace(opts.WriteDir) != "" {
				writeDir, err = config.ExpandPath(opts.WriteDir)
				if err != nil {
					return withExitCode(exitcode.InvalidUsage, fmt.Errorf("resolve --write-dir: %w", err))
				}
			}

			if err := ensurePromoteDependencies(); err != nil {
				return withExitCode(exitcode.MissingDependency, err)
			}

			ctx := cmd.Context()
			if ctx == nil {
				ctx = context.Background()
			}

			fmt.Fprintf(app.IO.Out, "promote-freedl: indexing free-dl titles in %s\n", freeDLDir)
			freeDLFiles, err := collectPromoteMediaFiles(ctx, freeDLDir, opts.ProbeTimeout)
			if err != nil {
				return withExitCode(exitcode.RuntimeFailure, fmt.Errorf("scan free-dl directory: %w", err))
			}
			fmt.Fprintf(app.IO.Out, "promote-freedl: indexed free-dl files=%d\n", len(freeDLFiles))
			fmt.Fprintf(app.IO.Out, "promote-freedl: indexing library titles in %s\n", libraryDir)
			libraryFiles, err := collectPromoteMediaFiles(ctx, libraryDir, opts.ProbeTimeout)
			if err != nil {
				return withExitCode(exitcode.RuntimeFailure, fmt.Errorf("scan library directory: %w", err))
			}
			fmt.Fprintf(app.IO.Out, "promote-freedl: indexed library files=%d\n", len(libraryFiles))
			if len(freeDLFiles) == 0 {
				fmt.Fprintln(app.IO.Out, "promote-freedl: no media files found in --free-dl-dir")
				return nil
			}
			if len(libraryFiles) == 0 {
				fmt.Fprintln(app.IO.Out, "promote-freedl: no media files found in --library-dir")
				return nil
			}

			matchPlan := buildPromoteAssignments(libraryFiles, freeDLFiles, opts.MinMatchScore, opts.AmbiguityGap)
			assignments := matchPlan.Assignments
			previewMode := app.Opts.DryRun || !opts.Apply
			if !opts.Apply && !app.Opts.DryRun {
				fmt.Fprintln(app.IO.Out, "promote-freedl: preview mode (set --apply to write changes)")
			}
			fmt.Fprintf(
				app.IO.Out,
				"promote-freedl: free_dl=%d library=%d matched=%d ambiguous=%d min_match_score=%d ambiguity_gap=%d mode=%s\n",
				len(freeDLFiles),
				len(libraryFiles),
				len(assignments),
				len(matchPlan.Ambiguous),
				opts.MinMatchScore,
				opts.AmbiguityGap,
				map[bool]string{true: "preview", false: "apply"}[previewMode],
			)

			planned := 0
			replaced := 0
			skipped := len(matchPlan.Ambiguous)
			failed := 0
			processed := 0
			for _, ambiguous := range matchPlan.Ambiguous {
				fmt.Fprintf(
					app.IO.Out,
					"[skip] %s (ambiguous-match top=%d second=%d best=%s alt=%s)\n",
					ambiguous.Library.Rel,
					ambiguous.BestScore,
					ambiguous.AltScore,
					ambiguous.Best.Rel,
					ambiguous.Alternative.Rel,
				)
			}
			for _, assignment := range assignments {
				if opts.ReplaceLimit > 0 && processed >= opts.ReplaceLimit {
					break
				}
				processed++

				sourceProbe, sourceProbeErr := probePromoteAudioWithTimeout(ctx, assignment.FreeDL.Path, opts.ProbeTimeout)
				if sourceProbeErr != nil {
					skipped++
					if app.Opts.Verbose {
						fmt.Fprintf(
							app.IO.ErrOut,
							"[skip] %s <= %s (probe-source-failed: %v)\n",
							assignment.Library.Rel,
							assignment.FreeDL.Rel,
							sourceProbeErr,
						)
					}
					continue
				}

				decision := decidePromoteAction(opts, assignment, sourceProbe)
				if decision.Mode == promoteActionSkip {
					skipped++
					if app.Opts.Verbose {
						fmt.Fprintf(
							app.IO.Out,
							"[skip] %s <= %s (%s)\n",
							assignment.Library.Rel,
							assignment.FreeDL.Rel,
							decision.Reason,
						)
					}
					continue
				}
				planned++

				outputPath, outputErr := resolvePromoteOutputPath(opts, assignment, writeDir)
				if outputErr != nil {
					skipped++
					if app.Opts.Verbose {
						fmt.Fprintf(
							app.IO.Out,
							"[skip] %s <= %s (output-policy: %v)\n",
							assignment.Library.Rel,
							assignment.FreeDL.Rel,
							outputErr,
						)
					}
					continue
				}

				if previewMode {
					fmt.Fprintf(
						app.IO.Out,
						"[plan] %s <= %s (score=%d mode=%s)\n",
						assignment.Library.Rel,
						assignment.FreeDL.Rel,
						assignment.Score,
						decision.Mode,
					)
					continue
				}

				if writeDir != "" && !opts.Overwrite {
					if _, statErr := os.Stat(outputPath); statErr == nil {
						skipped++
						if app.Opts.Verbose {
							fmt.Fprintf(
								app.IO.Out,
								"[skip] %s (output exists; use --overwrite)\n",
								assignment.Library.Rel,
							)
						}
						continue
					} else if statErr != nil && !errors.Is(statErr, os.ErrNotExist) {
						failed++
						fmt.Fprintf(
							app.IO.ErrOut,
							"[fail] %s <= %s (stat output: %v)\n",
							assignment.Library.Rel,
							assignment.FreeDL.Rel,
							statErr,
						)
						continue
					}
				}

				if applyErr := applyPromoteReplacement(ctx, opts, assignment, outputPath, decision); applyErr != nil {
					failed++
					fmt.Fprintf(
						app.IO.ErrOut,
						"[fail] %s <= %s (%v)\n",
						assignment.Library.Rel,
						assignment.FreeDL.Rel,
						applyErr,
					)
					continue
				}
				replaced++
				fmt.Fprintf(
					app.IO.Out,
					"[done] %s <= %s (score=%d mode=%s)\n",
					assignment.Library.Rel,
					assignment.FreeDL.Rel,
					assignment.Score,
					decision.Mode,
				)
			}

			fmt.Fprintf(
				app.IO.Out,
				"promote-freedl: summary planned=%d replaced=%d skipped=%d failed=%d\n",
				planned,
				replaced,
				skipped,
				failed,
			)
			if failed > 0 {
				return withExitCode(exitcode.PartialSuccess, fmt.Errorf("promote-freedl completed with %d failure(s)", failed))
			}
			return nil
		},
	}

	cmd.Flags().StringVar(&opts.FreeDLDir, "free-dl-dir", "", "Directory containing downloaded free-DL files (required)")
	cmd.Flags().StringVar(&opts.LibraryDir, "library-dir", "", "Target library directory to match and upgrade (required)")
	cmd.Flags().StringVar(&opts.WriteDir, "write-dir", "", "Optional output directory for upgraded files (keeps --library-dir untouched)")
	cmd.Flags().StringVar(&opts.TargetFormat, "target-format", opts.TargetFormat, "Target output format: auto, wav, mp3-320, or aac-256")
	cmd.Flags().BoolVar(&opts.Apply, "apply", false, "Apply changes (default is preview-only)")
	cmd.Flags().BoolVar(&opts.Overwrite, "overwrite", false, "Overwrite existing files in --write-dir")
	cmd.Flags().IntVar(&opts.MinMatchScore, "min-match-score", opts.MinMatchScore, "Minimum fuzzy match score (0-100)")
	cmd.Flags().StringVar(&opts.AACBitrate, "aac-bitrate", opts.AACBitrate, "AAC bitrate used for encoded replacements")
	cmd.Flags().StringVar(&opts.MP3Bitrate, "mp3-bitrate", opts.MP3Bitrate, "MP3 bitrate used for encoded replacements")
	cmd.Flags().DurationVar(&opts.ProbeTimeout, "probe-timeout", opts.ProbeTimeout, "Per-file ffprobe timeout for title/audio probing")
	cmd.Flags().IntVar(&opts.MinAACKbps, "min-aac-kbps", opts.MinAACKbps, "Minimum AAC bitrate treated as high-quality lossy source")
	cmd.Flags().IntVar(&opts.MinMP3Kbps, "min-mp3-kbps", opts.MinMP3Kbps, "Minimum MP3 bitrate treated as high-quality lossy source")
	cmd.Flags().IntVar(&opts.MinOpusKbps, "min-opus-kbps", opts.MinOpusKbps, "Minimum Opus/Vorbis bitrate treated as high-quality lossy source")
	cmd.Flags().IntVar(&opts.AmbiguityGap, "ambiguity-gap", opts.AmbiguityGap, "Minimum score gap between top two candidates; lower gaps are skipped as ambiguous (0 disables)")
	cmd.Flags().IntVar(&opts.ReplaceLimit, "replace-limit", 0, "Limit number of matched replacements (0 = no limit)")

	return cmd
}

func ensurePromoteDependencies() error {
	for _, bin := range []string{"ffprobe", "ffmpeg"} {
		if _, err := lookPathFn(bin); err != nil {
			return fmt.Errorf("required dependency %q not found in PATH", bin)
		}
	}
	return nil
}

func collectPromoteMediaFiles(ctx context.Context, root string, probeTimeout time.Duration) ([]promoteMediaFile, error) {
	trimmedRoot := strings.TrimSpace(root)
	if trimmedRoot == "" {
		return nil, fmt.Errorf("empty root path")
	}
	info, err := os.Stat(trimmedRoot)
	if err != nil {
		return nil, err
	}
	if !info.IsDir() {
		return nil, fmt.Errorf("path is not a directory: %s", trimmedRoot)
	}

	files := make([]promoteMediaFile, 0)
	err = filepath.WalkDir(trimmedRoot, func(path string, d os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if d.IsDir() {
			return nil
		}
		ext := strings.ToLower(filepath.Ext(d.Name()))
		if !isPromoteMediaExt(ext) {
			return nil
		}
		base := strings.TrimSpace(strings.TrimSuffix(d.Name(), filepath.Ext(d.Name())))
		tags, tagsErr := probePromoteTagsWithTimeout(ctx, path, probeTimeout)
		matchName := strings.TrimSpace(tags.Title)
		if tagsErr != nil || matchName == "" {
			matchName = base
		}
		key := normalizePromoteKey(matchName)
		if key == "" {
			return nil
		}
		rel, relErr := filepath.Rel(trimmedRoot, path)
		if relErr != nil {
			return relErr
		}
		files = append(files, promoteMediaFile{
			Path:         path,
			Rel:          filepath.ToSlash(rel),
			Ext:          ext,
			MatchName:    matchName,
			Title:        strings.TrimSpace(tags.Title),
			Artist:       strings.TrimSpace(tags.Artist),
			Comment:      strings.TrimSpace(tags.Comment),
			Key:          key,
			TitleKey:     key,
			ArtistKey:    normalizePromoteKey(tags.Artist),
			SourceURLKey: normalizePromoteURLKey(tags.Comment),
			Tokens:       tokenizePromoteKey(key),
		})
		return nil
	})
	if err != nil {
		return nil, err
	}
	sort.SliceStable(files, func(i, j int) bool {
		return files[i].Rel < files[j].Rel
	})
	return files, nil
}

func isPromoteMediaExt(ext string) bool {
	switch ext {
	case ".m4a", ".mp3", ".flac", ".opus", ".ogg", ".wav", ".aac", ".aif", ".aiff":
		return true
	default:
		return false
	}
}

func normalizePromoteKey(raw string) string {
	trimmed := strings.TrimSpace(strings.ToLower(raw))
	if trimmed == "" {
		return ""
	}
	var b strings.Builder
	prevSpace := false
	for _, r := range trimmed {
		switch {
		case unicode.IsLetter(r) || unicode.IsDigit(r):
			b.WriteRune(r)
			prevSpace = false
		case unicode.IsSpace(r):
			if !prevSpace {
				b.WriteRune(' ')
				prevSpace = true
			}
		default:
			if !prevSpace {
				b.WriteRune(' ')
				prevSpace = true
			}
		}
	}
	key := strings.TrimSpace(b.String())
	if key == "" {
		return ""
	}
	return strings.Join(tokenizePromoteKey(key), " ")
}

func tokenizePromoteKey(key string) []string {
	if strings.TrimSpace(key) == "" {
		return nil
	}
	rawTokens := strings.Fields(strings.TrimSpace(key))
	ignore := map[string]struct{}{
		"free": {}, "dl": {}, "mstr": {}, "master": {}, "hq": {},
	}
	tokens := make([]string, 0, len(rawTokens))
	for _, token := range rawTokens {
		if _, ignored := ignore[token]; ignored {
			continue
		}
		tokens = append(tokens, token)
	}
	return tokens
}

func normalizePromoteURLKey(raw string) string {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return ""
	}

	// Some files store multiple values in comment fields; pick the first URL-looking token.
	candidate := trimmed
	for _, token := range strings.Fields(trimmed) {
		lowered := strings.ToLower(strings.TrimSpace(token))
		if strings.HasPrefix(lowered, "http://") || strings.HasPrefix(lowered, "https://") {
			candidate = token
			break
		}
	}

	parsed, err := url.Parse(strings.TrimSpace(candidate))
	if err != nil || parsed.Scheme == "" || parsed.Hostname() == "" {
		return ""
	}
	parsed.Scheme = strings.ToLower(parsed.Scheme)
	parsed.Host = strings.ToLower(parsed.Host)
	parsed.RawQuery = ""
	parsed.Fragment = ""
	normalized := strings.TrimSpace(parsed.String())
	return strings.TrimSuffix(normalized, "/")
}

func buildPromoteAssignments(
	libraryFiles []promoteMediaFile,
	freeDLFiles []promoteMediaFile,
	minScore int,
	ambiguityGap int,
) promoteAssignmentPlan {
	candidates := make([]promotePairCandidate, 0)
	ambiguous := make([]promoteAmbiguousMatch, 0)
	libraryAllowed := make([]bool, len(libraryFiles))
	for i := range libraryAllowed {
		libraryAllowed[i] = true
	}
	for libraryIdx, libraryFile := range libraryFiles {
		local := make([]promotePairCandidate, 0)
		for freeDLIdx, freeDLFile := range freeDLFiles {
			score := scorePromoteMatch(libraryFile, freeDLFile)
			if score < minScore {
				continue
			}
			local = append(local, promotePairCandidate{
				LibraryIdx: libraryIdx,
				FreeDLIdx:  freeDLIdx,
				Score:      score,
			})
		}
		sortPromoteCandidates(local, libraryFiles, freeDLFiles)
		if ambiguityGap > 0 && len(local) > 1 {
			best := local[0]
			next := local[1]
			if best.Score-next.Score < ambiguityGap {
				libraryAllowed[libraryIdx] = false
				ambiguous = append(ambiguous, promoteAmbiguousMatch{
					Library:     libraryFile,
					Best:        freeDLFiles[best.FreeDLIdx],
					Alternative: freeDLFiles[next.FreeDLIdx],
					BestScore:   best.Score,
					AltScore:    next.Score,
				})
			}
		}
	}

	for libraryIdx, libraryFile := range libraryFiles {
		if !libraryAllowed[libraryIdx] {
			continue
		}
		for freeDLIdx, freeDLFile := range freeDLFiles {
			score := scorePromoteMatch(libraryFile, freeDLFile)
			if score < minScore {
				continue
			}
			candidates = append(candidates, promotePairCandidate{
				LibraryIdx: libraryIdx,
				FreeDLIdx:  freeDLIdx,
				Score:      score,
			})
		}
	}
	sortPromoteCandidates(candidates, libraryFiles, freeDLFiles)

	usedLibrary := make([]bool, len(libraryFiles))
	usedFreeDL := make([]bool, len(freeDLFiles))
	assignments := make([]promoteAssignment, 0)
	for _, candidate := range candidates {
		if usedLibrary[candidate.LibraryIdx] || usedFreeDL[candidate.FreeDLIdx] {
			continue
		}
		usedLibrary[candidate.LibraryIdx] = true
		usedFreeDL[candidate.FreeDLIdx] = true
		assignments = append(assignments, promoteAssignment{
			Library: libraryFiles[candidate.LibraryIdx],
			FreeDL:  freeDLFiles[candidate.FreeDLIdx],
			Score:   candidate.Score,
		})
	}
	sort.SliceStable(assignments, func(i, j int) bool {
		return assignments[i].Library.Rel < assignments[j].Library.Rel
	})
	sort.SliceStable(ambiguous, func(i, j int) bool {
		return ambiguous[i].Library.Rel < ambiguous[j].Library.Rel
	})
	return promoteAssignmentPlan{
		Assignments: assignments,
		Ambiguous:   ambiguous,
	}
}

func sortPromoteCandidates(candidates []promotePairCandidate, libraryFiles []promoteMediaFile, freeDLFiles []promoteMediaFile) {
	sort.SliceStable(candidates, func(i, j int) bool {
		if candidates[i].Score != candidates[j].Score {
			return candidates[i].Score > candidates[j].Score
		}
		libI := libraryFiles[candidates[i].LibraryIdx].Rel
		libJ := libraryFiles[candidates[j].LibraryIdx].Rel
		if libI != libJ {
			return libI < libJ
		}
		return freeDLFiles[candidates[i].FreeDLIdx].Rel < freeDLFiles[candidates[j].FreeDLIdx].Rel
	})
}

func scorePromoteMatch(libraryFile promoteMediaFile, freeDLFile promoteMediaFile) int {
	if libraryFile.SourceURLKey != "" && freeDLFile.SourceURLKey != "" && libraryFile.SourceURLKey == freeDLFile.SourceURLKey {
		return 100
	}

	if libraryFile.TitleKey != "" && freeDLFile.TitleKey != "" && libraryFile.TitleKey == freeDLFile.TitleKey {
		if libraryFile.ArtistKey != "" && freeDLFile.ArtistKey != "" && libraryFile.ArtistKey == freeDLFile.ArtistKey {
			return 100
		}
		if libraryFile.ArtistKey == "" || freeDLFile.ArtistKey == "" {
			return 92
		}
	}

	if libraryFile.Key == "" || freeDLFile.Key == "" {
		return 0
	}

	score := 0
	if libraryFile.Key == freeDLFile.Key {
		score += 90
	}
	if strings.Contains(libraryFile.Key, freeDLFile.Key) || strings.Contains(freeDLFile.Key, libraryFile.Key) {
		score += 55
	}

	libraryTokenSet := map[string]struct{}{}
	for _, token := range libraryFile.Tokens {
		libraryTokenSet[token] = struct{}{}
	}
	freeTokenSet := map[string]struct{}{}
	for _, token := range freeDLFile.Tokens {
		freeTokenSet[token] = struct{}{}
	}

	common := 0
	for token := range libraryTokenSet {
		if _, exists := freeTokenSet[token]; exists {
			common++
		}
	}
	if common > 0 {
		union := len(libraryTokenSet) + len(freeTokenSet) - common
		if union > 0 {
			score += int((float64(common) / float64(union)) * 45.0)
		}
		if len(libraryFile.Tokens) > 0 && len(freeDLFile.Tokens) > 0 && libraryFile.Tokens[0] == freeDLFile.Tokens[0] {
			score += 10
		}
	}

	if score > 91 {
		score = 91
	}
	if score < 0 {
		return 0
	}
	return score
}

func probePromoteTagsWithTimeout(
	ctx context.Context,
	path string,
	timeout time.Duration,
) (promoteTagProbe, error) {
	if timeout <= 0 {
		return probeTagsFn(ctx, path)
	}
	probeCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	return probeTagsFn(probeCtx, path)
}

func probePromoteAudioWithTimeout(
	ctx context.Context,
	path string,
	timeout time.Duration,
) (promoteAudioProbe, error) {
	if timeout <= 0 {
		return probeAudioFn(ctx, path)
	}
	probeCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	return probeAudioFn(probeCtx, path)
}

func probePromoteTags(ctx context.Context, path string) (promoteTagProbe, error) {
	args := []string{
		"-v", "error",
		"-show_entries", "format_tags=title,artist,comment",
		"-of", "json",
		path,
	}
	cmd := exec.CommandContext(ctx, "ffprobe", args...)
	output, err := cmd.CombinedOutput()
	if err != nil {
		trimmedOutput := strings.TrimSpace(string(output))
		if trimmedOutput == "" {
			return promoteTagProbe{}, err
		}
		return promoteTagProbe{}, fmt.Errorf("%v: %s", err, trimmedOutput)
	}

	var payload struct {
		Format struct {
			Tags map[string]string `json:"tags"`
		} `json:"format"`
	}
	if err := json.Unmarshal(output, &payload); err != nil {
		return promoteTagProbe{}, err
	}
	if payload.Format.Tags == nil {
		return promoteTagProbe{}, nil
	}
	return promoteTagProbe{
		Title:   strings.TrimSpace(payload.Format.Tags["title"]),
		Artist:  strings.TrimSpace(payload.Format.Tags["artist"]),
		Comment: strings.TrimSpace(payload.Format.Tags["comment"]),
	}, nil
}

func probePromoteAudio(ctx context.Context, path string) (promoteAudioProbe, error) {
	args := []string{
		"-v", "error",
		"-select_streams", "a:0",
		"-show_entries", "stream=codec_name,bit_rate:format=bit_rate,size,duration",
		"-of", "json",
		path,
	}
	cmd := exec.CommandContext(ctx, "ffprobe", args...)
	output, err := cmd.CombinedOutput()
	if err != nil {
		trimmedOutput := strings.TrimSpace(string(output))
		if trimmedOutput == "" {
			return promoteAudioProbe{}, err
		}
		return promoteAudioProbe{}, fmt.Errorf("%v: %s", err, trimmedOutput)
	}

	var payload struct {
		Streams []struct {
			CodecName string `json:"codec_name"`
			BitRate   string `json:"bit_rate"`
		} `json:"streams"`
		Format struct {
			BitRate  string `json:"bit_rate"`
			Size     string `json:"size"`
			Duration string `json:"duration"`
		} `json:"format"`
	}
	if err := json.Unmarshal(output, &payload); err != nil {
		return promoteAudioProbe{}, err
	}
	if len(payload.Streams) == 0 {
		return promoteAudioProbe{}, fmt.Errorf("missing audio stream")
	}
	stream := payload.Streams[0]
	streamBitrate := 0
	if rawBitrate := strings.TrimSpace(stream.BitRate); rawBitrate != "" {
		if parsed, parseErr := strconv.Atoi(rawBitrate); parseErr == nil {
			streamBitrate = parsed
		}
	}
	formatBitrate := 0
	if rawBitrate := strings.TrimSpace(payload.Format.BitRate); rawBitrate != "" {
		if parsed, parseErr := strconv.Atoi(rawBitrate); parseErr == nil {
			formatBitrate = parsed
		}
	}
	effective := streamBitrate
	if effective <= 0 {
		effective = formatBitrate
	}
	if effective <= 0 {
		sizeBytes := int64(0)
		durationSeconds := 0.0
		if rawSize := strings.TrimSpace(payload.Format.Size); rawSize != "" {
			if parsed, parseErr := strconv.ParseInt(rawSize, 10, 64); parseErr == nil {
				sizeBytes = parsed
			}
		}
		if rawDuration := strings.TrimSpace(payload.Format.Duration); rawDuration != "" {
			if parsed, parseErr := strconv.ParseFloat(rawDuration, 64); parseErr == nil {
				durationSeconds = parsed
			}
		}
		if sizeBytes > 0 && durationSeconds > 0 {
			effective = int(math.Round((float64(sizeBytes) * 8) / durationSeconds))
		}
	}
	return promoteAudioProbe{
		Codec:            strings.ToLower(strings.TrimSpace(stream.CodecName)),
		Bitrate:          streamBitrate,
		FormatBitrate:    formatBitrate,
		EffectiveBitrate: effective,
	}, nil
}

func decidePromoteAction(
	opts promoteFreeDLOptions,
	assignment promoteAssignment,
	sourceProbe promoteAudioProbe,
) promoteDecision {
	policy, ok := resolvePromoteTargetPolicy(opts, assignment.Library.Ext)
	if !ok {
		return promoteDecision{
			Mode:   promoteActionSkip,
			Reason: fmt.Sprintf("unsupported-target-format-%s-for-%s", opts.TargetFormat, assignment.Library.Ext),
		}
	}
	sourceCodec := normalizePromoteCodec(sourceProbe.Codec)
	if isPromoteLossless(assignment.FreeDL, sourceProbe) {
		return promoteDecision{Mode: policy.LosslessAction}
	}

	if !isHighQualityLossySource(opts, sourceProbe) {
		return promoteDecision{
			Mode:   promoteActionSkip,
			Reason: "source-not-lossless-or-hq-lossy",
		}
	}

	if !isPromoteCodecCompatible(sourceCodec, policy.DesiredCodec) {
		return promoteDecision{
			Mode:   promoteActionSkip,
			Reason: fmt.Sprintf("hq-lossy-source-codec-%s-target-codec-%s", sourceCodec, policy.DesiredCodec),
		}
	}
	return promoteDecision{Mode: promoteActionCopyAudio}
}

func normalizePromoteTargetFormat(raw string) (string, error) {
	trimmed := strings.TrimSpace(strings.ToLower(raw))
	switch trimmed {
	case "", promoteTargetAuto:
		return promoteTargetAuto, nil
	case promoteTargetWAV, promoteTargetMP3320, promoteTargetAAC256:
		return trimmed, nil
	default:
		return "", fmt.Errorf("invalid --target-format %q (expected: auto, wav, mp3-320, aac-256)", raw)
	}
}

func resolvePromoteTargetPolicy(opts promoteFreeDLOptions, libraryExt string) (promoteTargetPolicy, bool) {
	switch opts.TargetFormat {
	case promoteTargetWAV:
		return promoteTargetPolicy{
			DesiredCodec:   promoteCodecWAV,
			OutputExt:      ".wav",
			LosslessAction: promoteActionEncodeWAV,
		}, true
	case promoteTargetMP3320:
		return promoteTargetPolicy{
			DesiredCodec:   promoteCodecMP3,
			OutputExt:      ".mp3",
			LosslessAction: promoteActionEncodeMP3,
		}, true
	case promoteTargetAAC256:
		return promoteTargetPolicy{
			DesiredCodec:   promoteCodecAAC,
			OutputExt:      ".m4a",
			LosslessAction: promoteActionEncodeAAC,
		}, true
	case promoteTargetAuto:
		if strings.EqualFold(libraryExt, ".mp3") {
			return promoteTargetPolicy{
				DesiredCodec:   promoteCodecMP3,
				OutputExt:      ".mp3",
				LosslessAction: promoteActionEncodeMP3,
			}, true
		}
		switch strings.ToLower(strings.TrimSpace(libraryExt)) {
		case ".m4a", ".aac", ".mp4":
			return promoteTargetPolicy{
				DesiredCodec:   promoteCodecAAC,
				OutputExt:      strings.ToLower(strings.TrimSpace(libraryExt)),
				LosslessAction: promoteActionEncodeAAC,
			}, true
		default:
			return promoteTargetPolicy{}, false
		}
	default:
		return promoteTargetPolicy{}, false
	}
}

func normalizePromoteCodec(raw string) string {
	codec := strings.ToLower(strings.TrimSpace(raw))
	switch codec {
	case "mp3":
		return promoteCodecMP3
	case "aac":
		return promoteCodecAAC
	case "libmp3lame":
		return promoteCodecMP3
	default:
		return codec
	}
}

func isPromoteCodecCompatible(sourceCodec string, desiredCodec string) bool {
	src := strings.ToLower(strings.TrimSpace(sourceCodec))
	dst := strings.ToLower(strings.TrimSpace(desiredCodec))
	switch dst {
	case promoteCodecAAC:
		return src == promoteCodecAAC
	case promoteCodecMP3:
		return src == promoteCodecMP3
	case promoteCodecWAV:
		return strings.HasPrefix(src, "pcm_")
	default:
		return src == dst
	}
}

func isPromoteLossless(file promoteMediaFile, probe promoteAudioProbe) bool {
	switch strings.ToLower(strings.TrimSpace(file.Ext)) {
	case ".wav", ".aif", ".aiff", ".flac":
		return true
	}
	codec := normalizePromoteCodec(probe.Codec)
	switch {
	case strings.HasPrefix(codec, "pcm_"):
		return true
	case codec == "flac":
		return true
	case codec == "alac":
		return true
	case codec == "ape":
		return true
	case codec == "wavpack":
		return true
	default:
		return false
	}
}

func isHighQualityLossySource(opts promoteFreeDLOptions, probe promoteAudioProbe) bool {
	codec := normalizePromoteCodec(probe.Codec)
	bitrate := probe.EffectiveBitrate
	if bitrate <= 0 {
		bitrate = probe.Bitrate
	}
	switch codec {
	case promoteCodecMP3:
		return bitrate >= opts.MinMP3Kbps*1000
	case promoteCodecAAC:
		return bitrate >= opts.MinAACKbps*1000
	case "opus", "vorbis":
		return bitrate >= opts.MinOpusKbps*1000
	default:
		return false
	}
}

func applyPromoteReplacement(
	ctx context.Context,
	opts promoteFreeDLOptions,
	assignment promoteAssignment,
	outputPath string,
	decision promoteDecision,
) error {
	if strings.TrimSpace(outputPath) == "" {
		return fmt.Errorf("empty output path")
	}

	if strings.TrimSpace(opts.WriteDir) != "" {
		if err := os.MkdirAll(filepath.Dir(outputPath), 0o755); err != nil {
			return err
		}
		return runPromoteFFmpeg(ctx, opts, assignment, outputPath, decision)
	}

	tempFile, err := os.CreateTemp(filepath.Dir(assignment.Library.Path), ".udl-promote-*"+filepath.Ext(assignment.Library.Path))
	if err != nil {
		return err
	}
	tempPath := tempFile.Name()
	_ = tempFile.Close()
	_ = os.Remove(tempPath)

	if err := runPromoteFFmpeg(ctx, opts, assignment, tempPath, decision); err != nil {
		_ = os.Remove(tempPath)
		return err
	}
	if err := fileops.ReplaceFileSafely(tempPath, assignment.Library.Path); err != nil {
		_ = os.Remove(tempPath)
		return err
	}
	return nil
}

func runPromoteFFmpegCommand(
	ctx context.Context,
	opts promoteFreeDLOptions,
	assignment promoteAssignment,
	outputPath string,
	decision promoteDecision,
) error {
	args := []string{
		"-hide_banner",
		"-loglevel", "error",
		"-y",
		"-i", assignment.FreeDL.Path,
		"-i", assignment.Library.Path,
		"-map", "0:a:0",
		"-map_metadata", "1",
	}
	switch decision.Mode {
	case promoteActionCopyAudio:
		args = append(args, "-c:a", "copy")
	case promoteActionEncodeMP3:
		args = append(args, "-c:a", "libmp3lame", "-b:a", opts.MP3Bitrate)
	case promoteActionEncodeAAC:
		args = append(args, "-c:a", "aac", "-b:a", opts.AACBitrate)
	case promoteActionEncodeWAV:
		args = append(args, "-c:a", "pcm_s16le")
	default:
		return fmt.Errorf("unsupported promote action mode: %s", decision.Mode)
	}
	// WAV containers do not support embedded cover-art video streams.
	if !strings.EqualFold(filepath.Ext(outputPath), ".wav") {
		args = append(args, "-map", "1:v?", "-c:v", "copy")
	}
	args = append(args, outputPath)

	cmd := exec.CommandContext(ctx, "ffmpeg", args...)
	output, err := cmd.CombinedOutput()
	if err != nil {
		trimmedOutput := strings.TrimSpace(string(output))
		if trimmedOutput == "" {
			return err
		}
		return fmt.Errorf("%v: %s", err, trimmedOutput)
	}
	return nil
}

func resolvePromoteOutputPath(opts promoteFreeDLOptions, assignment promoteAssignment, writeDir string) (string, error) {
	policy, ok := resolvePromoteTargetPolicy(opts, assignment.Library.Ext)
	if !ok {
		return "", fmt.Errorf("unsupported target policy for %s", assignment.Library.Rel)
	}

	desiredExt := policy.OutputExt
	if strings.TrimSpace(writeDir) != "" {
		rel := assignment.Library.Rel
		if desiredExt != "" {
			rel = strings.TrimSuffix(rel, filepath.Ext(rel)) + desiredExt
		}
		return filepath.Join(writeDir, filepath.FromSlash(rel)), nil
	}

	if desiredExt != "" && !strings.EqualFold(filepath.Ext(assignment.Library.Path), desiredExt) {
		return "", fmt.Errorf(
			"in-place replacement path extension %s does not match target format extension %s",
			filepath.Ext(assignment.Library.Path),
			desiredExt,
		)
	}
	return assignment.Library.Path, nil
}
