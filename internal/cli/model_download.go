package cli

import (
	"errors"
	"fmt"
	"io"

	"github.com/quantmind-br/model-loader/internal/config"
	"github.com/quantmind-br/model-loader/internal/service/downloadmgr"
	"github.com/spf13/cobra"
)

func init() {
	registerModelDownloadCmd()
	registerModelDownloadsCmd()
}

// registerModelDownloadCmd registers `model download <repo> <file>`.
func registerModelDownloadCmd() {
	var (
		snapshot bool
		wait     bool
	)
	dlCmd := &cobra.Command{
		Use:   "download <repo-id> <filename>",
		Short: "Download a file from a HuggingFace repository",
		Long: `Download a single file from a HuggingFace repository.

The file is placed under the first configured model search path. With --wait the
command blocks until the download finishes and prints the result; without it the
download runs in the background and the command prints the download id immediately.

With --json the output is a single object:
  id      string — download id
  status  string — "started" (no --wait) or "completed"
  dest    string — absolute destination path`,
		Example: `  model-loader model download org/my-model model.gguf
  model-loader model download org/my-model model.gguf --wait
  model-loader model download org/my-model model.gguf --wait --json`,
		Args: cobra.ExactArgs(2),
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
				cmd.OutOrStdout(), cmd.ErrOrStderr(), mgr, buildHFClient(),
				firstPath(cfg.Models.SearchPaths), args[0], args[1], snapshot, wait,
			))
		},
	}
	dlCmd.Flags().BoolVar(&snapshot, "snapshot", false, "place the file under a {org}__{repo} subdirectory (snapshot layout)")
	dlCmd.Flags().BoolVar(&wait, "wait", false, "block until the download finishes")
	modelCmd.AddCommand(dlCmd)
}

// printDownloadProgress emits a plain progress line for a non-terminal event.
// Known totals print once per 10% decile crossed (the first 0% line is
// intentional — it signals the download is alive); unknown totals print once
// per 256 MiB.
func printDownloadProgress(errw io.Writer, filename string, st downloadmgr.State, lastDecile *int, lastChunk *int64) {
	if st.Total > 0 {
		decile := int(st.Bytes * 10 / st.Total)
		if decile > *lastDecile {
			*lastDecile = decile
			fmt.Fprintf(errw, "downloading %s: %s / %s (%d%%)\n",
				filename, humanBytes(st.Bytes), humanBytes(st.Total), st.Bytes*100/st.Total)
		}
	} else {
		chunk := st.Bytes / (256 << 20)
		if chunk > *lastChunk {
			*lastChunk = chunk
			fmt.Fprintf(errw, "downloading %s: %s\n", filename, humanBytes(st.Bytes))
		}
	}
}

// startDownload resolves the destination, enqueues a download, and either
// returns immediately (printing the id) or blocks until the download reaches a
// terminal state when wait is true.
func startDownload(out io.Writer, errw io.Writer, mgr downloadManager, hub hubClient, searchPath, repoID, filename string, snapshot, wait bool) error {
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

	lastDecile := -1
	lastChunk := int64(-1)
	for ev := range sub {
		if ev.ID != id {
			continue
		}
		if !ev.State.Status.IsTerminal() {
			printDownloadProgress(errw, filename, ev.State, &lastDecile, &lastChunk)
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

// downloadItem is the JSON view of a download's state.
type downloadItem struct {
	ID       string `json:"id"`
	Repo     string `json:"repo,omitempty"`
	Filename string `json:"filename,omitempty"`
	Status   string `json:"status"`
	Bytes    int64  `json:"bytes"`
	Total    int64  `json:"total"`
}

// registerModelDownloadsCmd registers `model downloads` and its subcommands.
func registerModelDownloadsCmd() {
	downloadsCmd := &cobra.Command{
		Use:   "downloads",
		Short: "List and manage downloads",
		Long: `List active, queued, and recently completed downloads.

With --json the output is a JSON array of download objects ([] when empty).
Each element includes:
  id        string — download id
  repo      string — HuggingFace repo id
  filename  string — filename within the repo
  status    string — queued | active | completed | failed | cancelled | abandoned
  bytes     int    — bytes downloaded so far
  total     int    — total file size in bytes (0 if unknown)`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return runWithManager(cmd, func(out io.Writer, mgr downloadManager) error {
				return listDownloads(out, mgr, jsonOut)
			})
		},
	}
	cancelCmd := &cobra.Command{
		Use:   "cancel <id>",
		Short: "Cancel a download",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runWithManager(cmd, func(out io.Writer, mgr downloadManager) error {
				return cancelDownload(out, mgr, args[0], jsonOut)
			})
		},
	}
	resumeCmd := &cobra.Command{
		Use:   "resume <id>",
		Short: "Resume an abandoned or failed download",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runWithManager(cmd, func(out io.Writer, mgr downloadManager) error {
				return resumeDownload(out, mgr, args[0], jsonOut)
			})
		},
	}
	downloadsCmd.AddCommand(cancelCmd)
	downloadsCmd.AddCommand(resumeCmd)
	modelCmd.AddCommand(downloadsCmd)
}

func listDownloads(out io.Writer, mgr downloadManager, asJSON bool) error {
	states := mgr.Snapshot()
	if asJSON {
		items := make([]downloadItem, 0, len(states))
		for _, s := range states {
			items = append(items, downloadItem{
				ID: string(s.ID), Repo: s.Spec.RepoID, Filename: s.Spec.Filename,
				Status: s.Status.String(), Bytes: s.Bytes, Total: s.Total,
			})
		}
		return emitJSON(out, items)
	}
	if len(states) == 0 {
		fmt.Fprintln(out, "no downloads")
		return nil
	}
	rows := make([][]string, 0, len(states))
	for _, s := range states {
		progress := humanBytes(s.Bytes)
		if s.Total > 0 {
			progress = fmt.Sprintf("%s / %s", humanBytes(s.Bytes), humanBytes(s.Total))
		}
		rows = append(rows, []string{
			string(s.ID), dashOr(s.Spec.RepoID), dashOr(s.Spec.Filename), s.Status.String(), progress,
		})
	}
	printTable(out, []string{"ID", "REPO", "FILE", "STATUS", "PROGRESS"}, rows)
	return nil
}

// resolveDownloadID matches ref against a download id exactly, then by unique prefix.
func resolveDownloadID(mgr downloadManager, ref string) (downloadmgr.ID, error) {
	states := mgr.Snapshot()
	s, err := resolveByPrefix(states, ref, func(s downloadmgr.State) []string { return []string{string(s.ID)} })
	if err != nil {
		var amb *ambiguousMatchError[downloadmgr.State]
		if errors.As(err, &amb) {
			labels := make([]string, len(amb.Matches))
			for i, m := range amb.Matches {
				labels[i] = string(m.ID)
			}
			return "", fmt.Errorf("ambiguous download id %q matches %d downloads: %s; use a longer prefix", ref, len(amb.Matches), formatCandidates(labels))
		}
		return "", fmt.Errorf("download not found: %s", ref)
	}
	return s.ID, nil
}

func cancelDownload(out io.Writer, mgr downloadManager, ref string, asJSON bool) error {
	id, err := resolveDownloadID(mgr, ref)
	if err != nil {
		return err
	}
	if err := mgr.Cancel(id); err != nil {
		return fmt.Errorf("cancel: %w", err)
	}
	if asJSON {
		return emitJSON(out, map[string]string{"id": string(id), "status": "cancelled"})
	}
	fmt.Fprintf(out, "cancelled %s\n", id)
	return nil
}

func resumeDownload(out io.Writer, mgr downloadManager, ref string, asJSON bool) error {
	id, err := resolveDownloadID(mgr, ref)
	if err != nil {
		return err
	}
	if err := mgr.Resume(id); err != nil {
		if errors.Is(err, downloadmgr.ErrNotResumable) {
			return fmt.Errorf("download not resumable: %s", id)
		}
		return fmt.Errorf("resume: %w", err)
	}
	if asJSON {
		return emitJSON(out, map[string]string{"id": string(id), "status": "resuming"})
	}
	fmt.Fprintf(out, "resuming %s\n", id)
	return nil
}
