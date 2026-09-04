// Package sdnotify implements the client side of systemd's sd_notify(3)
// protocol without external dependencies: datagrams carrying READY=1,
// STATUS=..., WATCHDOG=1 and STOPPING=1 sent to $NOTIFY_SOCKET.
//
// All entry points are no-ops (return nil) when NOTIFY_SOCKET is unset, so a
// manually run `model-loader serve` behaves exactly as before. Watchdog
// pings honor $WATCHDOG_PID: when it names another PID the heartbeat stays
// disabled, per sd_watchdog_enabled(3).
package sdnotify

import (
	"os"
	"strconv"
	"strings"
	"time"
)

// Enabled reports whether the process runs under a supervisor that consumes
// sd_notify messages ($NOTIFY_SOCKET is set and non-empty).
func Enabled() bool {
	return strings.TrimSpace(os.Getenv("NOTIFY_SOCKET")) != ""
}

// Ready notifies the supervisor that startup completed (READY=1).
func Ready() error {
	return NotifyState("READY=1")
}

// Stopping notifies the supervisor that shutdown began (STOPPING=1).
func Stopping() error {
	return NotifyState("STOPPING=1")
}

// Status sends a free-form STATUS= line (visible in systemctl status).
func Status(s string) error {
	return NotifyState("STATUS=" + s)
}

// Watchdog sends WATCHDOG=1. Callers must gate on WatchdogInterval first;
// sending without a configured watchdog is harmless but pointless.
func Watchdog() error {
	return NotifyState("WATCHDOG=1")
}

// NotifyState sends a raw state string to $NOTIFY_SOCKET. No-op when the
// socket is unset. Errors are returned so callers can log them; a failed
// notify must never crash the proxy.
func NotifyState(state string) error {
	sock := strings.TrimSpace(os.Getenv("NOTIFY_SOCKET"))
	if sock == "" {
		return nil
	}
	return send(sock, state)
}

// WatchdogInterval parses $WATCHDOG_USEC (microseconds) into the interval at
// which the supervisor expects WATCHDOG=1 pings. The second return value is
// false when no watchdog is configured or when $WATCHDOG_PID names a PID
// other than ours — in both cases the caller must not ping.
//
// Per sd_watchdog_enabled(3), pings should go out at half the configured
// interval; use HalfInterval for the ticker.
func WatchdogInterval() (interval time.Duration, ok bool) {
	usec, err := strconv.ParseUint(strings.TrimSpace(os.Getenv("WATCHDOG_USEC")), 10, 64)
	if err != nil || usec == 0 {
		return 0, false
	}
	if pidStr := strings.TrimSpace(os.Getenv("WATCHDOG_PID")); pidStr != "" {
		pid, err := strconv.Atoi(pidStr)
		if err != nil || pid != os.Getpid() {
			return 0, false
		}
	}
	return time.Duration(usec) * time.Microsecond, true
}

// HalfInterval returns interval/2, the recommended ping period. It floors at
// one second so a tiny WATCHDOG_USEC cannot busy-loop the probe.
func HalfInterval(interval time.Duration) time.Duration {
	h := interval / 2
	if h < time.Second {
		h = time.Second
	}
	return h
}

// ProbeTimeout returns a per-probe HTTP timeout derived from the watchdog
// interval: a quarter of the interval, clamped to [1s, 5s]. A probe must
// finish well before the next ping is due, and must never approach the
// supervisor's kill deadline.
func ProbeTimeout(interval time.Duration) time.Duration {
	t := interval / 4
	if t < time.Second {
		t = time.Second
	}
	if t > 5*time.Second {
		t = 5 * time.Second
	}
	return t
}
