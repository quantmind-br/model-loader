package cli

import (
	"errors"
	"testing"
)

func TestExecute_MapsExitError(t *testing.T) {
	// A command returning *ExitError{Code:2} must surface as exit code 2.
	rootCmd.SetArgs([]string{"_exittest"})
	t.Cleanup(func() { rootCmd.SetArgs(nil) })

	if code := Execute(); code != 2 {
		t.Fatalf("expected exit code 2 from ExitError, got %d", code)
	}
}

func TestExitError_IsError(t *testing.T) {
	var err error = &ExitError{Code: 7}
	var ee *ExitError
	if !errors.As(err, &ee) || ee.Code != 7 {
		t.Fatalf("ExitError should round-trip through errors.As")
	}
}
