//go:build !windows

package engine

import (
	"context"
	"errors"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"
)

func TestSubprocessRunnerCancellationKillsProcessGroupAndReturns130(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	childPID := make(chan int, 1)
	result := make(chan ExecResult, 1)
	runner := NewSubprocessRunner(nil, nil, nil)
	go func() {
		result <- runner.Run(ctx, ExecSpec{
			Bin:  "sh",
			Args: []string{"-c", "sleep 30 & child=$!; echo $child; wait $child"},
			StdoutObservers: []func(string){func(line string) {
				pid, err := strconv.Atoi(strings.TrimSpace(line))
				if err == nil {
					select {
					case childPID <- pid:
					default:
					}
				}
			}},
		})
	}()

	var pid int
	select {
	case pid = <-childPID:
	case <-time.After(2 * time.Second):
		t.Fatal("child process did not start")
	}
	cancel()

	select {
	case got := <-result:
		if got.ExitCode != 130 || !got.Interrupted {
			t.Fatalf("canceled result = %+v, want interrupted exit code 130", got)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("runner did not return promptly after cancellation")
	}

	deadline := time.Now().Add(2 * time.Second)
	for {
		err := syscall.Kill(pid, 0)
		if errors.Is(err, syscall.ESRCH) {
			break
		}
		if err != nil {
			t.Fatalf("probe child process %d: %v", pid, err)
		}
		if time.Now().After(deadline) {
			t.Fatalf("child process %d survived process-group cancellation", pid)
		}
		time.Sleep(10 * time.Millisecond)
	}
}
