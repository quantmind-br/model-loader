// Readiness + watchdog wiring for `model-loader serve` under systemd.
//
// When the process runs under systemd (NOTIFY_SOCKET set) the proxy:
//  1. probes its own /_status over loopback after Start,
//  2. sends READY=1 only once the probe answers HTTP 200 with running=true,
//  3. while WATCHDOG_USEC is configured, keeps probing at half the interval
//     and sends WATCHDOG=1 only after a healthy probe — never on a blind
//     ticker, so a wedged HTTP layer stops the heartbeat and systemd
//     restarts the unit,
//  4. sends STOPPING=1 on signal shutdown.
//
// Outside systemd every function is a no-op (nil heartbeat, nil error) and
// serve behaves exactly as before. A readiness failure is returned as a
// startup error so systemd can restart the unit.
package cli

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"time"

	"github.com/quantmind-br/model-loader/internal/sdnotify"
)

// notifyLoopbackHost maps a wildcard bind to a loopback dial target so the
// readiness/watchdog probes reach our own listener. Anything else is used
// verbatim (including a hostname or external bind — the probe just follows
// the configured address).
func notifyLoopbackHost(host string) string {
	switch host {
	case "", "0.0.0.0", "::":
		return "127.0.0.1"
	}
	return host
}

// probeProxyStatus GETs /_status and reports whether the proxy answers
// healthy (HTTP 200 with running=true). Any transport error, non-200 status,
// decode failure, or running=false counts as unhealthy — the watchdog must
// stop pulsing in all of those cases.
func probeProxyStatus(client *http.Client, addr string) bool {
	resp, err := client.Get("http://" + addr + "/_status")
	if err != nil {
		return false
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return false
	}
	var st struct {
		Running bool `json:"running"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&st); err != nil {
		return false
	}
	return st.Running
}

// serveReadiness waits for the proxy's /_status to answer healthy, then sends
// READY=1. No-op (nil) when NOTIFY_SOCKET is unset. A hard failure is
// returned so the caller can abort startup and let systemd restart the unit.
func serveReadiness(host string, port int, timeout time.Duration) error {
	if !sdnotify.Enabled() {
		return nil
	}
	addr := net.JoinHostPort(notifyLoopbackHost(host), fmt.Sprintf("%d", port))
	client := &http.Client{Timeout: 2 * time.Second}
	deadline := time.Now().Add(timeout)
	for {
		if probeProxyStatus(client, addr) {
			if err := sdnotify.Ready(); err != nil {
				return fmt.Errorf("sd_notify READY: %w", err)
			}
			return nil
		}
		if time.Now().After(deadline) {
			return fmt.Errorf("readiness probe %s/_status unhealthy after %s", addr, timeout)
		}
		time.Sleep(200 * time.Millisecond)
	}
}

// startServeWatchdog starts the systemd watchdog heartbeat when both
// NOTIFY_SOCKET and WATCHDOG_USEC are configured (with a matching
// WATCHDOG_PID). It returns a stop func; nil when disabled. Each tick probes
// /_status first and sends WATCHDOG=1 only on a healthy answer.
func startServeWatchdog(host string, port int, logger *slog.Logger) (stop func()) {
	interval, ok := sdnotify.WatchdogInterval()
	if !ok || !sdnotify.Enabled() {
		return nil
	}
	addr := net.JoinHostPort(notifyLoopbackHost(host), fmt.Sprintf("%d", port))
	client := &http.Client{Timeout: sdnotify.ProbeTimeout(interval)}
	ticker := time.NewTicker(sdnotify.HalfInterval(interval))
	done := make(chan struct{})
	go func() {
		defer ticker.Stop()
		for {
			select {
			case <-done:
				return
			case <-ticker.C:
				if probeProxyStatus(client, addr) {
					if err := sdnotify.Watchdog(); err != nil {
						logger.Warn("serve_watchdog_notify_failed", "err", err)
					}
				} else {
					logger.Warn("serve_watchdog_probe_unhealthy")
				}
			}
		}
	}()
	return func() { close(done) }
}

// stopServeWatchdog invokes a stop func returned by startServeWatchdog,
// tolerating nil (watchdog disabled).
func stopServeWatchdog(stop func()) {
	if stop != nil {
		stop()
	}
}
