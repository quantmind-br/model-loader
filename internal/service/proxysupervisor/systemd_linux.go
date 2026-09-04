//go:build linux

package proxysupervisor

import (
	"context"
	"fmt"
	"os/exec"
	"runtime"
	"strconv"
	"strings"
	"time"
)

// ProxyUnitName is the user-systemd unit that owns the proxy when the TUI
// manages it through systemd. Kept constant: the unit file is provisioned by
// deploy/cloudflare-model-loader/install.sh, and the supervisor only
// consumes it — never writes it.
const ProxyUnitName = "model-loader-proxy.service"

// detectSystemd reports whether the proxy unit is loaded in user systemd.
// All failure modes (non-Linux, no systemctl binary, unit absent) select the
// legacy detached-process strategy. Detection runs once at New; afterwards a
// systemd-selected supervisor never falls back mid-operation, which would
// risk two concurrent proxies.
func detectSystemd() bool {
	if runtime.GOOS != "linux" {
		return false
	}
	if _, err := exec.LookPath("systemctl"); err != nil {
		return false
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	out, err := runSystemctl(ctx, "--user", "show", ProxyUnitName, "-p", "LoadState", "--value")
	if err != nil {
		return false
	}
	return strings.TrimSpace(out) == "loaded"
}

// unitProps queries ActiveState, SubState and MainPID in one show call.
type unitProps struct {
	active  string
	sub     string
	mainPID int
}

func queryUnit(ctx context.Context) (unitProps, error) {
	out, err := runSystemctl(ctx, "--user", "show", ProxyUnitName,
		"-p", "ActiveState", "-p", "SubState", "-p", "MainPID", "--value")
	if err != nil {
		return unitProps{}, err
	}
	lines := strings.Split(strings.TrimSpace(out), "\n")
	var p unitProps
	if len(lines) > 0 {
		p.active = strings.TrimSpace(lines[0])
	}
	if len(lines) > 1 {
		p.sub = strings.TrimSpace(lines[1])
	}
	if len(lines) > 2 {
		p.mainPID, _ = strconv.Atoi(strings.TrimSpace(lines[2]))
	}
	return p, nil
}

// waitUnitState polls queryUnit until cond holds or ctx expires. Polling is
// deliberate: systemctl --user show is cheap and there is no watch API that
// survives the TUI's short-lived Start/Stop calls.
func waitUnitState(ctx context.Context, cond func(unitProps) bool) (unitProps, error) {
	ticker := time.NewTicker(200 * time.Millisecond)
	defer ticker.Stop()
	for {
		p, err := queryUnit(ctx)
		if err == nil && cond(p) {
			return p, nil
		}
		select {
		case <-ctx.Done():
			if err != nil {
				return unitProps{}, fmt.Errorf("proxy unit state wait: last query: %w: %w", err, ctx.Err())
			}
			last, qerr := queryUnit(context.Background())
			if qerr != nil {
				return unitProps{}, fmt.Errorf("proxy unit state wait: %w", ctx.Err())
			}
			return last, fmt.Errorf("proxy unit state wait: timeout (active=%s sub=%s pid=%d): %w",
				last.active, last.sub, last.mainPID, ctx.Err())
		case <-ticker.C:
		}
	}
}

func unitRunning(p unitProps) bool {
	return p.active == "active" && p.sub == "running" && p.mainPID > 0
}
