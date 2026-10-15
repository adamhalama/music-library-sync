package auth

import (
	"errors"
	"fmt"
	"os"
	"strings"
)

const (
	navidromeKeychainService = "udl.navidrome"
	navidromeKeychainAccount = "default"

	// EnvNavidromePassword is an escape hatch for headless/CI use. The GUI and
	// TUI always write to Keychain instead.
	EnvNavidromePassword = "UDL_NAVIDROME_PASSWORD"
)

// ErrNavidromePasswordNotFound means no password is stored anywhere.
var ErrNavidromePasswordNotFound = errors.New("navidrome password not found")

// NavidromePasswordResolver reads the shared account password. The password is
// never written to configuration, plans, logs, or command arguments.
type NavidromePasswordResolver struct {
	Getenv  func(string) string
	Command commandRunner
}

// ResolveNavidromePassword returns the stored password.
func ResolveNavidromePassword() (string, error) {
	value, _, err := ResolveNavidromePasswordWithSource()
	return value, err
}

// ResolveNavidromePasswordWithSource also reports where the password came from.
func ResolveNavidromePasswordWithSource() (string, CredentialStorageSource, error) {
	return NavidromePasswordResolver{Getenv: os.Getenv, Command: runCommandOutput}.ResolveWithSource()
}

// Resolve returns the password without its source.
func (r NavidromePasswordResolver) Resolve() (string, error) {
	value, _, err := r.ResolveWithSource()
	return value, err
}

// ResolveWithSource prefers the environment override, then Keychain.
func (r NavidromePasswordResolver) ResolveWithSource() (string, CredentialStorageSource, error) {
	getenv := r.Getenv
	if getenv == nil {
		getenv = os.Getenv
	}
	if value := strings.TrimSpace(getenv(EnvNavidromePassword)); value != "" {
		return value, CredentialStorageSourceEnv, nil
	}
	command := r.Command
	if command == nil {
		command = runCommandOutput
	}
	value := keychainCredential(command, navidromeKeychainService, navidromeKeychainAccount)
	if value == "" {
		return "", CredentialStorageSourceNone, ErrNavidromePasswordNotFound
	}
	return value, CredentialStorageSourceKeychain, nil
}

// HasNavidromePassword reports presence without returning the secret.
func HasNavidromePassword() bool {
	value, _, err := ResolveNavidromePasswordWithSource()
	return err == nil && strings.TrimSpace(value) != ""
}

// SaveNavidromePassword stores or replaces the password in macOS Keychain.
func SaveNavidromePassword(password string) error {
	if err := saveKeychainCredential(runCommandOutput, navidromeKeychainService, navidromeKeychainAccount, password); err != nil {
		return fmt.Errorf("save navidrome password to keychain: %w", err)
	}
	return nil
}

// RemoveNavidromePassword clears the stored password.
func RemoveNavidromePassword() error {
	if err := deleteKeychainCredential(runCommandOutput, navidromeKeychainService, navidromeKeychainAccount); err != nil {
		return fmt.Errorf("remove navidrome password from keychain: %w", err)
	}
	return nil
}

// InspectNavidromePassword reports credential health for the credentials UI.
func InspectNavidromePassword(stateDir string) CredentialStatus {
	status := CredentialStatus{
		Kind:    CredentialKindNavidromePassword,
		Title:   "Navidrome account password",
		Health:  CredentialHealthMissing,
		Summary: "Missing. Save the Navidrome account password in macOS Keychain.",
	}
	value, source, err := ResolveNavidromePasswordWithSource()
	if err == nil && strings.TrimSpace(value) != "" {
		status.StorageSource = source
		switch source {
		case CredentialStorageSourceKeychain:
			status.Health = CredentialHealthAvailable
			status.Summary = "Available in macOS Keychain."
		case CredentialStorageSourceEnv:
			status.Health = CredentialHealthExternalOverride
			status.Summary = "Available via environment override."
		default:
			status.Health = CredentialHealthAvailable
			status.Summary = "Available."
		}
	} else if err != nil && !errors.Is(err, ErrNavidromePasswordNotFound) {
		status.Summary = err.Error()
	}
	return applyCredentialMetadata(status, stateDir)
}
