package main

import (
	"fmt"
	"net/http"
	"os"
	"time"

	"github.com/quantmind-br/model-loader/internal/service/downloadmgr"
)

// runDownloadWorker is the entry point for `model-loader download
// <state-path>`. It blocks until the download finishes, fails, or
// receives SIGTERM. The on-disk record at state-path is the source of
// truth for outcome — exit code is 0 in nearly all cases (the TUI
// reads status, not exit code) except when the state file itself is
// unusable.
func runDownloadWorker() int {
	if len(os.Args) < 2 {
		fmt.Fprintln(os.Stderr, "usage: model-loader download <state-path>")
		return 2
	}
	statePath := os.Args[1]

	ua := os.Getenv("MODEL_LOADER_USER_AGENT")
	if ua == "" {
		ua = "model-loader/dev"
	}
	httpClient := &http.Client{
		Timeout: 0, // long downloads — no client-wide timeout, ctx drives cancellation
		Transport: &http.Transport{
			ResponseHeaderTimeout: 60 * time.Second,
			IdleConnTimeout:       90 * time.Second,
		},
	}

	return downloadmgr.RunWorker(downloadmgr.WorkerConfig{
		StatePath: statePath,
		UserAgent: ua,
		Client:    httpClient,
	})
}
