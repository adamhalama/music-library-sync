package engine

import (
	"slices"

	"github.com/jaa/update-downloads/internal/config"
)

type DownloadOrder string
type PlanWindow string

const (
	DownloadOrderNewestFirst DownloadOrder = "newest_first"
	DownloadOrderOldestFirst DownloadOrder = "oldest_first"
	// DefaultDownloadOrder is the single fallback for sync planning and
	// execution. Standalone Free DL owns its separate string default.
	DefaultDownloadOrder DownloadOrder = DownloadOrderOldestFirst

	PlanWindowFirst  PlanWindow = "first"
	PlanWindowLatest PlanWindow = "latest"
)

func NormalizeDownloadOrder(order DownloadOrder) DownloadOrder {
	switch order {
	case DownloadOrderNewestFirst:
		return DownloadOrderNewestFirst
	case DownloadOrderOldestFirst:
		return DownloadOrderOldestFirst
	default:
		return DefaultDownloadOrder
	}
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

func orderForExecution[T any](items []T, order DownloadOrder) []T {
	ordered := append([]T(nil), items...)
	if NormalizeDownloadOrder(order) == DownloadOrderOldestFirst {
		slices.Reverse(ordered)
	}
	return ordered
}
