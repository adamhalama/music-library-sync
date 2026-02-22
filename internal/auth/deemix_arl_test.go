package auth

import (
	"errors"
	"testing"
)

func TestDeemixARLResolverPrefersEnv(t *testing.T) {
	resolver := DeemixARLResolver{
		Getenv: func(key string) string {
			if key == "UDL_DEEMIX_ARL" {
				return " env-arl "
			}
			return ""
		},
		Command: func(name string, args ...string) ([]byte, error) {
			return nil, errors.New("should not execute command")
		},
	}

	got, err := resolver.Resolve()
	if err != nil {
		t.Fatalf("resolve arl: %v", err)
	}
	if got != "env-arl" {
		t.Fatalf("unexpected arl. got=%q", got)
	}
}

func TestDeemixARLResolverUsesKeychainLookup(t *testing.T) {
	resolver := DeemixARLResolver{
		Getenv: func(key string) string { return "" },
		Command: func(name string, args ...string) ([]byte, error) {
			return []byte("keychain-arl\n"), nil
		},
	}

	got, err := resolver.Resolve()
	if err != nil {
		t.Fatalf("resolve arl: %v", err)
	}
	if got != "keychain-arl" {
		t.Fatalf("unexpected arl. got=%q", got)
	}
}

func TestDeemixARLResolverNotFound(t *testing.T) {
	resolver := DeemixARLResolver{
		Getenv: func(key string) string { return "" },
		Command: func(name string, args ...string) ([]byte, error) {
			return nil, errors.New("not found")
		},
	}

	_, err := resolver.Resolve()
	if !errors.Is(err, ErrDeemixARLNotFound) {
		t.Fatalf("expected ErrDeemixARLNotFound, got %v", err)
	}
}
