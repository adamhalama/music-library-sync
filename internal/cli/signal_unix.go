//go:build !windows

package cli

import (
	"os"
	"syscall"
)

func interruptSignals() []os.Signal {
	return []os.Signal{syscall.SIGINT, syscall.SIGTERM}
}
