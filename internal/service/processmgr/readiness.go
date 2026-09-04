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
// loaded. The label differs by mode -- "API Key: <key>" under the wrapper's
// forced --silent, "API Key:      <key>" otherwise -- so match the key itself
// anywhere in the log rather than the surrounding text.
var unslothKeyRe = regexp.MustCompile(`sk-unsloth-[0-9a-f]{32}`)

// freetokenReadyRe matches the line FreeToken logs from run_api_server's
// _on_ready callback, the moment the backend supervisor has collected every
// worker's ready ack and the admission gate flips to "serving".
//
// FreeToken cannot be probed over HTTP the way the other kinds are: uvicorn
// binds the port BEFORE the weights are loaded (api_server.py "Early-bind", so
// the desktop app can poll load progress), and GET /health answers 200 the
// whole time -- with a {"status": "loading", "progress": {...}} body while the
// checkpoint streams in. A 200 there means "the HTTP server is up", not "the
// model is loaded", so WaitHealthy would report ready during load and the
// proxy would forward the first request into a 503. The log line is the only
// signal that marks the actual transition.
var freetokenReadyRe = regexp.MustCompile(`API server is ready to serve on`)

// WaitReady blocks until the backend is ready to serve and returns any upstream
// auth token the proxy must inject. Unsloth waits for its printed API key;
// freetoken waits for its ready line (its /health lies during load) and needs
// no token; every other kind polls /health and returns an empty token.
func (m *fsManager) WaitReady(inst domain.RunningInstance, timeout time.Duration, attemptID string) (string, error) {
	switch inst.Kind {
	case domain.BackendKindUnsloth:
		lg := m.logger.With("pid", inst.PID, "port", inst.Port, "attempt_id", attemptID)
		lg.Info("readiness_start", "mode", "unsloth_log_token", "timeout", timeout)
		token, err := waitForLogToken(inst.LogPath, unslothKeyRe, timeout, inst.PID)
		if err != nil {
			lg.Warn("readiness_timeout", "err", err)
			return "", err
		}
		lg.Info("readiness_ok", "mode", "unsloth_log_token")
		return token, nil
	case domain.BackendKindFreeToken:
		lg := m.logger.With("pid", inst.PID, "port", inst.Port, "attempt_id", attemptID)
		lg.Info("readiness_start", "mode", "freetoken_log_token", "timeout", timeout)
		// The match is the literal ready line, not a credential: FreeToken
		// serves unauthenticated, so the proxy injects no upstream token.
		if _, err := waitForLogToken(inst.LogPath, freetokenReadyRe, timeout, inst.PID); err != nil {
			lg.Warn("readiness_timeout", "err", err)
			return "", err
		}
		lg.Info("readiness_ok", "mode", "freetoken_log_token")
		return "", nil
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
	// The longest pattern any caller passes is unsloth's sk-unsloth-<32 hex>
	// at 43 bytes (freetoken's ready line prefix is 31), so a 64-byte overlap
	// covers a match split across two reads.
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
