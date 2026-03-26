package auth

import (
	"errors"
	"testing"
)

func TestSoundCloudClientIDResolverPrefersEnv(t *testing.T) {
	resolver := SoundCloudClientIDResolver{
		Getenv: func(key string) string {
			if key == "SCDL_CLIENT_ID" {
				return " env-client-id "
			}
			return ""
		},
		Command: func(name string, args ...string) ([]byte, error) {
			return nil, errors.New("should not read keychain")
		},
	}

	value, source, err := resolver.ResolveWithSource()
	if err != nil {
		t.Fatalf("resolve soundcloud client id: %v", err)
	}
	if value != "env-client-id" {
		t.Fatalf("unexpected client id %q", value)
	}
	if source != CredentialStorageSourceEnv {
		t.Fatalf("unexpected source %q", source)
	}
}

func TestSoundCloudClientIDResolverFallsBackToKeychain(t *testing.T) {
	resolver := SoundCloudClientIDResolver{
		Getenv: func(key string) string { return "" },
		Command: func(name string, args ...string) ([]byte, error) {
			return []byte("keychain-client-id\n"), nil
		},
	}

	value, source, err := resolver.ResolveWithSource()
	if err != nil {
		t.Fatalf("resolve soundcloud client id: %v", err)
	}
	if value != "keychain-client-id" {
		t.Fatalf("unexpected client id %q", value)
	}
	if source != CredentialStorageSourceKeychain {
		t.Fatalf("unexpected source %q", source)
	}
}
