package cli

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"path/filepath"
	"time"

	"github.com/quantmind-br/model-loader/internal/config"
	"github.com/quantmind-br/model-loader/internal/service/downloadmgr"
	"github.com/quantmind-br/model-loader/internal/service/hfhub"
	"github.com/quantmind-br/model-loader/internal/service/modelscanner"
	"github.com/spf13/cobra"
)

// userAgent is sent on HuggingFace Hub requests and to download workers,
// mirroring the value used by the TUI build in cmd/model-loader/main.go.
const userAgent = "model-loader/dev"

var modelCmd = &cobra.Command{
	Use:   "model",
	Short: "Scan local models, search HuggingFace, and manage downloads",
}

func init() {
	rootCmd.AddCommand(modelCmd)
}

// hubClient is the narrow slice of *hfhub.Client the CLI needs, so commands can
// be unit-tested against a fake.
type hubClient interface {
	Search(ctx context.Context, query string, limit int) ([]hfhub.SearchResult, error)
	RepoInfo(ctx context.Context, repoID string) (*hfhub.RepoInfo, error)
	DownloadURL(repoID, filename string) string
}

// downloadManager is the narrow slice of *downloadmgr.Manager the CLI needs.
type downloadManager interface {
	Start(spec downloadmgr.Spec) (downloadmgr.ID, error)
	Cancel(id downloadmgr.ID) error
	Resume(id downloadmgr.ID) error
	Snapshot() []downloadmgr.State
	Subscribe() <-chan downloadmgr.Event
	StartPolling()
}

func buildScanner() modelscanner.Scanner { return modelscanner.New() }

func buildHFClient() *hfhub.Client {
	return hfhub.NewClient(&http.Client{Timeout: 30 * time.Second}, userAgent)
}

// buildDownloadManager constructs a Manager rooted at <stateDir>/downloads and
// reconciles persisted state so Snapshot/Cancel/Resume see existing downloads.
func buildDownloadManager(cfg config.AppConfig) (*downloadmgr.Manager, error) {
	dir := filepath.Join(cfg.Paths.StateDir, "downloads")
	m := downloadmgr.NewManager(dir, 3).WithUserAgent(userAgent)
	if err := m.Reconcile(); err != nil {
		return nil, err
	}
	return m, nil
}

// firstPath returns the first configured search path, or "" when none exist.
func firstPath(paths []string) string {
	if len(paths) == 0 {
		return ""
	}
	return paths[0]
}

// exitOnErr prints err to errw and returns *ExitError{1}; nil returns nil.
func exitOnErr(errw io.Writer, err error) error {
	if err != nil {
		fmt.Fprintln(errw, err)
		return &ExitError{Code: 1}
	}
	return nil
}

// runWithManager loads config, builds+reconciles a download Manager, runs fn,
// and maps errors to *ExitError. Used by the downloads subcommands.
func runWithManager(cmd *cobra.Command, fn func(out io.Writer, mgr downloadManager) error) error {
	cfg, err := config.Load()
	if err != nil {
		fmt.Fprintf(cmd.ErrOrStderr(), "config error: %v\n", err)
		return &ExitError{Code: 1}
	}
	mgr, err := buildDownloadManager(cfg)
	if err != nil {
		fmt.Fprintf(cmd.ErrOrStderr(), "download manager: %v\n", err)
		return &ExitError{Code: 1}
	}
	defer mgr.Close()
	return exitOnErr(cmd.ErrOrStderr(), fn(cmd.OutOrStdout(), mgr))
}
