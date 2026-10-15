package agent

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/jaa/update-downloads/internal/auth"
	"github.com/jaa/update-downloads/internal/config"
	"github.com/jaa/update-downloads/internal/navidrome"
	"github.com/jaa/update-downloads/internal/playlists"
)

func TestPlaylistServiceUsesServerNavidromeConfiguration(t *testing.T) {
	for _, explicit := range []bool{true, false} {
		t.Run(fmt.Sprintf("explicit=%t", explicit), func(t *testing.T) {
			root := t.TempDir()
			t.Setenv("HOME", root)
			t.Setenv("XDG_CONFIG_HOME", filepath.Join(root, "user-config"))
			t.Setenv(navidrome.EnvConfigPath, "")
			t.Setenv(auth.EnvNavidromePassword, "test-password")
			calls := 0
			fake := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				calls++
				if got := r.URL.Query().Get("u"); got != "configured-user" {
					t.Errorf("username = %q, want configured-user", got)
				}
				w.Header().Set("Content-Type", "application/json")
				switch {
				case strings.Contains(r.URL.Path, "getPlaylists"):
					fmt.Fprint(w, `{"subsonic-response":{"status":"ok","playlists":{"playlist":[{"id":"p1","name":"Configured playlist","songCount":1}]}}}`)
				case strings.Contains(r.URL.Path, "getPlaylist"):
					fmt.Fprint(w, `{"subsonic-response":{"status":"ok","playlist":{"id":"p1","name":"Configured playlist","entry":[{"id":"s1","title":"Configured track"}]}}}`)
				default:
					t.Errorf("unexpected endpoint %s", r.URL.Path)
				}
			}))
			defer fake.Close()
			workDir := filepath.Join(root, "project")
			if err := os.MkdirAll(workDir, 0o700); err != nil {
				t.Fatal(err)
			}
			path := navidrome.ProjectConfigPath(workDir)
			if explicit {
				// An explicit selection must not merge even a malformed project config.
				if err := os.WriteFile(path, []byte("server: ["), 0o600); err != nil {
					t.Fatal(err)
				}
				path = filepath.Join(root, "selected-navidrome.yaml")
			}
			body := fmt.Sprintf("version: 1\nserver:\n  username: configured-user\n  url: %s\n", fake.URL)
			if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
				t.Fatal(err)
			}
			s := &Server{WorkingDir: workDir}
			if explicit {
				s.NavidromeConfigPath = path
			}
			service := s.playlistService()
			items, err := service.ListProviderPlaylists(context.Background(), playlists.ProviderNavidrome)
			if err != nil {
				t.Fatalf("ListProviderPlaylists: %v", err)
			}
			if len(items) != 1 || items[0].ID != "p1" {
				t.Fatalf("playlists = %#v", items)
			}
			main := config.Config{}
			main.Defaults.StateDir = filepath.Join(root, "state")
			result, err := service.Refresh(context.Background(), main, playlists.Definition{
				ID: "configured", Name: "Configured", Provider: playlists.ProviderNavidrome,
				ProviderPlaylist: "Configured playlist", ProviderPlaylistID: "p1",
			})
			if err != nil {
				t.Fatalf("Refresh: %v", err)
			}
			if len(result.Snapshot.Tracks) != 1 || result.Snapshot.Tracks[0].ProviderID != "s1" {
				t.Fatalf("snapshot = %#v", result.Snapshot)
			}
			if calls != 2 {
				t.Fatalf("API calls = %d, want 2", calls)
			}
		})
	}
}

func TestPlaylistServiceRejectsInvalidExplicitNavidromeConfig(t *testing.T) {
	root := t.TempDir()
	t.Setenv("HOME", root)
	t.Setenv("XDG_CONFIG_HOME", root)
	t.Setenv(navidrome.EnvConfigPath, "")
	t.Setenv(auth.EnvNavidromePassword, "test-password")
	for _, malformed := range []bool{false, true} {
		t.Run(fmt.Sprintf("malformed=%t", malformed), func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "selected-navidrome.yaml")
			if malformed {
				if err := os.WriteFile(path, []byte("server: ["), 0o600); err != nil {
					t.Fatal(err)
				}
			}
			s := &Server{WorkingDir: root, NavidromeConfigPath: path}
			_, err := s.playlistService().ListProviderPlaylists(context.Background(), playlists.ProviderNavidrome)
			if err == nil || !strings.Contains(err.Error(), path) {
				t.Fatalf("error = %v, want selected config path %s", err, path)
			}
		})
	}
}

func TestPlaylistServicePreservesInjectedDependenciesAndOptions(t *testing.T) {
	factoryCalled := false
	injected := playlists.Service{
		ProviderFactory: func(provider string) (playlists.Provider, error) {
			factoryCalled = true
			return agentPlaylistProvider{}, nil
		},
		NavidromeLoadOptions: navidrome.LoadOptions{
			ExplicitPath: "/injected/navidrome.yaml", Env: map[string]string{},
		},
	}
	s := &Server{
		WorkingDir: "/server/project", NavidromeConfigPath: "/server/navidrome.yaml",
		PlaylistService: &injected,
	}
	service := s.playlistService()
	if service.NavidromeLoadOptions.ExplicitPath != "/injected/navidrome.yaml" ||
		service.NavidromeLoadOptions.WorkingDir != s.WorkingDir || service.NavidromeLoadOptions.Env == nil {
		t.Fatalf("load options = %#v", service.NavidromeLoadOptions)
	}
	if injected.NavidromeLoadOptions.WorkingDir != "" {
		t.Fatal("server mutated injected service")
	}
	if _, err := service.ListProviderPlaylists(context.Background(), playlists.ProviderNavidrome); err != nil || !factoryCalled {
		t.Fatalf("injected factory called = %t, error = %v", factoryCalled, err)
	}
	injected.NavidromeLoadOptions = navidrome.LoadOptions{WorkingDir: "/injected/project"}
	service = s.playlistService()
	if service.NavidromeLoadOptions.ExplicitPath != s.NavidromeConfigPath || service.NavidromeLoadOptions.WorkingDir != "/injected/project" {
		t.Fatalf("load options = %#v", service.NavidromeLoadOptions)
	}
}
