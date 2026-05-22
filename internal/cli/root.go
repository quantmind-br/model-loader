// Package cli is the cobra command tree exposing model-loader functionality
// without the interactive TUI. It depends on internal/app for service wiring
// and MUST NOT import the bubbletea TUI (internal/ui) — the TUI is registered
// as the root's default action through the TUIRunner callback to avoid a cycle.
package cli

import (
	"errors"
	"fmt"

	"github.com/spf13/cobra"
)

// TUIRunner is set by package main to the interactive TUI entrypoint. It runs
// when model-loader is invoked with no subcommand. The string argument is the
// resolved --log-level (empty means "use config/env default").
var TUIRunner func(logLevel string) int

// logLevel holds the value of the persistent --log-level flag, read by
// subcommands and passed to app.Bootstrap.
var logLevel string

// ExitError carries a specific process exit code out of a command's RunE so
// Execute can translate it. Commands that need a non-1 failure code (e.g.
// benchmark's exit 2 gate) return *ExitError.
type ExitError struct{ Code int }

func (e *ExitError) Error() string { return fmt.Sprintf("exit code %d", e.Code) }

var rootCmd = &cobra.Command{
	Use:   "model-loader",
	Short: "Manage llama.cpp profiles and llama-server processes",
	Long: "model-loader manages LLM server profiles and processes.\n" +
		"Run with no subcommand to launch the interactive TUI.",
	SilenceUsage:  true,
	SilenceErrors: true,
	RunE: func(cmd *cobra.Command, args []string) error {
		if TUIRunner == nil {
			return errors.New("no TUI runner registered")
		}
		if code := TUIRunner(logLevel); code != 0 {
			return &ExitError{Code: code}
		}
		return nil
	},
}

func init() {
	rootCmd.PersistentFlags().StringVar(&logLevel, "log-level", "",
		"override log level (debug|info|warn|error); also reads $MODEL_LOADER_LOG_LEVEL and config logging.level")
}

// Execute runs the cobra tree and returns the process exit code. An *ExitError
// surfaces its Code; any other error yields 1; success yields 0.
func Execute() int {
	err := rootCmd.Execute()
	var ee *ExitError
	if errors.As(err, &ee) {
		return ee.Code
	}
	if err != nil {
		fmt.Fprintln(rootCmd.ErrOrStderr(), err)
		return 1
	}
	return 0
}
