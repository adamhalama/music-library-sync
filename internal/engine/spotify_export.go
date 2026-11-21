package engine

import (
	"context"
	"sort"
)

// SpotifyPlaylistTrack is one track of a Spotify playlist in playlist order.
type SpotifyPlaylistTrack struct {
	ID       string `json:"id"`
	Title    string `json:"title"`
	Artist   string `json:"artist,omitempty"`
	Position int    `json:"position"`
}

// SpotifyStateEntry is what a Spotify sync state file records for one track.
// LocalPath is relative to the source target_dir as written by sync.
type SpotifyStateEntry struct {
	DisplayName string `json:"display_name,omitempty"`
	LocalPath   string `json:"local_path,omitempty"`
}

// FetchSpotifyPlaylistOrder returns every track of a public Spotify playlist in
// the playlist's own order, read from the embed page (the Web API refuses
// client-credentials tokens for playlist items).
func FetchSpotifyPlaylistOrder(ctx context.Context, playlistURL string) ([]SpotifyPlaylistTrack, error) {
	playlistID, err := resolveSpotifyPlaylistID(playlistURL)
	if err != nil {
		return nil, err
	}
	remote, err := enumerateSpotifyPlaylistTracksViaEmbed(ctx, playlistID)
	if err != nil {
		return nil, err
	}
	return exportSpotifyPlaylistTracks(remote), nil
}

// ParseSpotifyEmbedPlaylistOrder parses a saved embed page; it exists so callers
// can test order handling without the network.
func ParseSpotifyEmbedPlaylistOrder(document string) ([]SpotifyPlaylistTrack, error) {
	remote, err := parseSpotifyEmbedPlaylistHTML(document)
	if err != nil {
		return nil, err
	}
	return exportSpotifyPlaylistTracks(remote), nil
}

func exportSpotifyPlaylistTracks(remote []spotifyRemoteTrack) []SpotifyPlaylistTrack {
	sorted := append([]spotifyRemoteTrack(nil), remote...)
	sort.SliceStable(sorted, func(i, j int) bool { return sorted[i].Position < sorted[j].Position })
	out := make([]SpotifyPlaylistTrack, 0, len(sorted))
	for _, track := range sorted {
		out = append(out, SpotifyPlaylistTrack{
			ID:       track.ID,
			Title:    track.Title,
			Artist:   track.Artist,
			Position: track.Position,
		})
	}
	return out
}

// LoadSpotifyStateEntries reads a Spotify sync state file (v1 or v2, plus the
// legacy spotDL JSON form) and returns the per-track entries keyed by Spotify
// track ID. IDs known without metadata map to an empty entry.
func LoadSpotifyStateEntries(stateFilePath string) (map[string]SpotifyStateEntry, error) {
	state, _, err := loadSpotifySyncState(stateFilePath)
	if err != nil {
		return nil, err
	}
	out := make(map[string]SpotifyStateEntry, len(state.KnownIDs))
	for id := range state.KnownIDs {
		out[id] = SpotifyStateEntry{}
	}
	for id, entry := range state.Entries {
		out[id] = SpotifyStateEntry{DisplayName: entry.DisplayName, LocalPath: entry.LocalPath}
	}
	return out, nil
}
