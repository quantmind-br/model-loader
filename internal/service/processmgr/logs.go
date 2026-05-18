package processmgr

import (
	"fmt"
	"io"
	"os"
)

// TailLogs opens the on-disk log file for a tracked instance and returns it
// as an io.ReadCloser. Caller closes.
func (m *fsManager) TailLogs(pid int) (io.ReadCloser, error) {
	logPath := m.logPathForPID(pid)
	if logPath == "" {
		return nil, ErrUnknownPID
	}
	f, err := os.Open(logPath)
	if err != nil {
		return nil, fmt.Errorf("open log: %w", err)
	}
	return f, nil
}

// logPathForPID returns the on-disk log file for pid, or "" if unknown.
func (m *fsManager) logPathForPID(pid int) string {
	for _, ri := range m.List() {
		if ri.PID == pid {
			return ri.LogPath
		}
	}
	return ""
}
