package benchmark

import (
	"bytes"
	"log/slog"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/quantmind-br/model-loader/internal/log"
)

// harnessLogRe matches newHarnessLog's "<label>-<yyyymmdd>-<hhmmss>.log". The
// label (group 1) can itself contain hyphens (terminal-bench, swe-bench-pro),
// so the split anchors on the timestamp suffix, not the first hyphen.
var harnessLogRe = regexp.MustCompile(`^(.+)-(\d{8}-\d{6})\.log$`)

// harnessTailMax bounds the in-memory tail kept for error messages and
// transcripts. The full output lives in the teed log file when a log dir is
// configured.
const harnessTailMax = 64 << 10

// harnessLineBufMax bounds the partial-line accumulator used to feed live output
// lines to onLine: a newline-less flood discards oldest bytes rather than growing.
const harnessLineBufMax = 4 << 10

// harnessLog is the sink for an external harness's merged stdout/stderr: a
// bounded in-memory tail plus, when a log dir is configured, a tee to a
// persistent file. It replaces the unbounded in-memory capture that grew
// without limit on hours-long runs and died with the process (BR7).
type harnessLog struct {
	mu      sync.Mutex
	tail    []byte
	file    *os.File
	path    string
	onLine  func(string) // when set, receives each complete non-blank output line live
	lineBuf []byte       // partial-line accumulator for onLine (bounded by harnessLineBufMax)
}

// newHarnessLog opens the persistent log file <dir>/<label>-<timestamp>.log
// (dir created on demand). An empty dir or a failed open degrades to the
// bounded in-memory tail only — capture must never fail the run.
func newHarnessLog(dir, label string, onLine func(string)) *harnessLog {
	h := &harnessLog{onLine: onLine}
	if dir == "" {
		return h
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return h
	}
	path := filepath.Join(dir, label+"-"+time.Now().Format("20060102-150405")+".log")
	f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		return h
	}
	h.file, h.path = f, path
	return h
}

// Write implements io.Writer for exec.Cmd stdout/stderr (stdout and stderr
// copy concurrently, so everything serializes under mu). It never reports an
// error: a failed file write degrades to the in-memory tail.
func (h *harnessLog) Write(p []byte) (int, error) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.file != nil {
		_, _ = h.file.Write(p)
	}
	h.tail = append(h.tail, p...)
	if over := len(h.tail) - harnessTailMax; over > 0 {
		copy(h.tail, h.tail[over:])
		h.tail = h.tail[:harnessTailMax]
	}
	if h.onLine != nil {
		h.emitLinesLocked(p)
	}
	return len(p), nil
}

// emitLinesLocked accumulates p and forwards each complete non-blank line
// (CR-trimmed) to onLine. Runs under mu. The trailing partial line is compacted
// to the front and bounded so a newline-less flood stays memory-safe.
func (h *harnessLog) emitLinesLocked(p []byte) {
	h.lineBuf = append(h.lineBuf, p...)
	start := 0
	for {
		i := bytes.IndexByte(h.lineBuf[start:], '\n')
		if i < 0 {
			break
		}
		line := strings.TrimRight(string(h.lineBuf[start:start+i]), "\r")
		if j := strings.LastIndexByte(line, '\r'); j >= 0 {
			line = line[j+1:] // bare-CR (\r) progress redraw: keep only the final overwrite segment so embedded CRs never reach a renderer (BR16)
		}
		start += i + 1
		if strings.TrimSpace(line) != "" {
			h.onLine(line)
		}
	}
	if start > 0 {
		h.lineBuf = append(h.lineBuf[:0], h.lineBuf[start:]...)
	}
	if over := len(h.lineBuf) - harnessLineBufMax; over > 0 {
		h.lineBuf = append(h.lineBuf[:0], h.lineBuf[over:]...)
	}
}

// Tail returns the bounded in-memory tail captured so far.
func (h *harnessLog) Tail() string {
	h.mu.Lock()
	defer h.mu.Unlock()
	return string(h.tail)
}

// diagTail renders the output tail for error messages, naming the persistent
// log file when one exists so the operator can read past the tail.
func (h *harnessLog) diagTail() string {
	t := tbTail(h.Tail())
	if h.path != "" {
		t += "\n[full harness log: " + h.path + "]"
	}
	return t
}

// transcript renders the harness-output transcript entry: the persistent log
// reference (when present) plus the bounded tail.
func (h *harnessLog) transcript() string {
	if h.path != "" {
		return "[full harness log: " + h.path + "]\n" + h.Tail()
	}
	return h.Tail()
}

// Close closes the teed log file, if any.
func (h *harnessLog) Close() {
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.file != nil {
		_ = h.file.Close()
		h.file = nil
	}
}

// PruneHarnessLogs keeps the keepPerMode newest harness logs per mode label in
// dir (by mtime) and removes older ones, bounding the otherwise unbounded
// growth of the per-run agentic harness logs the BR7 tee writes. Grouping per
// mode (mirroring PruneBackendLogs' per-profile keep) means frequent
// terminal-bench runs can't evict a rarely-run deep-swe log. Newest-first per
// group keeps each mode's in-progress live log. Files not matching the harness
// naming are left untouched. Best-effort: a missing dir is a no-op and per-file
// errors are logged and skipped. Returns files removed.
func PruneHarnessLogs(dir string, keepPerMode int, logger *slog.Logger) int {
	if logger == nil {
		logger = log.Nop()
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		return 0
	}
	type fileInfo struct {
		name  string
		mtime int64
	}
	byMode := map[string][]fileInfo{}
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		m := harnessLogRe.FindStringSubmatch(e.Name())
		if m == nil {
			continue // not a harness log
		}
		info, err := e.Info()
		if err != nil {
			continue
		}
		byMode[m[1]] = append(byMode[m[1]], fileInfo{name: e.Name(), mtime: info.ModTime().UnixNano()})
	}
	removed := 0
	for _, files := range byMode {
		if len(files) <= keepPerMode {
			continue
		}
		sort.Slice(files, func(i, j int) bool { return files[i].mtime > files[j].mtime }) // newest first
		for _, f := range files[keepPerMode:] {
			if err := os.Remove(filepath.Join(dir, f.name)); err != nil {
				logger.Warn("harness_log_prune_failed", "file", f.name, "err", err)
				continue
			}
			removed++
		}
	}
	return removed
}
