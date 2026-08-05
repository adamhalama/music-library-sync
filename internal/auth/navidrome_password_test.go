package auth

import (
	"errors"
	"strings"
	"testing"
)

func TestNavidromePasswordPrefersEnvironmentOverride(t *testing.T) {
	resolver := NavidromePasswordResolver{
		Getenv: func(key string) string {
			if key == EnvNavidromePassword {
				return "  from-env  "
			}
			return ""
		},
		Command: func(string, ...string) ([]byte, error) {
			t.Fatalf("keychain must not be consulted when the override is set")
			return nil, nil
		},
	}
	value, source, err := resolver.ResolveWithSource()
	if err != nil {
		t.Fatalf("ResolveWithSource: %v", err)
	}
	if value != "from-env" {
		t.Fatalf("value = %q", value)
	}
	if source != CredentialStorageSourceEnv {
		t.Fatalf("source = %q", source)
	}
}

func TestNavidromePasswordReadsKeychain(t *testing.T) {
	var args []string
	resolver := NavidromePasswordResolver{
		Getenv: func(string) string { return "" },
		Command: func(_ string, commandArgs ...string) ([]byte, error) {
			args = commandArgs
			return []byte("stored-secret\n"), nil
		},
	}
	value, source, err := resolver.ResolveWithSource()
	if err != nil {
		t.Fatalf("ResolveWithSource: %v", err)
	}
	if value != "stored-secret" {
		t.Fatalf("value = %q", value)
	}
	if source != CredentialStorageSourceKeychain {
		t.Fatalf("source = %q", source)
	}
	// The read must never pass a secret on the command line; only the service
	// and account identify the item.
	joined := strings.Join(args, " ")
	if strings.Contains(joined, "stored-secret") {
		t.Fatalf("keychain read leaked the secret into argv: %s", joined)
	}
	if !strings.Contains(joined, navidromeKeychainService) {
		t.Fatalf("keychain read did not target the UDL service: %s", joined)
	}
}

func TestNavidromePasswordMissingIsTyped(t *testing.T) {
	resolver := NavidromePasswordResolver{
		Getenv:  func(string) string { return "" },
		Command: func(string, ...string) ([]byte, error) { return nil, errors.New("not found") },
	}
	_, source, err := resolver.ResolveWithSource()
	if !errors.Is(err, ErrNavidromePasswordNotFound) {
		t.Fatalf("error = %v, want ErrNavidromePasswordNotFound", err)
	}
	if source != CredentialStorageSourceNone {
		t.Fatalf("source = %q", source)
	}
}
