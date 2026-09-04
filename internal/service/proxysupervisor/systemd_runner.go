package proxysupervisor

import (
	"bytes"
	"context"
	"fmt"
	"os/exec"
	"strings"
)

// systemctlRunner executes systemctl --user. runSystemctl is a package
// variable (not a Supervisor field) so tests can stub the whole transport;
// production always shells out to the real systemctl.
type systemctlRunner func(ctx context.Context, args ...string) (string, error)

var runSystemctl systemctlRunner = func(ctx context.Context, args ...string) (string, error) {
	cmd := exec.CommandContext(ctx, "systemctl", args...)
	var out bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = &out
	if err := cmd.Run(); err != nil {
		return out.String(), fmt.Errorf("systemctl %s: %w: %s", strings.Join(args, " "), err, strings.TrimSpace(out.String()))
	}
	return out.String(), nil
}
