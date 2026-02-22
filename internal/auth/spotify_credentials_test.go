package auth

import (
	"errors"
	"testing"
)

func TestSpotifyCredentialsResolverPrefersEnv(t *testing.T) {
	resolver := SpotifyCredentialsResolver{
		Getenv: func(key string) string {
			switch key {
			case "UDL_SPOTIFY_CLIENT_ID":
				return "client-id"
			case "UDL_SPOTIFY_CLIENT_SECRET":
				return "client-secret"
			default:
				return ""
			}
		},
		ReadFile: func(path string) ([]byte, error) {
			return nil, errors.New("should not read file")
		},
		Command: func(name string, args ...string) ([]byte, error) {
			return nil, errors.New("should not read keychain")
		},
	}

	creds, err := resolver.Resolve()
	if err != nil {
		t.Fatalf("resolve credentials: %v", err)
	}
	if creds.ClientID != "client-id" || creds.ClientSecret != "client-secret" {
		t.Fatalf("unexpected credentials: %+v", creds)
	}
}

func TestSpotifyCredentialsResolverRequiresBothEnvValues(t *testing.T) {
	resolver := SpotifyCredentialsResolver{
		Getenv: func(key string) string {
			if key == "UDL_SPOTIFY_CLIENT_ID" {
				return "client-id"
			}
			return ""
		},
		Command: func(name string, args ...string) ([]byte, error) {
			return nil, errors.New("should not read keychain")
		},
	}

	_, err := resolver.Resolve()
	if err == nil {
		t.Fatalf("expected error when only one env var is set")
	}
}

func TestSpotifyCredentialsResolverFallsBackToSpotDLConfig(t *testing.T) {
	resolver := SpotifyCredentialsResolver{
		Getenv: func(key string) string { return "" },
		Command: func(name string, args ...string) ([]byte, error) {
			return nil, errors.New("not found")
		},
		HomeDir: func() (string, error) {
			return "/home/tester", nil
		},
		ReadFile: func(path string) ([]byte, error) {
			if path != "/home/tester/.spotdl/config.json" {
				t.Fatalf("unexpected config path: %s", path)
			}
			return []byte(`{"client_id":"a","client_secret":"b"}`), nil
		},
	}

	creds, err := resolver.Resolve()
	if err != nil {
		t.Fatalf("resolve credentials: %v", err)
	}
	if creds.ClientID != "a" || creds.ClientSecret != "b" {
		t.Fatalf("unexpected credentials: %+v", creds)
	}
}

func TestSpotifyCredentialsResolverNotFound(t *testing.T) {
	resolver := SpotifyCredentialsResolver{
		Getenv: func(key string) string { return "" },
		Command: func(name string, args ...string) ([]byte, error) {
			return nil, errors.New("not found")
		},
		HomeDir: func() (string, error) {
			return "/home/tester", nil
		},
		ReadFile: func(path string) ([]byte, error) {
			return nil, errors.New("missing")
		},
	}

	_, err := resolver.Resolve()
	if !errors.Is(err, ErrSpotifyCredentialsNotFound) {
		t.Fatalf("expected ErrSpotifyCredentialsNotFound, got %v", err)
	}
}

func TestSpotifyCredentialsResolverFallsBackToKeychain(t *testing.T) {
	resolver := SpotifyCredentialsResolver{
		Getenv: func(key string) string { return "" },
		Command: func(name string, args ...string) ([]byte, error) {
			account := ""
			for i := 0; i < len(args)-1; i++ {
				if args[i] == "-a" {
					account = args[i+1]
					break
				}
			}
			if name != "security" {
				return nil, errors.New("unexpected command")
			}
			if account == "client_id" {
				return []byte("keychain-client-id"), nil
			}
			if account == "client_secret" {
				return []byte("keychain-client-secret"), nil
			}
			return nil, errors.New("missing")
		},
		ReadFile: func(path string) ([]byte, error) {
			return nil, errors.New("should not read file")
		},
	}

	creds, err := resolver.Resolve()
	if err != nil {
		t.Fatalf("resolve credentials: %v", err)
	}
	if creds.ClientID != "keychain-client-id" || creds.ClientSecret != "keychain-client-secret" {
		t.Fatalf("unexpected credentials from keychain: %+v", creds)
	}
}
