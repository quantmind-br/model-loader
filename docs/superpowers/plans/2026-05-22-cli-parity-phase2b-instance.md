# CLI Parity Phase 2b — Instance Commands Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking. **Depends on Phase 2a** (shared `output.go` helpers + `ExitError`).

**Goal:** Expose the Server tab's instance lifecycle and observability through `model-loader instance …` subcommands (list/start/stop/restart/logs/metrics/show/history), with start/stop/restart taking the single-instance flock and read-only commands running lockless.

**Architecture:** Thin cobra `RunE`s that bootstrap services and delegate to pure functions taking the `processmgr.Manager` interface and an `io.Writer`. Tests use a hand-written `fakeManager` implementing `Manager` — no real processes. Lifecycle commands acquire `app.AcquireSingleInstanceLock(cfg.Paths.StateDir)` *before* bootstrap and fail fast (exit 1) when the TUI holds it, exactly like `benchmark.go`.

**Tech Stack:** Go 1.26.2, spf13/cobra, processmgr, monitor/metricsstore.

---

## File Structure

- `internal/cli/instance.go` (Create) — `instance` parent, `instance list`, `instance show`, `instance history`, and `resolveInstance`.
- `internal/cli/instance_lifecycle.go` (Create) — `instance start`, `stop`, `restart` (flock-guarded).
- `internal/cli/instance_observe.go` (Create) — `instance logs` (`--follow`), `instance metrics` (`--watch`).
- `internal/cli/instance_test.go` (Create) — `fakeManager` test double + list/show/history/resolve tests.
- `internal/cli/instance_lifecycle_test.go` (Create) — start/stop/restart logic tests against `fakeManager`.
- `internal/cli/instance_observe_test.go` (Create) — logs/metrics rendering tests.

Reference signatures (verbatim, from the codebase):

```go
// internal/service/processmgr/processmgr.go
type Manager interface {
	Launch(p domain.Profile, mode LaunchMode, attemptID string) (domain.RunningInstance, error)
	Kill(pid int) error
	List() []domain.RunningInstance
	WaitHealthy(pid, port int, timeout time.Duration, attemptID string) error
	TailLogs(pid int) (io.ReadCloser, error)
	Close() error
	GetExitInfo(pid int) (ExitInfo, bool)
	History() []domain.ExitedInstance
}
const ( LaunchBackground LaunchMode = iota; LaunchForeground )

// internal/domain/instance.go — domain.RunningInstance fields:
//   ProfileID string; PID int; Port int; LogPath string; BinaryPath string;
//   StartedAt time.Time; Background bool; Crashed bool; ExitedAt *time.Time;
//   RestartCount int; ExitCode *int; ExitReason string; StderrTail []string
// domain.ExitedInstance: ProfileID, PID, Port, StartedAt, ExitedAt, DurationSeconds, Crashed, ExitCode, ExitReason

// internal/service/metricsstore/store.go
func Read(dataDir, profileID string, since time.Time) ([]Record, error)
// Record: TS int64; TokensPerSec float64; PromptEvalTPS float64; TTFTMs int64; RPS float64; SlotUtilization float64

// metrics dir = filepath.Join(cfg.Paths.StateDir, "metrics")
// attempt id    = log.NewAttemptID()   (import internal/log)
```

---

## Task 1: `instance` parent + `list` + `show` + `history` + resolution

**Files:**
- Create: `internal/cli/instance.go`
- Create: `internal/cli/instance_test.go`

- [ ] **Step 1: Write the failing test** — `internal/cli/instance_test.go`

```go
package cli

import (
	"bytes"
	"encoding/json"
	"io"
	"strings"
	"testing"
	"time"

	"github.com/quantmind-br/model-loader/internal/domain"
	"github.com/quantmind-br/model-loader/internal/service/processmgr"
)

// fakeManager implements processmgr.Manager for CLI tests.
type fakeManager struct {
	running  []domain.RunningInstance
	exited   []domain.ExitedInstance
	killed   []int
	launched []domain.Profile
	launchErr error
	tail     string
}

func (f *fakeManager) Launch(p domain.Profile, mode processmgr.LaunchMode, attemptID string) (domain.RunningInstance, error) {
	if f.launchErr != nil {
		return domain.RunningInstance{}, f.launchErr
	}
	f.launched = append(f.launched, p)
	ri := domain.RunningInstance{ProfileID: p.ID, PID: 4242, Port: 8080}
	f.running = append(f.running, ri)
	return ri, nil
}
func (f *fakeManager) Kill(pid int) error { f.killed = append(f.killed, pid); return nil }
func (f *fakeManager) List() []domain.RunningInstance { return f.running }
func (f *fakeManager) WaitHealthy(pid, port int, timeout time.Duration, attemptID string) error { return nil }
func (f *fakeManager) TailLogs(pid int) (io.ReadCloser, error) {
	return io.NopCloser(strings.NewReader(f.tail)), nil
}
func (f *fakeManager) Close() error { return nil }
func (f *fakeManager) GetExitInfo(pid int) (processmgr.ExitInfo, bool) { return processmgr.ExitInfo{}, false }
func (f *fakeManager) History() []domain.ExitedInstance { return f.exited }

func TestListInstances_TableAndJSON(t *testing.T) {
	m := &fakeManager{running: []domain.RunningInstance{
		{ProfileID: "alpha", PID: 100, Port: 8080, StartedAt: time.Now()},
	}}
	var buf bytes.Buffer
	if err := listInstances(&buf, m, false); err != nil {
		t.Fatalf("list: %v", err)
	}
	if !strings.Contains(buf.String(), "alpha") || !strings.Contains(buf.String(), "100") {
		t.Fatalf("table missing data: %q", buf.String())
	}

	buf.Reset()
	if err := listInstances(&buf, m, true); err != nil {
		t.Fatalf("list json: %v", err)
	}
	var got []domain.RunningInstance
	if err := json.Unmarshal(buf.Bytes(), &got); err != nil {
		t.Fatalf("not JSON: %v", err)
	}
	if len(got) != 1 || got[0].PID != 100 {
		t.Fatalf("json wrong: %+v", got)
	}
}

func TestResolveInstance_ByPIDAndProfile(t *testing.T) {
	m := &fakeManager{running: []domain.RunningInstance{
		{ProfileID: "alpha", PID: 100},
		{ProfileID: "beta", PID: 200},
	}}
	if ri, err := resolveInstance(m, "200"); err != nil || ri.ProfileID != "beta" {
		t.Fatalf("by pid: %+v %v", ri, err)
	}
	if ri, err := resolveInstance(m, "alpha"); err != nil || ri.PID != 100 {
		t.Fatalf("by profile id: %+v %v", ri, err)
	}
	if _, err := resolveInstance(m, "nope"); err == nil {
		t.Fatalf("expected not found")
	}
}

func TestHistory_JSON(t *testing.T) {
	m := &fakeManager{exited: []domain.ExitedInstance{{ProfileID: "alpha", PID: 100, DurationSeconds: 42}}}
	var buf bytes.Buffer
	if err := listHistory(&buf, m, true); err != nil {
		t.Fatalf("history: %v", err)
	}
	if !strings.Contains(buf.String(), "alpha") {
		t.Fatalf("history json missing: %q", buf.String())
	}
}
```

- [ ] **Step 2: Run to verify it fails**

Run: `go test ./internal/cli/ -run 'TestListInstances|TestResolveInstance|TestHistory' -v`
Expected: FAIL — undefined `listInstances`, `resolveInstance`, `listHistory`.

- [ ] **Step 3: Create `internal/cli/instance.go`**

```go
package cli

import (
	"fmt"
	"io"
	"strconv"
	"strings"
	"time"

	"github.com/quantmind-br/model-loader/internal/app"
	"github.com/quantmind-br/model-loader/internal/domain"
	"github.com/quantmind-br/model-loader/internal/service/processmgr"
	"github.com/spf13/cobra"
)

var instanceCmd = &cobra.Command{
	Use:   "instance",
	Short: "Manage llama-server instances",
}

func init() {
	rootCmd.AddCommand(instanceCmd)

	instanceCmd.AddCommand(&cobra.Command{
		Use:   "list",
		Short: "List running instances",
		RunE: instanceReadRunE(func(out io.Writer, mgr processmgr.Manager, _ string, _ []string) error {
			return listInstances(out, mgr, jsonOut)
		}),
	})
	instanceCmd.AddCommand(&cobra.Command{
		Use:   "show <pid|id>",
		Short: "Show a single instance",
		Args:  cobra.ExactArgs(1),
		RunE: instanceReadRunE(func(out io.Writer, mgr processmgr.Manager, _ string, args []string) error {
			return showInstance(out, mgr, args[0], jsonOut)
		}),
	})
	instanceCmd.AddCommand(&cobra.Command{
		Use:   "history",
		Short: "List exited instances",
		RunE: instanceReadRunE(func(out io.Writer, mgr processmgr.Manager, _ string, _ []string) error {
			return listHistory(out, mgr, jsonOut)
		}),
	})
}

// instanceReadRunE wires Bootstrap (no lock — read-only) + error→ExitError.
// The string passed to fn is cfg.Paths.StateDir (for the metrics dir, etc.).
func instanceReadRunE(fn func(out io.Writer, mgr processmgr.Manager, stateDir string, args []string) error) func(*cobra.Command, []string) error {
	return func(cmd *cobra.Command, args []string) error {
		svc, err := app.Bootstrap(logLevel)
		if err != nil {
			return &ExitError{Code: 1}
		}
		defer svc.Close()
		if err := fn(cmd.OutOrStdout(), svc.Mgr, svc.Cfg.Paths.StateDir, args); err != nil {
			fmt.Fprintln(cmd.ErrOrStderr(), err)
			return &ExitError{Code: 1}
		}
		return nil
	}
}

// resolveInstance matches ref against PID (numeric) then ProfileID (exact, then unique prefix).
func resolveInstance(mgr processmgr.Manager, ref string) (domain.RunningInstance, error) {
	insts := mgr.List()
	if pid, err := strconv.Atoi(ref); err == nil {
		for _, ri := range insts {
			if ri.PID == pid {
				return ri, nil
			}
		}
	}
	var byPrefix []domain.RunningInstance
	for _, ri := range insts {
		if ri.ProfileID == ref {
			return ri, nil
		}
		if strings.HasPrefix(ri.ProfileID, ref) {
			byPrefix = append(byPrefix, ri)
		}
	}
	switch len(byPrefix) {
	case 1:
		return byPrefix[0], nil
	case 0:
		return domain.RunningInstance{}, fmt.Errorf("instance not found: %s", ref)
	default:
		return domain.RunningInstance{}, fmt.Errorf("ambiguous instance ref %q matches %d instances; use the pid", ref, len(byPrefix))
	}
}

func instanceStatus(ri domain.RunningInstance) string {
	switch {
	case ri.Crashed:
		return "crashed"
	case ri.ExitedAt != nil:
		return "exited"
	default:
		return "running"
	}
}

func listInstances(w io.Writer, mgr processmgr.Manager, asJSON bool) error {
	insts := mgr.List()
	if asJSON {
		if insts == nil {
			insts = []domain.RunningInstance{}
		}
		return emitJSON(w, insts)
	}
	rows := make([][]string, 0, len(insts))
	for _, ri := range insts {
		rows = append(rows, []string{
			strconv.Itoa(ri.PID),
			clip(ri.ProfileID, 24),
			strconv.Itoa(ri.Port),
			instanceStatus(ri),
			ri.StartedAt.Format(time.RFC3339),
		})
	}
	printTable(w, []string{"PID", "PROFILE", "PORT", "STATUS", "STARTED"}, rows)
	if len(rows) == 0 {
		fmt.Fprintln(w, "(no running instances)")
	}
	return nil
}

func showInstance(w io.Writer, mgr processmgr.Manager, ref string, asJSON bool) error {
	ri, err := resolveInstance(mgr, ref)
	if err != nil {
		return err
	}
	if asJSON {
		return emitJSON(w, ri)
	}
	fmt.Fprintf(w, "PID:      %d\n", ri.PID)
	fmt.Fprintf(w, "Profile:  %s\n", ri.ProfileID)
	fmt.Fprintf(w, "Port:     %d\n", ri.Port)
	fmt.Fprintf(w, "Status:   %s\n", instanceStatus(ri))
	fmt.Fprintf(w, "Started:  %s\n", ri.StartedAt.Format(time.RFC3339))
	fmt.Fprintf(w, "Log:      %s\n", dashOr(ri.LogPath))
	if ri.BinaryPath != "" {
		fmt.Fprintf(w, "Binary:   %s\n", ri.BinaryPath)
	}
	return nil
}

func listHistory(w io.Writer, mgr processmgr.Manager, asJSON bool) error {
	hist := mgr.History()
	if asJSON {
		if hist == nil {
			hist = []domain.ExitedInstance{}
		}
		return emitJSON(w, hist)
	}
	rows := make([][]string, 0, len(hist))
	for _, ei := range hist {
		rows = append(rows, []string{
			strconv.Itoa(ei.PID),
			clip(ei.ProfileID, 24),
			ei.ExitedAt.Format(time.RFC3339),
			strconv.FormatInt(ei.DurationSeconds, 10) + "s",
		})
	}
	printTable(w, []string{"PID", "PROFILE", "EXITED", "DURATION"}, rows)
	if len(rows) == 0 {
		fmt.Fprintln(w, "(no exited instances)")
	}
	return nil
}
```

- [ ] **Step 4: Run to verify it passes**

Run: `go test ./internal/cli/ -run 'TestListInstances|TestResolveInstance|TestHistory' -v`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/cli/instance.go internal/cli/instance_test.go
git commit -m "feat(cli): add instance list/show/history and resolution"
```

---

## Task 2: `instance start` / `stop` / `restart` (flock-guarded)

**Files:**
- Create: `internal/cli/instance_lifecycle.go`
- Create: `internal/cli/instance_lifecycle_test.go`

The pure cores are `startInstance`, `stopInstance`, `restartInstance`, taking the `Manager` (+ `Store` for start/restart). The flock is acquired in `RunE` before bootstrap, mirroring `benchmark.go`.

- [ ] **Step 1: Write the failing test** — `internal/cli/instance_lifecycle_test.go`

```go
package cli

import (
	"strings"
	"testing"

	"github.com/quantmind-br/model-loader/internal/domain"
)

func TestStartInstance_LaunchesResolvedProfile(t *testing.T) {
	store := newTempStore(t)
	seed(t, store, "alpha", "Alpha")
	m := &fakeManager{}
	var out strings.Builder
	if err := startInstance(&out, m, store, "alpha", false); err != nil {
		t.Fatalf("start: %v", err)
	}
	if len(m.launched) != 1 || m.launched[0].ID != "alpha" {
		t.Fatalf("did not launch alpha: %+v", m.launched)
	}
	if !strings.Contains(out.String(), "4242") {
		t.Fatalf("expected pid in output: %q", out.String())
	}
}

func TestStopInstance_KillsResolvedPID(t *testing.T) {
	m := &fakeManager{running: []domain.RunningInstance{{ProfileID: "alpha", PID: 100}}}
	var out strings.Builder
	if err := stopInstance(&out, m, "100"); err != nil {
		t.Fatalf("stop: %v", err)
	}
	if len(m.killed) != 1 || m.killed[0] != 100 {
		t.Fatalf("did not kill 100: %+v", m.killed)
	}
}

func TestRestartInstance_KillsThenLaunches(t *testing.T) {
	store := newTempStore(t)
	seed(t, store, "alpha", "Alpha")
	m := &fakeManager{running: []domain.RunningInstance{{ProfileID: "alpha", PID: 100, Background: true}}}
	var out strings.Builder
	if err := restartInstance(&out, m, store, "100"); err != nil {
		t.Fatalf("restart: %v", err)
	}
	if len(m.killed) != 1 || m.killed[0] != 100 {
		t.Fatalf("did not kill old pid: %+v", m.killed)
	}
	if len(m.launched) != 1 || m.launched[0].ID != "alpha" {
		t.Fatalf("did not relaunch: %+v", m.launched)
	}
}
```

- [ ] **Step 2: Run to verify it fails**

Run: `go test ./internal/cli/ -run 'TestStartInstance|TestStopInstance|TestRestartInstance' -v`
Expected: FAIL — undefined symbols.

- [ ] **Step 3: Create `internal/cli/instance_lifecycle.go`**

```go
package cli

import (
	"fmt"
	"io"

	"github.com/quantmind-br/model-loader/internal/app"
	"github.com/quantmind-br/model-loader/internal/config"
	"github.com/quantmind-br/model-loader/internal/log"
	"github.com/quantmind-br/model-loader/internal/service/processmgr"
	"github.com/quantmind-br/model-loader/internal/service/profilestore"
	"github.com/spf13/cobra"
)

func init() {
	var foreground bool
	startCmd := &cobra.Command{
		Use:   "start <profile>",
		Short: "Launch an instance from a profile",
		Args:  cobra.ExactArgs(1),
		RunE: instanceLifecycleRunE(func(out io.Writer, svc *app.Services, args []string) error {
			return startInstance(out, svc.Mgr, svc.Store, args[0], foreground)
		}),
	}
	startCmd.Flags().BoolVar(&foreground, "foreground", false, "run in the foreground (default: background)")
	instanceCmd.AddCommand(startCmd)

	instanceCmd.AddCommand(&cobra.Command{
		Use:   "stop <pid|id>",
		Short: "Stop a running instance",
		Args:  cobra.ExactArgs(1),
		RunE: instanceLifecycleRunE(func(out io.Writer, svc *app.Services, args []string) error {
			return stopInstance(out, svc.Mgr, args[0])
		}),
	})
	instanceCmd.AddCommand(&cobra.Command{
		Use:   "restart <pid|id>",
		Short: "Restart a running instance",
		Args:  cobra.ExactArgs(1),
		RunE: instanceLifecycleRunE(func(out io.Writer, svc *app.Services, args []string) error {
			return restartInstance(out, svc.Mgr, svc.Store, args[0])
		}),
	})
}

// instanceLifecycleRunE acquires the single-instance flock before bootstrap and
// fails fast if the TUI/serve holds it, then runs fn. Mirrors benchmark.go.
func instanceLifecycleRunE(fn func(out io.Writer, svc *app.Services, args []string) error) func(*cobra.Command, []string) error {
	return func(cmd *cobra.Command, args []string) error {
		cfg, err := config.Load()
		if err != nil {
			fmt.Fprintf(cmd.ErrOrStderr(), "config error: %v\n", err)
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
			fmt.Fprintln(cmd.ErrOrStderr(), "another model-loader instance is running (TUI/serve) — close it first, or run on a headless host.")
			return &ExitError{Code: 1}
		}
		svc, err := app.Bootstrap(logLevel)
		if err != nil {
			return &ExitError{Code: 1}
		}
		defer svc.Close()
		if err := fn(cmd.OutOrStdout(), svc, args); err != nil {
			fmt.Fprintln(cmd.ErrOrStderr(), err)
			return &ExitError{Code: 1}
		}
		return nil
	}
}

func launchMode(foreground bool) processmgr.LaunchMode {
	if foreground {
		return processmgr.LaunchForeground
	}
	return processmgr.LaunchBackground
}

func startInstance(out io.Writer, mgr processmgr.Manager, store profilestore.Store, ref string, foreground bool) error {
	prof, err := resolveProfileRef(store, ref)
	if err != nil {
		return err
	}
	ri, err := mgr.Launch(prof, launchMode(foreground), log.NewAttemptID())
	if err != nil {
		return fmt.Errorf("launch: %w", err)
	}
	if jsonOut {
		return emitJSON(out, ri)
	}
	fmt.Fprintf(out, "started %s — pid %d port %d\n", prof.ID, ri.PID, ri.Port)
	return nil
}

func stopInstance(out io.Writer, mgr processmgr.Manager, ref string) error {
	ri, err := resolveInstance(mgr, ref)
	if err != nil {
		return err
	}
	if err := mgr.Kill(ri.PID); err != nil {
		return fmt.Errorf("kill: %w", err)
	}
	fmt.Fprintf(out, "stopped pid %d (%s)\n", ri.PID, ri.ProfileID)
	return nil
}

func restartInstance(out io.Writer, mgr processmgr.Manager, store profilestore.Store, ref string) error {
	ri, err := resolveInstance(mgr, ref)
	if err != nil {
		return err
	}
	prof, err := store.Get(ri.ProfileID)
	if err != nil {
		return fmt.Errorf("load profile %s: %w", ri.ProfileID, err)
	}
	if err := mgr.Kill(ri.PID); err != nil {
		return fmt.Errorf("kill: %w", err)
	}
	mode := processmgr.LaunchBackground
	if !ri.Background {
		mode = processmgr.LaunchForeground
	}
	newRI, err := mgr.Launch(prof, mode, log.NewAttemptID())
	if err != nil {
		return fmt.Errorf("relaunch: %w", err)
	}
	fmt.Fprintf(out, "restarted %s — old pid %d, new pid %d\n", prof.ID, ri.PID, newRI.PID)
	return nil
}
```

> Verify before coding: `log.NewAttemptID()` exists in `internal/log` (used at `internal/ui/pages/server_restart.go:156`). Verify `app.Services` is exported with `Mgr` and `Store` fields (it is — `internal/app/bootstrap.go`).

- [ ] **Step 4: Run to verify it passes + vet**

Run: `go test ./internal/cli/... && go vet ./internal/cli/...`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/cli/instance_lifecycle.go internal/cli/instance_lifecycle_test.go
git commit -m "feat(cli): add instance start/stop/restart with single-instance flock"
```

---

## Task 3: `instance logs` (`--follow`) + `instance metrics` (`--watch`)

**Files:**
- Create: `internal/cli/instance_observe.go`
- Create: `internal/cli/instance_observe_test.go`

`instance logs` opens `mgr.TailLogs(pid)` and copies to stdout; `--follow` re-reads from EOF on an interval until Ctrl-C. `instance metrics` reads `metricsstore.Read(<stateDir>/metrics, profileID, since)` and prints the latest record (or all within a window); `--watch` re-polls.

- [ ] **Step 1: Write the failing test** — `internal/cli/instance_observe_test.go`

```go
package cli

import (
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/quantmind-br/model-loader/internal/domain"
	"github.com/quantmind-br/model-loader/internal/service/metricsstore"
)

func TestPrintLogsOnce_CopiesTail(t *testing.T) {
	m := &fakeManager{
		running: []domain.RunningInstance{{ProfileID: "alpha", PID: 100}},
		tail:    "line one\nline two\n",
	}
	var out strings.Builder
	if err := printLogsOnce(&out, m, "100"); err != nil {
		t.Fatalf("logs: %v", err)
	}
	if !strings.Contains(out.String(), "line one") || !strings.Contains(out.String(), "line two") {
		t.Fatalf("log copy missing: %q", out.String())
	}
}

func TestPrintMetricsOnce_ReadsLatest(t *testing.T) {
	dir := t.TempDir()
	metricsDir := filepath.Join(dir, "metrics")
	if err := metricsstore.Append(metricsDir, "alpha", metricsstore.Record{
		TS: time.Now().Unix(), TokensPerSec: 42.5, RPS: 1.2,
	}); err != nil {
		t.Fatalf("append: %v", err)
	}
	m := &fakeManager{running: []domain.RunningInstance{{ProfileID: "alpha", PID: 100}}}
	var out strings.Builder
	if err := printMetricsOnce(&out, m, metricsDir, "100", false); err != nil {
		t.Fatalf("metrics: %v", err)
	}
	if !strings.Contains(out.String(), "42.5") {
		t.Fatalf("metrics missing tok/s: %q", out.String())
	}
}

func TestPrintMetricsOnce_JSON(t *testing.T) {
	dir := t.TempDir()
	metricsDir := filepath.Join(dir, "metrics")
	_ = metricsstore.Append(metricsDir, "alpha", metricsstore.Record{TS: time.Now().Unix(), TokensPerSec: 5})
	m := &fakeManager{running: []domain.RunningInstance{{ProfileID: "alpha", PID: 100}}}
	var out strings.Builder
	if err := printMetricsOnce(&out, m, metricsDir, "100", true); err != nil {
		t.Fatalf("metrics json: %v", err)
	}
	if !strings.Contains(out.String(), "tokens_per_sec") {
		t.Fatalf("expected JSON record field, got %q", out.String())
	}
}
```

- [ ] **Step 2: Run to verify it fails**

Run: `go test ./internal/cli/ -run 'TestPrintLogsOnce|TestPrintMetricsOnce' -v`
Expected: FAIL — undefined symbols.

- [ ] **Step 3: Create `internal/cli/instance_observe.go`**

```go
package cli

import (
	"fmt"
	"io"
	"time"

	"github.com/quantmind-br/model-loader/internal/service/metricsstore"
	"github.com/quantmind-br/model-loader/internal/service/processmgr"
	"github.com/spf13/cobra"
)

func init() {
	var follow bool
	logsCmd := &cobra.Command{
		Use:   "logs <pid|id>",
		Short: "Print (or follow) an instance's log",
		Args:  cobra.ExactArgs(1),
		RunE: instanceReadRunE(func(out io.Writer, mgr processmgr.Manager, _ string, args []string) error {
			if follow {
				return followLogs(out, mgr, args[0], 1*time.Second)
			}
			return printLogsOnce(out, mgr, args[0])
		}),
	}
	logsCmd.Flags().BoolVarP(&follow, "follow", "f", false, "follow the log (like tail -f)")
	instanceCmd.AddCommand(logsCmd)

	var watch bool
	var interval time.Duration
	metricsCmd := &cobra.Command{
		Use:   "metrics <pid|id>",
		Short: "Show recent metrics for an instance",
		Args:  cobra.ExactArgs(1),
		RunE: instanceReadRunE(func(out io.Writer, mgr processmgr.Manager, stateDir string, args []string) error {
			metricsDir := stateDir + "/metrics"
			if watch {
				return watchMetrics(out, mgr, metricsDir, args[0], interval)
			}
			return printMetricsOnce(out, mgr, metricsDir, args[0], jsonOut)
		}),
	}
	metricsCmd.Flags().BoolVar(&watch, "watch", false, "poll and redraw until interrupted")
	metricsCmd.Flags().DurationVar(&interval, "interval", 2*time.Second, "poll interval for --watch")
	instanceCmd.AddCommand(metricsCmd)
}

func printLogsOnce(out io.Writer, mgr processmgr.Manager, ref string) error {
	ri, err := resolveInstance(mgr, ref)
	if err != nil {
		return err
	}
	rc, err := mgr.TailLogs(ri.PID)
	if err != nil {
		return err
	}
	defer rc.Close()
	_, err = io.Copy(out, rc)
	return err
}

// followLogs copies the log, then re-opens on interval and copies any growth.
// It runs until the process is interrupted (Ctrl-C terminates the command).
func followLogs(out io.Writer, mgr processmgr.Manager, ref string, interval time.Duration) error {
	if err := printLogsOnce(out, mgr, ref); err != nil {
		return err
	}
	ri, err := resolveInstance(mgr, ref)
	if err != nil {
		return err
	}
	// Best-effort tail: re-open and skip already-emitted bytes. For simplicity
	// and robustness we track the byte offset already copied.
	var offset int64
	if rc, e := mgr.TailLogs(ri.PID); e == nil {
		n, _ := io.Copy(io.Discard, rc)
		offset = n
		rc.Close()
	}
	for {
		time.Sleep(interval)
		rc, e := mgr.TailLogs(ri.PID)
		if e != nil {
			return e
		}
		skipped, _ := io.CopyN(io.Discard, rc, offset)
		n, _ := io.Copy(out, rc)
		rc.Close()
		offset = skipped + n
	}
}

func printMetricsOnce(out io.Writer, mgr processmgr.Manager, metricsDir, ref string, asJSON bool) error {
	ri, err := resolveInstance(mgr, ref)
	if err != nil {
		return err
	}
	recs, err := metricsstore.Read(metricsDir, ri.ProfileID, time.Now().Add(-5*time.Minute))
	if err != nil {
		return err
	}
	if len(recs) == 0 {
		fmt.Fprintln(out, "(no recent metrics)")
		return nil
	}
	last := recs[len(recs)-1]
	if asJSON {
		return emitJSON(out, last)
	}
	fmt.Fprintf(out, "Profile: %s  pid %d\n", ri.ProfileID, ri.PID)
	fmt.Fprintf(out, "tok/s:   %.1f\n", last.TokensPerSec)
	fmt.Fprintf(out, "TTFT:    %dms\n", last.TTFTMs)
	fmt.Fprintf(out, "RPS:     %.2f\n", last.RPS)
	fmt.Fprintf(out, "Slot util: %.0f%%\n", last.SlotUtilization*100)
	return nil
}

func watchMetrics(out io.Writer, mgr processmgr.Manager, metricsDir, ref string, interval time.Duration) error {
	for {
		fmt.Fprint(out, "\033[H\033[2J") // clear screen
		if err := printMetricsOnce(out, mgr, metricsDir, ref, false); err != nil {
			return err
		}
		time.Sleep(interval)
	}
}
```

> Build the metrics dir with `filepath.Join(stateDir, "metrics")` rather than string concatenation; the snippet uses `+ "/metrics"` for brevity — switch to `filepath.Join` and import `path/filepath` in the final code.

- [ ] **Step 4: Run to verify it passes**

Run: `go test ./internal/cli/ -run 'TestPrintLogsOnce|TestPrintMetricsOnce' -v`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/cli/instance_observe.go internal/cli/instance_observe_test.go
git commit -m "feat(cli): add instance logs (--follow) and metrics (--watch)"
```

---

## Task 4: Registration test + build smoke

**Files:**
- Create: `internal/cli/instance_registration_test.go`

- [ ] **Step 1: Write the test**

```go
package cli

import "testing"

func TestInstanceSubcommandsRegistered(t *testing.T) {
	for _, name := range []string{"list", "show", "history", "start", "stop", "restart", "logs", "metrics"} {
		cmd, _, err := rootCmd.Find([]string{"instance", name})
		if err != nil || cmd == nil || cmd.Name() != name {
			t.Fatalf("instance %s not registered: cmd=%v err=%v", name, cmd, err)
		}
	}
}
```

- [ ] **Step 2: Run + build**

Run: `go test ./internal/cli/... && go build ./... && make build`
Expected: PASS, binary builds.

- [ ] **Step 3: Manual smoke (no TUI running)**

```bash
./bin/model-loader instance list
./bin/model-loader instance list --json
./bin/model-loader instance history
# start requires a real profile + binary; skip if unavailable in the sandbox
```
Expected: list/history print sensible empty-state output without a TUI running.

- [ ] **Step 4: Commit**

```bash
git add internal/cli/instance_registration_test.go
git commit -m "test(cli): assert all instance subcommands are registered"
```

---

## Self-Review Notes

- **Spec coverage:** every row of the spec's `instance` table maps to a task (list/show/history→T1, start/stop/restart→T2, logs/metrics→T3). `--watch`/`--follow`/`--interval` covered in T1/T3.
- **Concurrency:** start/stop/restart take `app.AcquireSingleInstanceLock` before bootstrap and exit 1 if the TUI holds it (matches the spec's lifecycle-lock row); read-only list/show/history/logs/metrics run lockless.
- **Restart fidelity:** mirrors `server_restart.go` — Kill then Launch with the prior `Background` mode and a fresh `log.NewAttemptID()`.
- **Type consistency:** `resolveInstance`, `instanceStatus`, `launchMode`, `instanceReadRunE`, `instanceLifecycleRunE` each defined once. `fakeManager` (test double) defined once in `instance_test.go` and reused by lifecycle/observe tests.
- **Verify-before-coding flags:** `log.NewAttemptID()`; `metricsstore.Append`/`Read` signatures; `processmgr.ExitInfo` type name for the `fakeManager.GetExitInfo` return; `domain.RunningInstance` field names used in tables.
