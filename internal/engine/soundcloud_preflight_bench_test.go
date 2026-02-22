package engine

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func BenchmarkPreflightBudget_ParseRemote_1000(b *testing.B)  { benchmarkParseRemote(b, 1000) }
func BenchmarkPreflightBudget_ParseRemote_5000(b *testing.B)  { benchmarkParseRemote(b, 5000) }
func BenchmarkPreflightBudget_ParseRemote_10000(b *testing.B) { benchmarkParseRemote(b, 10000) }

func BenchmarkPreflightBudget_ParseState_1000(b *testing.B)  { benchmarkParseState(b, 1000) }
func BenchmarkPreflightBudget_ParseState_5000(b *testing.B)  { benchmarkParseState(b, 5000) }
func BenchmarkPreflightBudget_ParseState_10000(b *testing.B) { benchmarkParseState(b, 10000) }

func BenchmarkPreflightBudget_ParseArchive_1000(b *testing.B)  { benchmarkParseArchive(b, 1000) }
func BenchmarkPreflightBudget_ParseArchive_5000(b *testing.B)  { benchmarkParseArchive(b, 5000) }
func BenchmarkPreflightBudget_ParseArchive_10000(b *testing.B) { benchmarkParseArchive(b, 10000) }

func BenchmarkPreflightBudget_LocalScan_1000(b *testing.B)  { benchmarkLocalScan(b, 1000) }
func BenchmarkPreflightBudget_LocalScan_5000(b *testing.B)  { benchmarkLocalScan(b, 5000) }
func BenchmarkPreflightBudget_LocalScan_10000(b *testing.B) { benchmarkLocalScan(b, 10000) }

func BenchmarkPreflightBudget_FullPlan_1000(b *testing.B)  { benchmarkFullPlan(b, 1000) }
func BenchmarkPreflightBudget_FullPlan_5000(b *testing.B)  { benchmarkFullPlan(b, 5000) }
func BenchmarkPreflightBudget_FullPlan_10000(b *testing.B) { benchmarkFullPlan(b, 10000) }

func benchmarkParseRemote(b *testing.B, total int) {
	payload := buildRemotePayload(total)
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		tracks := parseSoundCloudTrackList(payload)
		if len(tracks) != total {
			b.Fatalf("unexpected track count: got=%d want=%d", len(tracks), total)
		}
	}
}

func benchmarkParseState(b *testing.B, total int) {
	tmp := b.TempDir()
	path := filepath.Join(tmp, "state.sync.scdl")
	if err := os.WriteFile(path, []byte(buildStatePayload(total)), 0o644); err != nil {
		b.Fatalf("write state payload: %v", err)
	}

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		state, err := parseSoundCloudSyncState(path)
		if err != nil {
			b.Fatalf("parse state: %v", err)
		}
		if len(state.Entries) != total {
			b.Fatalf("unexpected entries: got=%d want=%d", len(state.Entries), total)
		}
	}
}

func benchmarkParseArchive(b *testing.B, total int) {
	tmp := b.TempDir()
	path := filepath.Join(tmp, "archive.txt")
	if err := os.WriteFile(path, []byte(buildArchivePayload(total)), 0o644); err != nil {
		b.Fatalf("write archive payload: %v", err)
	}

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		known, err := parseSoundCloudArchive(path)
		if err != nil {
			b.Fatalf("parse archive: %v", err)
		}
		if len(known) != total {
			b.Fatalf("unexpected known ids: got=%d want=%d", len(known), total)
		}
	}
}

func benchmarkLocalScan(b *testing.B, total int) {
	targetDir := filepath.Join(b.TempDir(), "target")
	if err := os.MkdirAll(targetDir, 0o755); err != nil {
		b.Fatalf("mkdir target dir: %v", err)
	}
	writeLocalMediaFixture(b, targetDir, total)

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		index := scanLocalMediaTitleIndex(targetDir)
		if len(index) != total {
			b.Fatalf("unexpected local index size: got=%d want=%d", len(index), total)
		}
	}
}

func benchmarkFullPlan(b *testing.B, total int) {
	tmp := b.TempDir()
	targetDir := filepath.Join(tmp, "target")
	if err := os.MkdirAll(targetDir, 0o755); err != nil {
		b.Fatalf("mkdir target dir: %v", err)
	}
	writeLocalMediaFixture(b, targetDir, total)

	statePath := filepath.Join(tmp, "state.sync.scdl")
	archivePath := filepath.Join(tmp, "archive.txt")
	if err := os.WriteFile(statePath, []byte(buildStatePayload(total/2)), 0o644); err != nil {
		b.Fatalf("write state payload: %v", err)
	}
	if err := os.WriteFile(archivePath, []byte(buildArchivePayload(total)), 0o644); err != nil {
		b.Fatalf("write archive payload: %v", err)
	}
	remotePayload := buildRemotePayload(total)

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		tracks := parseSoundCloudTrackList(remotePayload)
		state, err := parseSoundCloudSyncState(statePath)
		if err != nil {
			b.Fatalf("parse state: %v", err)
		}
		archiveKnownIDs, err := parseSoundCloudArchive(archivePath)
		if err != nil {
			b.Fatalf("parse archive: %v", err)
		}
		preflight, _, _, _ := buildSoundCloudPreflight(
			tracks,
			state,
			archiveKnownIDs,
			targetDir,
			SoundCloudModeBreak,
		)
		if preflight.RemoteTotal != total {
			b.Fatalf("unexpected remote total: got=%d want=%d", preflight.RemoteTotal, total)
		}
	}
}

func buildRemotePayload(total int) []byte {
	var b strings.Builder
	b.Grow(total * 64)
	for i := 1; i <= total; i++ {
		id := strconvID(i)
		title := fmt.Sprintf("Track %d", i)
		fmt.Fprintf(&b, "%s\t%s\thttps://soundcloud.com/u/%s\n", id, title, id)
	}
	return []byte(b.String())
}

func buildStatePayload(total int) string {
	var b strings.Builder
	b.Grow(total * 40)
	for i := 1; i <= total; i++ {
		id := strconvID(i)
		title := fmt.Sprintf("Track %d", i)
		fmt.Fprintf(&b, "soundcloud %s %s.m4a\n", id, title)
	}
	return b.String()
}

func buildArchivePayload(total int) string {
	var b strings.Builder
	b.Grow(total * 16)
	for i := 1; i <= total; i++ {
		fmt.Fprintf(&b, "soundcloud %s\n", strconvID(i))
	}
	return b.String()
}

func writeLocalMediaFixture(tb testing.TB, targetDir string, total int) {
	tb.Helper()
	for i := 1; i <= total; i++ {
		filename := filepath.Join(targetDir, fmt.Sprintf("Track %d.m4a", i))
		if err := os.WriteFile(filename, []byte("x"), 0o644); err != nil {
			tb.Fatalf("write fixture %d: %v", i, err)
		}
	}
}

func strconvID(i int) string {
	return fmt.Sprintf("%d", i)
}
