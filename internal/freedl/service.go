package freedl

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"math"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"
	"unicode"

	"github.com/jaa/update-downloads/internal/config"
	"github.com/jaa/update-downloads/internal/engine"
	"github.com/jaa/update-downloads/internal/fileops"
)

const (
	TargetAuto   = "auto"
	TargetWAV    = "wav"
	TargetMP3320 = "mp3-320"
	TargetAAC256 = "aac-256"
)

type Service struct {
	Now      func() time.Time
	LookPath func(string) (string, error)
}

type Quality struct {
	Codec            string `json:"codec,omitempty"`
	Bitrate          int    `json:"bitrate,omitempty"`
	FormatBitrate    int    `json:"format_bitrate,omitempty"`
	EffectiveBitrate int    `json:"effective_bitrate,omitempty"`
	Lossless         bool   `json:"lossless"`
	Error            string `json:"error,omitempty"`
}

type PlanRow struct {
	Index        int                          `json:"index"`
	RemoteID     string                       `json:"remote_id"`
	RemoteURL    string                       `json:"remote_url,omitempty"`
	Title        string                       `json:"title"`
	LocalPath    string                       `json:"local_path,omitempty"`
	LocalQuality Quality                      `json:"local_quality"`
	FreeDLProbe  engine.SoundCloudFreeDLProbe `json:"free_dl_probe"`
	Selectable   bool                         `json:"selectable"`
	Selected     bool                         `json:"selected"`
	SkipReason   string                       `json:"skip_reason,omitempty"`
}

type CapturePlan struct {
	RunID      string    `json:"run_id"`
	CreatedAt  time.Time `json:"created_at"`
	Job        Job       `json:"job"`
	Rows       []PlanRow `json:"rows"`
	BufferRoot string    `json:"buffer_root"`
	LogDir     string    `json:"log_dir"`
}

type PromotionAction string

const (
	PromotionSkip      PromotionAction = "skip"
	PromotionCopyAudio PromotionAction = "copy-audio"
	PromotionEncodeAAC PromotionAction = "encode-aac"
	PromotionEncodeMP3 PromotionAction = "encode-mp3"
	PromotionEncodeWAV PromotionAction = "encode-wav"
)

type PromotionRow struct {
	Index           int             `json:"index"`
	LibraryPath     string          `json:"library_path"`
	FreeDLPath      string          `json:"free_dl_path"`
	BackupPath      string          `json:"backup_path"`
	OutputPath      string          `json:"output_path"`
	Title           string          `json:"title"`
	Score           int             `json:"score"`
	OriginalQuality Quality         `json:"original_quality"`
	SourceQuality   Quality         `json:"source_quality"`
	Action          PromotionAction `json:"action"`
	Reason          string          `json:"reason,omitempty"`
	Selected        bool            `json:"selected"`
}

type PromotionPlan struct {
	RunID        string         `json:"run_id"`
	CreatedAt    time.Time      `json:"created_at"`
	Job          Job            `json:"job"`
	TargetFormat string         `json:"target_format"`
	Rows         []PromotionRow `json:"rows"`
	BackupRoot   string         `json:"backup_root"`
	LogDir       string         `json:"log_dir"`
}

type PromotionResult struct {
	RunID    string               `json:"run_id"`
	Rows     []PromotionRowResult `json:"rows"`
	Replaced int                  `json:"replaced"`
	Skipped  int                  `json:"skipped"`
	Failed   int                  `json:"failed"`
}

type PromotionRowResult struct {
	Index       int    `json:"index"`
	LibraryPath string `json:"library_path"`
	BackupPath  string `json:"backup_path,omitempty"`
	Status      string `json:"status"`
	Error       string `json:"error,omitempty"`
	SHA256      string `json:"sha256,omitempty"`
}

type mediaFile struct {
	Path      string
	Rel       string
	Ext       string
	Title     string
	Artist    string
	Comment   string
	Key       string
	TitleKey  string
	ArtistKey string
	URLKey    string
	Tokens    []string
	Quality   Quality
}

func (s Service) BuildCapturePlan(ctx context.Context, main config.Config, job Job) (CapturePlan, error) {
	now := s.now()
	runID := now.Format("20060102-150405")
	libraryDir, err := config.ExpandPath(job.LibraryDir)
	if err != nil {
		return CapturePlan{}, fmt.Errorf("resolve library_dir: %w", err)
	}
	bufferRoot, err := config.ExpandPath(job.BufferDir)
	if err != nil {
		return CapturePlan{}, fmt.Errorf("resolve buffer_dir: %w", err)
	}
	logDir, err := config.ExpandPath(job.LogDir)
	if err != nil {
		return CapturePlan{}, fmt.Errorf("resolve log_dir: %w", err)
	}

	source := sourceForJob(job, libraryDir)
	provider := engine.NewSCDLPlanProvider()
	sourcePlan, err := provider.Build(ctx, main, source, engine.SyncOptions{PlanLimit: job.PlanLimit})
	if err != nil {
		return CapturePlan{}, err
	}
	localFiles, _ := collectMediaFiles(ctx, libraryDir, 2*time.Second, true)
	localByTitle := indexMediaByTitle(localFiles)

	rows := make([]PlanRow, 0, len(sourcePlan.Rows()))
	for _, row := range sourcePlan.Rows() {
		local := bestLocalForTitle(row.Title, localByTitle)
		quality := Quality{}
		localPath := ""
		if local != nil {
			quality = local.Quality
			localPath = local.Path
		}
		probe := engine.ProbeSoundCloudFreeDL(ctx, row)
		selectable := row.Toggleable && probe.Status == engine.SoundCloudFreeDLAvailable
		skipReason := ""
		if !row.Toggleable {
			skipReason = "already-present"
		} else if probe.Status != engine.SoundCloudFreeDLAvailable {
			skipReason = string(probe.Status)
		}
		rows = append(rows, PlanRow{
			Index:        row.Index,
			RemoteID:     row.RemoteID,
			RemoteURL:    row.RemoteURL,
			Title:        row.Title,
			LocalPath:    localPath,
			LocalQuality: quality,
			FreeDLProbe:  probe,
			Selectable:   selectable,
			Selected:     selectable,
			SkipReason:   skipReason,
		})
	}
	plan := CapturePlan{
		RunID:      runID,
		CreatedAt:  now,
		Job:        job,
		Rows:       rows,
		BufferRoot: filepath.Join(bufferRoot, runID),
		LogDir:     filepath.Join(logDir, runID),
	}
	_ = WriteJSON(filepath.Join(plan.LogDir, "capture-plan.json"), plan)
	return plan, nil
}

func (s Service) BuildPromotionPlan(ctx context.Context, job Job, runID string, targetFormat string) (PromotionPlan, error) {
	now := s.now()
	if strings.TrimSpace(targetFormat) == "" {
		targetFormat = job.TargetFormat
	}
	if err := validateTargetFormat(targetFormat); err != nil {
		return PromotionPlan{}, err
	}
	libraryDir, err := config.ExpandPath(job.LibraryDir)
	if err != nil {
		return PromotionPlan{}, fmt.Errorf("resolve library_dir: %w", err)
	}
	bufferDir, err := config.ExpandPath(job.BufferDir)
	if err != nil {
		return PromotionPlan{}, fmt.Errorf("resolve buffer_dir: %w", err)
	}
	backupDir, err := config.ExpandPath(job.BackupDir)
	if err != nil {
		return PromotionPlan{}, fmt.Errorf("resolve backup_dir: %w", err)
	}
	logDir, err := config.ExpandPath(job.LogDir)
	if err != nil {
		return PromotionPlan{}, fmt.Errorf("resolve log_dir: %w", err)
	}
	freeFiles, err := collectMediaFiles(ctx, filepath.Join(bufferDir, runID, "downloads"), 2*time.Second, true)
	if err != nil {
		return PromotionPlan{}, fmt.Errorf("scan buffer downloads: %w", err)
	}
	libraryFiles, err := collectMediaFiles(ctx, libraryDir, 2*time.Second, true)
	if err != nil {
		return PromotionPlan{}, fmt.Errorf("scan library: %w", err)
	}
	assignments := buildAssignments(libraryFiles, freeFiles, job.MinMatchScore, job.AmbiguityGap)
	rows := make([]PromotionRow, 0, len(assignments))
	for idx, assignment := range assignments {
		action, reason := decidePromotion(targetFormat, assignment.free, assignment.free.Quality, assignment.library.Ext)
		selected := action != PromotionSkip
		outputPath, outputErr := promotionOutputPath(targetFormat, assignment.library)
		if outputErr != nil {
			action = PromotionSkip
			reason = outputErr.Error()
			selected = false
		}
		backupPath := filepath.Join(backupDir, runID, filepath.FromSlash(assignment.library.Rel))
		rows = append(rows, PromotionRow{
			Index:           idx + 1,
			LibraryPath:     assignment.library.Path,
			FreeDLPath:      assignment.free.Path,
			BackupPath:      backupPath,
			OutputPath:      outputPath,
			Title:           firstNonEmpty(assignment.library.Title, assignment.free.Title, strings.TrimSuffix(filepath.Base(assignment.library.Path), filepath.Ext(assignment.library.Path))),
			Score:           assignment.score,
			OriginalQuality: assignment.library.Quality,
			SourceQuality:   assignment.free.Quality,
			Action:          action,
			Reason:          reason,
			Selected:        selected,
		})
	}
	if job.ReplaceLimit > 0 {
		selected := 0
		for i := range rows {
			if !rows[i].Selected {
				continue
			}
			selected++
			if selected > job.ReplaceLimit {
				rows[i].Selected = false
				rows[i].Reason = "replace-limit"
			}
		}
	}
	plan := PromotionPlan{
		RunID:        runID,
		CreatedAt:    now,
		Job:          job,
		TargetFormat: targetFormat,
		Rows:         rows,
		BackupRoot:   filepath.Join(backupDir, runID),
		LogDir:       filepath.Join(logDir, runID),
	}
	_ = WriteJSON(filepath.Join(plan.LogDir, "promotion-plan.json"), plan)
	return plan, nil
}

func (s Service) ApplyPromotionPlan(ctx context.Context, plan PromotionPlan) PromotionResult {
	result := PromotionResult{RunID: plan.RunID}
	for _, row := range plan.Rows {
		rowResult := PromotionRowResult{Index: row.Index, LibraryPath: row.LibraryPath, BackupPath: row.BackupPath}
		if !row.Selected || row.Action == PromotionSkip {
			rowResult.Status = "skipped"
			rowResult.Error = row.Reason
			result.Skipped++
			result.Rows = append(result.Rows, rowResult)
			continue
		}
		sum, err := backupOriginal(row.LibraryPath, row.BackupPath)
		if err != nil {
			rowResult.Status = "failed"
			rowResult.Error = "backup failed: " + err.Error()
			result.Failed++
			result.Rows = append(result.Rows, rowResult)
			continue
		}
		rowResult.SHA256 = sum
		if err := applyReplacement(ctx, plan, row); err != nil {
			rowResult.Status = "failed"
			rowResult.Error = "replace failed: " + err.Error()
			result.Failed++
			result.Rows = append(result.Rows, rowResult)
			continue
		}
		rowResult.Status = "replaced"
		result.Replaced++
		result.Rows = append(result.Rows, rowResult)
	}
	_ = WriteJSON(filepath.Join(plan.LogDir, "promotion-result.json"), result)
	return result
}

func WriteJSON(path string, value any) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	payload, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		return err
	}
	payload = append(payload, '\n')
	return os.WriteFile(path, payload, 0o644)
}

func AppendEvent(path string, value any) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	file, err := os.OpenFile(path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		return err
	}
	defer file.Close()
	payload, err := json.Marshal(value)
	if err != nil {
		return err
	}
	_, err = file.Write(append(payload, '\n'))
	return err
}

func (s Service) now() time.Time {
	if s.Now != nil {
		return s.Now()
	}
	return time.Now()
}

func sourceForJob(job Job, targetDir string) config.Source {
	return config.Source{
		ID:        job.ID,
		Type:      config.SourceTypeSoundCloud,
		Enabled:   true,
		TargetDir: targetDir,
		URL:       job.SourceURL,
		StateFile: job.StateFile,
		Adapter:   config.AdapterSpec{Kind: "scdl-freedl"},
		Sync: config.SyncPolicy{
			BreakOnExisting: boolPtr(true),
			AskOnExisting:   boolPtr(false),
			LocalIndexCache: boolPtr(false),
		},
	}
}

func boolPtr(v bool) *bool { return &v }

func collectMediaFiles(ctx context.Context, root string, timeout time.Duration, withQuality bool) ([]mediaFile, error) {
	info, err := os.Stat(root)
	if err != nil {
		return nil, err
	}
	if !info.IsDir() {
		return nil, fmt.Errorf("%s is not a directory", root)
	}
	files := []mediaFile{}
	err = filepath.WalkDir(root, func(path string, d os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if d.IsDir() || !isMediaExt(filepath.Ext(d.Name())) {
			return nil
		}
		tags, _ := probeTags(ctx, path, timeout)
		title := firstNonEmpty(tags.Title, strings.TrimSuffix(d.Name(), filepath.Ext(d.Name())))
		rel, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		quality := Quality{}
		if withQuality {
			quality = probeQuality(ctx, path, timeout)
		}
		file := mediaFile{
			Path:      path,
			Rel:       filepath.ToSlash(rel),
			Ext:       strings.ToLower(filepath.Ext(path)),
			Title:     strings.TrimSpace(tags.Title),
			Artist:    strings.TrimSpace(tags.Artist),
			Comment:   strings.TrimSpace(tags.Comment),
			Key:       normalizeKey(title),
			TitleKey:  normalizeKey(tags.Title),
			ArtistKey: normalizeKey(tags.Artist),
			URLKey:    normalizeURLKey(tags.Comment),
			Tokens:    tokenize(title),
			Quality:   quality,
		}
		files = append(files, file)
		return nil
	})
	return files, err
}

type tagProbe struct{ Title, Artist, Comment string }

func probeTags(ctx context.Context, path string, timeout time.Duration) (tagProbe, error) {
	probeCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	cmd := exec.CommandContext(probeCtx, "ffprobe", "-v", "error", "-show_entries", "format_tags=title,artist,comment", "-of", "json", path)
	output, err := cmd.CombinedOutput()
	if err != nil {
		return tagProbe{}, err
	}
	var payload struct {
		Format struct {
			Tags map[string]string `json:"tags"`
		} `json:"format"`
	}
	if err := json.Unmarshal(output, &payload); err != nil {
		return tagProbe{}, err
	}
	return tagProbe{
		Title:   payload.Format.Tags["title"],
		Artist:  payload.Format.Tags["artist"],
		Comment: payload.Format.Tags["comment"],
	}, nil
}

func probeQuality(ctx context.Context, path string, timeout time.Duration) Quality {
	probeCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	cmd := exec.CommandContext(
		probeCtx,
		"ffprobe",
		"-v", "error",
		"-select_streams", "a:0",
		"-show_entries", "stream=codec_name,bit_rate:format=bit_rate,size,duration",
		"-of", "json",
		path,
	)
	output, err := cmd.CombinedOutput()
	if err != nil {
		return Quality{Error: strings.TrimSpace(err.Error())}
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
		return Quality{Error: err.Error()}
	}
	if len(payload.Streams) == 0 {
		return Quality{Error: "missing audio stream"}
	}
	streamBitrate := atoi(payload.Streams[0].BitRate)
	formatBitrate := atoi(payload.Format.BitRate)
	effective := firstPositive(streamBitrate, formatBitrate)
	if effective <= 0 {
		sizeBytes, _ := strconv.ParseInt(strings.TrimSpace(payload.Format.Size), 10, 64)
		duration, _ := strconv.ParseFloat(strings.TrimSpace(payload.Format.Duration), 64)
		if sizeBytes > 0 && duration > 0 {
			effective = int(math.Round((float64(sizeBytes) * 8) / duration))
		}
	}
	codec := strings.ToLower(strings.TrimSpace(payload.Streams[0].CodecName))
	return Quality{
		Codec:            codec,
		Bitrate:          streamBitrate,
		FormatBitrate:    formatBitrate,
		EffectiveBitrate: effective,
		Lossless:         isLosslessCodec(codec) || isLosslessExt(filepath.Ext(path)),
	}
}

func indexMediaByTitle(files []mediaFile) map[string][]mediaFile {
	index := map[string][]mediaFile{}
	for _, file := range files {
		for _, key := range []string{file.TitleKey, file.Key} {
			if key != "" {
				index[key] = append(index[key], file)
			}
		}
	}
	return index
}

func bestLocalForTitle(title string, index map[string][]mediaFile) *mediaFile {
	key := normalizeKey(title)
	if key == "" {
		return nil
	}
	files := index[key]
	if len(files) == 0 {
		return nil
	}
	return &files[0]
}

type assignment struct {
	library mediaFile
	free    mediaFile
	score   int
}

func buildAssignments(libraryFiles, freeFiles []mediaFile, minScore, ambiguityGap int) []assignment {
	candidates := []assignment{}
	for _, library := range libraryFiles {
		best := assignment{score: -1}
		second := -1
		for _, free := range freeFiles {
			score := scoreMatch(library, free)
			if score > best.score {
				second = best.score
				best = assignment{library: library, free: free, score: score}
			} else if score > second {
				second = score
			}
		}
		if best.score < minScore {
			continue
		}
		if ambiguityGap > 0 && second >= 0 && best.score-second < ambiguityGap {
			continue
		}
		candidates = append(candidates, best)
	}
	sort.SliceStable(candidates, func(i, j int) bool {
		return candidates[i].library.Rel < candidates[j].library.Rel
	})
	return candidates
}

func scoreMatch(library, free mediaFile) int {
	if library.URLKey != "" && free.URLKey != "" && library.URLKey == free.URLKey {
		return 100
	}
	if library.TitleKey != "" && free.TitleKey != "" && library.TitleKey == free.TitleKey {
		if library.ArtistKey == "" || free.ArtistKey == "" || library.ArtistKey == free.ArtistKey {
			return 96
		}
	}
	if library.Key == "" || free.Key == "" {
		return 0
	}
	if library.Key == free.Key {
		return 90
	}
	if strings.Contains(library.Key, free.Key) || strings.Contains(free.Key, library.Key) {
		return 80
	}
	return 0
}

func decidePromotion(targetFormat string, source mediaFile, quality Quality, libraryExt string) (PromotionAction, string) {
	targetCodec, _, ok := targetPolicy(targetFormat, libraryExt)
	if !ok {
		return PromotionSkip, "unsupported target format"
	}
	sourceCodec := normalizeCodec(quality.Codec)
	if quality.Lossless {
		switch targetCodec {
		case "wav":
			return PromotionEncodeWAV, ""
		case "mp3":
			return PromotionEncodeMP3, ""
		case "aac":
			return PromotionEncodeAAC, ""
		}
	}
	if !highQualityLossy(sourceCodec, quality.EffectiveBitrate) {
		return PromotionSkip, "source-not-lossless-or-hq-lossy"
	}
	if sourceCodec != targetCodec {
		return PromotionSkip, fmt.Sprintf("source codec %s does not match target %s", sourceCodec, targetCodec)
	}
	return PromotionCopyAudio, ""
}

func promotionOutputPath(targetFormat string, library mediaFile) (string, error) {
	_, ext, ok := targetPolicy(targetFormat, library.Ext)
	if !ok {
		return "", fmt.Errorf("unsupported target policy")
	}
	if ext != "" && !strings.EqualFold(filepath.Ext(library.Path), ext) {
		return "", fmt.Errorf("in-place replacement extension %s does not match target %s", filepath.Ext(library.Path), ext)
	}
	return library.Path, nil
}

func targetPolicy(format string, libraryExt string) (codec, ext string, ok bool) {
	switch strings.ToLower(strings.TrimSpace(format)) {
	case "", TargetAuto:
		switch strings.ToLower(libraryExt) {
		case ".mp3":
			return "mp3", ".mp3", true
		case ".m4a", ".aac", ".mp4":
			return "aac", strings.ToLower(libraryExt), true
		default:
			return "", "", false
		}
	case TargetWAV:
		return "wav", ".wav", true
	case TargetMP3320:
		return "mp3", ".mp3", true
	case TargetAAC256:
		return "aac", ".m4a", true
	default:
		return "", "", false
	}
}

func validateTargetFormat(format string) error {
	_, _, ok := targetPolicy(format, ".mp3")
	if ok || strings.EqualFold(format, TargetWAV) || strings.EqualFold(format, TargetAAC256) {
		return nil
	}
	return fmt.Errorf("invalid target format %q", format)
}

func backupOriginal(sourcePath, backupPath string) (string, error) {
	if err := os.MkdirAll(filepath.Dir(backupPath), 0o755); err != nil {
		return "", err
	}
	if _, err := os.Stat(backupPath); err == nil {
		return "", fmt.Errorf("backup already exists: %s", backupPath)
	} else if err != nil && !os.IsNotExist(err) {
		return "", err
	}
	payload, err := os.ReadFile(sourcePath)
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(payload)
	if err := os.WriteFile(backupPath, payload, 0o644); err != nil {
		return "", err
	}
	return hex.EncodeToString(sum[:]), nil
}

func applyReplacement(ctx context.Context, plan PromotionPlan, row PromotionRow) error {
	tempFile, err := os.CreateTemp(filepath.Dir(row.LibraryPath), ".udl-freedl-promote-*"+filepath.Ext(row.LibraryPath))
	if err != nil {
		return err
	}
	tempPath := tempFile.Name()
	_ = tempFile.Close()
	_ = os.Remove(tempPath)
	if err := runFFmpeg(ctx, plan, row, tempPath); err != nil {
		_ = os.Remove(tempPath)
		return err
	}
	if err := fileops.ReplaceFileSafely(tempPath, row.LibraryPath); err != nil {
		_ = os.Remove(tempPath)
		return err
	}
	return nil
}

func runFFmpeg(ctx context.Context, plan PromotionPlan, row PromotionRow, outputPath string) error {
	args := []string{
		"-hide_banner", "-loglevel", "error", "-y",
		"-i", row.FreeDLPath,
		"-i", row.LibraryPath,
		"-map", "0:a:0",
		"-map_metadata", "1",
	}
	switch row.Action {
	case PromotionCopyAudio:
		args = append(args, "-c:a", "copy")
	case PromotionEncodeMP3:
		args = append(args, "-c:a", "libmp3lame", "-b:a", "320k")
	case PromotionEncodeAAC:
		args = append(args, "-c:a", "aac", "-b:a", "256k")
	case PromotionEncodeWAV:
		args = append(args, "-c:a", "pcm_s16le")
	default:
		return fmt.Errorf("unsupported action %s", row.Action)
	}
	if !strings.EqualFold(filepath.Ext(outputPath), ".wav") {
		args = append(args, "-map", "1:v?", "-c:v", "copy")
	}
	args = append(args, outputPath)
	cmd := exec.CommandContext(ctx, "ffmpeg", args...)
	output, err := cmd.CombinedOutput()
	if err != nil {
		if trimmed := strings.TrimSpace(string(output)); trimmed != "" {
			return fmt.Errorf("%v: %s", err, trimmed)
		}
		return err
	}
	return nil
}

func normalizeKey(raw string) string {
	raw = strings.ToLower(strings.TrimSpace(raw))
	var builder strings.Builder
	lastSpace := false
	for _, r := range raw {
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			builder.WriteRune(r)
			lastSpace = false
			continue
		}
		if !lastSpace {
			builder.WriteRune(' ')
			lastSpace = true
		}
	}
	return strings.TrimSpace(builder.String())
}

func tokenize(raw string) []string {
	key := normalizeKey(raw)
	if key == "" {
		return nil
	}
	return strings.Fields(key)
}

func normalizeURLKey(raw string) string {
	for _, field := range strings.Fields(raw) {
		if strings.Contains(field, "soundcloud.com/") {
			return strings.Trim(strings.ToLower(field), ".,;()[]{}")
		}
	}
	return ""
}

func isMediaExt(ext string) bool {
	switch strings.ToLower(ext) {
	case ".mp3", ".m4a", ".aac", ".mp4", ".wav", ".aif", ".aiff", ".flac", ".ogg", ".opus":
		return true
	default:
		return false
	}
}

func isLosslessExt(ext string) bool {
	switch strings.ToLower(ext) {
	case ".wav", ".aif", ".aiff", ".flac":
		return true
	default:
		return false
	}
}

func isLosslessCodec(codec string) bool {
	codec = strings.ToLower(strings.TrimSpace(codec))
	return strings.HasPrefix(codec, "pcm_") || codec == "flac" || codec == "alac" || codec == "ape" || codec == "wavpack"
}

func normalizeCodec(codec string) string {
	switch strings.ToLower(strings.TrimSpace(codec)) {
	case "mp3", "libmp3lame":
		return "mp3"
	case "aac":
		return "aac"
	default:
		return strings.ToLower(strings.TrimSpace(codec))
	}
}

func highQualityLossy(codec string, bitrate int) bool {
	switch codec {
	case "mp3":
		return bitrate >= 320000
	case "aac":
		return bitrate >= 256000
	case "opus", "vorbis":
		return bitrate >= 192000
	default:
		return false
	}
}

func atoi(raw string) int {
	value, _ := strconv.Atoi(strings.TrimSpace(raw))
	return value
}
