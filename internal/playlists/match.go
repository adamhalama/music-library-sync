package playlists

import (
	"path/filepath"
	"strings"
	"unicode"
)

type MatchStatus string

const (
	MatchPath      MatchStatus = "path"
	MatchMetadata  MatchStatus = "metadata"
	MatchAmbiguous MatchStatus = "ambiguous"
	MatchNone      MatchStatus = "none"
)

type Match struct {
	Status MatchStatus
	Index  int
}

func MatchTrack(snapshot Snapshot, localPath, remoteTitle string) Match {
	path := cleanPath(localPath)
	if path != "" {
		matches := []int{}
		for idx, track := range snapshot.Tracks {
			if cleanPath(track.Path) == path {
				matches = append(matches, idx)
			}
		}
		if len(matches) == 1 {
			return Match{Status: MatchPath, Index: matches[0]}
		}
		if len(matches) > 1 {
			return Match{Status: MatchAmbiguous, Index: -1}
		}
	}

	remoteKey := normalizeText(remoteTitle)
	if remoteKey == "" {
		return Match{Status: MatchNone, Index: -1}
	}
	matches := []int{}
	for idx, track := range snapshot.Tracks {
		titleKey := normalizeText(track.Title)
		artistTitleKey := normalizeText(track.Artist + " " + track.Title)
		titleArtistKey := normalizeText(track.Title + " " + track.Artist)
		if remoteKey == artistTitleKey || remoteKey == titleArtistKey || (track.Artist == "" && remoteKey == titleKey) {
			matches = append(matches, idx)
		}
	}
	if len(matches) == 1 {
		return Match{Status: MatchMetadata, Index: matches[0]}
	}
	if len(matches) > 1 {
		return Match{Status: MatchAmbiguous, Index: -1}
	}
	return Match{Status: MatchNone, Index: -1}
}

func cleanPath(path string) string {
	path = strings.TrimSpace(path)
	if path == "" {
		return ""
	}
	return filepath.Clean(path)
}

func normalizeText(value string) string {
	value = strings.ToLower(strings.TrimSpace(value))
	var out strings.Builder
	space := false
	for _, r := range value {
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			out.WriteRune(r)
			space = false
			continue
		}
		if out.Len() > 0 && !space {
			out.WriteByte(' ')
			space = true
		}
	}
	return strings.TrimSpace(out.String())
}
