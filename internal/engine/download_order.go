package engine

import (
	"slices"

	"github.com/jaa/update-downloads/internal/config"
)

type DownloadOrder string

const (
	DownloadOrderNewestFirst DownloadOrder = "newest_first"
	DownloadOrderOldestFirst DownloadOrder = "oldest_first"
)

func NormalizeDownloadOrder(order DownloadOrder) DownloadOrder {
	if order == DownloadOrderOldestFirst {
		return DownloadOrderOldestFirst
	}
	return DownloadOrderNewestFirst
}

func SupportsDownloadOrder(source config.Source) bool {
	return source.Type == config.SourceTypeSoundCloud ||
		(source.Type == config.SourceTypeSpotify && source.Adapter.Kind == "deemix")
}

func orderForExecution[T any](items []T, order DownloadOrder) []T {
	ordered := append([]T(nil), items...)
	if NormalizeDownloadOrder(order) == DownloadOrderOldestFirst {
		slices.Reverse(ordered)
	}
	return ordered
}
