package processmgr

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"github.com/quantmind-br/model-loader/internal/domain"
)

// TestLaunch_FastCrashNotClobberedInRegistry guards audit P-C4: the reaper
// (waitEnrichment) starts only after the launch registry upsert commits, so a
// fast-crash's Crashed delta can never be overwritten by the running delta.
func TestLaunch_FastCrashNotClobberedInRegistry(t *testing.T) {
	dir := t.TempDir()
	fb := fakeBinary(t)
	mgr := New(Config{
		Resolver:     func(_ domain.Profile) (string, domain.BackendKind, error) { return fb, "", nil },
		LogDir:       filepath.Join(dir, "logs"),
		RegistryPath: filepath.Join(dir, "instances.json"),
		// Instant "crash": the reaper marks the instance Crashed immediately.
		WaitFunc: func(*exec.Cmd) error { return nil },
	})
	p := domain.Profile{ID: "crashy", Name: "Crashy", Model: "/dev/null", Args: map[string]any{}}
	inst, err := mgr.Launch(p, LaunchBackground, "")
	if err != nil {
		t.Fatalf("Launch: %v", err)
	}
	defer mgr.Kill(inst.PID)

	// Poll until the reaper's Crashed delta lands in the registry file.
	deadline := time.Now().Add(2 * time.Second)
	crashed := false
	for time.Now().Before(deadline) {
		if reg, err := loadRegistry(mgr.registryPath); err == nil {
			for _, ri := range reg {
				if ri.PID == inst.PID && ri.Crashed {
					crashed = true
				}
			}
			if crashed {
				break
			}
		}
		time.Sleep(10 * time.Millisecond)
	}
	if !crashed {
		t.Fatalf("registry entry for pid %d never became Crashed", inst.PID)
	}

	// The launch's running delta must never clobber the Crashed delta after.
	time.Sleep(50 * time.Millisecond)
	reg, err := loadRegistry(mgr.registryPath)
	if err != nil {
		t.Fatalf("loadRegistry: %v", err)
	}
	found := false
	for _, ri := range reg {
		if ri.PID == inst.PID {
			found = true
			if !ri.Crashed {
				t.Errorf("registry entry pid %d = %+v, want Crashed=true (ordering regression)", inst.PID, ri)
			}
		}
	}
	if !found {
		t.Errorf("registry entry pid %d missing after crash", inst.PID)
	}
}

// TestLaunchForeground_SentinelResetOnLogDirError guards audit N-C8: a pre-Start
// error (log-dir MkdirAll) must reset the fgPID sentinel so a retry is not
// wrongly rejected as ErrForegroundBusy.
func TestLaunchForeground_SentinelResetOnLogDirError(t *testing.T) {
	dir := t.TempDir()
	fb := fakeBinary(t)
	// logDir points at an existing FILE, so MkdirAll fails every call.
	badLogDir := filepath.Join(dir, "not-a-dir")
	if err := os.WriteFile(badLogDir, []byte("x"), 0o644); err != nil {
		t.Fatalf("seed file: %v", err)
	}
	mgr := New(Config{
		Resolver:     func(_ domain.Profile) (string, domain.BackendKind, error) { return fb, "", nil },
		LogDir:       badLogDir,
		RegistryPath: filepath.Join(dir, "instances.json"),
	})
	p := domain.Profile{ID: "fg", Name: "FG", Model: "/dev/null", Args: map[string]any{}}

	if _, err := mgr.Launch(p, LaunchForeground, ""); err == nil {
		t.Fatal("expected mkdir error on first foreground launch")
	}
	// Sentinel must be reset: a retry must not be wrongly rejected as busy.
	if _, err := mgr.Launch(p, LaunchForeground, ""); errors.Is(err, ErrForegroundBusy) {
		t.Fatalf("second launch returned ErrForegroundBusy; sentinel not reset")
	}
}
