package app_test

import (
	"path/filepath"
	"testing"

	"github.com/quantmind-br/model-loader/internal/app"
)

func TestBootstrap_WiresServices(t *testing.T) {
	dir := t.TempDir()

	// config.Load() resolves paths via os.UserHomeDir() and os.UserConfigDir(),
	// which honour $HOME and $XDG_CONFIG_HOME. Redirecting those two env-vars
	// to a temp dir makes the test fully hermetic without any production change.
	t.Setenv("HOME", dir)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(dir, ".config"))

	svc, err := app.Bootstrap("info")
	if err != nil {
		t.Fatalf("Bootstrap: %v", err)
	}
	defer svc.Close()

	if svc.Store == nil || svc.Mgr == nil || svc.Resolver == nil || svc.Logger == nil {
		t.Fatalf("Bootstrap returned incomplete Services: %+v", svc)
	}
	if svc.Cfg.Paths.StateDir == "" {
		t.Fatalf("expected Cfg populated, got empty StateDir")
	}
}
