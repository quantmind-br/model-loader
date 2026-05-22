# CLI Parity — Phase 1 (Foundation) Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Migrate the hand-rolled `flag`-based subcommand dispatch onto cobra and extract the service-wiring `bootstrap` into a reusable `internal/app` package, preserving the exact behavior of the existing `serve`, `import`, `benchmark`, and `download` commands and the default TUI.

**Architecture:** A new `internal/app` package owns `Bootstrap()` (config + logger + service wiring + reconcile) and the single-instance flock. A new `internal/cli` package owns a cobra command tree and an `Execute() int` entrypoint that maps a typed `ExitError` to a process exit code. `package main` becomes a 3-line shim that registers `runTUI` as the cobra root's default action via a `cli.TUIRunner` callback (avoiding an import cycle, since the TUI pulls in bubbletea/ui which `internal/cli` must not). Phase 1 ships zero new user-facing commands — it is a behavior-preserving refactor that unblocks Phases 2–5.

**Tech Stack:** Go 1.26.2, `github.com/spf13/cobra` (new), `github.com/spf13/pflag` (already an indirect dep via viper), existing service packages under `internal/service/*`.

---

## File Structure

- **Create** `internal/app/bootstrap.go` — `Services` struct (exported fields), `Bootstrap(cliLevel string) (*Services, error)`, `(*Services).Close()`, plus the relocated helpers `ensureDefaultCatalog`, `buildResolver`, and the `restartHelper`.
- **Create** `internal/app/lock.go` — relocated `AcquireSingleInstanceLock` (exported).
- **Create** `internal/app/bootstrap_test.go` — bootstrap-against-temp-dirs test.
- **Create** `internal/app/lock_test.go` — lock acquire/contention test.
- **Create** `internal/cli/root.go` — `rootCmd`, persistent `--log-level`, `TUIRunner` callback, `ExitError`, `Execute() int`.
- **Create** `internal/cli/root_test.go` — Execute/ExitError mapping + root metadata tests.
- **Create** `internal/cli/serve.go` — `serve` cobra command.
- **Create** `internal/cli/importcmd.go` — `import` cobra command.
- **Create** `internal/cli/benchmark.go` — `benchmark` cobra command + relocated benchmark print/format helpers.
- **Create** `internal/cli/download.go` — hidden `download` worker cobra command.
- **Modify** `cmd/model-loader/main.go` — strip `flag` dispatch, change `runTUI` to `runTUI(cliLevel string) int` using `app.Bootstrap`/`app.AcquireSingleInstanceLock`, set `cli.TUIRunner = runTUI`, call `cli.Execute()`.
- **Delete** `cmd/model-loader/bootstrap.go`, `cmd/model-loader/lock.go`, `cmd/model-loader/benchmark.go`, `cmd/model-loader/download.go` (contents relocated).

---

## Task 1: Add the cobra dependency

**Files:**
- Modify: `go.mod`, `go.sum`

- [ ] **Step 1: Add cobra**

Run:
```bash
cd /home/diogo/dev/model-loader && go get github.com/spf13/cobra@latest
```
Expected: `go.mod` gains `github.com/spf13/cobra vX.Y.Z` in the `require` block; `github.com/spf13/pflag` is promoted from indirect to direct (cobra uses it).

- [ ] **Step 2: Verify the module still builds**

Run:
```bash
go build ./...
```
Expected: builds with no errors (nothing imports cobra yet, this just confirms the dependency resolves).

- [ ] **Step 3: Commit**

```bash
git add go.mod go.sum
git commit -m "build: add spf13/cobra dependency for CLI"
```

---

## Task 2: Create `internal/app` with `Services` + `Bootstrap`

This relocates the wiring out of `package main`. The body of `Bootstrap` is the current `bootstrap` body from `cmd/model-loader/bootstrap.go:57-148`, with the return shape changed from `(cfg, logger, closeLog, svc, err)` to `(*Services, error)`.

**Files:**
- Create: `internal/app/bootstrap.go`
- Create: `internal/app/bootstrap_test.go`

- [ ] **Step 1: Write the failing test**

Create `internal/app/bootstrap_test.go`:
```go
package app_test

import (
	"path/filepath"
	"testing"

	"github.com/quantmind-br/model-loader/internal/app"
)

func TestBootstrap_WiresServices(t *testing.T) {
	dir := t.TempDir()
	// Point every config path at the temp dir so Bootstrap never touches
	// the developer's real ~/.config / ~/.local/state.
	t.Setenv("MODEL_LOADER_PATHS_STATE_DIR", filepath.Join(dir, "state"))
	t.Setenv("MODEL_LOADER_PATHS_PROFILES_DIR", filepath.Join(dir, "profiles"))
	t.Setenv("MODEL_LOADER_PATHS_BACKENDS_DIR", filepath.Join(dir, "backends"))
	t.Setenv("MODEL_LOADER_PATHS_LOG_DIR", filepath.Join(dir, "logs"))

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
```

> If the config env-var keys above do not match `internal/config` mapstructure tags, open `internal/config/config.go`, read the viper key names, and adjust the `t.Setenv` keys to the real ones (the test's intent — redirect all paths to a temp dir — is what matters).

- [ ] **Step 2: Run the test to verify it fails**

Run:
```bash
go test ./internal/app/ -run TestBootstrap_WiresServices -v
```
Expected: FAIL — package `internal/app` does not exist yet (`no Go files` / `undefined: app.Bootstrap`).

- [ ] **Step 3: Create `internal/app/bootstrap.go`**

Move the imports and the bodies of `bootstrap`, `restartHelper`, `ensureDefaultCatalog`, and `buildResolver` here. `ensureDefaultCatalog` and `buildResolver` currently live in `cmd/model-loader/main.go:296-345`; `restartHelper` and `bootstrap` in `cmd/model-loader/bootstrap.go`. Transform: package becomes `app`; `bootServices` becomes the exported `Services` with the field renames below; the return signature collapses to `(*Services, error)`; `closeLog` is stored on `Services` and run by `Close()`.

```go
// Package app wires every service the model-loader needs and is the single
// bootstrap reused by the TUI, the CLI, and the headless serve/benchmark paths.
package app

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"

	"github.com/quantmind-br/model-loader/internal/config"
	"github.com/quantmind-br/model-loader/internal/domain"
	"github.com/quantmind-br/model-loader/internal/log"
	"github.com/quantmind-br/model-loader/internal/service/backendcatalog"
	"github.com/quantmind-br/model-loader/internal/service/backendschema"
	"github.com/quantmind-br/model-loader/internal/service/migration"
	"github.com/quantmind-br/model-loader/internal/service/processmgr"
	"github.com/quantmind-br/model-loader/internal/service/profilestore"
	"github.com/quantmind-br/model-loader/internal/service/validator"
)

// Services is the dependency container shared by the TUI, CLI, and headless
// entrypoints. Callers MUST defer Close() to flush the log file and stop the
// process manager.
type Services struct {
	Cfg           config.AppConfig
	Logger        *slog.Logger
	Store         profilestore.Store
	CatalogStore  backendcatalog.Store
	SchemaStore   backendcatalog.SchemaStore
	SchemaManager *backendschema.Manager
	Resolver      backendcatalog.Resolver
	DefaultSchema domain.FlagSchema
	Mgr           processmgr.Manager
	Val           validator.Validator

	closeLog func()
}

// Close stops the process manager and flushes the logger, in that order.
func (s *Services) Close() {
	if s == nil {
		return
	}
	if s.Mgr != nil {
		s.Mgr.Close()
	}
	if s.closeLog != nil {
		s.closeLog()
	}
}

type restartHelper struct {
	store profilestore.Store
	mgr   processmgr.Manager
}

func (r *restartHelper) restart(profileID string) {
	if r.store == nil || r.mgr == nil {
		return
	}
	p, err := r.store.Get(profileID)
	if err != nil {
		return
	}
	_, _ = r.mgr.Launch(p, processmgr.LaunchBackground, "watchdog")
}

// Bootstrap loads config, builds the logger, wires every service and performs
// initial migrations + reconcile. On error the message is routed to stderr and
// any partially-built logger is closed before returning.
func Bootstrap(cliLevel string) (*Services, error) {
	cfg, err := config.Load()
	if err != nil {
		fmt.Fprintf(os.Stderr, "config error: %v\n", err)
		return nil, err
	}

	level := log.ResolveLevel(cliLevel, os.Getenv("MODEL_LOADER_LOG_LEVEL"), cfg.Logging.Level)
	logger, closeLog, err := log.New(log.Config{Dir: cfg.Paths.LogDir, Level: level})
	if err != nil {
		fmt.Fprintf(os.Stderr, "log init error: %v\n", err)
		return nil, err
	}
	logger.Info("app_start",
		"log_dir", cfg.Paths.LogDir,
		"state_dir", cfg.Paths.StateDir,
		"level", level.String())

	store, err := profilestore.NewFSStore(cfg.Paths.ProfilesDir)
	if err != nil {
		logger.Error("boot_failed", "step", "profile_store", "err", err)
		fmt.Fprintf(os.Stderr, "profile store: %v\n", err)
		closeLog()
		return nil, err
	}

	catalogStore := backendcatalog.NewFSStore(cfg.Paths.BackendsDir)
	schemaStore := backendcatalog.NewFSSchemaStore(cfg.Paths.BackendsDir)

	schemaManager := backendschema.NewManager(catalogStore, schemaStore)
	schemaManager.Register(domain.BackendKindLlamaServer, backendschema.NewLlamaServerGenerator(schemaStore))
	schemaManager.Register(domain.BackendKindSGLang, backendschema.NewSGLangGenerator(schemaStore))
	schemaManager.Register(domain.BackendKindVLLM, backendschema.NewVLLMGenerator(schemaStore))
	schemaManager.Register(domain.BackendKindDFlash, backendschema.NewDFlashGenerator(schemaStore))
	schemaManager.Register(domain.BackendKindBuunLlamaCpp, backendschema.NewBuunServerGenerator(schemaStore))

	migrator := migration.NewService(cfg, store, catalogStore, schemaStore, schemaManager)
	migReport, mErr := migrator.Run(context.Background())
	if mErr != nil {
		fmt.Fprintf(os.Stderr, "migration: %v\n", mErr)
		logger.Error("migration_failed", "err", mErr)
	}
	for _, w := range migReport.Warnings {
		fmt.Fprintf(os.Stderr, "migration warning: %s\n", w)
		logger.Warn("migration_warning", "msg", w)
	}

	resolver := backendcatalog.NewResolver(catalogStore, schemaStore, logger)
	defaultSchema := ensureDefaultCatalog(catalogStore, schemaStore, schemaManager, cfg.Paths.LlamaServerBinaryPath, logger)
	if _, perr := backendschema.EnsurePresentations(catalogStore, schemaStore); perr != nil {
		logger.Warn("ensure_presentations_failed", "err", perr)
	}

	helper := &restartHelper{}
	mgr := processmgr.New(processmgr.Config{
		Resolver:     buildResolver(resolver),
		LogDir:       cfg.Paths.LogDir,
		RegistryPath: filepath.Join(cfg.Paths.StateDir, "instances.json"),
		HistoryPath:  filepath.Join(cfg.Paths.StateDir, "instances-history.json"),
		LastUsedSink: store,
		Logger:       logger,
		RestartFunc:  helper.restart,
	})
	if rErr := mgr.Reconcile(); rErr != nil {
		logger.Error("reconcile_failed", "err", rErr)
		fmt.Fprintf(os.Stderr, "instance recovery: %v\n", rErr)
	}

	helper.store = store
	helper.mgr = mgr

	return &Services{
		Cfg:           cfg,
		Logger:        logger,
		Store:         store,
		CatalogStore:  catalogStore,
		SchemaStore:   schemaStore,
		SchemaManager: schemaManager,
		Resolver:      resolver,
		DefaultSchema: defaultSchema,
		Mgr:           mgr,
		Val:           val(logger),
		closeLog:      closeLog,
	}, nil
}

func val(logger *slog.Logger) validator.Validator { return validator.New(logger) }
```

Then paste the bodies of `ensureDefaultCatalog` (from `cmd/model-loader/main.go:296-335`) and `buildResolver` (from `cmd/model-loader/main.go:337-345`) verbatim into this file — they are already lowercase/unexported and used only inside `Bootstrap`, so no signature change is needed. Their imports (`domain`, `backendcatalog`, `backendschema`) are already in the import block above.

- [ ] **Step 4: Run the test to verify it passes**

Run:
```bash
go test ./internal/app/ -run TestBootstrap_WiresServices -v
```
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/app/bootstrap.go internal/app/bootstrap_test.go
git commit -m "feat(app): extract Bootstrap + Services into internal/app"
```

---

## Task 3: Move the single-instance lock into `internal/app`

**Files:**
- Create: `internal/app/lock.go`
- Create: `internal/app/lock_test.go`

- [ ] **Step 1: Write the failing test**

Create `internal/app/lock_test.go`:
```go
package app_test

import (
	"testing"

	"github.com/quantmind-br/model-loader/internal/app"
)

func TestAcquireSingleInstanceLock_Contention(t *testing.T) {
	dir := t.TempDir()

	release1, acquired1, err := app.AcquireSingleInstanceLock(dir)
	if err != nil {
		t.Fatalf("first acquire: %v", err)
	}
	if !acquired1 {
		t.Fatalf("first acquire should succeed on a fresh dir")
	}
	defer release1()

	// A second acquire on the same fd-backed lock within the SAME process
	// still observes the held flock and must report not-acquired.
	release2, acquired2, err := app.AcquireSingleInstanceLock(dir)
	if err != nil {
		t.Fatalf("second acquire: %v", err)
	}
	if acquired2 {
		if release2 != nil {
			release2()
		}
		t.Fatalf("second acquire should fail while the first holds the lock")
	}

	release1()
	release3, acquired3, err := app.AcquireSingleInstanceLock(dir)
	if err != nil {
		t.Fatalf("third acquire: %v", err)
	}
	if !acquired3 {
		t.Fatalf("acquire should succeed after the first is released")
	}
	release3()
}
```

> Note: `flock` is per-open-file-description, so two separate `os.OpenFile` opens in the same process do contend — this test is valid without spawning a subprocess.

- [ ] **Step 2: Run the test to verify it fails**

Run:
```bash
go test ./internal/app/ -run TestAcquireSingleInstanceLock_Contention -v
```
Expected: FAIL — `undefined: app.AcquireSingleInstanceLock`.

- [ ] **Step 3: Create `internal/app/lock.go`**

Move the body of `acquireSingleInstanceLock` from `cmd/model-loader/lock.go:19-40` verbatim, renaming it to the exported `AcquireSingleInstanceLock` and changing the package to `app`:
```go
package app

import (
	"fmt"
	"os"
	"path/filepath"
	"syscall"
)

// AcquireSingleInstanceLock takes an exclusive, non-blocking advisory lock on
// <stateDir>/model-loader.lock. It stops a second process from racing on the
// shared state files (instances.json / proxy-state.json).
//
// Returns (release, true, nil) when the lock was acquired — call release on
// exit to drop it. Returns (nil, false, nil) when another live process already
// holds the lock. The kernel releases the lock automatically if this process
// dies, so a crash never leaves a stale lock behind.
func AcquireSingleInstanceLock(stateDir string) (release func(), acquired bool, err error) {
	if err := os.MkdirAll(stateDir, 0o755); err != nil {
		return nil, false, fmt.Errorf("mkdir state dir: %w", err)
	}
	path := filepath.Join(stateDir, "model-loader.lock")
	f, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR, 0o644)
	if err != nil {
		return nil, false, fmt.Errorf("open lock file: %w", err)
	}
	if err := syscall.Flock(int(f.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		_ = f.Close()
		if err == syscall.EWOULDBLOCK {
			return nil, false, nil
		}
		return nil, false, fmt.Errorf("flock: %w", err)
	}
	release = func() {
		_ = syscall.Flock(int(f.Fd()), syscall.LOCK_UN)
		_ = f.Close()
	}
	return release, true, nil
}
```

- [ ] **Step 4: Run the test to verify it passes**

Run:
```bash
go test ./internal/app/ -run TestAcquireSingleInstanceLock_Contention -v
```
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/app/lock.go internal/app/lock_test.go
git commit -m "feat(app): move single-instance flock into internal/app"
```

---

## Task 4: Create `internal/cli` root, `ExitError`, and `Execute`

**Files:**
- Create: `internal/cli/root.go`
- Create: `internal/cli/root_test.go`

- [ ] **Step 1: Write the failing test**

Create `internal/cli/root_test.go`:
```go
package cli

import (
	"errors"
	"testing"
)

func TestExecute_MapsExitError(t *testing.T) {
	// A command returning *ExitError{Code:2} must surface as exit code 2.
	rootCmd.SetArgs([]string{"_exittest"})
	t.Cleanup(func() { rootCmd.SetArgs(nil) })

	if code := Execute(); code != 2 {
		t.Fatalf("expected exit code 2 from ExitError, got %d", code)
	}
}

func TestExitError_IsError(t *testing.T) {
	var err error = &ExitError{Code: 7}
	var ee *ExitError
	if !errors.As(err, &ee) || ee.Code != 7 {
		t.Fatalf("ExitError should round-trip through errors.As")
	}
}
```

> The `_exittest` hidden command is added in Step 3 purely so this test can drive `Execute()` end-to-end. It is fine to leave it in the tree as a hidden no-op test hook, or guard its registration behind the test; this plan leaves it registered and hidden.

- [ ] **Step 2: Run the test to verify it fails**

Run:
```bash
go test ./internal/cli/ -run TestExecute_MapsExitError -v
```
Expected: FAIL — package has no Go files / `undefined: rootCmd`.

- [ ] **Step 3: Create `internal/cli/root.go`**

```go
// Package cli is the cobra command tree exposing model-loader functionality
// without the interactive TUI. It depends on internal/app for service wiring
// and MUST NOT import the bubbletea TUI (internal/ui) — the TUI is registered
// as the root's default action through the TUIRunner callback to avoid a cycle.
package cli

import (
	"errors"
	"fmt"

	"github.com/spf13/cobra"
)

// TUIRunner is set by package main to the interactive TUI entrypoint. It runs
// when model-loader is invoked with no subcommand. The string argument is the
// resolved --log-level (empty means "use config/env default").
var TUIRunner func(logLevel string) int

// logLevel holds the value of the persistent --log-level flag, read by
// subcommands and passed to app.Bootstrap.
var logLevel string

// ExitError carries a specific process exit code out of a command's RunE so
// Execute can translate it. Commands that need a non-1 failure code (e.g.
// benchmark's exit 2 gate) return *ExitError.
type ExitError struct{ Code int }

func (e *ExitError) Error() string { return fmt.Sprintf("exit code %d", e.Code) }

var rootCmd = &cobra.Command{
	Use:   "model-loader",
	Short: "Manage llama.cpp profiles and llama-server processes",
	Long: "model-loader manages LLM server profiles and processes.\n" +
		"Run with no subcommand to launch the interactive TUI.",
	SilenceUsage:  true,
	SilenceErrors: true,
	RunE: func(cmd *cobra.Command, args []string) error {
		if TUIRunner == nil {
			return errors.New("no TUI runner registered")
		}
		if code := TUIRunner(logLevel); code != 0 {
			return &ExitError{Code: code}
		}
		return nil
	},
}

func init() {
	rootCmd.PersistentFlags().StringVar(&logLevel, "log-level", "",
		"override log level (debug|info|warn|error); also reads $MODEL_LOADER_LOG_LEVEL and config logging.level")

	// Hidden test hook so Execute() can be exercised end-to-end.
	rootCmd.AddCommand(&cobra.Command{
		Use:    "_exittest",
		Hidden: true,
		RunE:   func(*cobra.Command, []string) error { return &ExitError{Code: 2} },
	})
}

// Execute runs the cobra tree and returns the process exit code. An *ExitError
// surfaces its Code; any other error yields 1; success yields 0.
func Execute() int {
	err := rootCmd.Execute()
	var ee *ExitError
	if errors.As(err, &ee) {
		return ee.Code
	}
	if err != nil {
		fmt.Fprintln(rootCmd.ErrOrStderr(), err)
		return 1
	}
	return 0
}
```

- [ ] **Step 4: Run the tests to verify they pass**

Run:
```bash
go test ./internal/cli/ -run 'TestExecute_MapsExitError|TestExitError_IsError' -v
```
Expected: PASS for both.

- [ ] **Step 5: Commit**

```bash
git add internal/cli/root.go internal/cli/root_test.go
git commit -m "feat(cli): add cobra root, ExitError, and Execute"
```

---

## Task 5: Migrate the `serve` command

**Files:**
- Create: `internal/cli/serve.go`
- Create: `internal/cli/serve_test.go`

- [ ] **Step 1: Write the failing test**

Create `internal/cli/serve_test.go`:
```go
package cli

import "testing"

func TestServeCommand_Registered(t *testing.T) {
	cmd, _, err := rootCmd.Find([]string{"serve"})
	if err != nil || cmd == nil || cmd.Name() != "serve" {
		t.Fatalf("serve command not registered: cmd=%v err=%v", cmd, err)
	}
	if cmd.Flags().Lookup("host") == nil || cmd.Flags().Lookup("port") == nil {
		t.Fatalf("serve must expose --host and --port flags")
	}
}
```

- [ ] **Step 2: Run the test to verify it fails**

Run:
```bash
go test ./internal/cli/ -run TestServeCommand_Registered -v
```
Expected: FAIL — `serve` not found.

- [ ] **Step 3: Create `internal/cli/serve.go`**

Port the body of `runServe` (`cmd/model-loader/main.go:187-244`) into a cobra `RunE`, replacing the `flag.String/Int` declarations with cobra flag vars and `bootstrap()` with `app.Bootstrap()`:
```go
package cli

import (
	"context"
	"fmt"
	"os/signal"
	"syscall"
	"time"

	"github.com/spf13/cobra"

	"github.com/quantmind-br/model-loader/internal/app"
	"github.com/quantmind-br/model-loader/internal/service/httpproxy"
)

func init() {
	var host string
	var port int

	cmd := &cobra.Command{
		Use:   "serve",
		Short: "Run the headless HTTP proxy in the foreground",
		RunE: func(cmd *cobra.Command, args []string) error {
			svc, err := app.Bootstrap(logLevel)
			if err != nil {
				return &ExitError{Code: 1}
			}
			defer svc.Close()

			if host != "" {
				svc.Cfg.Serve.Host = host
			}
			if port != 0 {
				svc.Cfg.Serve.Port = port
			}

			ctx, stop := signal.NotifyContext(context.Background(),
				syscall.SIGINT, syscall.SIGTERM)
			defer stop()

			srv := httpproxy.New(httpproxy.Config{
				Host:                svc.Cfg.Serve.Host,
				Port:                svc.Cfg.Serve.Port,
				HealthCheckTimeout:  120 * time.Second,
				MaxBodyBuffer:       8 << 20,
				ShutdownGracePeriod: 10 * time.Second,
			}, httpproxy.Deps{
				ProfileStore: svc.Store,
				ProcessMgr:   svc.Mgr,
				Logger:       svc.Logger,
			})

			if err := srv.Start(ctx); err != nil {
				svc.Logger.Error("serve_start_failed", "err", err)
				fmt.Fprintf(cmd.ErrOrStderr(), "start: %v\n", err)
				return &ExitError{Code: 1}
			}
			fmt.Fprintf(cmd.OutOrStdout(), "Listening on %s:%d (logs: %s)\n",
				svc.Cfg.Serve.Host, svc.Cfg.Serve.Port, svc.Cfg.Paths.LogDir)
			svc.Logger.Info("serve_listening",
				"host", svc.Cfg.Serve.Host, "port", svc.Cfg.Serve.Port)

			<-ctx.Done()
			svc.Logger.Info("serve_signal_received")

			shCtx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
			defer cancel()
			if err := srv.Stop(shCtx); err != nil {
				svc.Logger.Error("serve_stop_failed", "err", err)
				fmt.Fprintf(cmd.ErrOrStderr(), "stop: %v\n", err)
				return &ExitError{Code: 1}
			}
			return nil
		},
	}
	cmd.Flags().StringVar(&host, "host", "", "override bind host")
	cmd.Flags().IntVar(&port, "port", 0, "override bind port")
	rootCmd.AddCommand(cmd)
}
```

- [ ] **Step 4: Run the test to verify it passes**

Run:
```bash
go test ./internal/cli/ -run TestServeCommand_Registered -v
```
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/cli/serve.go internal/cli/serve_test.go
git commit -m "feat(cli): migrate serve onto cobra"
```

---

## Task 6: Migrate the `import` command

**Files:**
- Create: `internal/cli/importcmd.go`
- Create: `internal/cli/importcmd_test.go`

- [ ] **Step 1: Write the failing test**

Create `internal/cli/importcmd_test.go`:
```go
package cli

import "testing"

func TestImportCommand_Registered(t *testing.T) {
	cmd, _, err := rootCmd.Find([]string{"import"})
	if err != nil || cmd == nil || cmd.Name() != "import" {
		t.Fatalf("import command not registered: cmd=%v err=%v", cmd, err)
	}
	if cmd.Flags().Lookup("mode") == nil {
		t.Fatalf("import must expose --mode flag")
	}
}
```

- [ ] **Step 2: Run the test to verify it fails**

Run:
```bash
go test ./internal/cli/ -run TestImportCommand_Registered -v
```
Expected: FAIL — `import` not found.

- [ ] **Step 3: Create `internal/cli/importcmd.go`**

Port `runImport` (`cmd/model-loader/main.go:250-282`), turning the positional usage into `cobra.ExactArgs(1)`:
```go
package cli

import (
	"fmt"

	"github.com/spf13/cobra"

	"github.com/quantmind-br/model-loader/internal/app"
	"github.com/quantmind-br/model-loader/internal/service/profilestore"
)

func init() {
	var modeFlag string

	cmd := &cobra.Command{
		Use:   "import <path>",
		Short: "Import a profile bundle from a JSON file",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			path := args[0]
			mode := profilestore.ConflictModeMerge
			if modeFlag != "" {
				mode = profilestore.ConflictMode(modeFlag)
			}

			svc, err := app.Bootstrap(logLevel)
			if err != nil {
				return &ExitError{Code: 1}
			}
			defer svc.Close()

			res, err := profilestore.ImportBundle(svc.Store, path, mode)
			if err != nil {
				svc.Logger.Error("import_failed", "err", err)
				fmt.Fprintf(cmd.ErrOrStderr(), "import failed: %v\n", err)
				return &ExitError{Code: 1}
			}
			fmt.Fprintf(cmd.OutOrStdout(), "Import result: added=%d skipped=%d renamed=%d replaced=%d\n",
				res.Added, res.Skipped, res.Renamed, res.Replaced)
			return nil
		},
	}
	cmd.Flags().StringVar(&modeFlag, "mode", "merge", "conflict resolution: merge|overwrite|rename")
	rootCmd.AddCommand(cmd)
}
```

- [ ] **Step 4: Run the test to verify it passes**

Run:
```bash
go test ./internal/cli/ -run TestImportCommand_Registered -v
```
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/cli/importcmd.go internal/cli/importcmd_test.go
git commit -m "feat(cli): migrate import onto cobra"
```

---

## Task 7: Migrate the `benchmark` command

This moves the benchmark logic and all its print/format helpers out of `cmd/model-loader/benchmark.go`. The `--list/--compare/--transcript/--profile/--mode/--json/--min-solve` flags and the 0/1/2 exit-code contract are preserved exactly.

**Files:**
- Create: `internal/cli/benchmark.go`
- Create: `internal/cli/benchmark_test.go`

- [ ] **Step 1: Write the failing test**

Create `internal/cli/benchmark_test.go`:
```go
package cli

import "testing"

func TestBenchmarkCommand_Registered(t *testing.T) {
	cmd, _, err := rootCmd.Find([]string{"benchmark"})
	if err != nil || cmd == nil || cmd.Name() != "benchmark" {
		t.Fatalf("benchmark command not registered: cmd=%v err=%v", cmd, err)
	}
	for _, f := range []string{"profile", "mode", "list", "compare", "json", "min-solve", "transcript"} {
		if cmd.Flags().Lookup(f) == nil {
			t.Fatalf("benchmark must expose --%s flag", f)
		}
	}
}

func TestParseBenchMode(t *testing.T) {
	if m, ok := parseBenchMode("judge"); !ok || m == "" {
		t.Fatalf("judge should parse")
	}
	if _, ok := parseBenchMode("bogus"); ok {
		t.Fatalf("bogus mode must not parse")
	}
}
```

- [ ] **Step 2: Run the test to verify it fails**

Run:
```bash
go test ./internal/cli/ -run 'TestBenchmarkCommand_Registered|TestParseBenchMode' -v
```
Expected: FAIL — `benchmark` not found / `undefined: parseBenchMode`.

- [ ] **Step 3: Create `internal/cli/benchmark.go`**

(a) Copy the helper functions `parseBenchMode`, `emitJSON`, `printRun`, `benchPrintList`, `benchPrintHistory`, `benchPrintCompare`, `benchPrintTranscript`, `indent`, `dashOr`, `clip` verbatim from `cmd/model-loader/benchmark.go:162-340` into this file (they are unexported and self-contained; only the package line changes to `cli`).

(b) Add the command, porting `runBenchmark`'s body (`cmd/model-loader/benchmark.go:26-160`). Replace the `flag.*` vars with cobra flag vars, `acquireSingleInstanceLock` with `app.AcquireSingleInstanceLock`, `bootstrap` with `app.Bootstrap`, and the `return N` exit codes with `return &ExitError{Code: N}` (return `nil` for code 0):
```go
package cli

import (
	"context"
	"fmt"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"

	"github.com/spf13/cobra"

	"github.com/quantmind-br/model-loader/internal/app"
	"github.com/quantmind-br/model-loader/internal/config"
	"github.com/quantmind-br/model-loader/internal/service/benchmark"
	"github.com/quantmind-br/model-loader/internal/service/benchmarkstore"
	"github.com/quantmind-br/model-loader/internal/service/monitor"
)

func init() {
	var (
		profileID  string
		modeStr    string
		list       bool
		compare    bool
		asJSON     bool
		minSolve   float64
		transcript string
	)

	cmd := &cobra.Command{
		Use:   "benchmark",
		Short: "Run, list, or compare profile benchmarks",
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, err := config.Load()
			if err != nil {
				fmt.Fprintf(cmd.ErrOrStderr(), "config error: %v\n", err)
				return &ExitError{Code: 1}
			}
			store := benchmarkstore.New(filepath.Join(cfg.Paths.StateDir, "benchmark", "runs"))

			// Read-only queries: no bootstrap (its Reconcile would rewrite
			// instances.json), no lock.
			if transcript != "" {
				return benchExit(benchPrintTranscript(store, transcript, asJSON))
			}
			if list {
				return benchExit(benchPrintList(store, asJSON))
			}
			if compare && profileID == "" {
				return benchExit(benchPrintCompare(store, asJSON))
			}
			if profileID == "" {
				fmt.Fprintln(cmd.ErrOrStderr(), "usage:")
				fmt.Fprintln(cmd.ErrOrStderr(), "  model-loader benchmark --profile <id> [--mode judge|longctx|llama-bench] [--json] [--min-solve N]")
				fmt.Fprintln(cmd.ErrOrStderr(), "  model-loader benchmark --list [--json]")
				fmt.Fprintln(cmd.ErrOrStderr(), "  model-loader benchmark --compare [--profile <id>] [--json]")
				fmt.Fprintln(cmd.ErrOrStderr(), "  model-loader benchmark --transcript <run-id> [--json]")
				return &ExitError{Code: 1}
			}
			if compare {
				return benchExit(benchPrintHistory(store, profileID, asJSON))
			}

			mode, ok := parseBenchMode(modeStr)
			if !ok {
				fmt.Fprintf(cmd.ErrOrStderr(), "unknown mode %q (want judge|longctx|llama-bench)\n", modeStr)
				return &ExitError{Code: 1}
			}

			release, acquired, lErr := app.AcquireSingleInstanceLock(cfg.Paths.StateDir)
			if lErr != nil {
				fmt.Fprintf(cmd.ErrOrStderr(), "single-instance lock: %v\n", lErr)
			}
			if release != nil {
				defer release()
			}
			if !acquired {
				fmt.Fprintln(cmd.ErrOrStderr(), "another model-loader instance is running (TUI/serve) — close it first.")
				return &ExitError{Code: 1}
			}

			svc, err := app.Bootstrap(logLevel)
			if err != nil {
				return &ExitError{Code: 1}
			}
			defer svc.Close()

			mon := monitor.New(monitor.Config{NvidiaSMIPath: "nvidia-smi"})
			runner, err := benchmark.NewRunner(svc.Store, svc.Mgr, mon, svc.Resolver, benchmark.Config{
				MaxTokens:         cfg.Benchmark.MaxTokens,
				Temperature:       cfg.Benchmark.Temperature,
				Timeout:           time.Duration(cfg.Benchmark.TimeoutSec) * time.Second,
				LongContextTokens: cfg.Benchmark.LongContextTokens,
				SaveTranscripts:   true,
				Judge: benchmark.JudgeEndpoint{
					BaseURL: cfg.Benchmark.Judge.BaseURL,
					APIKey:  cfg.Benchmark.Judge.APIKey,
					Model:   cfg.Benchmark.Judge.Model,
					Samples: cfg.Benchmark.Judge.Samples,
				},
				LlamaBenchPresets: cfg.Benchmark.LlamaBench.Presets,
				LlamaBenchReps:    cfg.Benchmark.LlamaBench.Repetitions,
			})
			if err != nil {
				svc.Logger.Error("benchmark_engine_init_failed", "err", err)
				fmt.Fprintf(cmd.ErrOrStderr(), "benchmark engine: %v\n", err)
				return &ExitError{Code: 1}
			}

			ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
			defer stop()

			progress := make(chan benchmark.Progress, 32)
			drained := make(chan struct{})
			go func() {
				for p := range progress {
					switch p.Phase {
					case "launch":
						fmt.Fprintln(cmd.ErrOrStderr(), "launching backend / waiting for /health…")
					case "infer", "score":
						fmt.Fprintf(cmd.ErrOrStderr(), "[%d/%d] %s (%s)\n", p.Index, p.Total, p.ProblemName, p.Phase)
					}
				}
				close(drained)
			}()

			run, err := runner.Run(ctx, benchmark.RunConfig{ProfileID: profileID, Mode: mode}, progress)
			close(progress)
			<-drained
			if err != nil {
				fmt.Fprintf(cmd.ErrOrStderr(), "run failed: %v\n", err)
				return &ExitError{Code: 1}
			}
			if err := store.Save(run); err != nil {
				fmt.Fprintf(cmd.ErrOrStderr(), "warning: could not save run: %v\n", err)
			}
			if len(run.Transcript) > 0 {
				fmt.Fprintf(cmd.ErrOrStderr(), "transcript: %s\n", store.TranscriptPath(run.ID))
				fmt.Fprintf(cmd.ErrOrStderr(), "inspect raw output: model-loader benchmark --transcript %s\n", run.ID)
			}

			if asJSON {
				emitJSON(run)
			} else {
				printRun(run)
			}
			if minSolve >= 0 && run.Aggregate.SolveRate < minSolve {
				fmt.Fprintf(cmd.ErrOrStderr(), "FAIL: solve rate %.2f below --min-solve %.2f\n", run.Aggregate.SolveRate, minSolve)
				return &ExitError{Code: 2}
			}
			return nil
		},
	}

	cmd.Flags().StringVar(&profileID, "profile", "", "profile id to benchmark")
	cmd.Flags().StringVar(&modeStr, "mode", "judge", "scoring mode: judge | longctx | llama-bench")
	cmd.Flags().BoolVar(&list, "list", false, "list saved runs and exit")
	cmd.Flags().BoolVar(&compare, "compare", false, "with --profile: that profile's run history; alone: latest run per profile")
	cmd.Flags().BoolVar(&asJSON, "json", false, "emit JSON instead of text")
	cmd.Flags().Float64Var(&minSolve, "min-solve", -1, "exit code 2 if solve rate < this (0..1); -1 disables")
	cmd.Flags().StringVar(&transcript, "transcript", "", "print the raw transcript of a saved run id and exit")
	rootCmd.AddCommand(cmd)
}

// benchExit maps the legacy int return of the bench print helpers (0 ok, 1 err)
// onto the cobra error contract.
func benchExit(code int) error {
	if code == 0 {
		return nil
	}
	return &ExitError{Code: code}
}
```

> The bench print helpers still write to `os.Stdout`/`os.Stderr` directly (they were copied verbatim). That preserves current behavior exactly; redirect-to-`cmd` plumbing is deferred to the Phase 5 output polish.

- [ ] **Step 4: Run the tests to verify they pass**

Run:
```bash
go test ./internal/cli/ -run 'TestBenchmarkCommand_Registered|TestParseBenchMode' -v
```
Expected: PASS for both.

- [ ] **Step 5: Commit**

```bash
git add internal/cli/benchmark.go internal/cli/benchmark_test.go
git commit -m "feat(cli): migrate benchmark onto cobra"
```

---

## Task 8: Migrate the hidden `download` worker command

The `download` subcommand is an internal subprocess contract spawned by `downloadmgr`. It must keep accepting exactly one positional `<state-path>` and ignore flags. It is hidden from help.

**Files:**
- Create: `internal/cli/download.go`
- Create: `internal/cli/download_test.go`

- [ ] **Step 1: Write the failing test**

Create `internal/cli/download_test.go`:
```go
package cli

import "testing"

func TestDownloadCommand_HiddenAndPositional(t *testing.T) {
	cmd, _, err := rootCmd.Find([]string{"download"})
	if err != nil || cmd == nil || cmd.Name() != "download" {
		t.Fatalf("download command not registered: cmd=%v err=%v", cmd, err)
	}
	if !cmd.Hidden {
		t.Fatalf("download worker command must be hidden from help")
	}
}
```

- [ ] **Step 2: Run the test to verify it fails**

Run:
```bash
go test ./internal/cli/ -run TestDownloadCommand_HiddenAndPositional -v
```
Expected: FAIL — `download` not found.

- [ ] **Step 3: Create `internal/cli/download.go`**

Port `runDownloadWorker` (`cmd/model-loader/download.go:18-42`) into a hidden cobra command. The original read `os.Args[1]`; cobra hands the positional as `args[0]`:
```go
package cli

import (
	"net/http"
	"os"
	"time"

	"github.com/spf13/cobra"

	"github.com/quantmind-br/model-loader/internal/service/downloadmgr"
)

func init() {
	cmd := &cobra.Command{
		Use:                "download <state-path>",
		Short:              "Internal: run a single download worker (spawned by the manager)",
		Hidden:             true,
		DisableFlagParsing: true, // worker argv is a contract; never reinterpret flags
		Args:               cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			statePath := args[0]

			ua := os.Getenv("MODEL_LOADER_USER_AGENT")
			if ua == "" {
				ua = "model-loader/dev"
			}
			httpClient := &http.Client{
				Timeout: 0, // long downloads — ctx drives cancellation
				Transport: &http.Transport{
					ResponseHeaderTimeout: 60 * time.Second,
					IdleConnTimeout:       90 * time.Second,
				},
			}

			code := downloadmgr.RunWorker(downloadmgr.WorkerConfig{
				StatePath: statePath,
				UserAgent: ua,
				Client:    httpClient,
			})
			if code != 0 {
				return &ExitError{Code: code}
			}
			return nil
		},
	}
	rootCmd.AddCommand(cmd)
}
```

> `DisableFlagParsing: true` together with `ExactArgs(1)` is intentional — but note that with flag parsing disabled, `--help` reaches `args`. Since this command is hidden and only ever invoked by the manager with a single path, that is acceptable. If the manager's spawn argv is later found to include leading flags, revisit `internal/service/downloadmgr` spawn code (out of scope here).

- [ ] **Step 4: Run the test to verify it passes**

Run:
```bash
go test ./internal/cli/ -run TestDownloadCommand_HiddenAndPositional -v
```
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/cli/download.go internal/cli/download_test.go
git commit -m "feat(cli): migrate hidden download worker onto cobra"
```

---

## Task 9: Rewire `cmd/model-loader/main.go` and delete the relocated files

**Files:**
- Modify: `cmd/model-loader/main.go`
- Delete: `cmd/model-loader/bootstrap.go`, `cmd/model-loader/lock.go`, `cmd/model-loader/benchmark.go`, `cmd/model-loader/download.go`

- [ ] **Step 1: Replace `main()` with the cobra shim**

In `cmd/model-loader/main.go`, replace the entire `main()` function (lines 36-57) with:
```go
func main() {
	cli.TUIRunner = runTUI
	os.Exit(cli.Execute())
}
```

- [ ] **Step 2: Change `runTUI` to accept the log level and use `app.Bootstrap`**

Change the signature `func runTUI() int` to `func runTUI(cliLevel string) int`. Delete its first two statements (the `flag.String("log-level", ...)` declaration at line 60 and `flag.Parse()` at line 61). Replace the early-lock block (lines 68-81) and the `bootstrap` call (line 83) with the `app`-based equivalents, and update every `svc.` / `cfg` / `logger` / `closeLog` reference in the rest of the function per the rename table below. The top of the new `runTUI` body becomes:
```go
func runTUI(cliLevel string) int {
	// Single-instance guard: a second interactive TUI sharing the same state
	// dir would race on instances.json / proxy-state.json. Acquire the lock
	// before bootstrap so we don't run the boot-time reconcile from a duplicate
	// session. Config-load failures here are non-fatal — Bootstrap reports them.
	if early, cErr := config.Load(); cErr == nil {
		release, acquired, lErr := app.AcquireSingleInstanceLock(early.Paths.StateDir)
		if lErr != nil {
			fmt.Fprintf(os.Stderr, "single-instance lock: %v\n", lErr)
		}
		if release != nil {
			defer release()
		}
		if !acquired {
			fmt.Fprintln(os.Stderr, "model-loader is already running (another instance holds the state lock).")
			fmt.Fprintln(os.Stderr, "Close the other session first — two instances would clobber instances.json / proxy-state.json.")
			return 1
		}
	}

	svc, err := app.Bootstrap(cliLevel)
	if err != nil {
		return 1
	}
	defer svc.Close()
	// ... rest of the function continues, using svc.Cfg / svc.Logger / svc.Mgr etc.
```

Apply these exact renames throughout the remainder of `runTUI` (the wiring block at lines 90-184):

| Old reference | New reference |
|---|---|
| `cfg` | `svc.Cfg` |
| `logger` | `svc.Logger` |
| `svc.store` | `svc.Store` |
| `svc.mgr` | `svc.Mgr` |
| `svc.catalogStore` | `svc.CatalogStore` |
| `svc.schemaStore` | `svc.SchemaStore` |
| `svc.schemaManager` | `svc.SchemaManager` |
| `svc.resolver` | `svc.Resolver` |
| `svc.defaultSchema` | `svc.DefaultSchema` |
| `svc.val` | `svc.Val` |
| `defer closeLog()` | (remove — `defer svc.Close()` already covers it) |
| `defer svc.mgr.Close()` | (remove — `svc.Close()` calls `Mgr.Close()`) |

> `svc.Close()` runs `Mgr.Close()` then `closeLog()`, matching the original defer order (`mgr.Close` before `closeLog`). Removing the two separate defers and keeping a single `defer svc.Close()` preserves that order.

- [ ] **Step 3: Fix the import block in `main.go`**

`main.go` keeps the TUI-only wiring, so it still imports bubbletea, `internal/ui`, `internal/ui/pages`, `internal/service/{backendcatalog,backendschema,benchmark,benchmarkstore,downloadmgr,hfhub,monitor,modelscanner,proxysupervisor}`, `internal/config`, `internal/domain`. It NO LONGER needs `flag`, `net/http` stays (used by hfClient), `syscall` is removed (lock moved), and it must ADD `github.com/quantmind-br/model-loader/internal/app` and `github.com/quantmind-br/model-loader/internal/cli`. The helpers `ensureDefaultCatalog`/`buildResolver` moved to `internal/app`, so delete them from `main.go` (lines 296-345) — but `resolveExportDir` (284-294) and `parseTab` (347-362) stay. Let the compiler drive the final import list: run `go build ./cmd/model-loader/` and remove/add imports until it is clean (`goimports -w cmd/model-loader/main.go` if available).

- [ ] **Step 4: Delete the relocated files**

Run:
```bash
git rm cmd/model-loader/bootstrap.go cmd/model-loader/lock.go cmd/model-loader/benchmark.go cmd/model-loader/download.go
```

- [ ] **Step 5: Build the command**

Run:
```bash
go build ./...
```
Expected: builds clean. If it fails, the errors will point at leftover `bootstrap(`/`acquireSingleInstanceLock(`/`runServe`/`runImport`/`runBenchmark`/`runDownloadWorker` references in `main.go` — remove those now-dead functions (`runServe`, `runImport`, and any leftover dispatch) since their behavior now lives in `internal/cli`.

- [ ] **Step 6: Vet**

Run:
```bash
go vet ./...
```
Expected: no findings.

- [ ] **Step 7: Commit**

```bash
git add cmd/model-loader/main.go
git commit -m "refactor(cmd): dispatch through cobra via cli.Execute, drop hand-rolled flag parsing"
```

---

## Task 10: End-to-end verification

**Files:** none (verification only).

- [ ] **Step 1: Run the full test suite**

Run:
```bash
go test ./...
```
Expected: PASS. (The pre-existing `internal/service/llamahelp` golden test may fail locally if a real `llama-server` is installed — see memory note `llamahelp-golden-env-failure`. That is not a regression from this work.)

- [ ] **Step 2: Build the binary**

Run:
```bash
make build
```
Expected: `bin/model-loader` produced.

- [ ] **Step 3: Smoke-test command dispatch (help)**

Run:
```bash
./bin/model-loader --help
./bin/model-loader benchmark --help
./bin/model-loader serve --help
./bin/model-loader import --help
```
Expected: root help lists `serve`, `import`, `benchmark` (and NOT the hidden `download` or `_exittest`); each subcommand prints its flags. No panics.

- [ ] **Step 4: Smoke-test a read-only command**

Run:
```bash
./bin/model-loader benchmark --list
```
Expected: prints "no benchmark runs yet" (or the saved-runs table), exit code 0. Confirm with `echo $?`.

- [ ] **Step 5: Smoke-test the benchmark exit-code gate path is wired**

Run:
```bash
./bin/model-loader benchmark
echo "exit=$?"
```
Expected: prints the usage block to stderr and `exit=1` (no `--profile` given).

- [ ] **Step 6: Confirm the TUI still launches as the default action**

Run (interactive — launch then quit with `q`):
```bash
./bin/model-loader
```
Expected: the 5-tab TUI opens and quits cleanly. (Skip in non-interactive CI; covered by the build + dispatch tests.)

- [ ] **Step 7: Final commit (if any verification fixes were needed)**

```bash
git add -A
git commit -m "test: verify Phase 1 CLI foundation end-to-end"
```

---

## Notes for Phases 2–5

Each subsequent domain (`profile`, `instance`, `model`, `backend`, `proxy`) gets its own plan, written against the real service signatures at that time, and follows the same pattern established here: one cobra file per domain in `internal/cli`, commands call `app.Bootstrap`, lifecycle commands call `app.AcquireSingleInstanceLock` and return `*ExitError`, read-only commands skip the lock. The global `--json` flag and `output.go` rendering helpers are introduced in Phase 2 (the first phase that adds new read commands needing both table and JSON output).
