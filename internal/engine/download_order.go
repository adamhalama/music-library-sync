package engine

import (
	"net/url"
	"slices"
	"strings"

	"github.com/jaa/update-downloads/internal/config"
)

type DownloadOrder string
type PlanWindow string

const (
	DownloadOrderNewestFirst DownloadOrder = "newest_first"
	DownloadOrderOldestFirst DownloadOrder = "oldest_first"

	PlanWindowFirst  PlanWindow = "first"
	PlanWindowLatest PlanWindow = "latest"
)

func NormalizeDownloadOrder(order DownloadOrder) DownloadOrder {
	if order == DownloadOrderOldestFirst {
		return DownloadOrderOldestFirst
	}
	return DownloadOrderNewestFirst
}

func DownloadOrderForSource(source config.Source) DownloadOrder {
	return NormalizeDownloadOrder(DownloadOrder(source.Sync.DownloadOrder))
}

func NormalizePlanWindow(window PlanWindow) PlanWindow {
	if window == PlanWindowLatest {
		return PlanWindowLatest
	}
	return PlanWindowFirst
}

func DefaultPlanWindowForSource(source config.Source) PlanWindow {
	if source.Type == config.SourceTypeSpotify && source.Adapter.Kind == "deemix" {
		return PlanWindowLatest
	}
	return PlanWindowFirst
}

func EffectivePlanWindow(source config.Source, opts SyncOptions) PlanWindow {
	if opts.PlanWindow == "" {
		return DefaultPlanWindowForSource(source)
	}
	return NormalizePlanWindow(opts.PlanWindow)
}

func SupportsDownloadOrder(source config.Source) bool {
	return source.Type == config.SourceTypeSoundCloud ||
		(source.Type == config.SourceTypeSpotify && source.Adapter.Kind == "deemix")
}

func SupportsPlanWindow(source config.Source) bool {
	return source.Type == config.SourceTypeSpotify && source.Adapter.Kind == "deemix"
}

func SupportsPlan(source config.Source) bool {
	return source.Type == config.SourceTypeSoundCloud && source.Adapter.Kind == "scdl" ||
		source.Type == config.SourceTypeSpotify && source.Adapter.Kind == "deemix"
}

// SoundCloudEnumerationOldestFirst reports whether yt-dlp enumerates set
// positions (earliest-added first), rather than a newest-first user stream.
// User roots honor scdl's mode flags through effectiveSoundCloudListURL.
func SoundCloudEnumerationOldestFirst(source config.Source) bool {
	if source.Type != config.SourceTypeSoundCloud {
		return false
	}
	parsed, err := url.Parse(effectiveSoundCloudListURL(source))
	if err != nil {
		return false
	}
	parts := strings.Split(strings.Trim(parsed.Path, "/"), "/")
	return len(parts) >= 3 && (parts[1] == "sets" || parts[1] == "playlists") ||
		len(parts) >= 2 && parts[0] == "playlists"
}

// OrderSoundCloudTracks returns a copy in added order for this URL kind.
func OrderSoundCloudTracks(source config.Source, tracks []SoundCloudRemoteTrack, order DownloadOrder) []SoundCloudRemoteTrack {
	return orderForExecution(tracks, order, SoundCloudEnumerationOldestFirst(source))
}

func orderForExecution[T any](items []T, order DownloadOrder, enumerationOldestFirst ...bool) []T {
	ordered := append([]T(nil), items...)
	nativeOldest := len(enumerationOldestFirst) > 0 && enumerationOldestFirst[0]
	if (NormalizeDownloadOrder(order) == DownloadOrderOldestFirst) != nativeOldest {
		slices.Reverse(ordered)
	}
	return ordered
}
