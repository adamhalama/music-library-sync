package auth

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
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
	return DeemixARLResolver{
		Getenv:  os.Getenv,
		Command: runCommandOutput,
	}.Resolve()
}

func (r DeemixARLResolver) Resolve() (string, error) {
	getenv := r.Getenv
	if getenv == nil {
		getenv = os.Getenv
	}
	if value := strings.TrimSpace(getenv("UDL_DEEMIX_ARL")); value != "" {
		return value, nil
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
		return "", ErrDeemixARLNotFound
	}
	value := strings.TrimSpace(string(raw))
	if value == "" {
		return "", ErrDeemixARLNotFound
	}
	return value, nil
}

func SaveDeemixARL(arl string) error {
	trimmed := strings.TrimSpace(arl)
	if trimmed == "" {
		return fmt.Errorf("deemix ARL must not be empty")
	}
	_, err := runCommandOutput(
		"security",
		"add-generic-password",
		"-U",
		"-s", deemixKeychainService,
		"-a", deemixKeychainAccount,
		"-w", trimmed,
	)
	if err != nil {
		return fmt.Errorf("save deemix ARL to keychain: %w", err)
	}
	return nil
}

func runCommandOutput(name string, args ...string) ([]byte, error) {
	return exec.Command(name, args...).Output()
}
