package metricsstore

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"time"
)

type Record struct {
	TS              int64   `json:"ts"`
	TokensPerSec    float64 `json:"tokens_per_sec"`
	PromptEvalTPS   float64 `json:"prompt_eval_tps"`
	TTFTMs          int64   `json:"ttft_ms"`
	RPS             float64 `json:"rps"`
	SlotUtilization float64 `json:"slot_utilization"`
}

func path(dataDir, profileID string) string {
	return filepath.Join(dataDir, profileID+".jsonl")
}

func Append(dataDir, profileID string, rec Record) error {
	if dataDir == "" || profileID == "" {
		return fmt.Errorf("metricsstore: dataDir and profileID required")
	}
	if err := os.MkdirAll(dataDir, 0o755); err != nil {
		return fmt.Errorf("metricsstore: mkdir: %w", err)
	}
	f, err := os.OpenFile(path(dataDir, profileID), os.O_APPEND|os.O_WRONLY|os.O_CREATE, 0o644)
	if err != nil {
		return fmt.Errorf("metricsstore: open: %w", err)
	}
	defer f.Close()
	line, err := json.Marshal(rec)
	if err != nil {
		return fmt.Errorf("metricsstore: marshal: %w", err)
	}
	line = append(line, '\n')
	if _, err := f.Write(line); err != nil {
		return fmt.Errorf("metricsstore: write: %w", err)
	}
	return nil
}

func Read(dataDir, profileID string, since time.Time) ([]Record, error) {
	p := path(dataDir, profileID)
	f, err := os.Open(p)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("metricsstore: open: %w", err)
	}
	defer f.Close()

	var recs []Record
	sc := bufio.NewScanner(f)
	sinceUnix := since.Unix()
	for sc.Scan() {
		var rec Record
		if err := json.Unmarshal(sc.Bytes(), &rec); err != nil {
			continue
		}
		if rec.TS >= sinceUnix {
			recs = append(recs, rec)
		}
	}
	if err := sc.Err(); err != nil {
		return nil, fmt.Errorf("metricsstore: scan: %w", err)
	}
	return recs, nil
}

func Compact(dataDir, profileID string, retention time.Duration, maxBytes int64) error {
	p := path(dataDir, profileID)
	info, err := os.Stat(p)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return fmt.Errorf("metricsstore: stat: %w", err)
	}

	cutoff := time.Now().Add(-retention).Unix()
	recs, err := Read(dataDir, profileID, time.Time{})
	if err != nil {
		return err
	}

	var keep []Record
	for _, r := range recs {
		if r.TS >= cutoff {
			keep = append(keep, r)
		}
	}

	needRewrite := len(keep) != len(recs)
	if !needRewrite && info.Size() <= maxBytes {
		return nil
	}

	for estimateSize(keep) > maxBytes && len(keep) > 1 {
		drop := len(keep) / 10
		if drop < 1 {
			drop = 1
		}
		keep = keep[drop:]
	}

	tmp := p + ".tmp"
	f, err := createWriter(tmp)
	if err != nil {
		return fmt.Errorf("metricsstore: create tmp: %w", err)
	}
	// On any marshal/write/close failure, drop the half-written temp file and
	// return the error so the original (still-valid) file is never replaced by
	// a truncated rewrite.
	if err := writeRecords(f, keep); err != nil {
		f.Close()
		_ = os.Remove(tmp)
		return err
	}
	if err := f.Close(); err != nil {
		_ = os.Remove(tmp)
		return fmt.Errorf("metricsstore: close tmp: %w", err)
	}
	return os.Rename(tmp, p)
}

// createWriter opens the compaction temp file. It is a package-level seam so
// tests can inject a writer that fails mid-rewrite.
var createWriter = func(name string) (io.WriteCloser, error) {
	return os.Create(name)
}

// writeRecords marshals each record as a single JSON line and writes it to w,
// returning the first marshal or write error encountered.
func writeRecords(w io.Writer, recs []Record) error {
	for _, rec := range recs {
		line, err := json.Marshal(rec)
		if err != nil {
			return fmt.Errorf("metricsstore: marshal: %w", err)
		}
		line = append(line, '\n')
		if _, err := w.Write(line); err != nil {
			return fmt.Errorf("metricsstore: write: %w", err)
		}
	}
	return nil
}

func estimateSize(recs []Record) int64 {
	var n int64
	for _, r := range recs {
		line, _ := json.Marshal(r)
		n += int64(len(line)) + 1
	}
	return n
}
