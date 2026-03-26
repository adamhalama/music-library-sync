package auth

import (
	"errors"
	"fmt"
	"os"
	"strings"
)

const (
	deemixKeychainService = "udl.deemix"
	deemixKeychainAccount = "default"
)

var ErrDeemixARLNotFound = errors.New("deemix ARL not found")

type commandRunner func(name string, args ...string) ([]byte, error)

type DeemixARLResolver struct {
	Getenv  func(string) string
	Command commandRunner
}

func ResolveDeemixARL() (string, error) {
	value, _, err := ResolveDeemixARLWithSource()
	return value, err
}

func ResolveDeemixARLWithSource() (string, CredentialStorageSource, error) {
	return DeemixARLResolver{
		Getenv:  os.Getenv,
		Command: runCommandOutput,
	}.ResolveWithSource()
}

func (r DeemixARLResolver) Resolve() (string, error) {
	value, _, err := r.ResolveWithSource()
	return value, err
}

func (r DeemixARLResolver) ResolveWithSource() (string, CredentialStorageSource, error) {
	getenv := r.Getenv
	if getenv == nil {
		getenv = os.Getenv
	}
	if value := strings.TrimSpace(getenv("UDL_DEEMIX_ARL")); value != "" {
		return value, CredentialStorageSourceEnv, nil
	}

	command := r.Command
	if command == nil {
		command = runCommandOutput
	}
	raw, err := command(
		"security",
		"find-generic-password",
		"-s", deemixKeychainService,
		"-a", deemixKeychainAccount,
		"-w",
	)
	if err != nil {
		return "", CredentialStorageSourceNone, ErrDeemixARLNotFound
	}
	value := strings.TrimSpace(string(raw))
	if value == "" {
		return "", CredentialStorageSourceNone, ErrDeemixARLNotFound
	}
	return value, CredentialStorageSourceKeychain, nil
}

func SaveDeemixARL(arl string) error {
	if err := saveKeychainCredential(runCommandOutput, deemixKeychainService, deemixKeychainAccount, arl); err != nil {
		return fmt.Errorf("save deemix ARL to keychain: %w", err)
	}
	return nil
}

func RemoveDeemixARL() error {
	if err := deleteKeychainCredential(runCommandOutput, deemixKeychainService, deemixKeychainAccount); err != nil {
		return fmt.Errorf("remove deemix ARL from keychain: %w", err)
	}
	return nil
}
