package compact

import (
	"regexp"
	"strconv"
	"strings"
)

var noisyDownloadProgressPattern = regexp.MustCompile(`^\[download\]\s+[0-9]+(?:\.[0-9]+)?%.*\(frag\s+[0-9]+/[0-9]+\)$`)
var fragmentProgressPattern = regexp.MustCompile(`^\[download\]\s+([0-9]+(?:\.[0-9]+)?)%.*\(frag\s+[0-9]+/[0-9]+\)$`)
var downloadItemPattern = regexp.MustCompile(`^\[download\] Downloading item ([0-9]+) of ([0-9]+)$`)
var downloadDestinationPattern = regexp.MustCompile(`^\[download\] Destination: (.+)$`)
var alreadyDownloadedPattern = regexp.MustCompile(`^\[download\] (.+) has already been downloaded$`)
var spotDLFoundSongsPattern = regexp.MustCompile(`^Found ([0-9]+) songs in .+$`)
var spotDLDownloadedPattern = regexp.MustCompile(`^Downloaded "(.+)":\s+https?://.+$`)
var spotDLLookupErrorPattern = regexp.MustCompile(`^LookupError: No results found for song: (.+)$`)
var spotDLAudioProviderPattern = regexp.MustCompile(`^AudioProviderError: YT-DLP download error -.*$`)

type LineEventKind string

const (
	LineEventDownloadItem          LineEventKind = "download_item"
	LineEventDownloadDestination   LineEventKind = "download_destination"
	LineEventAlreadyDownloaded     LineEventKind = "already_downloaded"
	LineEventFragmentProgress      LineEventKind = "fragment_progress"
	LineEventNoisyDownloadProgress LineEventKind = "noisy_progress"
	LineEventSpotDLFoundSongs      LineEventKind = "spotdl_found_songs"
	LineEventSpotDLDownloaded      LineEventKind = "spotdl_downloaded"
	LineEventSpotDLLookupError     LineEventKind = "spotdl_lookup_error"
	LineEventSpotDLAudioProvider   LineEventKind = "spotdl_audio_provider_error"
)

type LineEvent struct {
	Kind    LineEventKind
	Index   int
	Total   int
	Percent float64
	Text    string
}

func ParseLine(line string) (LineEvent, bool) {
	if match := downloadItemPattern.FindStringSubmatch(line); len(match) == 3 {
		index, _ := strconv.Atoi(match[1])
		total, _ := strconv.Atoi(match[2])
		return LineEvent{Kind: LineEventDownloadItem, Index: index, Total: total}, true
	}
	if match := downloadDestinationPattern.FindStringSubmatch(line); len(match) == 2 {
		return LineEvent{Kind: LineEventDownloadDestination, Text: strings.TrimSpace(match[1])}, true
	}
	if match := alreadyDownloadedPattern.FindStringSubmatch(line); len(match) == 2 {
		return LineEvent{Kind: LineEventAlreadyDownloaded, Text: strings.TrimSpace(match[1])}, true
	}
	if match := fragmentProgressPattern.FindStringSubmatch(line); len(match) == 2 {
		percent, _ := strconv.ParseFloat(match[1], 64)
		return LineEvent{Kind: LineEventFragmentProgress, Percent: percent}, true
	}
	if noisyDownloadProgressPattern.MatchString(line) {
		return LineEvent{Kind: LineEventNoisyDownloadProgress}, true
	}
	if match := spotDLFoundSongsPattern.FindStringSubmatch(line); len(match) == 2 {
		total, _ := strconv.Atoi(match[1])
		return LineEvent{Kind: LineEventSpotDLFoundSongs, Total: total}, true
	}
	if match := spotDLDownloadedPattern.FindStringSubmatch(line); len(match) == 2 {
		return LineEvent{Kind: LineEventSpotDLDownloaded, Text: strings.TrimSpace(match[1])}, true
	}
	if match := spotDLLookupErrorPattern.FindStringSubmatch(line); len(match) == 2 {
		return LineEvent{Kind: LineEventSpotDLLookupError, Text: strings.TrimSpace(match[1])}, true
	}
	if spotDLAudioProviderPattern.MatchString(line) {
		return LineEvent{Kind: LineEventSpotDLAudioProvider}, true
	}
	return LineEvent{}, false
}

func MatchesSpotDLFoundSongs(line string) bool {
	return spotDLFoundSongsPattern.MatchString(line)
}

func MatchesSpotDLDownloaded(line string) bool {
	return spotDLDownloadedPattern.MatchString(line)
}

func MatchesSpotDLLookupError(line string) bool {
	return spotDLLookupErrorPattern.MatchString(line)
}

func MatchesSpotDLAudioProvider(line string) bool {
	return spotDLAudioProviderPattern.MatchString(line)
}
