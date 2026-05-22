# CLI Parity Phase 2a — Profile Commands + Shared Foundation Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Expose every Profiles-tab capability through scriptable `model-loader profile …` subcommands (list/show/create/edit/delete/duplicate/rename/pin/unpin/export/import/validate), plus the shared CLI output/locking foundation the rest of Phase 2 depends on.

**Architecture:** Thin cobra `RunE`s that bootstrap services and delegate to small pure functions taking interfaces (`profilestore.Store`, `backendcatalog.Resolver`, `validator.Validator`) and an `io.Writer`. Those functions are unit-tested against a real `FSStore` in a temp dir — no mocks. Output (table vs JSON) is centralized in `output.go`; profile read-modify-write is serialized across CLI processes by a CLI-local sibling-`.lock` flock helper (the TUI does not take this lock — CLI-vs-TUI safety still rides on the store's atomic writes + the TUI's reload).

**Tech Stack:** Go 1.26.2, spf13/cobra, text/tabwriter, syscall.Flock.

---

## File Structure

- `internal/cli/output.go` (Create) — shared render helpers (`emitJSON`, `printTable`, `dashOr`, `clip`, `indent`) + the global `--json` persistent flag var (`jsonOut`). Becomes the single home for the JSON/table helpers currently duplicated in `benchmark.go`.
- `internal/cli/output_test.go` (Create) — table/JSON helper tests.
- `internal/cli/filelock.go` (Create) — `withProfileLock` advisory flock helper.
- `internal/cli/filelock_test.go` (Create) — concurrency test for the lock.
- `internal/cli/profile.go` (Create) — `profile` parent command, `profile list`, `profile show`, and `resolveProfileRef`.
- `internal/cli/profile_edit.go` (Create) — `profile create`, `profile edit`, `assembleProfile`, `coerceArgs`, flag parsing helpers.
- `internal/cli/profile_crud.go` (Create) — `profile delete`, `duplicate`, `rename`, `pin`, `unpin`.
- `internal/cli/profile_bundle.go` (Create) — `profile export`, `profile import`, `profile validate`.
- `internal/cli/profile_test.go`, `profile_edit_test.go`, `profile_crud_test.go`, `profile_bundle_test.go` (Create) — per-file tests.
- `internal/cli/benchmark.go` (Modify) — drop local `emitJSON`/`clip`/`dashOr`/`indent`; call the shared ones from `output.go` with an explicit `io.Writer`.
- `internal/cli/root.go` (Modify) — register the `--json` persistent flag.

Shared test helper (define once, in `profile_test.go`): `newTempStore(t)` returns a `*profilestore.FSStore` rooted at `t.TempDir()`.

---

## Task 1: Shared output helpers + global `--json` flag

**Files:**
- Create: `internal/cli/output.go`
- Create: `internal/cli/output_test.go`
- Modify: `internal/cli/root.go` (init)
- Modify: `internal/cli/benchmark.go` (remove duplicated helpers, pass writer)

- [ ] **Step 1: Write the failing test** — `internal/cli/output_test.go`

```go
package cli

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"
)

func TestEmitJSON_Indented(t *testing.T) {
	var buf bytes.Buffer
	if err := emitJSON(&buf, map[string]int{"a": 1}); err != nil {
		t.Fatalf("emitJSON: %v", err)
	}
	var got map[string]int
	if err := json.Unmarshal(buf.Bytes(), &got); err != nil {
		t.Fatalf("output not valid JSON: %v", err)
	}
	if got["a"] != 1 {
		t.Fatalf("round-trip failed: %v", got)
	}
	if !strings.Contains(buf.String(), "\n") {
		t.Fatalf("expected indented (multi-line) JSON, got %q", buf.String())
	}
}

func TestPrintTable_AlignsColumns(t *testing.T) {
	var buf bytes.Buffer
	printTable(&buf, []string{"ID", "NAME"}, [][]string{
		{"a", "Alpha"},
		{"bb", "Beta"},
	})
	out := buf.String()
	if !strings.Contains(out, "ID") || !strings.Contains(out, "NAME") {
		t.Fatalf("missing header: %q", out)
	}
	if !strings.Contains(out, "Alpha") || !strings.Contains(out, "Beta") {
		t.Fatalf("missing rows: %q", out)
	}
}

func TestDashOr(t *testing.T) {
	if dashOr("") != "-" {
		t.Fatalf("empty should render as dash")
	}
	if dashOr("x") != "x" {
		t.Fatalf("non-empty should pass through")
	}
}
```

- [ ] **Step 2: Run to verify it fails**

Run: `go test ./internal/cli/ -run 'TestEmitJSON_Indented|TestPrintTable|TestDashOr' -v`
Expected: FAIL — `undefined: emitJSON` / `printTable` (benchmark.go's `emitJSON` has no `io.Writer` param, so it won't match).

- [ ] **Step 3: Create `internal/cli/output.go`**

```go
package cli

import (
	"encoding/json"
	"fmt"
	"io"
	"strings"
	"text/tabwriter"
)

// jsonOut is bound to the global persistent --json flag. Commands check it to
// choose machine-readable JSON over human tables.
var jsonOut bool

// emitJSON writes v as indented JSON followed by a newline.
func emitJSON(w io.Writer, v any) error {
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	return enc.Encode(v)
}

// printTable writes a left-aligned, space-padded table.
func printTable(w io.Writer, headers []string, rows [][]string) {
	tw := tabwriter.NewWriter(w, 0, 2, 2, ' ', 0)
	fmt.Fprintln(tw, strings.Join(headers, "\t"))
	for _, r := range rows {
		fmt.Fprintln(tw, strings.Join(r, "\t"))
	}
	_ = tw.Flush()
}

// dashOr renders empty strings as "-" for table cells.
func dashOr(s string) string {
	if s == "" {
		return "-"
	}
	return s
}

// clip truncates s to max runes, appending an ellipsis when shortened.
func clip(s string, max int) string {
	if len(s) <= max {
		return s
	}
	if max <= 1 {
		return s[:max]
	}
	return s[:max-1] + "…"
}

// indent prefixes every line of s with pad; "(empty)" for the empty string.
func indent(s, pad string) string {
	if s == "" {
		return pad + "(empty)"
	}
	lines := strings.Split(s, "\n")
	for i, l := range lines {
		lines[i] = pad + l
	}
	return strings.Join(lines, "\n")
}
```

- [ ] **Step 4: Remove the duplicated helpers from `internal/cli/benchmark.go`**

Delete `benchmark.go`'s local `emitJSON`, `clip`, `dashOr`, and `indent` definitions (now in `output.go`). Update its `emitJSON(x)` call sites to `emitJSON(os.Stdout, x)`. Verify `os` is still imported (it is). Run `grep -n "emitJSON(" internal/cli/benchmark.go` and fix each call.

- [ ] **Step 5: Register the global `--json` flag in `internal/cli/root.go`**

In `root.go`'s `init()`, after the `--log-level` registration, add:

```go
	rootCmd.PersistentFlags().BoolVar(&jsonOut, "json", false,
		"emit machine-readable JSON instead of human-readable tables")
```

- [ ] **Step 6: Run tests + vet**

Run: `go test ./internal/cli/... && go vet ./internal/cli/...`
Expected: PASS, no vet errors. Confirm benchmark tests still pass (helpers relocated, not removed).

- [ ] **Step 7: Commit**

```bash
git add internal/cli/output.go internal/cli/output_test.go internal/cli/root.go internal/cli/benchmark.go
git commit -m "feat(cli): add shared output helpers and global --json flag"
```

---

## Task 2: Per-file advisory lock helper

**Files:**
- Create: `internal/cli/filelock.go`
- Create: `internal/cli/filelock_test.go`

- [ ] **Step 1: Write the failing test** — `internal/cli/filelock_test.go`

```go
package cli

import (
	"sync"
	"testing"
)

func TestWithProfileLock_Serializes(t *testing.T) {
	dir := t.TempDir()
	const id = "p1"
	var counter int
	var wg sync.WaitGroup
	for i := 0; i < 50; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_ = withProfileLock(dir, id, func() error {
				// Non-atomic read-modify-write; the lock must serialize it.
				c := counter
				c++
				counter = c
				return nil
			})
		}()
	}
	wg.Wait()
	if counter != 50 {
		t.Fatalf("lock failed to serialize: counter=%d want 50", counter)
	}
}

func TestWithProfileLock_PropagatesError(t *testing.T) {
	dir := t.TempDir()
	sentinel := errSentinel{}
	err := withProfileLock(dir, "p1", func() error { return sentinel })
	if err != sentinel {
		t.Fatalf("expected fn error to propagate, got %v", err)
	}
}

type errSentinel struct{}

func (errSentinel) Error() string { return "sentinel" }
```

- [ ] **Step 2: Run to verify it fails**

Run: `go test ./internal/cli/ -run TestWithProfileLock -v`
Expected: FAIL — `undefined: withProfileLock`.

- [ ] **Step 3: Create `internal/cli/filelock.go`**

```go
package cli

import (
	"os"
	"path/filepath"
	"syscall"
)

// withProfileLock serializes a read-modify-write on a single profile across
// processes via an advisory flock on a sibling ".<id>.lock" file in the
// profiles directory. It is CLI-local: the TUI does not take this lock, so
// CLI-vs-TUI safety still relies on the store's atomic writes and the TUI's
// reload. This closes the CLI-vs-CLI race on the same profile.
//
// Lock-file creation or flock failures are non-fatal: the mutation proceeds
// unserialized rather than being blocked by an environment quirk.
func withProfileLock(profilesDir, id string, fn func() error) error {
	if profilesDir == "" || id == "" {
		return fn()
	}
	lockPath := filepath.Join(profilesDir, "."+id+".lock")
	f, err := os.OpenFile(lockPath, os.O_CREATE|os.O_RDWR, 0o644)
	if err != nil {
		return fn()
	}
	defer f.Close()
	if err := syscall.Flock(int(f.Fd()), syscall.LOCK_EX); err != nil {
		return fn()
	}
	defer func() { _ = syscall.Flock(int(f.Fd()), syscall.LOCK_UN) }()
	return fn()
}
```

- [ ] **Step 4: Run to verify it passes**

Run: `go test ./internal/cli/ -run TestWithProfileLock -v`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/cli/filelock.go internal/cli/filelock_test.go
git commit -m "feat(cli): add per-file advisory lock helper for profile mutations"
```

---

## Task 3: `profile` parent + `profile list` + `profile show` + ref resolution

**Files:**
- Create: `internal/cli/profile.go`
- Create: `internal/cli/profile_test.go`

- [ ] **Step 1: Write the failing test** — `internal/cli/profile_test.go`

```go
package cli

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"

	"github.com/quantmind-br/model-loader/internal/domain"
	"github.com/quantmind-br/model-loader/internal/service/profilestore"
)

// newTempStore returns an FSStore rooted at a temp dir. Shared across profile tests.
func newTempStore(t *testing.T) *profilestore.FSStore {
	t.Helper()
	s, err := profilestore.NewFSStore(t.TempDir())
	if err != nil {
		t.Fatalf("NewFSStore: %v", err)
	}
	return s
}

func seed(t *testing.T, s profilestore.Store, id, name string) domain.Profile {
	t.Helper()
	p := domain.Profile{ID: id, Name: name, Model: "m.gguf", Args: map[string]any{}}
	if err := s.Create(p); err != nil {
		t.Fatalf("seed %s: %v", id, err)
	}
	got, _ := s.Get(id)
	return got
}

func TestListProfiles_Table(t *testing.T) {
	s := newTempStore(t)
	seed(t, s, "alpha", "Alpha")
	seed(t, s, "beta", "Beta")
	var buf bytes.Buffer
	if err := listProfiles(&buf, s, false); err != nil {
		t.Fatalf("listProfiles: %v", err)
	}
	out := buf.String()
	if !strings.Contains(out, "alpha") || !strings.Contains(out, "beta") {
		t.Fatalf("table missing ids: %q", out)
	}
}

func TestListProfiles_JSON(t *testing.T) {
	s := newTempStore(t)
	seed(t, s, "alpha", "Alpha")
	var buf bytes.Buffer
	if err := listProfiles(&buf, s, true); err != nil {
		t.Fatalf("listProfiles json: %v", err)
	}
	var got []domain.Profile
	if err := json.Unmarshal(buf.Bytes(), &got); err != nil {
		t.Fatalf("not JSON array: %v (%q)", err, buf.String())
	}
	if len(got) != 1 || got[0].ID != "alpha" {
		t.Fatalf("unexpected json: %+v", got)
	}
}

func TestResolveProfileRef_ByIDAndName(t *testing.T) {
	s := newTempStore(t)
	seed(t, s, "alpha", "Alpha One")
	// by id
	if p, err := resolveProfileRef(s, "alpha"); err != nil || p.ID != "alpha" {
		t.Fatalf("by id: %+v %v", p, err)
	}
	// by exact name (case-insensitive)
	if p, err := resolveProfileRef(s, "alpha one"); err != nil || p.ID != "alpha" {
		t.Fatalf("by name: %+v %v", p, err)
	}
	// not found
	if _, err := resolveProfileRef(s, "nope"); err == nil {
		t.Fatalf("expected not-found error")
	}
}

func TestShowProfile_JSON(t *testing.T) {
	s := newTempStore(t)
	seed(t, s, "alpha", "Alpha")
	var buf bytes.Buffer
	if err := showProfile(&buf, s, "alpha", true); err != nil {
		t.Fatalf("showProfile: %v", err)
	}
	var got domain.Profile
	if err := json.Unmarshal(buf.Bytes(), &got); err != nil {
		t.Fatalf("not JSON object: %v", err)
	}
	if got.ID != "alpha" {
		t.Fatalf("wrong profile: %+v", got)
	}
}
```

- [ ] **Step 2: Run to verify it fails**

Run: `go test ./internal/cli/ -run 'TestListProfiles|TestResolveProfileRef|TestShowProfile' -v`
Expected: FAIL — `undefined: listProfiles`, `resolveProfileRef`, `showProfile`.

- [ ] **Step 3: Create `internal/cli/profile.go`**

```go
package cli

import (
	"errors"
	"fmt"
	"io"
	"os"
	"sort"
	"strings"

	"github.com/quantmind-br/model-loader/internal/app"
	"github.com/quantmind-br/model-loader/internal/domain"
	"github.com/quantmind-br/model-loader/internal/service/profilestore"
	"github.com/spf13/cobra"
)

var profileCmd = &cobra.Command{
	Use:   "profile",
	Short: "Manage llama.cpp profiles",
}

func init() {
	rootCmd.AddCommand(profileCmd)

	profileCmd.AddCommand(&cobra.Command{
		Use:   "list",
		Short: "List profiles",
		RunE: func(cmd *cobra.Command, args []string) error {
			svc, err := app.Bootstrap(logLevel)
			if err != nil {
				return &ExitError{Code: 1}
			}
			defer svc.Close()
			if err := listProfiles(cmd.OutOrStdout(), svc.Store, jsonOut); err != nil {
				fmt.Fprintln(cmd.ErrOrStderr(), err)
				return &ExitError{Code: 1}
			}
			return nil
		},
	})

	profileCmd.AddCommand(&cobra.Command{
		Use:   "show <id|name>",
		Short: "Print a profile's canonical JSON / details",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			svc, err := app.Bootstrap(logLevel)
			if err != nil {
				return &ExitError{Code: 1}
			}
			defer svc.Close()
			if err := showProfile(cmd.OutOrStdout(), svc.Store, args[0], jsonOut); err != nil {
				fmt.Fprintln(cmd.ErrOrStderr(), err)
				return &ExitError{Code: 1}
			}
			return nil
		},
	})
}

// resolveProfileRef looks up a profile by id, then falls back to a
// case-insensitive exact Name match. Ambiguous name matches are an error.
func resolveProfileRef(store profilestore.Store, ref string) (domain.Profile, error) {
	if p, err := store.Get(ref); err == nil {
		return p, nil
	} else if !errors.Is(err, profilestore.ErrNotFound) {
		return domain.Profile{}, err
	}
	all, err := store.List()
	if err != nil {
		return domain.Profile{}, err
	}
	var matches []domain.Profile
	for _, p := range all {
		if strings.EqualFold(p.Name, ref) {
			matches = append(matches, p)
		}
	}
	switch len(matches) {
	case 1:
		return matches[0], nil
	case 0:
		return domain.Profile{}, fmt.Errorf("profile not found: %s", ref)
	default:
		return domain.Profile{}, fmt.Errorf("ambiguous profile name %q matches %d profiles; use the id", ref, len(matches))
	}
}

func listProfiles(w io.Writer, store profilestore.Store, asJSON bool) error {
	profiles, err := store.List()
	if err != nil {
		return err
	}
	sort.Slice(profiles, func(i, j int) bool { return profiles[i].ID < profiles[j].ID })
	if asJSON {
		if profiles == nil {
			profiles = []domain.Profile{}
		}
		return emitJSON(w, profiles)
	}
	rows := make([][]string, 0, len(profiles))
	for _, p := range profiles {
		pin := ""
		if p.Pinned {
			pin = "★"
		}
		rows = append(rows, []string{p.ID, clip(p.Name, 32), clip(p.Model, 40), dashOr(p.Launch.BackendID), pin})
	}
	printTable(w, []string{"ID", "NAME", "MODEL", "BACKEND", "PIN"}, rows)
	if len(rows) == 0 {
		fmt.Fprintln(w, "(no profiles)")
	}
	return nil
}

func showProfile(w io.Writer, store profilestore.Store, ref string, asJSON bool) error {
	p, err := resolveProfileRef(store, ref)
	if err != nil {
		return err
	}
	if asJSON {
		return emitJSON(w, p)
	}
	fmt.Fprintf(w, "ID:          %s\n", p.ID)
	fmt.Fprintf(w, "Name:        %s\n", p.Name)
	fmt.Fprintf(w, "Model:       %s\n", dashOr(p.Model))
	fmt.Fprintf(w, "Backend:     %s\n", dashOr(p.Launch.BackendID))
	fmt.Fprintf(w, "Pinned:      %t\n", p.Pinned)
	if p.Description != "" {
		fmt.Fprintf(w, "Description: %s\n", p.Description)
	}
	fmt.Fprintln(w, "Args:")
	keys := make([]string, 0, len(p.Args))
	for k := range p.Args {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		fmt.Fprintf(w, "  %s = %v\n", k, p.Args[k])
	}
	if len(p.ExtraArgs) > 0 {
		fmt.Fprintf(w, "ExtraArgs:   %s\n", strings.Join(p.ExtraArgs, " "))
	}
	return nil
}

// ensure os import is used even if a future edit drops the only reference.
var _ = os.Stdout
```

> Note: remove the `var _ = os.Stdout` line and the `os` import if `os` ends up unused after implementation; it is only a guard against an import-cycle false start.

- [ ] **Step 4: Run to verify it passes**

Run: `go test ./internal/cli/ -run 'TestListProfiles|TestResolveProfileRef|TestShowProfile' -v`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/cli/profile.go internal/cli/profile_test.go
git commit -m "feat(cli): add profile list/show commands and ref resolution"
```

---

## Task 4: `profile create` + `profile edit`

**Files:**
- Create: `internal/cli/profile_edit.go`
- Create: `internal/cli/profile_edit_test.go`

The pure, tested core is `assembleProfile` (merge flags over a base profile) and `coerceArgs` (string→typed by schema). `RunE` wires bootstrap + resolver + validator + store, and wraps the edit's read-modify-write in `withProfileLock`.

- [ ] **Step 1: Write the failing test** — `internal/cli/profile_edit_test.go`

```go
package cli

import (
	"testing"

	"github.com/quantmind-br/model-loader/internal/domain"
)

func testSchema() domain.FlagSchema {
	return domain.NewFlagSchema([]domain.FlagSpec{
		{Long: "port", Type: domain.FlagTypeInt},
		{Long: "temp", Type: domain.FlagTypeFloat},
		{Long: "flash-attn", Type: domain.FlagTypeBool},
		{Long: "alias", Type: domain.FlagTypeString},
	})
}

func TestCoerceArgs_TypesBySchema(t *testing.T) {
	got := coerceArgs(map[string]string{
		"port":       "8080",
		"temp":       "0.7",
		"flash-attn": "true",
		"alias":      "my-model",
		"unknown":    "raw",
	}, testSchema())

	if got["port"] != 8080 {
		t.Errorf("port = %v (%T), want int 8080", got["port"], got["port"])
	}
	if got["temp"] != 0.7 {
		t.Errorf("temp = %v, want 0.7", got["temp"])
	}
	if got["flash-attn"] != true {
		t.Errorf("flash-attn = %v, want true", got["flash-attn"])
	}
	if got["alias"] != "my-model" {
		t.Errorf("alias = %v", got["alias"])
	}
	if got["unknown"] != "raw" {
		t.Errorf("unknown flag should stay string: %v", got["unknown"])
	}
}

func TestAssembleProfile_FlagsOverrideBase(t *testing.T) {
	base := domain.Profile{
		ID:    "p1",
		Name:  "Old",
		Model: "old.gguf",
		Args:  map[string]any{"port": 1, "ctx-size": 2048},
		Launch: domain.LaunchConfig{BackendID: "llama"},
	}
	in := profileInput{
		name:      "New",
		model:     "new.gguf",
		backend:   "vllm",
		args:      map[string]string{"port": "8080"},
		extraArgs: []string{"--verbose"},
		env:       []domain.EnvVar{{Key: "CUDA_VISIBLE_DEVICES", Value: "0"}},
		setName:   true, setModel: true, setBackend: true,
	}
	out := assembleProfile(base, in, testSchema())

	if out.ID != "p1" {
		t.Errorf("id must be preserved: %s", out.ID)
	}
	if out.Name != "New" || out.Model != "new.gguf" || out.Launch.BackendID != "vllm" {
		t.Errorf("scalar overrides failed: %+v", out)
	}
	if out.Args["port"] != 8080 {
		t.Errorf("arg override/coerce failed: %v", out.Args["port"])
	}
	if out.Args["ctx-size"] != 2048 {
		t.Errorf("untouched base arg lost: %v", out.Args["ctx-size"])
	}
	if len(out.ExtraArgs) != 1 || out.ExtraArgs[0] != "--verbose" {
		t.Errorf("extra args: %+v", out.ExtraArgs)
	}
	if len(out.Launch.Env) != 1 || out.Launch.Env[0].Key != "CUDA_VISIBLE_DEVICES" {
		t.Errorf("env: %+v", out.Launch.Env)
	}
}

func TestParseKVPairs(t *testing.T) {
	m, err := parseKVPairs([]string{"a=1", "b=two=2"})
	if err != nil {
		t.Fatalf("parseKVPairs: %v", err)
	}
	if m["a"] != "1" || m["b"] != "two=2" {
		t.Fatalf("bad parse: %+v", m)
	}
	if _, err := parseKVPairs([]string{"nokey"}); err == nil {
		t.Fatalf("expected error for missing '='")
	}
}
```

> Confirm `domain.NewFlagSchema` constructor signature before relying on it. If the constructor differs (e.g. `domain.BuildFlagSchema` or a struct literal with a `Lookup` method), adjust `testSchema()` to build a `domain.FlagSchema` whose `Lookup(domain.CanonicalFlag(k))` returns the seeded `FlagSpec`s. The production code in `coerceArgs` only needs `schema.Lookup`.

- [ ] **Step 2: Run to verify it fails**

Run: `go test ./internal/cli/ -run 'TestCoerceArgs|TestAssembleProfile|TestParseKVPairs' -v`
Expected: FAIL — undefined symbols.

- [ ] **Step 3: Create `internal/cli/profile_edit.go`**

```go
package cli

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/quantmind-br/model-loader/internal/app"
	"github.com/quantmind-br/model-loader/internal/domain"
	"github.com/quantmind-br/model-loader/internal/service/backendcatalog"
	"github.com/quantmind-br/model-loader/internal/service/profilestore"
	"github.com/quantmind-br/model-loader/internal/service/validator"
	"github.com/spf13/cobra"
)

// profileInput holds the parsed flag values for create/edit. The set* booleans
// distinguish "flag provided" from "zero value", so edit only overrides what
// the user passed.
type profileInput struct {
	id, name, model, backend, description string
	args                                  map[string]string
	extraArgs                             []string
	env                                   []domain.EnvVar
	setName, setModel, setBackend, setDesc bool
}

func init() {
	profileCmd.AddCommand(newProfileWriteCmd("create", "Create a new profile"))
	profileCmd.AddCommand(newProfileWriteCmd("edit", "Edit an existing profile"))
}

func newProfileWriteCmd(verb, short string) *cobra.Command {
	var (
		file       string
		name       string
		model      string
		backend    string
		desc       string
		idFlag     string
		argPairs   []string
		extraArgs  []string
		envPairs   []string
	)
	use := verb
	args := cobra.NoArgs
	if verb == "edit" {
		use = "edit <id|name>"
		args = cobra.ExactArgs(1)
	}
	cmd := &cobra.Command{
		Use:   use,
		Short: short,
		Args:  args,
		RunE: func(cmd *cobra.Command, posArgs []string) error {
			argMap, err := parseKVPairs(argPairs)
			if err != nil {
				fmt.Fprintln(cmd.ErrOrStderr(), err)
				return &ExitError{Code: 1}
			}
			envMap, err := parseKVPairs(envPairs)
			if err != nil {
				fmt.Fprintln(cmd.ErrOrStderr(), err)
				return &ExitError{Code: 1}
			}
			in := profileInput{
				id:         idFlag,
				name:       name,
				model:      model,
				backend:    backend,
				description: desc,
				args:       argMap,
				extraArgs:  extraArgs,
				env:        kvToEnv(envMap),
				setName:    cmd.Flags().Changed("name"),
				setModel:   cmd.Flags().Changed("model"),
				setBackend: cmd.Flags().Changed("backend"),
				setDesc:    cmd.Flags().Changed("description"),
			}

			svc, err := app.Bootstrap(logLevel)
			if err != nil {
				return &ExitError{Code: 1}
			}
			defer svc.Close()

			isEdit := verb == "edit"
			ref := ""
			if isEdit {
				ref = posArgs[0]
			}
			code := runProfileWrite(cmd.OutOrStdout(), cmd.ErrOrStderr(), profileWriteDeps{
				store:    svc.Store,
				resolver: svc.Resolver,
				val:      svc.Val,
				dir:      svc.Cfg.Paths.ProfilesDir,
			}, isEdit, ref, file, in)
			if code != 0 {
				return &ExitError{Code: code}
			}
			return nil
		},
	}
	cmd.Flags().StringVarP(&file, "file", "f", "", "read base profile JSON from a file, or '-' for stdin")
	cmd.Flags().StringVar(&name, "name", "", "profile display name")
	cmd.Flags().StringVar(&model, "model", "", "model path / repo")
	cmd.Flags().StringVar(&backend, "backend", "", "backend id (e.g. llama-server, vllm)")
	cmd.Flags().StringVar(&desc, "description", "", "profile description")
	cmd.Flags().StringArrayVar(&argPairs, "arg", nil, "backend arg as key=value (repeatable)")
	cmd.Flags().StringArrayVar(&extraArgs, "extra-arg", nil, "raw extra CLI arg (repeatable)")
	cmd.Flags().StringArrayVar(&envPairs, "env", nil, "launch env var as KEY=value (repeatable)")
	if verb == "create" {
		cmd.Flags().StringVar(&idFlag, "id", "", "explicit profile id (default: slug of name)")
	}
	return cmd
}

type profileWriteDeps struct {
	store    profilestore.Store
	resolver backendcatalog.Resolver
	val      validator.Validator
	dir      string
}

// runProfileWrite returns a process exit code (0 ok, 1 error, 2 validation).
func runProfileWrite(out, errw io.Writer, deps profileWriteDeps, isEdit bool, ref, file string, in profileInput) int {
	// 1. Establish the base profile.
	var base domain.Profile
	if isEdit {
		p, err := resolveProfileRef(deps.store, ref)
		if err != nil {
			fmt.Fprintln(errw, err)
			return 1
		}
		base = p
	}
	// 2. Overlay --file/stdin JSON onto the base.
	if file != "" {
		raw, err := readInput(file)
		if err != nil {
			fmt.Fprintf(errw, "read profile input: %v\n", err)
			return 1
		}
		if err := json.Unmarshal(raw, &base); err != nil {
			fmt.Fprintf(errw, "parse profile JSON: %v\n", err)
			return 1
		}
	}
	// 3. Compute id for create.
	if !isEdit {
		base.ID = in.id
		if base.ID == "" {
			nm := in.name
			if nm == "" {
				nm = base.Name
			}
			base.ID = domain.Slugify(nm)
		}
		if base.ID == "" {
			fmt.Fprintln(errw, "create: a profile needs --name or --id")
			return 1
		}
	}
	// 4. Resolve schema for arg coercion + validation.
	schema, kind := resolveSchema(deps.resolver, base)
	// 5. Apply flag overrides.
	final := assembleProfile(base, in, schema)
	// 6. Validate.
	report := deps.val.Validate(final, schema, kind)
	for _, w := range report.Warnings {
		fmt.Fprintf(errw, "warning: %s: %s\n", w.Field, w.Message)
	}
	if report.HasBlockingErrors() {
		for _, e := range report.Errors {
			fmt.Fprintf(errw, "error: %s: %s\n", e.Field, e.Message)
		}
		return 2
	}
	// 7. Persist (edit serializes the RMW; create is a fresh file).
	persist := func() error {
		if isEdit {
			return deps.store.Save(final)
		}
		return deps.store.Create(final)
	}
	if isEdit {
		if err := withProfileLock(deps.dir, final.ID, persist); err != nil {
			fmt.Fprintf(errw, "save profile: %v\n", err)
			return 1
		}
	} else if err := persist(); err != nil {
		if errors.Is(err, profilestore.ErrDuplicateID) {
			fmt.Fprintf(errw, "profile id already exists: %s\n", final.ID)
			return 1
		}
		fmt.Fprintf(errw, "create profile: %v\n", err)
		return 1
	}
	if jsonOut {
		_ = emitJSON(out, final)
	} else {
		verb := "created"
		if isEdit {
			verb = "updated"
		}
		fmt.Fprintf(out, "%s profile %s\n", verb, final.ID)
	}
	return 0
}

func resolveSchema(resolver backendcatalog.Resolver, p domain.Profile) (domain.FlagSchema, domain.BackendKind) {
	if resolver == nil {
		return domain.FlagSchema{}, ""
	}
	rb, err := resolver.Resolve(p)
	if err != nil {
		return domain.FlagSchema{}, ""
	}
	return rb.Schema.ToFlagSchema(), rb.Backend.Kind
}

// assembleProfile overlays the user's flag input onto base, coercing args by schema.
func assembleProfile(base domain.Profile, in profileInput, schema domain.FlagSchema) domain.Profile {
	if in.setName {
		base.Name = in.name
	}
	if in.setModel {
		base.Model = in.model
	}
	if in.setBackend {
		base.Launch.BackendID = in.backend
	}
	if in.setDesc {
		base.Description = in.description
	}
	if len(in.args) > 0 {
		if base.Args == nil {
			base.Args = map[string]any{}
		}
		for k, v := range coerceArgs(in.args, schema) {
			base.Args[k] = v
		}
	}
	if base.Args == nil {
		base.Args = map[string]any{}
	}
	if len(in.extraArgs) > 0 {
		base.ExtraArgs = in.extraArgs
	}
	if len(in.env) > 0 {
		base.Launch.Env = in.env
	}
	if base.Meta.CreatedAt.IsZero() {
		base.Meta.CreatedAt = time.Now().UTC()
	}
	base.Meta.UpdatedAt = time.Now().UTC()
	if base.SchemaVersion == 0 {
		base.SchemaVersion = domain.SchemaVersion
	}
	return base
}

// coerceArgs converts string flag values to typed values by schema FlagType,
// mirroring configweb's draft.coerceArgs but for CLI string inputs.
func coerceArgs(in map[string]string, schema domain.FlagSchema) map[string]any {
	out := map[string]any{}
	for k, v := range in {
		spec, ok := schema.Lookup(domain.CanonicalFlag(k))
		if !ok {
			out[k] = v
			continue
		}
		switch spec.Type {
		case domain.FlagTypeInt:
			if n, err := strconv.Atoi(v); err == nil {
				out[k] = n
				continue
			}
			out[k] = v
		case domain.FlagTypeFloat:
			if f, err := strconv.ParseFloat(v, 64); err == nil {
				out[k] = f
				continue
			}
			out[k] = v
		case domain.FlagTypeBool:
			out[k] = v == "on" || v == "true" || v == "1"
		default:
			out[k] = v
		}
	}
	return out
}

func parseKVPairs(pairs []string) (map[string]string, error) {
	out := map[string]string{}
	for _, p := range pairs {
		idx := strings.IndexByte(p, '=')
		if idx < 0 {
			return nil, fmt.Errorf("expected key=value, got %q", p)
		}
		out[p[:idx]] = p[idx+1:]
	}
	return out, nil
}

func kvToEnv(m map[string]string) []domain.EnvVar {
	if len(m) == 0 {
		return nil
	}
	out := make([]domain.EnvVar, 0, len(m))
	for k, v := range m {
		out = append(out, domain.EnvVar{Key: k, Value: v})
	}
	return out
}

// readInput reads from a file path, or stdin when path is "-".
func readInput(path string) ([]byte, error) {
	if path == "-" {
		return io.ReadAll(os.Stdin)
	}
	return os.ReadFile(path)
}
```

> Verify before coding: `domain.FlagSchema` has a `Lookup(string) (FlagSpec, bool)` method and `domain.CanonicalFlag(string) string` exists (both used by configweb's `coerceArgs` — confirmed in `internal/service/configweb/draft.go:63`). Verify `rb.Schema.ToFlagSchema()` returns `domain.FlagSchema` (used in `bootstrap.go:169`).

- [ ] **Step 4: Run to verify pure-function tests pass**

Run: `go test ./internal/cli/ -run 'TestCoerceArgs|TestAssembleProfile|TestParseKVPairs' -v`
Expected: PASS.

- [ ] **Step 5: Add an integration test for `runProfileWrite`** — append to `profile_edit_test.go`

```go
func TestRunProfileWrite_CreateAndEdit(t *testing.T) {
	s := newTempStore(t)
	dir := s.Dir() // see note: expose Dir() or use a known temp path
	var out, errw strings.Builder

	// create
	code := runProfileWrite(&out, &errw, profileWriteDeps{store: s, dir: dir}, false, "",
		"", profileInput{id: "p1", name: "P1", model: "m.gguf", setName: true, setModel: true})
	if code != 0 {
		t.Fatalf("create code=%d err=%q", code, errw.String())
	}
	got, err := s.Get("p1")
	if err != nil || got.Name != "P1" {
		t.Fatalf("created profile missing/wrong: %+v %v", got, err)
	}

	// edit (nil resolver/val → schema empty, validator must be non-nil)
	out.Reset()
	errw.Reset()
	code = runProfileWrite(&out, &errw, profileWriteDeps{store: s, val: noopValidator{}, dir: dir}, true, "p1",
		"", profileInput{name: "P1-renamed", setName: true})
	if code != 0 {
		t.Fatalf("edit code=%d err=%q", code, errw.String())
	}
	got, _ = s.Get("p1")
	if got.Name != "P1-renamed" {
		t.Fatalf("edit did not apply: %+v", got)
	}
}

type noopValidator struct{}

func (noopValidator) Validate(domain.Profile, domain.FlagSchema, domain.BackendKind) validator.Report {
	return validator.Report{}
}
```

> The create branch passes a nil validator; guard `runProfileWrite` so a nil `deps.val` skips validation (treat as no errors). Add at the validation step: `if deps.val != nil { report := ...; ... }`. Adjust the production code accordingly and re-run.
>
> `s.Dir()`: if `FSStore` exposes no `Dir()` accessor, instead capture the temp dir in the test (`dir := t.TempDir(); s, _ := profilestore.NewFSStore(dir)`) rather than calling `newTempStore`. Prefer that — do not add an accessor just for tests.

- [ ] **Step 6: Run full cli tests + vet**

Run: `go test ./internal/cli/... && go vet ./internal/cli/...`
Expected: PASS.

- [ ] **Step 7: Commit**

```bash
git add internal/cli/profile_edit.go internal/cli/profile_edit_test.go
git commit -m "feat(cli): add profile create/edit with flags + JSON stdin/file"
```

---

## Task 5: `profile delete` / `duplicate` / `rename` / `pin` / `unpin`

**Files:**
- Create: `internal/cli/profile_crud.go`
- Create: `internal/cli/profile_crud_test.go`

- [ ] **Step 1: Write the failing test** — `internal/cli/profile_crud_test.go`

```go
package cli

import (
	"strings"
	"testing"

	"github.com/quantmind-br/model-loader/internal/service/profilestore"
)

func TestDeleteProfile(t *testing.T) {
	s := newTempStore(t)
	seed(t, s, "p1", "P1")
	var out strings.Builder
	if err := deleteProfile(&out, s, "p1"); err != nil {
		t.Fatalf("delete: %v", err)
	}
	if _, err := s.Get("p1"); err == nil {
		t.Fatalf("profile still present")
	}
}

func TestDuplicateProfile_DefaultID(t *testing.T) {
	s := newTempStore(t)
	seed(t, s, "p1", "P1")
	var out strings.Builder
	if err := duplicateProfile(&out, s, "p1", ""); err != nil {
		t.Fatalf("duplicate: %v", err)
	}
	if _, err := s.Get("p1-copy"); err != nil {
		t.Fatalf("expected p1-copy: %v", err)
	}
}

func TestRenameProfile(t *testing.T) {
	dir := t.TempDir()
	s, _ := profilestore.NewFSStore(dir)
	seed(t, s, "p1", "Old Name")
	var out strings.Builder
	if err := renameProfile(&out, s, dir, "p1", "Brand New"); err != nil {
		t.Fatalf("rename: %v", err)
	}
	got, err := s.Get("p1")
	if err != nil || got.Name != "Brand New" {
		t.Fatalf("rename did not apply: %+v %v", got, err)
	}
}

func TestSetPinned(t *testing.T) {
	dir := t.TempDir()
	s, _ := profilestore.NewFSStore(dir)
	seed(t, s, "p1", "P1")
	var out strings.Builder
	if err := setPinned(&out, s, dir, "p1", true); err != nil {
		t.Fatalf("pin: %v", err)
	}
	if got, _ := s.Get("p1"); !got.Pinned {
		t.Fatalf("not pinned")
	}
	if err := setPinned(&out, s, dir, "p1", false); err != nil {
		t.Fatalf("unpin: %v", err)
	}
	if got, _ := s.Get("p1"); got.Pinned {
		t.Fatalf("still pinned")
	}
}
```

- [ ] **Step 2: Run to verify it fails**

Run: `go test ./internal/cli/ -run 'TestDeleteProfile|TestDuplicateProfile|TestRenameProfile|TestSetPinned' -v`
Expected: FAIL — undefined symbols.

- [ ] **Step 3: Create `internal/cli/profile_crud.go`**

`rename` keeps the same id and only changes `Name` (the spec is `profile rename <id> <newname>`). Use `store.Save` under the lock — no id change means `store.Rename` is unnecessary.

```go
package cli

import (
	"fmt"
	"io"

	"github.com/quantmind-br/model-loader/internal/app"
	"github.com/quantmind-br/model-loader/internal/service/profilestore"
	"github.com/spf13/cobra"
)

func init() {
	profileCmd.AddCommand(&cobra.Command{
		Use:   "delete <id|name>",
		Short: "Delete a profile",
		Args:  cobra.ExactArgs(1),
		RunE:  profileMutationRunE(func(out io.Writer, s profilestore.Store, dir string, args []string) error {
			return deleteProfile(out, s, args[0])
		}),
	})
	profileCmd.AddCommand(&cobra.Command{
		Use:   "duplicate <id|name> [newid]",
		Short: "Duplicate a profile",
		Args:  cobra.RangeArgs(1, 2),
		RunE: profileMutationRunE(func(out io.Writer, s profilestore.Store, dir string, args []string) error {
			newID := ""
			if len(args) == 2 {
				newID = args[1]
			}
			return duplicateProfile(out, s, args[0], newID)
		}),
	})
	profileCmd.AddCommand(&cobra.Command{
		Use:   "rename <id|name> <newname>",
		Short: "Rename a profile's display name",
		Args:  cobra.ExactArgs(2),
		RunE: profileMutationRunE(func(out io.Writer, s profilestore.Store, dir string, args []string) error {
			return renameProfile(out, s, dir, args[0], args[1])
		}),
	})
	profileCmd.AddCommand(&cobra.Command{
		Use:   "pin <id|name>",
		Short: "Pin a profile",
		Args:  cobra.ExactArgs(1),
		RunE: profileMutationRunE(func(out io.Writer, s profilestore.Store, dir string, args []string) error {
			return setPinned(out, s, dir, args[0], true)
		}),
	})
	profileCmd.AddCommand(&cobra.Command{
		Use:   "unpin <id|name>",
		Short: "Unpin a profile",
		Args:  cobra.ExactArgs(1),
		RunE: profileMutationRunE(func(out io.Writer, s profilestore.Store, dir string, args []string) error {
			return setPinned(out, s, dir, args[0], false)
		}),
	})
}

// profileMutationRunE wires Bootstrap + error→ExitError for a mutation closure.
func profileMutationRunE(fn func(out io.Writer, s profilestore.Store, dir string, args []string) error) func(*cobra.Command, []string) error {
	return func(cmd *cobra.Command, args []string) error {
		svc, err := app.Bootstrap(logLevel)
		if err != nil {
			return &ExitError{Code: 1}
		}
		defer svc.Close()
		if err := fn(cmd.OutOrStdout(), svc.Store, svc.Cfg.Paths.ProfilesDir, args); err != nil {
			fmt.Fprintln(cmd.ErrOrStderr(), err)
			return &ExitError{Code: 1}
		}
		return nil
	}
}

func deleteProfile(out io.Writer, s profilestore.Store, ref string) error {
	p, err := resolveProfileRef(s, ref)
	if err != nil {
		return err
	}
	if err := s.Delete(p.ID); err != nil {
		return err
	}
	fmt.Fprintf(out, "deleted profile %s\n", p.ID)
	return nil
}

func duplicateProfile(out io.Writer, s profilestore.Store, ref, newID string) error {
	p, err := resolveProfileRef(s, ref)
	if err != nil {
		return err
	}
	if newID == "" {
		newID = p.ID + "-copy"
	}
	dup, err := s.Duplicate(p.ID, newID)
	if err != nil {
		return err
	}
	fmt.Fprintf(out, "duplicated %s as %s\n", p.ID, dup.ID)
	return nil
}

func renameProfile(out io.Writer, s profilestore.Store, dir, ref, newName string) error {
	p, err := resolveProfileRef(s, ref)
	if err != nil {
		return err
	}
	err = withProfileLock(dir, p.ID, func() error {
		p.Name = newName
		return s.Save(p)
	})
	if err != nil {
		return err
	}
	fmt.Fprintf(out, "renamed %s to %q\n", p.ID, newName)
	return nil
}

func setPinned(out io.Writer, s profilestore.Store, dir, ref string, pinned bool) error {
	p, err := resolveProfileRef(s, ref)
	if err != nil {
		return err
	}
	err = withProfileLock(dir, p.ID, func() error {
		p.Pinned = pinned
		return s.Save(p)
	})
	if err != nil {
		return err
	}
	verb := "pinned"
	if !pinned {
		verb = "unpinned"
	}
	fmt.Fprintf(out, "%s %s\n", verb, p.ID)
	return nil
}
```

- [ ] **Step 4: Run to verify it passes**

Run: `go test ./internal/cli/ -run 'TestDeleteProfile|TestDuplicateProfile|TestRenameProfile|TestSetPinned' -v`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/cli/profile_crud.go internal/cli/profile_crud_test.go
git commit -m "feat(cli): add profile delete/duplicate/rename/pin/unpin"
```

---

## Task 6: `profile export` / `profile import` / `profile validate`

**Files:**
- Create: `internal/cli/profile_bundle.go`
- Create: `internal/cli/profile_bundle_test.go`
- Modify: `internal/cli/importcmd.go` (mark the top-level `import` as a deprecated alias that delegates to the same logic)

`profile export [ids...] -o file` writes a `profilestore.ExportBundle` (filtered to `ids`, or all). `-o` omitted or `-` → stdout. `profile import <file> --mode` reuses `profilestore.ImportBundle`. `profile validate <id>` runs the validator and exits 2 on blocking errors.

- [ ] **Step 1: Write the failing test** — `internal/cli/profile_bundle_test.go`

```go
package cli

import (
	"bytes"
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"

	"github.com/quantmind-br/model-loader/internal/service/profilestore"
)

func TestExportProfiles_Subset(t *testing.T) {
	s := newTempStore(t)
	seed(t, s, "a", "A")
	seed(t, s, "b", "B")
	var buf bytes.Buffer
	if err := exportProfiles(&buf, s, []string{"a"}, "-"); err != nil {
		t.Fatalf("export: %v", err)
	}
	var bundle profilestore.ExportBundle
	if err := json.Unmarshal(buf.Bytes(), &bundle); err != nil {
		t.Fatalf("bundle not JSON: %v", err)
	}
	if len(bundle.Profiles) != 1 || bundle.Profiles[0].ID != "a" {
		t.Fatalf("subset export wrong: %+v", bundle.Profiles)
	}
}

func TestExportImportRoundTrip(t *testing.T) {
	src := newTempStore(t)
	seed(t, src, "a", "A")
	dir := t.TempDir()
	path := filepath.Join(dir, "bundle.json")
	var buf bytes.Buffer
	if err := exportProfiles(&buf, src, nil, path); err != nil {
		t.Fatalf("export: %v", err)
	}
	dst := newTempStore(t)
	var out bytes.Buffer
	if err := importProfiles(&out, dst, path, profilestore.ConflictModeMerge); err != nil {
		t.Fatalf("import: %v", err)
	}
	if _, err := dst.Get("a"); err != nil {
		t.Fatalf("imported profile missing: %v", err)
	}
}

func TestValidateProfile_OK(t *testing.T) {
	s := newTempStore(t)
	seed(t, s, "a", "A")
	var out strings.Builder
	code := validateProfile(&out, &out, s, noopValidator{}, nil, "a")
	if code != 0 {
		t.Fatalf("expected exit 0, got %d", code)
	}
}
```

- [ ] **Step 2: Run to verify it fails**

Run: `go test ./internal/cli/ -run 'TestExportProfiles|TestExportImportRoundTrip|TestValidateProfile' -v`
Expected: FAIL — undefined symbols.

- [ ] **Step 3: Create `internal/cli/profile_bundle.go`**

```go
package cli

import (
	"fmt"
	"io"
	"os"
	"time"

	"github.com/quantmind-br/model-loader/internal/app"
	"github.com/quantmind-br/model-loader/internal/domain"
	"github.com/quantmind-br/model-loader/internal/service/backendcatalog"
	"github.com/quantmind-br/model-loader/internal/service/profilestore"
	"github.com/quantmind-br/model-loader/internal/service/validator"
	"github.com/spf13/cobra"
)

func init() {
	var out string
	exportCmd := &cobra.Command{
		Use:   "export [id...]",
		Short: "Export profiles as a JSON bundle (all profiles if none given)",
		RunE: func(cmd *cobra.Command, args []string) error {
			svc, err := app.Bootstrap(logLevel)
			if err != nil {
				return &ExitError{Code: 1}
			}
			defer svc.Close()
			dest := out
			if dest == "" {
				dest = "-"
			}
			if err := exportProfiles(cmd.OutOrStdout(), svc.Store, args, dest); err != nil {
				fmt.Fprintln(cmd.ErrOrStderr(), err)
				return &ExitError{Code: 1}
			}
			return nil
		},
	}
	exportCmd.Flags().StringVarP(&out, "output", "o", "", "write bundle to file (default: stdout)")
	profileCmd.AddCommand(exportCmd)

	var mode string
	importCmd := &cobra.Command{
		Use:   "import <path>",
		Short: "Import a profile bundle",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			svc, err := app.Bootstrap(logLevel)
			if err != nil {
				return &ExitError{Code: 1}
			}
			defer svc.Close()
			cm := profilestore.ConflictMode(mode)
			if err := importProfiles(cmd.OutOrStdout(), svc.Store, args[0], cm); err != nil {
				fmt.Fprintln(cmd.ErrOrStderr(), err)
				return &ExitError{Code: 1}
			}
			return nil
		},
	}
	importCmd.Flags().StringVar(&mode, "mode", "merge", "conflict resolution: merge|overwrite|rename")
	profileCmd.AddCommand(importCmd)

	profileCmd.AddCommand(&cobra.Command{
		Use:   "validate <id|name>",
		Short: "Validate a profile against its backend schema",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			svc, err := app.Bootstrap(logLevel)
			if err != nil {
				return &ExitError{Code: 1}
			}
			defer svc.Close()
			code := validateProfile(cmd.OutOrStdout(), cmd.ErrOrStderr(), svc.Store, svc.Val, svc.Resolver, args[0])
			if code != 0 {
				return &ExitError{Code: code}
			}
			return nil
		},
	})
}

func exportProfiles(w io.Writer, store profilestore.Store, ids []string, dest string) error {
	all, err := store.List()
	if err != nil {
		return err
	}
	var selected []domain.Profile
	if len(ids) == 0 {
		selected = all
	} else {
		want := map[string]bool{}
		for _, id := range ids {
			p, rerr := resolveProfileRef(store, id)
			if rerr != nil {
				return rerr
			}
			want[p.ID] = true
		}
		for _, p := range all {
			if want[p.ID] {
				selected = append(selected, p)
			}
		}
	}
	if selected == nil {
		selected = []domain.Profile{}
	}
	bundle := profilestore.ExportBundle{
		SchemaVersion: profilestore.ExportBundleSchemaVersion,
		ExportedAt:    time.Now().UTC(),
		Profiles:      selected,
	}
	if dest == "-" || dest == "" {
		return emitJSON(w, bundle)
	}
	f, err := os.Create(dest)
	if err != nil {
		return err
	}
	defer f.Close()
	if err := emitJSON(f, bundle); err != nil {
		return err
	}
	fmt.Fprintf(w, "exported %d profile(s) to %s\n", len(selected), dest)
	return nil
}

func importProfiles(w io.Writer, store profilestore.Store, path string, mode profilestore.ConflictMode) error {
	res, err := profilestore.ImportBundle(store, path, mode)
	if err != nil {
		return err
	}
	if jsonOut {
		return emitJSON(w, res)
	}
	fmt.Fprintf(w, "import result: added=%d skipped=%d renamed=%d replaced=%d\n",
		res.Added, res.Skipped, res.Renamed, res.Replaced)
	return nil
}

// validateProfile returns an exit code: 0 ok, 1 lookup error, 2 blocking errors.
func validateProfile(out, errw io.Writer, store profilestore.Store, val validator.Validator, resolver backendcatalog.Resolver, ref string) int {
	p, err := resolveProfileRef(store, ref)
	if err != nil {
		fmt.Fprintln(errw, err)
		return 1
	}
	schema, kind := resolveSchema(resolver, p)
	if val == nil {
		fmt.Fprintln(out, "ok (no validator)")
		return 0
	}
	report := val.Validate(p, schema, kind)
	for _, wm := range report.Warnings {
		fmt.Fprintf(errw, "warning: %s: %s\n", wm.Field, wm.Message)
	}
	if report.HasBlockingErrors() {
		for _, e := range report.Errors {
			fmt.Fprintf(errw, "error: %s: %s\n", e.Field, e.Message)
		}
		return 2
	}
	fmt.Fprintf(out, "ok: %s is valid\n", p.ID)
	return 0
}
```

> `resolveSchema` is defined in `profile_edit.go` (Task 4) — reuse it; do not redefine.

- [ ] **Step 4: Demote the top-level `import` to a deprecated alias** — Modify `internal/cli/importcmd.go`

Keep the existing top-level `import` command working (the spec keeps it as a deprecated alias of `profile import`). Add `Deprecated: "use \"model-loader profile import\" instead"` to its `cobra.Command` so cobra prints a deprecation notice but the command still runs. Do not delete it.

- [ ] **Step 5: Run to verify it passes + full suite**

Run: `go test ./internal/cli/... && go vet ./internal/cli/...`
Expected: PASS.

- [ ] **Step 6: Commit**

```bash
git add internal/cli/profile_bundle.go internal/cli/profile_bundle_test.go internal/cli/importcmd.go
git commit -m "feat(cli): add profile export/import/validate; deprecate top-level import"
```

---

## Task 7: Build smoke + registration test

**Files:**
- Create: `internal/cli/profile_registration_test.go`

- [ ] **Step 1: Write the test**

```go
package cli

import "testing"

func TestProfileSubcommandsRegistered(t *testing.T) {
	for _, name := range []string{"list", "show", "create", "edit", "delete", "duplicate", "rename", "pin", "unpin", "export", "import", "validate"} {
		cmd, _, err := rootCmd.Find([]string{"profile", name})
		if err != nil || cmd == nil || cmd.Name() != name {
			t.Fatalf("profile %s not registered: cmd=%v err=%v", name, cmd, err)
		}
	}
}
```

- [ ] **Step 2: Run + build the binary**

Run: `go test ./internal/cli/... && go build ./... && make build`
Expected: PASS, binary builds.

- [ ] **Step 3: Manual smoke (no TUI running)**

```bash
./bin/model-loader profile list
./bin/model-loader profile create --name "Smoke Test" --model /tmp/x.gguf
./bin/model-loader profile list --json
./bin/model-loader profile show smoke-test
./bin/model-loader profile pin smoke-test
./bin/model-loader profile export smoke-test -o /tmp/b.json && cat /tmp/b.json
./bin/model-loader profile delete smoke-test
```
Expected: each prints sensible output; create→show→delete round-trips. (Note: this writes to the real profiles dir — delete the smoke profile at the end.)

- [ ] **Step 4: Commit**

```bash
git add internal/cli/profile_registration_test.go
git commit -m "test(cli): assert all profile subcommands are registered"
```

---

## Self-Review Notes

- **Spec coverage:** every row of the spec's `profile` table maps to a task (list→T3, show→T3, create→T4, edit→T4, delete/duplicate/rename/pin/unpin→T5, export/import/validate→T6). The deprecated top-level `import` alias is handled in T6 step 4.
- **Concurrency:** profile mutations that read-modify-write (edit, rename, pin/unpin) go through `withProfileLock` (T2); delete/duplicate are single store ops. Matches the approved "sibling .lock at CLI level" decision.
- **Validation gating:** create/edit/validate exit 2 on blocking errors, mirroring the benchmark exit-code contract via `ExitError`.
- **Type consistency:** `resolveSchema`, `coerceArgs`, `assembleProfile`, `resolveProfileRef`, `withProfileLock`, `emitJSON(w, …)` are each defined once and reused. `noopValidator` test double defined once in `profile_edit_test.go`.
- **Verify-before-coding flags** (re-check during implementation): `domain.NewFlagSchema` constructor name; `domain.FlagSchema.Lookup`; `domain.CanonicalFlag`; `rb.Schema.ToFlagSchema()` returns `domain.FlagSchema`; `profilestore.FSStore` temp-dir construction in tests (capture `t.TempDir()` rather than adding a `Dir()` accessor).
