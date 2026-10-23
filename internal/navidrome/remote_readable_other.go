//go:build !darwin && !linux

package navidrome

// Remote mode is supported on macOS. Refuse conservatively on other platforms
// without a nonblocking, no-follow regular-file access probe.
func readablePushSourceFile(string) bool { return false }
