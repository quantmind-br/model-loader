package processmgr

import (
	"errors"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/quantmind-br/model-loader/internal/domain"
)

func freePort(t *testing.T) int {
	t.Helper()
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer l.Close()
	return l.Addr().(*net.TCPAddr).Port
}

func fakeBinary(t *testing.T) string {
	t.Helper()
	abs, err := filepath.Abs("../../../testdata/fake-llama-server.sh")
	if err != nil {
		t.Fatal(err)
	}
	return abs
}

func newTestManager(t *testing.T) (*fsManager, string) {
	t.Helper()
	dir := t.TempDir()
	fb := fakeBinary(t)
	mgr := New(Config{
		Resolver:     func(_ domain.Profile) (string, domain.BackendKind, error) { return fb, "", nil },
		LogDir:       filepath.Join(dir, "logs"),
		RegistryPath: filepath.Join(dir, "instances.json"),
	})
	return mgr, dir
}

func TestKill_RecordsOperatorStopReason(t *testing.T) {
	mgr, dir := newTestManager(t)
	inst, err := mgr.Launch(domain.Profile{
		ID:    "operator-stop",
		Model: "/dev/null",
		Args:  map[string]any{},
	}, LaunchBackground, "")
	if err != nil {
		t.Fatalf("Launch: %v", err)
	}
	if err := mgr.Kill(inst.PID); err != nil {
		t.Fatalf("Kill: %v", err)
	}
	history := mgr.History()
	if len(history) != 1 {
		t.Fatalf("history = %+v, want one entry", history)
	}
	if got := history[0].ExitReason; got != domain.ExitReasonOperatorStop {
		t.Errorf("history exit reason = %q, want %q", got, domain.ExitReasonOperatorStop)
	}
	data, err := os.ReadFile(filepath.Join(dir, "instances-history.json"))
	if err != nil {
		t.Fatalf("read persisted history: %v", err)
	}
	if !strings.Contains(string(data), domain.ExitReasonOperatorStop) {
		t.Errorf("persisted history missing %q: %s", domain.ExitReasonOperatorStop, data)
	}
}

func TestManager_LaunchBackground_WaitsHealthyAndPersists(t *testing.T) {
	mgr, _ := newTestManager(t)
	p := domain.Profile{
		ID:    "smoke",
		Name:  "Smoke",
		Model: "/dev/null",
		Args:  map[string]any{},
	}
	inst, err := mgr.Launch(p, LaunchBackground, "")
	if err != nil {
		t.Fatalf("Launch: %v", err)
	}
	defer mgr.Kill(inst.PID)

	if inst.PID <= 0 || inst.Port <= 0 || !inst.Background {
		t.Fatalf("inst = %+v", inst)
	}

	if err := mgr.WaitHealthy(inst.PID, inst.Port, 5*time.Second, ""); err != nil {
		t.Fatalf("WaitHealthy: %v", err)
	}

	loaded, err := loadRegistry(mgr.registryPath)
	if err != nil {
		t.Fatalf("loadRegistry: %v", err)
	}
	found := false
	for _, ri := range loaded {
		if ri.PID == inst.PID {
			found = true
		}
	}
	if !found {
		t.Errorf("registry missing pid %d; got %+v", inst.PID, loaded)
	}
}

func TestManager_LaunchBackground_ProfileOverrideUsesEffectiveBinary(t *testing.T) {
	dir := t.TempDir()
	overrideBinary := fakeBinary(t)
	mgr := New(Config{
		Resolver:     func(_ domain.Profile) (string, domain.BackendKind, error) { return overrideBinary, "", nil },
		LogDir:       filepath.Join(dir, "logs"),
		RegistryPath: filepath.Join(dir, "instances.json"),
	})
	port := freePort(t)
	p := domain.Profile{
		ID:    "override",
		Model: "/dev/null",
		Args:  map[string]any{"port": float64(port)},
	}
	inst, err := mgr.Launch(p, LaunchBackground, "")
	if err != nil {
		t.Fatalf("Launch: %v", err)
	}
	defer mgr.Kill(inst.PID)

	if inst.BinaryPath != overrideBinary {
		t.Fatalf("BinaryPath = %q, want %q", inst.BinaryPath, overrideBinary)
	}
	if err := mgr.WaitHealthy(inst.PID, inst.Port, 5*time.Second, ""); err != nil {
		t.Fatalf("WaitHealthy: %v", err)
	}
}

func TestApplyProfileEnv(t *testing.T) {
	t.Run("EmptyReturnsNil", func(t *testing.T) {
		if got := applyProfileEnv(nil); got != nil {
			t.Errorf("nil input → %v, want nil", got)
		}
		if got := applyProfileEnv([]domain.EnvVar{}); got != nil {
			t.Errorf("empty slice → %v, want nil", got)
		}
	})
	t.Run("AppendsNewKey", func(t *testing.T) {
		got := applyProfileEnv([]domain.EnvVar{{Key: "MODELLOADER_TEST_NEW", Value: "yes"}})
		if !envContains(got, "MODELLOADER_TEST_NEW=yes") {
			t.Errorf("missing MODELLOADER_TEST_NEW=yes; got %v", got)
		}
	})
	t.Run("OverridesExistingKey", func(t *testing.T) {
		t.Setenv("MODELLOADER_TEST_OVERRIDE", "old")
		got := applyProfileEnv([]domain.EnvVar{{Key: "MODELLOADER_TEST_OVERRIDE", Value: "new"}})
		if !envContains(got, "MODELLOADER_TEST_OVERRIDE=new") {
			t.Errorf("override failed; got %v", got)
		}
		if envContains(got, "MODELLOADER_TEST_OVERRIDE=old") {
			t.Errorf("stale old value still present: %v", got)
		}
	})
	t.Run("LastValueWinsOnDuplicates", func(t *testing.T) {
		got := applyProfileEnv([]domain.EnvVar{
			{Key: "MODELLOADER_DUP", Value: "a"},
			{Key: "MODELLOADER_DUP", Value: "b"},
			{Key: "MODELLOADER_DUP", Value: "c"},
		})
		if !envContains(got, "MODELLOADER_DUP=c") {
			t.Errorf("want MODELLOADER_DUP=c; got %v", got)
		}
		if envContains(got, "MODELLOADER_DUP=a") || envContains(got, "MODELLOADER_DUP=b") {
			t.Errorf("earlier duplicates not overwritten: %v", got)
		}
	})
	t.Run("SkipsEmptyKey", func(t *testing.T) {
		got := applyProfileEnv([]domain.EnvVar{
			{Key: "", Value: "ignored"},
			{Key: "MODELLOADER_KEEP", Value: "ok"},
		})
		for _, kv := range got {
			if strings.HasPrefix(kv, "=") {
				t.Errorf("found empty-key entry: %q", kv)
			}
		}
		if !envContains(got, "MODELLOADER_KEEP=ok") {
			t.Errorf("non-empty key dropped; got %v", got)
		}
	})
	t.Run("InheritsBaseEnv", func(t *testing.T) {
		t.Setenv("MODELLOADER_INHERIT", "preserved")
		got := applyProfileEnv([]domain.EnvVar{{Key: "MODELLOADER_ADDED", Value: "extra"}})
		if !envContains(got, "MODELLOADER_INHERIT=preserved") {
			t.Errorf("inherited env dropped; got %v", got)
		}
	})
}

func envContains(env []string, want string) bool {
	return slices.Contains(env, want)
}

func TestBuildLaunchEnv(t *testing.T) {
	t.Run("NativeBackendNilEnvStaysNil", func(t *testing.T) {
		// Native (C++) backends keep the legacy fast-path: nil env so Go
		// inherits os.Environ() implicitly and no PYTHONUNBUFFERED is forced.
		for _, kind := range []domain.BackendKind{
			domain.BackendKindLlamaServer,
			domain.BackendKindBeeLlamaCpp,
			domain.BackendKindBuunLlamaCpp,
			domain.BackendKindIkLlamaCpp,
			domain.BackendKindDFlash,
		} {
			if got := buildLaunchEnv(kind, nil); got != nil {
				t.Errorf("kind %q: nil profile env → %v, want nil", kind, got)
			}
		}
	})
	t.Run("PythonBackendsForceUnbuffered", func(t *testing.T) {
		for _, kind := range []domain.BackendKind{
			domain.BackendKindVLLM,
			domain.BackendKindSGLang,
			domain.BackendKindUnsloth,
			domain.BackendKindFreeToken,
			domain.BackendKindStrata,
		} {
			got := buildLaunchEnv(kind, nil)
			if !envContains(got, "PYTHONUNBUFFERED=1") {
				t.Errorf("kind %q: missing PYTHONUNBUFFERED=1; got %v", kind, got)
			}
		}
	})
	t.Run("PythonBackendInheritsBaseEnv", func(t *testing.T) {
		t.Setenv("MODELLOADER_PY_INHERIT", "preserved")
		got := buildLaunchEnv(domain.BackendKindVLLM, nil)
		if !envContains(got, "MODELLOADER_PY_INHERIT=preserved") {
			t.Errorf("inherited env dropped; got %v", got)
		}
		if !envContains(got, "PYTHONUNBUFFERED=1") {
			t.Errorf("missing PYTHONUNBUFFERED=1; got %v", got)
		}
	})
	t.Run("PythonBackendKeepsProfileEnv", func(t *testing.T) {
		got := buildLaunchEnv(domain.BackendKindVLLM, []domain.EnvVar{
			{Key: "MODELLOADER_PY_EXTRA", Value: "ok"},
		})
		if !envContains(got, "MODELLOADER_PY_EXTRA=ok") {
			t.Errorf("profile env dropped; got %v", got)
		}
		if !envContains(got, "PYTHONUNBUFFERED=1") {
			t.Errorf("missing PYTHONUNBUFFERED=1; got %v", got)
		}
	})
	t.Run("RespectsExplicitProfileOverride", func(t *testing.T) {
		// A profile that explicitly sets PYTHONUNBUFFERED wins — we must not
		// append our own =1 on top of the user's choice.
		got := buildLaunchEnv(domain.BackendKindVLLM, []domain.EnvVar{
			{Key: "PYTHONUNBUFFERED", Value: "0"},
		})
		if !envContains(got, "PYTHONUNBUFFERED=0") {
			t.Errorf("explicit override lost; got %v", got)
		}
		if envContains(got, "PYTHONUNBUFFERED=1") {
			t.Errorf("forced =1 over explicit =0; got %v", got)
		}
	})
	t.Run("RespectsInheritedOverride", func(t *testing.T) {
		t.Setenv("PYTHONUNBUFFERED", "0")
		got := buildLaunchEnv(domain.BackendKindVLLM, nil)
		if !envContains(got, "PYTHONUNBUFFERED=0") {
			t.Errorf("inherited override lost; got %v", got)
		}
		if envContains(got, "PYTHONUNBUFFERED=1") {
			t.Errorf("forced =1 over inherited =0; got %v", got)
		}
	})
}

func TestManager_LaunchBackground_NoOverrideUsesDefaultBinary(t *testing.T) {
	fb := fakeBinary(t)
	mgr, _ := newTestManager(t)
	port := freePort(t)
	p := domain.Profile{
		ID:    "default-binary",
		Model: "/dev/null",
		Args:  map[string]any{"port": float64(port)},
	}
	inst, err := mgr.Launch(p, LaunchBackground, "")
	if err != nil {
		t.Fatalf("Launch: %v", err)
	}
	defer mgr.Kill(inst.PID)

	if inst.BinaryPath != fb {
		t.Fatalf("BinaryPath = %q, want %q", inst.BinaryPath, fb)
	}
}

func TestManager_LaunchBackground_InvalidEffectiveBinary(t *testing.T) {
	badBinary := filepath.Join(t.TempDir(), "does-not-exist")
	mgr := New(Config{
		Resolver:     func(_ domain.Profile) (string, domain.BackendKind, error) { return badBinary, "", nil },
		LogDir:       filepath.Join(t.TempDir(), "logs"),
		RegistryPath: filepath.Join(t.TempDir(), "instances.json"),
	})
	port := freePort(t)
	p := domain.Profile{
		ID:    "invalid-binary",
		Model: "/dev/null",
		Args:  map[string]any{"port": float64(port)},
	}
	_, err := mgr.Launch(p, LaunchBackground, "")
	if err == nil {
		t.Fatal("expected invalid binary error, got nil")
	}
	want := "start process"
	if !strings.Contains(err.Error(), want) {
		t.Fatalf("err = %v, want prefix containing %q", err, want)
	}
}

func TestManager_Launch_ModelMissing(t *testing.T) {
	mgr, _ := newTestManager(t)
	port := freePort(t)
	p := domain.Profile{
		ID:    "missing",
		Model: "/nonexistent/path/to/model.gguf",
		Args:  map[string]any{"port": float64(port)},
	}
	_, err := mgr.Launch(p, LaunchBackground, "")
	if err == nil {
		t.Fatal("expected ErrModelNotFound, got nil")
	}
	if !errors.Is(err, ErrModelNotFound) {
		t.Fatalf("err = %v, want ErrModelNotFound", err)
	}
}

func TestManager_WaitHealthy_TimesOut(t *testing.T) {
	mgr, _ := newTestManager(t)
	port := freePort(t)
	// A real, alive-for-the-whole-test PID (the test process itself) with
	// nothing listening on port — exercises the genuine "never became
	// healthy" timeout path, distinct from the dead-process fast-fail path.
	err := mgr.WaitHealthy(os.Getpid(), port, 300*time.Millisecond, "")
	if err == nil {
		t.Fatal("expected timeout error, got nil")
	}
	if !errors.Is(err, ErrHealthCheckTimeout) {
		t.Fatalf("err = %v, want ErrHealthCheckTimeout", err)
	}
}

func TestManager_WaitHealthy_DeadProcessFailsFast(t *testing.T) {
	mgr, _ := newTestManager(t)
	port := freePort(t)

	cmd := exec.Command("sh", "-c", "exit 0")
	if err := cmd.Start(); err != nil {
		t.Fatalf("start doomed process: %v", err)
	}
	_ = cmd.Wait()
	deadPID := cmd.Process.Pid

	start := time.Now()
	err := mgr.WaitHealthy(deadPID, port, 5*time.Second, "")
	elapsed := time.Since(start)

	if !errors.Is(err, ErrProcessExited) {
		t.Fatalf("err = %v, want ErrProcessExited", err)
	}
	if elapsed > time.Second {
		t.Fatalf("elapsed = %v, want fast failure (<1s)", elapsed)
	}
}

type sinkSpy struct {
	calls []string // ProfileIDs
}

func (s *sinkSpy) MarkLastUsed(profileID string, at time.Time) error {
	s.calls = append(s.calls, profileID)
	return nil
}

func TestManager_Launch_NotifiesLastUsedSink(t *testing.T) {
	dir := t.TempDir()
	spy := &sinkSpy{}
	mgr := New(Config{
		Resolver:     func(_ domain.Profile) (string, domain.BackendKind, error) { return fakeBinary(t), "", nil },
		LogDir:       filepath.Join(dir, "logs"),
		RegistryPath: filepath.Join(dir, "instances.json"),
		LastUsedSink: spy,
	})
	port := freePort(t)
	p := domain.Profile{ID: "tracked", Model: "/dev/null", Args: map[string]any{"port": float64(port)}}
	inst, err := mgr.Launch(p, LaunchBackground, "")
	if err != nil {
		t.Fatalf("Launch: %v", err)
	}
	defer mgr.Kill(inst.PID)

	if err := mgr.WaitHealthy(inst.PID, inst.Port, 5*time.Second, ""); err != nil {
		t.Fatalf("WaitHealthy: %v", err)
	}
	if len(spy.calls) != 1 || spy.calls[0] != "tracked" {
		t.Errorf("sink.calls = %v, want [tracked]", spy.calls)
	}
}

func TestTailLogs_HappyPath(t *testing.T) {
	mgr, _ := newTestManager(t)
	port := freePort(t)
	p := domain.Profile{
		ID:    "tail",
		Name:  "Tail",
		Model: "/dev/null",
		Args:  map[string]any{"port": float64(port)},
	}
	inst, err := mgr.Launch(p, LaunchBackground, "")
	if err != nil {
		t.Fatalf("Launch: %v", err)
	}
	defer mgr.Kill(inst.PID)

	if err := mgr.WaitHealthy(inst.PID, inst.Port, 5*time.Second, ""); err != nil {
		t.Fatalf("WaitHealthy: %v", err)
	}

	rc, err := mgr.TailLogs(inst.PID)
	if err != nil {
		t.Fatalf("TailLogs: %v", err)
	}
	defer rc.Close()

	buf := make([]byte, 256)
	n, _ := rc.Read(buf)
	if n == 0 {
		t.Fatal("TailLogs returned empty buffer")
	}
}

func TestTailLogs_UnknownPID(t *testing.T) {
	mgr, _ := newTestManager(t)
	if _, err := mgr.TailLogs(999_999); !errors.Is(err, ErrUnknownPID) {
		t.Fatalf("err = %v, want ErrUnknownPID", err)
	}
}

func TestManager_ResolverError(t *testing.T) {
	cfg := Config{
		Resolver:     func(_ domain.Profile) (string, domain.BackendKind, error) { return "", "", ErrBinaryNotFound },
		LogDir:       t.TempDir(),
		RegistryPath: filepath.Join(t.TempDir(), "i.json"),
	}
	mgr := New(cfg)
	port := freePort(t)
	p := domain.Profile{ID: "fail", Model: "/dev/null", Args: map[string]any{"port": float64(port)}}
	_, err := mgr.Launch(p, LaunchBackground, "")
	if err == nil {
		t.Fatal("expected error for resolver failure, got nil")
	}
	if !errors.Is(err, ErrBinaryNotFound) {
		t.Fatalf("err = %v, want ErrBinaryNotFound", err)
	}
}

func TestManager_Foreground_OnlyOneAllowed(t *testing.T) {
	mgr, _ := newTestManager(t)
	port1 := freePort(t)
	port2 := freePort(t)

	p1 := domain.Profile{ID: "fg1", Model: "/dev/null", Args: map[string]any{"port": float64(port1)}}
	inst1, err := mgr.Launch(p1, LaunchForeground, "")
	if err != nil {
		t.Fatalf("first foreground Launch: %v", err)
	}
	defer mgr.Kill(inst1.PID)

	p2 := domain.Profile{ID: "fg2", Model: "/dev/null", Args: map[string]any{"port": float64(port2)}}
	_, err = mgr.Launch(p2, LaunchForeground, "")
	if err == nil {
		t.Fatal("expected ErrForegroundBusy, got nil")
	}
	if !errors.Is(err, ErrForegroundBusy) {
		t.Fatalf("err = %v, want ErrForegroundBusy", err)
	}

	// background launch alongside fg1 must still succeed
	port3 := freePort(t)
	p3 := domain.Profile{ID: "bg1", Model: "/dev/null", Args: map[string]any{"port": float64(port3)}}
	inst3, err := mgr.Launch(p3, LaunchBackground, "")
	if err != nil {
		t.Fatalf("background launch alongside fg: %v", err)
	}
	defer mgr.Kill(inst3.PID)
}
