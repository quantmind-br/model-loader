package processmgr

import (
	"log/slog"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"github.com/quantmind-br/model-loader/internal/log"
)

// backendLogRe matches "<profile-id>-<port>.log" (launch.go's naming). The
// profile capture groups files across the ephemeral ports a profile churns
// through (every launch/swap picks a new port → a new file).
var backendLogRe = regexp.MustCompile(`^(.+)-(\d+)\.log$`)

// PruneBackendLogs deletes stale backend logs from logDir. Basenames in exempt
// (live registry + exit-history LogPaths) always survive, as do non-backend
// files (model-loader*.log, proxy.log — anything not matching backendLogRe).
// Of the rest, the keepPerProfile newest by mtime per profile survive; older
// ones are removed. Best-effort: per-file errors are logged and skipped.
// Returns the number of files removed (audit C1).
func PruneBackendLogs(logDir string, exempt map[string]struct{}, keepPerProfile int, logger *slog.Logger) int {
	if logger == nil {
		logger = log.Nop()
	}
	entries, err := os.ReadDir(logDir)
	if err != nil {
		return 0
	}
	type fileInfo struct {
		name  string
		mtime int64
	}
	byProfile := map[string][]fileInfo{}
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		name := e.Name()
		// Never touch the app log or the proxy log, even if an exotic profile
		// id somehow produced a matching name.
		if strings.HasPrefix(name, "model-loader") || name == "proxy.log" {
			continue
		}
		if _, ok := exempt[name]; ok {
			continue
		}
		m := backendLogRe.FindStringSubmatch(name)
		if m == nil {
			continue // not a backend log
		}
		info, err := e.Info()
		if err != nil {
			continue
		}
		profile := m[1]
		byProfile[profile] = append(byProfile[profile], fileInfo{name: name, mtime: info.ModTime().UnixNano()})
	}

	removed := 0
	for _, files := range byProfile {
		if len(files) <= keepPerProfile {
			continue
		}
		sort.Slice(files, func(i, j int) bool { return files[i].mtime > files[j].mtime }) // newest first
		for _, f := range files[keepPerProfile:] {
			if err := os.Remove(filepath.Join(logDir, f.name)); err != nil {
				logger.Warn("backend_log_prune_failed", "file", f.name, "err", err)
				continue
			}
			removed++
		}
	}
	return removed
}
