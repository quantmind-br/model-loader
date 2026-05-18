package downloadmgr

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"
)

// WorkerConfig is the input to RunWorker.
type WorkerConfig struct {
	// StatePath is the per-download JSON file the worker reads at start
	// and writes progress/terminal status to.
	StatePath string
	// UserAgent is set on outgoing HTTP requests.
	UserAgent string
	// Client overrides the default HTTP client (tests inject httptest).
	Client *http.Client
	// CheckpointBytes is how many bytes between disk progress writes.
	// Zero falls back to the package default.
	CheckpointBytes int64
	// CheckpointInterval is the max wall time between disk progress
	// writes (even with no new bytes). Zero falls back to the default.
	CheckpointInterval time.Duration
}

const (
	defaultCheckpointBytes    = 256 * 1024
	defaultCheckpointInterval = 500 * time.Millisecond
)

// RunWorker performs a single download to completion (or terminal failure)
// and updates the on-disk state file as it goes. Always returns a value
// safe to pass to os.Exit:
//   - 0 on success, cancellation, or recorded failure (state file holds
//     the actual outcome — the TUI reads status, not exit code).
//   - non-zero only on fatal initialization errors (bad state file, etc.)
//     where the TUI cannot even tell what went wrong from the record.
func RunWorker(cfg WorkerConfig) int {
	if cfg.StatePath == "" {
		fmt.Fprintln(os.Stderr, "download worker: empty state path")
		return 2
	}
	stateDir := filepath.Dir(cfg.StatePath)

	rec, err := LoadRecord(cfg.StatePath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "download worker: load state: %v\n", err)
		return 2
	}

	checkpointBytes := cfg.CheckpointBytes
	if checkpointBytes <= 0 {
		checkpointBytes = defaultCheckpointBytes
	}
	checkpointInterval := cfg.CheckpointInterval
	if checkpointInterval <= 0 {
		checkpointInterval = defaultCheckpointInterval
	}
	client := cfg.Client
	if client == nil {
		client = http.DefaultClient
	}

	// Update record: PID + status=active.
	rec.PID = os.Getpid()
	rec.Status = StatusActive
	if rec.StartedAt.IsZero() {
		rec.StartedAt = time.Now().UTC()
	}
	rec.Err = ""
	_ = SaveRecord(stateDir, rec)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// SIGTERM/SIGINT → mark cancelled, drop the partial, exit clean.
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGTERM, syscall.SIGINT)
	cancelled := make(chan struct{})
	go func() {
		select {
		case <-sigCh:
			close(cancelled)
			cancel()
		case <-ctx.Done():
		}
	}()
	defer signal.Stop(sigCh)

	err = runDownload(ctx, client, cfg.UserAgent, stateDir, &rec, checkpointBytes, checkpointInterval)

	select {
	case <-cancelled:
		rec.Status = StatusCancelled
		rec.Err = ""
		_ = os.Remove(rec.DestFile + ".partial")
	default:
		switch {
		case err == nil:
			rec.Status = StatusCompleted
			rec.Err = ""
		case errors.Is(err, context.Canceled):
			rec.Status = StatusCancelled
			rec.Err = ""
			_ = os.Remove(rec.DestFile + ".partial")
		default:
			rec.Status = StatusFailed
			rec.Err = err.Error()
		}
	}
	if e := SaveRecord(stateDir, rec); e != nil {
		fmt.Fprintf(os.Stderr, "download worker: save terminal state: %v\n", e)
	}
	return 0
}

// runDownload performs the HTTP transfer and incremental disk writes.
// It updates rec.Bytes and rec.Total in place; the caller persists the
// terminal status. Returns nil on success.
func runDownload(
	ctx context.Context,
	client *http.Client,
	userAgent, stateDir string,
	rec *DownloadRecord,
	checkpointBytes int64,
	checkpointInterval time.Duration,
) error {
	if rec.URL == "" {
		return errors.New("worker: empty URL")
	}
	if rec.DestFile == "" {
		return errors.New("worker: empty dest file")
	}
	if err := os.MkdirAll(filepath.Dir(rec.DestFile), 0o755); err != nil {
		return fmt.Errorf("mkdir dest: %w", err)
	}

	partialPath := rec.DestFile + ".partial"

	// Resume support: if a partial file is present with bytes >0, send
	// Range. The server may answer 206 (resume) or 200 (ignored — full
	// body) so we adapt the file open mode after seeing the response.
	var existing int64
	if info, err := os.Stat(partialPath); err == nil {
		existing = info.Size()
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, rec.URL, nil)
	if err != nil {
		return fmt.Errorf("build request: %w", err)
	}
	if userAgent != "" {
		req.Header.Set("User-Agent", userAgent)
	}
	if existing > 0 {
		req.Header.Set("Range", fmt.Sprintf("bytes=%d-", existing))
	}

	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("http: %w", err)
	}
	defer resp.Body.Close()

	var appendMode bool
	switch resp.StatusCode {
	case http.StatusOK:
		// Server ignored Range (or no resume requested). Truncate.
		appendMode = false
		rec.Bytes = 0
		rec.Total = resp.ContentLength
	case http.StatusPartialContent:
		appendMode = true
		rec.Bytes = existing
		rec.Total = totalFromContentRange(resp.Header.Get("Content-Range"))
		if rec.Total <= 0 && resp.ContentLength > 0 {
			rec.Total = existing + resp.ContentLength
		}
	default:
		return fmt.Errorf("http status %d", resp.StatusCode)
	}

	var f *os.File
	if appendMode {
		f, err = os.OpenFile(partialPath, os.O_WRONLY|os.O_APPEND|os.O_CREATE, 0o644)
	} else {
		f, err = os.OpenFile(partialPath, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0o644)
	}
	if err != nil {
		return fmt.Errorf("open partial: %w", err)
	}

	_ = SaveRecord(stateDir, *rec)

	pr := &checkpointReader{
		reader:   resp.Body,
		ctx:      ctx,
		bytes:    rec.Bytes,
		thresh:   checkpointBytes,
		interval: checkpointInterval,
		flush: func(total int64) {
			rec.Bytes = total
			_ = SaveRecord(stateDir, *rec)
		},
	}
	pr.lastFlush = time.Now()
	pr.lastFlushBytes = rec.Bytes

	_, copyErr := io.Copy(f, pr)
	closeErr := f.Close()
	// Final progress write so the UI doesn't show stale bytes.
	rec.Bytes = pr.bytes
	_ = SaveRecord(stateDir, *rec)

	if copyErr != nil {
		return copyErr
	}
	if closeErr != nil {
		return closeErr
	}
	if err := os.Rename(partialPath, rec.DestFile); err != nil {
		return fmt.Errorf("rename: %w", err)
	}
	return nil
}

// checkpointReader wraps an io.Reader, accumulates byte counts, and calls
// flush whenever either thresh bytes or interval has passed since the
// last flush. It also propagates ctx cancellation by returning the ctx
// error on Read so the HTTP body copy unwinds promptly on signal.
type checkpointReader struct {
	reader         io.Reader
	ctx            context.Context
	bytes          int64
	thresh         int64
	interval       time.Duration
	lastFlush      time.Time
	lastFlushBytes int64
	flush          func(total int64)
}

func (r *checkpointReader) Read(p []byte) (int, error) {
	if err := r.ctx.Err(); err != nil {
		return 0, err
	}
	n, err := r.reader.Read(p)
	if n > 0 {
		r.bytes += int64(n)
		bytesSince := r.bytes - r.lastFlushBytes
		if bytesSince >= r.thresh || time.Since(r.lastFlush) >= r.interval {
			r.flush(r.bytes)
			r.lastFlush = time.Now()
			r.lastFlushBytes = r.bytes
		}
	}
	return n, err
}

// totalFromContentRange parses the total size from a "Content-Range:
// bytes <start>-<end>/<total>" header. Returns 0 if the format does not
// match or total is "*".
func totalFromContentRange(h string) int64 {
	h = strings.TrimSpace(h)
	if h == "" {
		return 0
	}
	const prefix = "bytes "
	if !strings.HasPrefix(h, prefix) {
		return 0
	}
	rest := h[len(prefix):]
	slash := strings.LastIndex(rest, "/")
	if slash < 0 {
		return 0
	}
	totalStr := rest[slash+1:]
	if totalStr == "*" {
		return 0
	}
	n, err := strconv.ParseInt(totalStr, 10, 64)
	if err != nil {
		return 0
	}
	return n
}
