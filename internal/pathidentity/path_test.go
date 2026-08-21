package pathidentity

import "testing"

func TestCanonicalCrossProviderFixtures(t *testing.T) {
	tests := []struct {
		name string
		raw  string
		want string
	}{
		{name: "NFD to NFC", raw: "/Music/Café.mp3", want: "/Music/Café.mp3"},
		{name: "file URL", raw: "file:///Music/One%20Two.mp3", want: "/Music/One Two.mp3"},
		{name: "literal percent", raw: "file:///Music/100%2520mix.mp3", want: "/Music/100%20mix.mp3"},
		{name: "redundant components", raw: " /Music/set/../One.mp3 ", want: "/Music/One.mp3"},
		{name: "missing", raw: "  ", want: ""},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := Canonical(test.raw); got != test.want {
				t.Fatalf("Canonical(%q) = %q, want %q", test.raw, got, test.want)
			}
		})
	}
}

func TestCanonicalRemainsCaseSensitive(t *testing.T) {
	if Canonical("/Music/Track.mp3") == Canonical("/music/track.mp3") {
		t.Fatal("canonical path identity must not fold case")
	}
}

func TestFindHasNoMetadataFallbackAndPreservesAmbiguity(t *testing.T) {
	if match := Find("/Music/one.mp3", []string{"/Music/two.mp3"}); match.Count != 0 || match.Index != -1 {
		t.Fatalf("unexpected non-path match: %#v", match)
	}
	match := Find("file:///Music/Caf%C3%A9.mp3", []string{"/Music/Café.mp3", "/Music/Café.mp3"})
	if match.Count != 2 || match.Index != 0 {
		t.Fatalf("ambiguous canonical paths were collapsed: %#v", match)
	}
}
