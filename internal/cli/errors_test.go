package cli

import (
	"errors"
	"testing"

	"github.com/jaa/update-downloads/internal/exitcode"
)

func TestMapExitCode(t *testing.T) {
	tests := []struct {
		name string
		err  error
		want int
	}{
		{name: "nil", err: nil, want: exitcode.Success},
		{name: "coded", err: &ExitError{Code: exitcode.InvalidConfig, Err: errors.New("bad")}, want: exitcode.InvalidConfig},
		{name: "unknown command", err: errors.New("unknown command \"x\" for \"udl\""), want: exitcode.InvalidUsage},
		{name: "generic", err: errors.New("boom"), want: exitcode.RuntimeFailure},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := mapExitCode(tc.err); got != tc.want {
				t.Fatalf("mapExitCode() = %d, want %d", got, tc.want)
			}
		})
	}
}
