package processmgr

import (
	"errors"
	"fmt"
	"os"
	"regexp"
	"time"

	"github.com/quantmind-br/model-loader/internal/domain"
	"github.com/quantmind-br/model-loader/internal/service/internal/procutil"
)

// ErrReadyTimeout is returned when a backend does not signal readiness in time.
var ErrReadyTimeout = errors.New("backend did not become ready within timeout")

// unslothKeyRe matches the per-boot API key unsloth prints once the model is
// loaded: "API Key:      sk-unsloth-<32 hex>".
var unslothKeyRe = regexp.MustCompile(`sk-unsloth-[0-9a-f]{32}`)

// WaitReady blocks until the backend is ready to serve and returns any upstream
// auth token the proxy must inject. For unsloth, the printed key is both the
// readiness signal (it appears only after the model loads) and the token. For
// every other kind it polls /health and returns an empty token.
func (m *fsManager) WaitReady(inst domain.RunningInstance, timeout time.Duration, attemptID string) (string, error) {
	if inst.Kind == domain.BackendKindUnsloth {
		lg := m.logger.With("pid", inst.PID, "port", inst.Port, "attempt_id", attemptID)
		lg.Info("readiness_start", "mode", "unsloth_log_token", "timeout", timeout)
		token, err := waitForLogToken(inst.LogPath, unslothKeyRe, timeout, inst.PID)
		if err != nil {
			lg.Warn("readiness_timeout", "err", err)
			return "", err
		}
		lg.Info("readiness_ok", "mode", "unsloth_log_token")
		return token, nil
	}
	return "", m.WaitHealthy(inst.PID, inst.Port, timeout, attemptID)
}

// waitForLogToken polls logPath until re matches (returning the first match) or
// timeout elapses. Uses the same capped backoff as WaitHealthy. A missing file
// is treated as "not ready yet", not an error. Returns ErrProcessExited early
// if pid dies before the token appears.
func waitForLogToken(logPath string, re *regexp.Regexp, timeout time.Duration, pid int) (string, error) {
	deadline := time.Now().Add(timeout)
	delay := 100 * time.Millisecond
	const maxDelay = time.Second
	for {
		if !procutil.Alive(pid) {
			return "", fmt.Errorf("log %q: %w", logPath, ErrProcessExited)
		}
		if data, err := os.ReadFile(logPath); err == nil {
			if m := re.Find(data); m != nil {
				return string(m), nil
			}
		}
		remaining := time.Until(deadline)
		if remaining <= 0 {
			break
		}
		sleep := delay
		if sleep > remaining {
			sleep = remaining
		}
		time.Sleep(sleep)
		if delay < maxDelay {
			delay *= 2
			if delay > maxDelay {
				delay = maxDelay
			}
		}
	}
	return "", fmt.Errorf("log %q: %w", logPath, ErrReadyTimeout)
}
