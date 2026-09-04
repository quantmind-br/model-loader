//go:build !linux

package proxysupervisor

import "context"

// ProxyUnitName mirrors the Linux constant so callers compile everywhere;
// systemd units only exist on Linux.
const ProxyUnitName = "model-loader-proxy.service"

type unitProps struct {
	active  string
	sub     string
	mainPID int
}

// detectSystemd is always false off Linux: no user systemd, no unit.
func detectSystemd() bool {
	return false
}

func queryUnit(ctx context.Context) (unitProps, error) {
	return unitProps{}, nil
}

func waitUnitState(ctx context.Context, cond func(unitProps) bool) (unitProps, error) {
	return unitProps{}, context.Canceled
}

func unitRunning(p unitProps) bool {
	return false
}
