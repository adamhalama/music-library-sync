// Package pathidentity defines the only cross-provider track identity used by
// directional playlist sync. Identity is an exact canonical real path; this
// package deliberately accepts no artist or title metadata.
package pathidentity

import (
	"net/url"
	"path/filepath"
	"strings"
	"unicode/utf8"

	"golang.org/x/text/unicode/norm"
)

// Canonical decodes one file URL layer, cleans redundant path components, and
// folds valid UTF-8 into NFC. It does not lowercase: filesystem path identity
// remains case-sensitive even when UDL happens to run on a case-insensitive
// volume.
func Canonical(raw string) string {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return ""
	}
	if strings.HasPrefix(trimmed, "file:") {
		// url.Parse already percent-decodes Path. A second PathUnescape would
		// corrupt a literal percent sequence such as 100%2520mix.mp3.
		if parsed, err := url.Parse(trimmed); err == nil && parsed.Path != "" {
			trimmed = parsed.Path
		}
	}
	cleaned := filepath.Clean(trimmed)
	if utf8.ValidString(cleaned) {
		cleaned = norm.NFC.String(cleaned)
	}
	return cleaned
}

type Match struct {
	Index int
	Count int
}

// Find performs exact canonical-path matching only. Count greater than one is
// intentionally preserved as ambiguity rather than choosing a candidate.
func Find(raw string, candidates []string) Match {
	target := Canonical(raw)
	result := Match{Index: -1}
	if target == "" {
		return result
	}
	for index, candidate := range candidates {
		if Canonical(candidate) != target {
			continue
		}
		if result.Count == 0 {
			result.Index = index
		}
		result.Count++
	}
	return result
}
