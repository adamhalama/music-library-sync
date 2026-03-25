package auth

import (
	"fmt"
	"os/exec"
	"strings"
)

func runCommandOutput(name string, args ...string) ([]byte, error) {
	return exec.Command(name, args...).Output()
}

func keychainCredential(command commandRunner, service string, account string) string {
	raw, err := command(
		"security",
		"find-generic-password",
		"-s", service,
		"-a", account,
		"-w",
	)
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(raw))
}

func saveKeychainCredential(command commandRunner, service string, account string, value string) error {
	trimmed := strings.TrimSpace(value)
	if trimmed == "" {
		return fmt.Errorf("credential must not be empty")
	}
	if command == nil {
		command = runCommandOutput
	}
	if _, err := command(
		"security",
		"add-generic-password",
		"-U",
		"-s", service,
		"-a", account,
		"-w", trimmed,
	); err != nil {
		return err
	}
	return nil
}

func deleteKeychainCredential(command commandRunner, service string, account string) error {
	if command == nil {
		command = runCommandOutput
	}
	if _, err := command(
		"security",
		"delete-generic-password",
		"-s", service,
		"-a", account,
	); err != nil {
		lower := strings.ToLower(strings.TrimSpace(err.Error()))
		if strings.Contains(lower, "could not be found") || strings.Contains(lower, "item not found") {
			return nil
		}
		return err
	}
	return nil
}
