//go:build !unix

package navidrome

// lockMirror is a no-op where flock is unavailable; remote mode targets macOS.
func lockMirror(Resolved) (func(), error) {
	return func() {}, nil
}
