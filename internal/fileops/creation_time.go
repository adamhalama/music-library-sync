package fileops

import "time"

// CreationTime describes a filesystem creation time when the host supports it.
type CreationTime struct {
	Time      time.Time
	Available bool
}
