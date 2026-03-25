package auth

import (
	"errors"
	"fmt"
	"os"
	"strings"
)

const (
	soundCloudKeychainService = "udl.soundcloud"
	soundCloudKeychainAccount = "client_id"
)

var ErrSoundCloudClientIDNotFound = errors.New("soundcloud client id not found")

type SoundCloudClientIDResolver struct {
	Getenv  func(string) string
	Command commandRunner
}

func ResolveSoundCloudClientID() (string, error) {
	value, _, err := ResolveSoundCloudClientIDWithSource()
	return value, err
}

func ResolveSoundCloudClientIDWithSource() (string, CredentialStorageSource, error) {
	return SoundCloudClientIDResolver{
		Getenv:  os.Getenv,
		Command: runCommandOutput,
	}.ResolveWithSource()
}

func (r SoundCloudClientIDResolver) Resolve() (string, error) {
	value, _, err := r.ResolveWithSource()
	return value, err
}

func (r SoundCloudClientIDResolver) ResolveWithSource() (string, CredentialStorageSource, error) {
	getenv := r.Getenv
	if getenv == nil {
		getenv = os.Getenv
	}
	if value := strings.TrimSpace(getenv("SCDL_CLIENT_ID")); value != "" {
		return value, CredentialStorageSourceEnv, nil
	}

	command := r.Command
	if command == nil {
		command = runCommandOutput
	}
	value := keychainCredential(command, soundCloudKeychainService, soundCloudKeychainAccount)
	if value == "" {
		return "", CredentialStorageSourceNone, ErrSoundCloudClientIDNotFound
	}
	return value, CredentialStorageSourceKeychain, nil
}

func SaveSoundCloudClientID(clientID string) error {
	if err := saveKeychainCredential(runCommandOutput, soundCloudKeychainService, soundCloudKeychainAccount, clientID); err != nil {
		return fmt.Errorf("save soundcloud client id to keychain: %w", err)
	}
	return nil
}

func RemoveSoundCloudClientID() error {
	if err := deleteKeychainCredential(runCommandOutput, soundCloudKeychainService, soundCloudKeychainAccount); err != nil {
		return fmt.Errorf("remove soundcloud client id from keychain: %w", err)
	}
	return nil
}
