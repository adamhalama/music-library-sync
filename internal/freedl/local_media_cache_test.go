package freedl

import (
	"context"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestScanLocalMediaCachedReusesAndInvalidatesEntries(t *testing.T) {
	originalProbe := probeLocalMediaFn
	t.Cleanup(func() { probeLocalMediaFn = originalProbe })

	root := filepath.Join(t.TempDir(), "library")
	if err := os.MkdirAll(root, 0o755); err != nil {
		t.Fatal(err)
	}
	first := filepath.Join(root, "first.m4a")
	second := filepath.Join(root, "second.wav")
	if err := os.WriteFile(first, []byte("first"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(second, []byte("second"), 0o644); err != nil {
		t.Fatal(err)
	}

	var probes atomic.Int32
	probeLocalMediaFn = func(_ context.Context, libraryRoot string, candidate localMediaCandidate, _ time.Duration) (mediaFile, localMediaRecord) {
		probes.Add(1)
		record := localMediaRecord{
			Size:      candidate.Size,
			ModTimeNS: candidate.ModTimeNS,
			Title:     filepath.Base(candidate.Path),
			Quality:   Quality{Codec: "aac", EffectiveBitrate: 256000},
		}
		return mediaFileFromRecord(libraryRoot, candidate, record), record
	}

	cachePath := filepath.Join(t.TempDir(), "cache.json")
	files, _, err := scanLocalMediaCached(context.Background(), root, cachePath, time.Second, nil, nil, nil, nil, nil, nil)
	if err != nil {
		t.Fatalf("first scan: %v", err)
	}
	if len(files) != 2 || probes.Load() != 2 {
		t.Fatalf("expected two cold probes, files=%d probes=%d", len(files), probes.Load())
	}

	probes.Store(0)
	cachedResults := 0
	_, _, err = scanLocalMediaCached(context.Background(), root, cachePath, time.Second, nil, nil, nil, nil, nil, func(result localMediaResult) {
		if result.Cached {
			cachedResults++
		}
	})
	if err != nil {
		t.Fatalf("cached scan: %v", err)
	}
	if probes.Load() != 0 || cachedResults != 2 {
		t.Fatalf("expected complete cache hit, probes=%d cached=%d", probes.Load(), cachedResults)
	}

	if err := os.WriteFile(first, []byte("first changed"), 0o644); err != nil {
		t.Fatal(err)
	}
	probes.Store(0)
	_, _, err = scanLocalMediaCached(context.Background(), root, cachePath, time.Second, nil, nil, nil, nil, nil, nil)
	if err != nil {
		t.Fatalf("changed scan: %v", err)
	}
	if probes.Load() != 1 {
		t.Fatalf("expected only changed file to be reprobed, got %d", probes.Load())
	}

	if err := os.Remove(second); err != nil {
		t.Fatal(err)
	}
	if _, _, err := scanLocalMediaCached(context.Background(), root, cachePath, time.Second, nil, nil, nil, nil, nil, nil); err != nil {
		t.Fatalf("deleted-file scan: %v", err)
	}
	cache := loadLocalMediaCache(cachePath, root)
	if len(cache.Files) != 1 {
		t.Fatalf("expected deleted entry evicted, got %d records", len(cache.Files))
	}
}

func TestScanLocalMediaCachedLimitsProbeConcurrency(t *testing.T) {
	originalProbe := probeLocalMediaFn
	t.Cleanup(func() { probeLocalMediaFn = originalProbe })

	root := filepath.Join(t.TempDir(), "library")
	if err := os.MkdirAll(root, 0o755); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 12; i++ {
		path := filepath.Join(root, "track-"+string(rune('a'+i))+".m4a")
		if err := os.WriteFile(path, []byte("audio"), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	var active atomic.Int32
	var maximum atomic.Int32
	var mu sync.Mutex
	probeLocalMediaFn = func(_ context.Context, libraryRoot string, candidate localMediaCandidate, _ time.Duration) (mediaFile, localMediaRecord) {
		current := active.Add(1)
		mu.Lock()
		if current > maximum.Load() {
			maximum.Store(current)
		}
		mu.Unlock()
		time.Sleep(10 * time.Millisecond)
		active.Add(-1)
		record := localMediaRecord{
			Size:      candidate.Size,
			ModTimeNS: candidate.ModTimeNS,
			Quality:   Quality{Codec: "aac"},
		}
		return mediaFileFromRecord(libraryRoot, candidate, record), record
	}

	if _, _, err := scanLocalMediaCached(
		context.Background(),
		root,
		filepath.Join(t.TempDir(), "cache.json"),
		time.Second,
		nil,
		nil,
		nil,
		nil,
		nil,
		nil,
	); err != nil {
		t.Fatalf("scan: %v", err)
	}
	if maximum.Load() > localMediaProbeWorkers {
		t.Fatalf("probe concurrency exceeded limit: %d", maximum.Load())
	}
	if maximum.Load() < 2 {
		t.Fatalf("expected probes to run concurrently, max=%d", maximum.Load())
	}
}

func TestScanLocalMediaCachedSkipsColdFallbackWhenRowsResolved(t *testing.T) {
	originalProbe := probeLocalMediaFn
	t.Cleanup(func() { probeLocalMediaFn = originalProbe })

	root := filepath.Join(t.TempDir(), "library")
	if err := os.MkdirAll(root, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "unrelated.m4a"), []byte("audio"), 0o644); err != nil {
		t.Fatal(err)
	}
	var probes atomic.Int32
	probeLocalMediaFn = func(_ context.Context, root string, candidate localMediaCandidate, _ time.Duration) (mediaFile, localMediaRecord) {
		probes.Add(1)
		record := localMediaRecord{Size: candidate.Size, ModTimeNS: candidate.ModTimeNS, Quality: Quality{Codec: "aac"}}
		return mediaFileFromRecord(root, candidate, record), record
	}

	if _, _, err := scanLocalMediaCached(
		context.Background(),
		root,
		filepath.Join(t.TempDir(), "cache.json"),
		time.Second,
		nil,
		nil,
		func() bool { return false },
		nil,
		nil,
		nil,
	); err != nil {
		t.Fatalf("scan: %v", err)
	}
	if probes.Load() != 0 {
		t.Fatalf("expected exhaustive fallback to stay skipped, probes=%d", probes.Load())
	}
}
