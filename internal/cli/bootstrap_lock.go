package cli

import (
	"errors"
	"fmt"
	"io"

	"github.com/quantmind-br/model-loader/internal/app"
	"github.com/quantmind-br/model-loader/internal/config"
)

// errAnotherInstance signals the single-instance advisory lock is already held
// by another live process (TUI/serve). Callers print their own remediation hint.
var errAnotherInstance = errors.New("another model-loader instance is running")

// bootstrapWithLock acquires the single-instance flock before bootstrapping the
// shared services, so the headless write paths (benchmark, instance lifecycle)
// never race the TUI/serve on the shared state files. The returned release func
// closes the services and drops the lock — callers MUST defer it.
//
// Config-load failures are reported to errw and returned as-is. When the lock is
// held it returns errAnotherInstance (no message) so each command can phrase its
// own hint. Bootstrap failures are returned without an extra message (Bootstrap
// already logs to stderr).
func bootstrapWithLock(errw io.Writer, logLevel string) (*app.Services, func(), error) {
	cfg, err := config.Load()
	if err != nil {
		fmt.Fprintf(errw, "config error: %v\n", err)
		return nil, nil, err
	}
	release, acquired, lErr := app.AcquireSingleInstanceLock(cfg.Paths.StateDir)
	if lErr != nil {
		fmt.Fprintf(errw, "single-instance lock: %v\n", lErr)
	}
	if !acquired {
		if release != nil {
			release()
		}
		return nil, nil, errAnotherInstance
	}
	svc, err := app.Bootstrap(logLevel)
	if err != nil {
		release()
		return nil, nil, err
	}
	return svc, func() {
		svc.Close()
		release()
	}, nil
}
