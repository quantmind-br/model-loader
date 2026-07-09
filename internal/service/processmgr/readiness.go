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
//
// The log is read incrementally: a persistent offset advances across polls so
// each poll reads only newly-appended bytes, and a small carry tail is kept so
// a token split across two reads still matches (audit C7).
func waitForLogToken(logPath string, re *regexp.Regexp, timeout time.Duration, pid int) (string, error) {
	deadline := time.Now().Add(timeout)
	delay := 100 * time.Millisecond
	const maxDelay = time.Second
	// The token sk-unsloth-<32 hex> is 43 bytes; a 64-byte overlap covers a
	// match split across two reads.
	const carryMax = 64
	var offset int64
	var carry []byte
	buf := make([]byte, 32<<10)
	for {
		if !procutil.Alive(pid) {
			return "", fmt.Errorf("log %q: %w", logPath, ErrProcessExited)
		}
		if f, err := os.Open(logPath); err == nil {
			for {
				n, rerr := f.ReadAt(buf, offset)
				if n > 0 {
					combined := append(carry, buf[:n]...)
					if m := re.Find(combined); m != nil {
						_ = f.Close()
						return string(m), nil
					}
					offset += int64(n)
					tail := combined
					if len(tail) > carryMax {
						tail = tail[len(tail)-carryMax:]
					}
					// Fresh copy so the next iteration's append cannot alias buf.
					carry = append([]byte(nil), tail...)
				}
				if rerr != nil {
					break // io.EOF or short read: nothing more this poll
				}
			}
			_ = f.Close()
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
