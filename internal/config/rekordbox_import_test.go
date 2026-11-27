package config

import (
	"strings"
	"testing"
)

func TestRekordboxImportPolicySurvivesCanonicalSave(t *testing.T) {
	const base = `version: 1
sources:
  - id: sample
    type: spotify
    target_dir: /tmp/music
    url: https://open.spotify.com/playlist/sample
    adapter:
      kind: deemix
`
	for _, value := range []string{"omitted", "false", "true"} {
		t.Run(value, func(t *testing.T) {
			payload := base
			if value != "omitted" {
				payload += "    rekordbox_import:\n      new_only: " + value + "\n"
			}
			cfg, err := ParseSingleFile("config.yaml", []byte(payload))
			if err != nil {
				t.Fatal(err)
			}
			saved, err := MarshalCanonical(cfg)
			if err != nil {
				t.Fatal(err)
			}
			reloaded, err := ParseSingleFile("config.yaml", saved)
			if err != nil {
				t.Fatal(err)
			}
			if value == "omitted" && strings.Contains(string(saved), "rekordbox_import:") {
				t.Fatalf("omitted policy was written: %s", saved)
			}
			for _, got := range []*bool{cfg.Sources[0].RekordboxImport.NewOnly, reloaded.Sources[0].RekordboxImport.NewOnly} {
				switch value {
				case "omitted":
					if got != nil {
						t.Fatalf("expected absent setting, got %t", *got)
					}
				default:
					if got == nil || *got != (value == "true") {
						t.Fatalf("expected explicit %s on load and reload, got YAML:\n%s", value, saved)
					}
				}
			}
		})
	}
}
