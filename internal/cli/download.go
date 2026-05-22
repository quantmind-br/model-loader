package cli

import (
	"net/http"
	"os"
	"time"

	"github.com/spf13/cobra"

	"github.com/quantmind-br/model-loader/internal/service/downloadmgr"
)

func init() {
	cmd := &cobra.Command{
		Use:                "download <state-path>",
		Short:              "Internal: run a single download worker (spawned by the manager)",
		Hidden:             true,
		DisableFlagParsing: true, // worker argv is a contract; never reinterpret flags
		Args:               cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			statePath := args[0]

			ua := os.Getenv("MODEL_LOADER_USER_AGENT")
			if ua == "" {
				ua = "model-loader/dev"
			}
			httpClient := &http.Client{
				Timeout: 0, // long downloads — ctx drives cancellation
				Transport: &http.Transport{
					ResponseHeaderTimeout: 60 * time.Second,
					IdleConnTimeout:       90 * time.Second,
				},
			}

			code := downloadmgr.RunWorker(downloadmgr.WorkerConfig{
				StatePath: statePath,
				UserAgent: ua,
				Client:    httpClient,
			})
			if code != 0 {
				return &ExitError{Code: code}
			}
			return nil
		},
	}
	rootCmd.AddCommand(cmd)
}
