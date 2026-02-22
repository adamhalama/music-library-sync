package cli

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoadDotEnvFilesLoadsEnvAndLocalOverrides(t *testing.T) {
	tmp := t.TempDir()
	envPath := filepath.Join(tmp, ".env")
	localPath := filepath.Join(tmp, ".env.local")

	if err := os.WriteFile(envPath, []byte("UDL_DEEMIX_BIN=/tmp/bin/deemix-a\nUDL_THREADS=1\n"), 0o644); err != nil {
		t.Fatalf("write .env: %v", err)
	}
	if err := os.WriteFile(localPath, []byte("UDL_DEEMIX_BIN=/tmp/bin/deemix-b\n"), 0o644); err != nil {
		t.Fatalf("write .env.local: %v", err)
	}

	values := map[string]string{}
	setenv := func(k, v string) error {
		values[k] = v
		return nil
	}

	if err := loadDotEnvFiles(tmp, nil, setenv); err != nil {
		t.Fatalf("load dotenv files: %v", err)
	}
	if values["UDL_DEEMIX_BIN"] != "/tmp/bin/deemix-b" {
		t.Fatalf("expected .env.local to override .env, got %q", values["UDL_DEEMIX_BIN"])
	}
	if values["UDL_THREADS"] != "1" {
		t.Fatalf("expected UDL_THREADS from .env, got %q", values["UDL_THREADS"])
	}
}

func TestLoadDotEnvFilesDoesNotOverrideProcessEnv(t *testing.T) {
	tmp := t.TempDir()
	envPath := filepath.Join(tmp, ".env")
	if err := os.WriteFile(envPath, []byte("UDL_DEEMIX_BIN=/tmp/bin/deemix\n"), 0o644); err != nil {
		t.Fatalf("write .env: %v", err)
	}

	values := map[string]string{}
	setenv := func(k, v string) error {
		values[k] = v
		return nil
	}

	if err := loadDotEnvFiles(tmp, []string{"UDL_DEEMIX_BIN=/already/set"}, setenv); err != nil {
		t.Fatalf("load dotenv files: %v", err)
	}
	if _, exists := values["UDL_DEEMIX_BIN"]; exists {
		t.Fatalf("expected existing process env to be protected")
	}
}

func TestParseDotEnvLineSupportsExportAndQuotedValues(t *testing.T) {
	key, value, ok, err := parseDotEnvLine("export UDL_DEEMIX_BIN=\"/Users/test/.local/bin/deemix\"")
	if err != nil {
		t.Fatalf("parse line: %v", err)
	}
	if !ok || key != "UDL_DEEMIX_BIN" || value != "/Users/test/.local/bin/deemix" {
		t.Fatalf("unexpected parse result: ok=%v key=%q value=%q", ok, key, value)
	}

	key, value, ok, err = parseDotEnvLine("UDL_DEEMIX_ARL='abc123'")
	if err != nil {
		t.Fatalf("parse single-quoted line: %v", err)
	}
	if !ok || key != "UDL_DEEMIX_ARL" || value != "abc123" {
		t.Fatalf("unexpected single-quoted parse result: ok=%v key=%q value=%q", ok, key, value)
	}
}
