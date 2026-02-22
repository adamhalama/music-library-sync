package cli

import (
	"errors"
	"strings"

	"github.com/jaa/update-downloads/internal/exitcode"
)

type ExitError struct {
	Code int
	Err  error
}

func (e *ExitError) Error() string {
	if e.Err == nil {
		return ""
	}
	return e.Err.Error()
}

func (e *ExitError) Unwrap() error {
	return e.Err
}

func withExitCode(code int, err error) error {
	if err == nil {
		return nil
	}
	return &ExitError{Code: code, Err: err}
}

func mapExitCode(err error) int {
	if err == nil {
		return exitcode.Success
	}
	var coded *ExitError
	if errors.As(err, &coded) {
		return coded.Code
	}
	message := err.Error()
	if strings.Contains(message, "unknown command") || strings.Contains(message, "unknown flag") {
		return exitcode.InvalidUsage
	}
	return exitcode.RuntimeFailure
}
