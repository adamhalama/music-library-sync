package engine

import (
	"context"
	"errors"
	"net/url"
	"strings"
)

type SoundCloudFreeDLStatus string

const (
	SoundCloudFreeDLAvailable       SoundCloudFreeDLStatus = "available"
	SoundCloudFreeDLNoLink          SoundCloudFreeDLStatus = "no_free_dl"
	SoundCloudFreeDLUnsupportedHost SoundCloudFreeDLStatus = "unsupported_host"
	SoundCloudFreeDLLookupFailed    SoundCloudFreeDLStatus = "lookup_failed"
)

type SoundCloudFreeDLProbe struct {
	Status      SoundCloudFreeDLStatus `json:"status"`
	PurchaseURL string                 `json:"purchase_url,omitempty"`
	Host        string                 `json:"host,omitempty"`
	Err         string                 `json:"error,omitempty"`
}

func ProbeSoundCloudFreeDL(ctx context.Context, row PlanRow) SoundCloudFreeDLProbe {
	track := soundCloudRemoteTrack{
		ID:    strings.TrimSpace(row.RemoteID),
		Title: strings.TrimSpace(row.Title),
		URL:   strings.TrimSpace(row.RemoteURL),
	}
	metadata, err := fetchSoundCloudFreeDownloadMetadataFn(ctx, track)
	if errors.Is(err, errSoundCloudNoFreeDownloadLink) {
		return SoundCloudFreeDLProbe{Status: SoundCloudFreeDLNoLink}
	}
	if err != nil {
		return SoundCloudFreeDLProbe{Status: SoundCloudFreeDLLookupFailed, Err: err.Error()}
	}
	probe := SoundCloudFreeDLProbe{
		Status:      SoundCloudFreeDLAvailable,
		PurchaseURL: sanitizeSoundCloudFreeDownloadURL(metadata.PurchaseURL),
		Host:        freeDLHostLabel(metadata.PurchaseURL),
	}
	if !isHypedditPurchaseURL(metadata.PurchaseURL) {
		probe.Status = SoundCloudFreeDLUnsupportedHost
	}
	return probe
}

func freeDLHostLabel(raw string) string {
	parsed, err := url.Parse(strings.TrimSpace(raw))
	if err != nil {
		return ""
	}
	return strings.TrimPrefix(strings.ToLower(parsed.Host), "www.")
}
