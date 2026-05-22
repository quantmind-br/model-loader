package cli

import (
	"errors"
	"testing"

	"github.com/spf13/cobra"
)

// init registers a hidden test-only command so Execute() can be exercised
// end-to-end. It lives in the test file so the hook never ships in the
// production binary (only the test binary runs this init).
func init() {
	rootCmd.AddCommand(&cobra.Command{
		Use:    "_exittest",
		Hidden: true,
		RunE:   func(*cobra.Command, []string) error { return &ExitError{Code: 2} },
	})
}

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
