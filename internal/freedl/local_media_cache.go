package freedl

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
)

const (
	localMediaCacheSchema  = 1
	localMediaProbeWorkers = 4
)

type localMediaCache struct {
	Schema      int                         `json:"schema"`
	LibraryRoot string                      `json:"library_root"`
	Files       map[string]localMediaRecord `json:"files"`
}

type localMediaRecord struct {
	Size      int64   `json:"size"`
	ModTimeNS int64   `json:"mod_time_ns"`
	Title     string  `json:"title,omitempty"`
	Artist    string  `json:"artist,omitempty"`
	Comment   string  `json:"comment,omitempty"`
	Quality   Quality `json:"quality"`
}

type localMediaCandidate struct {
	Path      string
	Rel       string
	Size      int64
	ModTimeNS int64
}

type localMediaResult struct {
	File   mediaFile
	Cached bool
}

type localMediaScanProgress struct {
	Discovered int
	Cached     int
	Probed     int
	Total      int
}

var probeLocalMediaFn = probeLocalMedia

func scanLocalMediaCached(
	ctx context.Context,
	libraryRoot string,
	cachePath string,
	timeout time.Duration,
	probeReady <-chan struct{},
	priority func(localMediaCandidate) bool,
	shouldProbe func() bool,
	onProbe func(localMediaCandidate),
	onProgress func(localMediaScanProgress),
	onResult func(localMediaResult),
) ([]mediaFile, map[string]int, error) {
	candidates, titleCounts, err := discoverLocalMedia(libraryRoot)
	if err != nil {
		return nil, nil, err
	}
	if onProgress != nil {
		onProgress(localMediaScanProgress{Discovered: len(candidates), Total: len(candidates)})
	}

	cache := loadLocalMediaCache(cachePath, libraryRoot)
	nextCache := localMediaCache{
		Schema:      localMediaCacheSchema,
		LibraryRoot: cleanPath(libraryRoot),
		Files:       make(map[string]localMediaRecord, len(candidates)),
	}
	results := make([]mediaFile, 0, len(candidates))
	misses := make([]localMediaCandidate, 0, len(candidates))
	cachedCount := 0
	for _, candidate := range candidates {
		record, ok := cache.Files[candidate.Rel]
		if !ok || record.Size != candidate.Size || record.ModTimeNS != candidate.ModTimeNS {
			misses = append(misses, candidate)
			continue
		}
		file := mediaFileFromRecord(libraryRoot, candidate, record)
		results = append(results, file)
		nextCache.Files[candidate.Rel] = record
		cachedCount++
		if onResult != nil {
			onResult(localMediaResult{File: file, Cached: true})
		}
	}
	if onProgress != nil {
		onProgress(localMediaScanProgress{Discovered: len(candidates), Cached: cachedCount, Total: len(candidates)})
	}
	if probeReady != nil {
		select {
		case <-ctx.Done():
			return nil, nil, ctx.Err()
		case <-probeReady:
		}
	}
	if priority != nil {
		sort.SliceStable(misses, func(i, j int) bool {
			return priority(misses[i]) && !priority(misses[j])
		})
	}

	type probeResult struct {
		candidate localMediaCandidate
		file      mediaFile
		record    localMediaRecord
	}
	jobs := make(chan localMediaCandidate)
	probed := make(chan probeResult)
	var wg sync.WaitGroup
	workers := localMediaProbeWorkers
	if len(misses) < workers {
		workers = len(misses)
	}
	for range workers {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for candidate := range jobs {
				file, record := probeLocalMediaFn(ctx, libraryRoot, candidate, timeout)
				select {
				case <-ctx.Done():
					return
				case probed <- probeResult{candidate: candidate, file: file, record: record}:
				}
			}
		}()
	}
	go func() {
		defer close(probed)
		for _, candidate := range misses {
			if shouldProbe != nil && !shouldProbe() {
				break
			}
			if onProbe != nil {
				onProbe(candidate)
			}
			select {
			case <-ctx.Done():
				close(jobs)
				wg.Wait()
				return
			case jobs <- candidate:
			}
		}
		close(jobs)
		wg.Wait()
	}()

	probedCount := 0
	for result := range probed {
		results = append(results, result.file)
		if result.record.Quality.Error == "" {
			nextCache.Files[result.candidate.Rel] = result.record
		}
		probedCount++
		if onResult != nil {
			onResult(localMediaResult{File: result.file})
		}
		if onProgress != nil {
			onProgress(localMediaScanProgress{
				Discovered: len(candidates),
				Cached:     cachedCount,
				Probed:     probedCount,
				Total:      len(candidates),
			})
		}
	}
	if err := ctx.Err(); err != nil {
		return nil, nil, err
	}
	sort.SliceStable(results, func(i, j int) bool { return results[i].Rel < results[j].Rel })
	_ = storeLocalMediaCache(cachePath, nextCache)
	return results, titleCounts, nil
}

func discoverLocalMedia(root string) ([]localMediaCandidate, map[string]int, error) {
	info, err := os.Stat(root)
	if err != nil {
		return nil, nil, err
	}
	if !info.IsDir() {
		return nil, nil, fmt.Errorf("%s is not a directory", root)
	}
	candidates := []localMediaCandidate{}
	titleCounts := map[string]int{}
	err = filepath.WalkDir(root, func(path string, entry os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if entry.IsDir() || !isMediaExt(filepath.Ext(entry.Name())) {
			return nil
		}
		fileInfo, err := entry.Info()
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		rel = filepath.ToSlash(rel)
		candidates = append(candidates, localMediaCandidate{
			Path:      path,
			Rel:       rel,
			Size:      fileInfo.Size(),
			ModTimeNS: fileInfo.ModTime().UnixNano(),
		})
		key := normalizeKey(strings.TrimSuffix(entry.Name(), filepath.Ext(entry.Name())))
		if key != "" {
			titleCounts[key]++
		}
		return nil
	})
	sort.SliceStable(candidates, func(i, j int) bool { return candidates[i].Rel < candidates[j].Rel })
	return candidates, titleCounts, err
}

func probeLocalMedia(ctx context.Context, libraryRoot string, candidate localMediaCandidate, timeout time.Duration) (mediaFile, localMediaRecord) {
	probeCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	cmd := exec.CommandContext(
		probeCtx,
		"ffprobe",
		"-v", "error",
		"-select_streams", "a:0",
		"-show_entries", "stream=codec_name,bit_rate:format=bit_rate,size,duration:format_tags=title,artist,comment",
		"-of", "json",
		candidate.Path,
	)
	output, err := cmd.CombinedOutput()
	record := localMediaRecord{Size: candidate.Size, ModTimeNS: candidate.ModTimeNS}
	if err != nil {
		record.Quality.Error = strings.TrimSpace(err.Error())
		return mediaFileFromRecord(libraryRoot, candidate, record), record
	}
	var payload struct {
		Streams []struct {
			CodecName string `json:"codec_name"`
			BitRate   string `json:"bit_rate"`
		} `json:"streams"`
		Format struct {
			BitRate  string            `json:"bit_rate"`
			Size     string            `json:"size"`
			Duration string            `json:"duration"`
			Tags     map[string]string `json:"tags"`
		} `json:"format"`
	}
	if err := json.Unmarshal(output, &payload); err != nil {
		record.Quality.Error = err.Error()
		return mediaFileFromRecord(libraryRoot, candidate, record), record
	}
	record.Title = payload.Format.Tags["title"]
	record.Artist = payload.Format.Tags["artist"]
	record.Comment = payload.Format.Tags["comment"]
	if len(payload.Streams) == 0 {
		record.Quality.Error = "missing audio stream"
		return mediaFileFromRecord(libraryRoot, candidate, record), record
	}
	streamBitrate := atoi(payload.Streams[0].BitRate)
	formatBitrate := atoi(payload.Format.BitRate)
	effective := firstPositive(streamBitrate, formatBitrate)
	if effective <= 0 {
		sizeBytes, _ := strconv.ParseInt(strings.TrimSpace(payload.Format.Size), 10, 64)
		duration, _ := strconv.ParseFloat(strings.TrimSpace(payload.Format.Duration), 64)
		if sizeBytes > 0 && duration > 0 {
			effective = int((float64(sizeBytes) * 8) / duration)
		}
	}
	codec := strings.ToLower(strings.TrimSpace(payload.Streams[0].CodecName))
	record.Quality = Quality{
		Codec:            codec,
		Bitrate:          streamBitrate,
		FormatBitrate:    formatBitrate,
		EffectiveBitrate: effective,
		Lossless:         isLosslessCodec(codec) || isLosslessExt(filepath.Ext(candidate.Path)),
	}
	return mediaFileFromRecord(libraryRoot, candidate, record), record
}

func mediaFileFromRecord(root string, candidate localMediaCandidate, record localMediaRecord) mediaFile {
	fallbackTitle := strings.TrimSuffix(filepath.Base(candidate.Path), filepath.Ext(candidate.Path))
	title := firstNonEmpty(record.Title, fallbackTitle)
	return mediaFile{
		Path:      candidate.Path,
		Rel:       candidate.Rel,
		Ext:       strings.ToLower(filepath.Ext(candidate.Path)),
		Title:     strings.TrimSpace(record.Title),
		Artist:    strings.TrimSpace(record.Artist),
		Comment:   strings.TrimSpace(record.Comment),
		Key:       normalizeKey(title),
		TitleKey:  normalizeKey(record.Title),
		ArtistKey: normalizeKey(record.Artist),
		URLKey:    normalizeURLKey(record.Comment),
		Tokens:    tokenize(title),
		Quality:   record.Quality,
	}
}

func loadLocalMediaCache(path, libraryRoot string) localMediaCache {
	empty := localMediaCache{Schema: localMediaCacheSchema, LibraryRoot: cleanPath(libraryRoot), Files: map[string]localMediaRecord{}}
	payload, err := os.ReadFile(path)
	if err != nil {
		return empty
	}
	var cache localMediaCache
	if json.Unmarshal(payload, &cache) != nil ||
		cache.Schema != localMediaCacheSchema ||
		cleanPath(cache.LibraryRoot) != cleanPath(libraryRoot) ||
		cache.Files == nil {
		return empty
	}
	return cache
}

func storeLocalMediaCache(path string, cache localMediaCache) error {
	if strings.TrimSpace(path) == "" {
		return nil
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	payload, err := json.MarshalIndent(cache, "", "  ")
	if err != nil {
		return err
	}
	temp, err := os.CreateTemp(filepath.Dir(path), ".local-media-*.tmp")
	if err != nil {
		return err
	}
	tempPath := temp.Name()
	defer func() { _ = os.Remove(tempPath) }()
	if _, err := temp.Write(append(payload, '\n')); err != nil {
		_ = temp.Close()
		return err
	}
	if err := temp.Sync(); err != nil {
		_ = temp.Close()
		return err
	}
	if err := temp.Close(); err != nil {
		return err
	}
	return os.Rename(tempPath, path)
}

func localMediaCachePath(logDir string) string {
	return filepath.Join(logDir, ".cache", "local-media-v1.json")
}

func isContextError(err error) bool {
	return errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded)
}
