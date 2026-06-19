//go:build !darwin

package fileops

// ReadCreationTime reports that filesystem creation time is unavailable.
func ReadCreationTime(string) (CreationTime, error) {
	return CreationTime{}, nil
}

// RestoreCreationTime is a no-op on unsupported platforms.
func RestoreCreationTime(string, CreationTime) error {
	return nil
}
