package cli

import (
	"fmt"
	"io"

	"github.com/quantmind-br/model-loader/internal/config"
	"github.com/quantmind-br/model-loader/internal/service/downloadmgr"
	"github.com/spf13/cobra"
)

func init() {
	var (
		snapshot bool
		wait     bool
	)
	dlCmd := &cobra.Command{
		Use:   "download <repo-id> <filename>",
		Short: "Download a file from a HuggingFace repository",
		Args:  cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
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
			return exitOnErr(cmd.ErrOrStderr(), startDownload(
				cmd.OutOrStdout(), mgr, buildHFClient(),
				firstPath(cfg.Models.SearchPaths), args[0], args[1], snapshot, wait,
			))
		},
	}
	dlCmd.Flags().BoolVar(&snapshot, "snapshot", false, "place the file under a {org}__{repo} subdirectory (snapshot layout)")
	dlCmd.Flags().BoolVar(&wait, "wait", false, "block until the download finishes")
	modelCmd.AddCommand(dlCmd)
}

// startDownload resolves the destination, enqueues a download, and either
// returns immediately (printing the id) or blocks until the download reaches a
// terminal state when wait is true.
func startDownload(out io.Writer, mgr downloadManager, hub hubClient, searchPath, repoID, filename string, snapshot, wait bool) error {
	if searchPath == "" {
		return fmt.Errorf("no model search path configured (set models.search_paths)")
	}
	destDir, destFile, err := downloadmgr.ResolveDest(searchPath, repoID, filename, snapshot)
	if err != nil {
		return fmt.Errorf("resolve destination: %w", err)
	}
	url := hub.DownloadURL(repoID, filename)

	var sub <-chan downloadmgr.Event
	if wait {
		sub = mgr.Subscribe()
		mgr.StartPolling()
	}

	id, err := mgr.Start(downloadmgr.Spec{
		RepoID:     repoID,
		Filename:   filename,
		URL:        url,
		DestDir:    destDir,
		DestFile:   destFile,
		IsSnapshot: snapshot,
	})
	if err != nil {
		return fmt.Errorf("start download: %w", err)
	}

	if !wait {
		if jsonOut {
			return emitJSON(out, map[string]string{"id": string(id), "status": "started", "dest": destFile})
		}
		fmt.Fprintf(out, "started download %s → %s\n", id, destFile)
		return nil
	}

	for ev := range sub {
		if ev.ID != id || !ev.State.Status.IsTerminal() {
			continue
		}
		if ev.State.Status == downloadmgr.StatusCompleted {
			if jsonOut {
				return emitJSON(out, map[string]string{"id": string(id), "status": "completed", "dest": destFile})
			}
			fmt.Fprintf(out, "completed %s → %s\n", id, destFile)
			return nil
		}
		if ev.State.Err != nil {
			return fmt.Errorf("download %s ended: %s (%v)", id, ev.State.Status, ev.State.Err)
		}
		return fmt.Errorf("download %s ended: %s", id, ev.State.Status)
	}
	return nil
}
