# CLI Parity Phase 3 (`model`) Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Expose the TUI's ModelsPage functionality through the cobra CLI — scan local GGUF models, search HuggingFace, inspect repos, and start/list/cancel/resume downloads — scriptable with `--json` and reliable exit codes.

**Architecture:** Thin cobra `RunE`s build only the services they need (scanner / hub client / download manager) directly from `config.Load()` — these services are NOT on `app.Services` (they are instantiated locally in `runTUI`, so the CLI mirrors that wiring). Each `RunE` delegates to a small **pure function** taking a narrow interface + `io.Writer`, returning a raw `error` that `exitOnErr` maps to `*ExitError{Code:1}`. Downloads are file-based with detached workers (the `download` worker subcommand already exists), so download commands need NO single-instance lock and coexist with a running TUI.

**Tech Stack:** Go 1.26.2, spf13/cobra, text/tabwriter. Services: `modelscanner`, `hfhub`, `downloadmgr`.

---

## Verify Before Coding (read this first)

The signatures below were captured verbatim on 2026-05-22. Before writing each task, open the cited files and confirm they still match — if a signature drifted, adapt the task and note it. Do NOT invent APIs.

**`internal/config/config.go`**
- `func Load() (AppConfig, error)` (value, not pointer) — line 93
- `AppConfig.Models ModelsConfig` → `ModelsConfig.SearchPaths []string` (mapstructure `search_paths`)
- `AppConfig.Paths PathsConfig` → `PathsConfig.StateDir string`

**`internal/service/modelscanner/`**
- `func New() Scanner`
- `type Scanner interface { Scan(ctx context.Context, paths []string) (<-chan domain.ScanEvent, error) }`

**`internal/domain/model.go`**
```go
type ModelFile struct {
	Path         string
	SizeBytes    int64
	Name         string
	Quant        string
	Params       string
	Architecture string
	BlockCount   uint64
}
type ScanEventType int
const (
	ScanEventFile ScanEventType = iota
	ScanEventProgress
	ScanEventError
	ScanEventDone
)
type ScanEvent struct {
	Type  ScanEventType
	Root  string
	File  *ModelFile
	Count int
	Error error
}
```

**`internal/service/hfhub/`** (client.go / types.go)
- `func NewClient(httpClient *http.Client, userAgent string) *Client`
- `func (c *Client) Search(ctx context.Context, query string, limit int) ([]SearchResult, error)`
- `func (c *Client) RepoInfo(ctx context.Context, repoID string) (*RepoInfo, error)`
- `func (c *Client) DownloadURL(repoID, filename string) string`
```go
type SearchResult struct {
	ID, Author, ModelID string
	Tags                []string
	Downloads, Likes    int
	LastModified        time.Time
	LibraryName, PipelineTag string
}
func (r SearchResult) HasGGUFTag() bool
type RepoInfo struct { ID string; Siblings []Sibling; Tags []string }
type Sibling struct { RFilename string; Size int64 }
```

**`internal/service/downloadmgr/`** (manager.go / types.go / pathing.go)
- `func NewManager(stateDir string, maxConcurrent int) *Manager`
- `func (m *Manager) WithUserAgent(ua string) *Manager`
- `func (m *Manager) Reconcile() error`
- `func (m *Manager) Start(spec Spec) (ID, error)`
- `func (m *Manager) Cancel(id ID) error`
- `func (m *Manager) Resume(id ID) error`
- `func (m *Manager) Snapshot() []State`
- `func (m *Manager) Subscribe() <-chan Event`
- `func (m *Manager) StartPolling()`
- `func (m *Manager) Close() error`
- `func ResolveDest(searchPath, repoID, rfilename string, isSnapshot bool) (destDir, destFile string, err error)`
- `var ErrNotResumable`, `ErrNoSearchPath`, `ErrSearchPathMissing`, `ErrAlreadyExists`, `ErrPathTraversal`
```go
type ID string
type Spec struct { RepoID, Filename, URL, DestDir, DestFile string; IsSnapshot bool; Logger *slog.Logger }
type Status int // StatusQueued/Active/Completed/Failed/Cancelled/Abandoned
func (s Status) String() string      // "queued","active","completed","failed","cancelled","abandoned"
func (s Status) IsTerminal() bool
type State struct { ID ID; Spec Spec; Status Status; Bytes, Total int64; Err error; StartedAt time.Time }
type Event struct { ID ID; State State }
```

**Established CLI conventions (phases 1–2), match exactly:**
- `internal/cli/root.go`: `type ExitError struct{ Code int }`; package vars `logLevel string`, `jsonOut bool`.
- `internal/cli/output.go`: `emitJSON(w io.Writer, v any) error`, `printTable(w io.Writer, headers []string, rows [][]string)`, `dashOr(s string) string`, `clip(s string, max int) string`.
- Parent command pattern: package-level `var xCmd = &cobra.Command{...}`; each file's `init()` adds subcommands; the parent file's `init()` ends with `rootCmd.AddCommand(xCmd)`.
- Module path: `github.com/quantmind-br/model-loader`.

**HYGIENE:** The working tree has unrelated uncommitted `configweb` changes. Use targeted `git add <files>` ONLY — NEVER `git add -A`/`.`/`-a`. Work on `main` in place.

**LANGUAGE:** All user-facing strings (Short/Long help, table headers, messages) MUST be English.

---

## File Structure

| File | Responsibility |
|------|----------------|
| `internal/cli/model.go` | `modelCmd` parent, `userAgent` const, service builders, narrow interfaces, `exitOnErr`/`runWithManager`/`firstPath` helpers |
| `internal/cli/output.go` | (modify) add `humanBytes(int64) string` |
| `internal/cli/model_scan.go` | `model list` (local GGUF scan) + `listLocalModels` |
| `internal/cli/model_hub.go` | `model search` + `model info` + pure funcs |
| `internal/cli/model_download.go` | `model download` (start), `model downloads` (list/cancel/resume) + pure funcs |
| `internal/cli/model_test.go` | parent-registration test + `humanBytes` test |
| `internal/cli/model_scan_test.go` | `fakeScanner` + `listLocalModels` tests |
| `internal/cli/model_hub_test.go` | `fakeHub` + search/info tests |
| `internal/cli/model_download_test.go` | `fakeDLManager` + download tests + full command-tree assertion |

---

## Task 1: Scaffolding (parent command, builders, helpers, humanBytes)

**Files:**
- Create: `internal/cli/model.go`
- Modify: `internal/cli/output.go` (append `humanBytes`)
- Create: `internal/cli/model_test.go`

- [ ] **Step 1: Write the failing test**

Create `internal/cli/model_test.go`:

```go
package cli

import (
	"testing"

	"github.com/spf13/cobra"
)

// childByName returns the immediate subcommand of parent whose first Use word
// matches name, or nil.
func childByName(parent *cobra.Command, name string) *cobra.Command {
	for _, c := range parent.Commands() {
		if c.Name() == name {
			return c
		}
	}
	return nil
}

func TestModelCmd_RegisteredUnderRoot(t *testing.T) {
	if childByName(rootCmd, "model") == nil {
		t.Fatal("model command not registered under root")
	}
}

func TestHumanBytes(t *testing.T) {
	cases := []struct {
		in   int64
		want string
	}{
		{0, "0B"},
		{512, "512B"},
		{1024, "1.0KB"},
		{1536, "1.5KB"},
		{1048576, "1.0MB"},
		{-1, "?"},
	}
	for _, c := range cases {
		if got := humanBytes(c.in); got != c.want {
			t.Errorf("humanBytes(%d) = %q, want %q", c.in, got, c.want)
		}
	}
}
```

- [ ] **Step 2: Run the test to verify it fails**

Run: `go test ./internal/cli/ -run 'TestModelCmd_RegisteredUnderRoot|TestHumanBytes' -v`
Expected: compile failure / FAIL — `modelCmd` and `humanBytes` undefined.

- [ ] **Step 3: Add `humanBytes` to output.go**

Append to `internal/cli/output.go` (keep existing imports; `fmt` is already imported):

```go
// humanBytes renders a byte count in IEC-ish units (1024-based). Negative
// counts (unknown totals) render as "?".
func humanBytes(n int64) string {
	if n < 0 {
		return "?"
	}
	const unit = 1024
	if n < unit {
		return fmt.Sprintf("%dB", n)
	}
	div, exp := int64(unit), 0
	for x := n / unit; x >= unit; x /= unit {
		div *= unit
		exp++
	}
	return fmt.Sprintf("%.1f%cB", float64(n)/float64(div), "KMGTPE"[exp])
}
```

- [ ] **Step 4: Create model.go (parent + builders + helpers + interfaces)**

Create `internal/cli/model.go`:

```go
package cli

import (
	"fmt"
	"io"
	"net/http"
	"path/filepath"
	"time"

	"github.com/quantmind-br/model-loader/internal/config"
	"github.com/quantmind-br/model-loader/internal/service/downloadmgr"
	"github.com/quantmind-br/model-loader/internal/service/hfhub"
	"github.com/quantmind-br/model-loader/internal/service/modelscanner"
	"github.com/spf13/cobra"
)

// userAgent is sent on HuggingFace Hub requests and to download workers,
// mirroring the value used by the TUI build in cmd/model-loader/main.go.
const userAgent = "model-loader/dev"

var modelCmd = &cobra.Command{
	Use:   "model",
	Short: "Scan local models, search HuggingFace, and manage downloads",
}

func init() {
	rootCmd.AddCommand(modelCmd)
}

// hubClient is the narrow slice of *hfhub.Client the CLI needs, so commands can
// be unit-tested against a fake.
type hubClient interface {
	Search(ctx context.Context, query string, limit int) ([]hfhub.SearchResult, error)
	RepoInfo(ctx context.Context, repoID string) (*hfhub.RepoInfo, error)
	DownloadURL(repoID, filename string) string
}

// downloadManager is the narrow slice of *downloadmgr.Manager the CLI needs.
type downloadManager interface {
	Start(spec downloadmgr.Spec) (downloadmgr.ID, error)
	Cancel(id downloadmgr.ID) error
	Resume(id downloadmgr.ID) error
	Snapshot() []downloadmgr.State
	Subscribe() <-chan downloadmgr.Event
	StartPolling()
}

func buildScanner() modelscanner.Scanner { return modelscanner.New() }

func buildHFClient() *hfhub.Client {
	return hfhub.NewClient(&http.Client{Timeout: 30 * time.Second}, userAgent)
}

// buildDownloadManager constructs a Manager rooted at <stateDir>/downloads and
// reconciles persisted state so Snapshot/Cancel/Resume see existing downloads.
func buildDownloadManager(cfg config.AppConfig) (*downloadmgr.Manager, error) {
	dir := filepath.Join(cfg.Paths.StateDir, "downloads")
	m := downloadmgr.NewManager(dir, 3).WithUserAgent(userAgent)
	if err := m.Reconcile(); err != nil {
		return nil, err
	}
	return m, nil
}

// firstPath returns the first configured search path, or "" when none exist.
func firstPath(paths []string) string {
	if len(paths) == 0 {
		return ""
	}
	return paths[0]
}

// exitOnErr prints err to errw and returns *ExitError{1}; nil returns nil.
func exitOnErr(errw io.Writer, err error) error {
	if err != nil {
		fmt.Fprintln(errw, err)
		return &ExitError{Code: 1}
	}
	return nil
}

// runWithManager loads config, builds+reconciles a download Manager, runs fn,
// and maps errors to *ExitError. Used by the downloads subcommands.
func runWithManager(cmd *cobra.Command, fn func(out io.Writer, mgr *downloadmgr.Manager) error) error {
	cfg, err := config.Load()
	if err != nil {
		fmt.Fprintf(cmd.ErrOrStderr(), "config error: %v\n", err)
		return &ExitError{Code: 1}
	}
	mgr, err := buildDownloadManager(cfg)
	if err != nil {
		fmt.Fprintf(cmd.ErrOrStderr(), "download manager: %v\n", err)
		return &ExitError{Code: 1}
	}
	defer mgr.Close()
	return exitOnErr(cmd.ErrOrStderr(), fn(cmd.OutOrStdout(), mgr))
}
```

Note: this file imports `context` indirectly via the interface signatures — add `"context"` to the import block. (The `hubClient` interface uses `context.Context`.)

The corrected import block for `model.go`:

```go
import (
	"context"
	"fmt"
	"io"
	"net/http"
	"path/filepath"
	"time"

	"github.com/quantmind-br/model-loader/internal/config"
	"github.com/quantmind-br/model-loader/internal/service/downloadmgr"
	"github.com/quantmind-br/model-loader/internal/service/hfhub"
	"github.com/quantmind-br/model-loader/internal/service/modelscanner"
	"github.com/spf13/cobra"
)
```

- [ ] **Step 5: Run the test to verify it passes**

Run: `go test ./internal/cli/ -run 'TestModelCmd_RegisteredUnderRoot|TestHumanBytes' -v`
Expected: PASS (both tests).

Also run `go build ./...` — Expected: clean build. (`buildScanner`/`buildHFClient`/`runWithManager` are package-level and may be unused until later tasks — that is legal in Go and will NOT fail the build.)

- [ ] **Step 6: Commit**

```bash
git add internal/cli/model.go internal/cli/output.go internal/cli/model_test.go
git commit -m "feat(cli): scaffold model command parent, service builders, humanBytes"
```

---

## Task 2: `model list` — scan local GGUF models

**Files:**
- Create: `internal/cli/model_scan.go`
- Create: `internal/cli/model_scan_test.go`

- [ ] **Step 1: Write the failing test**

Create `internal/cli/model_scan_test.go`:

```go
package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/quantmind-br/model-loader/internal/domain"
)

// fakeScanner emits a fixed event sequence on Scan.
type fakeScanner struct {
	events  []domain.ScanEvent
	scanErr error
}

func (f *fakeScanner) Scan(ctx context.Context, paths []string) (<-chan domain.ScanEvent, error) {
	if f.scanErr != nil {
		return nil, f.scanErr
	}
	ch := make(chan domain.ScanEvent, len(f.events))
	for _, e := range f.events {
		ch <- e
	}
	close(ch)
	return ch, nil
}

func twoModelScanner() *fakeScanner {
	return &fakeScanner{events: []domain.ScanEvent{
		{Type: domain.ScanEventFile, File: &domain.ModelFile{Path: "/m/b.gguf", Name: "b.gguf", SizeBytes: 2048, Quant: "Q4_K_M", Params: "7B", Architecture: "llama"}},
		{Type: domain.ScanEventFile, File: &domain.ModelFile{Path: "/m/a.gguf", Name: "a.gguf", SizeBytes: 1024, Quant: "Q8_0", Params: "3B", Architecture: "qwen"}},
		{Type: domain.ScanEventDone},
	}}
}

func TestListLocalModels_Table(t *testing.T) {
	var out, errw bytes.Buffer
	if err := listLocalModels(&out, &errw, twoModelScanner(), []string{"/m"}, false); err != nil {
		t.Fatalf("listLocalModels: %v", err)
	}
	s := out.String()
	if !strings.Contains(s, "a.gguf") || !strings.Contains(s, "b.gguf") {
		t.Fatalf("table missing models: %q", s)
	}
	// Sorted by Path: a.gguf must appear before b.gguf.
	if strings.Index(s, "a.gguf") > strings.Index(s, "b.gguf") {
		t.Fatalf("models not sorted by path: %q", s)
	}
}

func TestListLocalModels_JSON(t *testing.T) {
	var out, errw bytes.Buffer
	if err := listLocalModels(&out, &errw, twoModelScanner(), []string{"/m"}, true); err != nil {
		t.Fatalf("listLocalModels: %v", err)
	}
	var items []modelListItem
	if err := json.Unmarshal(out.Bytes(), &items); err != nil {
		t.Fatalf("unmarshal: %v\n%s", err, out.String())
	}
	if len(items) != 2 || items[0].Name != "a.gguf" {
		t.Fatalf("unexpected items: %+v", items)
	}
}

func TestListLocalModels_NoPaths(t *testing.T) {
	var out, errw bytes.Buffer
	if err := listLocalModels(&out, &errw, twoModelScanner(), nil, false); err == nil {
		t.Fatal("expected error when no search paths configured")
	}
}

func TestListLocalModels_ScanErrorEvent(t *testing.T) {
	sc := &fakeScanner{events: []domain.ScanEvent{
		{Type: domain.ScanEventError, Root: "/bad", Error: errTestScan},
		{Type: domain.ScanEventDone},
	}}
	var out, errw bytes.Buffer
	if err := listLocalModels(&out, &errw, sc, []string{"/bad"}, false); err != nil {
		t.Fatalf("scan error events must not fail the command: %v", err)
	}
	if !strings.Contains(errw.String(), "/bad") {
		t.Fatalf("expected per-root warning on stderr: %q", errw.String())
	}
}

var errTestScan = errors.New("permission denied")
```

Add the missing import to the test file: `"errors"`.

- [ ] **Step 2: Run the test to verify it fails**

Run: `go test ./internal/cli/ -run TestListLocalModels -v`
Expected: compile failure — `listLocalModels` and `modelListItem` undefined.

- [ ] **Step 3: Implement model_scan.go**

Create `internal/cli/model_scan.go`:

```go
package cli

import (
	"context"
	"errors"
	"fmt"
	"io"
	"sort"

	"github.com/quantmind-br/model-loader/internal/config"
	"github.com/quantmind-br/model-loader/internal/domain"
	"github.com/quantmind-br/model-loader/internal/service/modelscanner"
	"github.com/spf13/cobra"
)

// modelListItem is the JSON/table view of a discovered local model.
type modelListItem struct {
	Path         string `json:"path"`
	Name         string `json:"name"`
	SizeBytes    int64  `json:"sizeBytes"`
	Quant        string `json:"quant,omitempty"`
	Params       string `json:"params,omitempty"`
	Architecture string `json:"architecture,omitempty"`
}

func init() {
	var paths []string
	cmd := &cobra.Command{
		Use:   "list",
		Short: "List local GGUF models found under the configured search paths",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			cfg, err := config.Load()
			if err != nil {
				fmt.Fprintf(cmd.ErrOrStderr(), "config error: %v\n", err)
				return &ExitError{Code: 1}
			}
			search := cfg.Models.SearchPaths
			if len(paths) > 0 {
				search = paths
			}
			return exitOnErr(cmd.ErrOrStderr(),
				listLocalModels(cmd.OutOrStdout(), cmd.ErrOrStderr(), buildScanner(), search, jsonOut))
		},
	}
	cmd.Flags().StringArrayVar(&paths, "path", nil, "override search path(s) to scan (repeatable)")
	modelCmd.AddCommand(cmd)
}

// listLocalModels scans paths and prints the discovered models. Per-root scan
// errors are warned to errw but do not fail the command.
func listLocalModels(out, errw io.Writer, scanner modelscanner.Scanner, paths []string, asJSON bool) error {
	if len(paths) == 0 {
		return errors.New("no model search paths configured (set models.search_paths or pass --path)")
	}
	ch, err := scanner.Scan(context.Background(), paths)
	if err != nil {
		return fmt.Errorf("scan: %w", err)
	}
	var items []modelListItem
	for ev := range ch {
		switch ev.Type {
		case domain.ScanEventFile:
			if ev.File != nil {
				items = append(items, modelListItem{
					Path:         ev.File.Path,
					Name:         ev.File.Name,
					SizeBytes:    ev.File.SizeBytes,
					Quant:        ev.File.Quant,
					Params:       ev.File.Params,
					Architecture: ev.File.Architecture,
				})
			}
		case domain.ScanEventError:
			if ev.Error != nil {
				fmt.Fprintf(errw, "warning: scan %s: %v\n", ev.Root, ev.Error)
			}
		}
	}
	sort.Slice(items, func(i, j int) bool { return items[i].Path < items[j].Path })

	if asJSON {
		if items == nil {
			items = []modelListItem{}
		}
		return emitJSON(out, items)
	}
	if len(items) == 0 {
		fmt.Fprintln(out, "no models found")
		return nil
	}
	rows := make([][]string, 0, len(items))
	for _, m := range items {
		rows = append(rows, []string{
			dashOr(m.Name), dashOr(m.Params), dashOr(m.Quant), humanBytes(m.SizeBytes), m.Path,
		})
	}
	printTable(out, []string{"NAME", "PARAMS", "QUANT", "SIZE", "PATH"}, rows)
	return nil
}
```

- [ ] **Step 4: Run the test to verify it passes**

Run: `go test ./internal/cli/ -run TestListLocalModels -v`
Expected: PASS (all four).

- [ ] **Step 5: Commit**

```bash
git add internal/cli/model_scan.go internal/cli/model_scan_test.go
git commit -m "feat(cli): add 'model list' to scan local GGUF models"
```

---

## Task 3: `model search` + `model info` — HuggingFace Hub

**Files:**
- Create: `internal/cli/model_hub.go`
- Create: `internal/cli/model_hub_test.go`

- [ ] **Step 1: Write the failing test**

Create `internal/cli/model_hub_test.go`:

```go
package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/quantmind-br/model-loader/internal/service/hfhub"
)

// fakeHub implements hubClient with canned results.
type fakeHub struct {
	results   []hfhub.SearchResult
	repo      *hfhub.RepoInfo
	searchErr error
	repoErr   error
}

func (f *fakeHub) Search(ctx context.Context, query string, limit int) ([]hfhub.SearchResult, error) {
	return f.results, f.searchErr
}
func (f *fakeHub) RepoInfo(ctx context.Context, repoID string) (*hfhub.RepoInfo, error) {
	return f.repo, f.repoErr
}
func (f *fakeHub) DownloadURL(repoID, filename string) string {
	return "https://hf/" + repoID + "/" + filename
}

func TestSearchHub_Table(t *testing.T) {
	h := &fakeHub{results: []hfhub.SearchResult{
		{ID: "org/model-a", Downloads: 100, Likes: 5, Tags: []string{"gguf"}, PipelineTag: "text-generation", LastModified: time.Now()},
		{ID: "org/model-b", Downloads: 3, Likes: 0, Tags: []string{"safetensors"}},
	}}
	var out bytes.Buffer
	if err := searchHub(&out, h, "qwen", 20, false); err != nil {
		t.Fatalf("searchHub: %v", err)
	}
	s := out.String()
	if !strings.Contains(s, "org/model-a") || !strings.Contains(s, "yes") {
		t.Fatalf("table missing data / gguf flag: %q", s)
	}
}

func TestSearchHub_JSON(t *testing.T) {
	h := &fakeHub{results: []hfhub.SearchResult{{ID: "org/x", Tags: []string{"gguf"}}}}
	var out bytes.Buffer
	if err := searchHub(&out, h, "x", 20, true); err != nil {
		t.Fatalf("searchHub: %v", err)
	}
	var items []searchItem
	if err := json.Unmarshal(out.Bytes(), &items); err != nil {
		t.Fatalf("unmarshal: %v\n%s", err, out.String())
	}
	if len(items) != 1 || !items[0].GGUF || items[0].ID != "org/x" {
		t.Fatalf("unexpected items: %+v", items)
	}
}

func TestSearchHub_EmptyQuery(t *testing.T) {
	var out bytes.Buffer
	if err := searchHub(&out, &fakeHub{}, "  ", 20, false); err == nil {
		t.Fatal("expected error for blank query")
	}
}

func TestShowRepoInfo_Table(t *testing.T) {
	h := &fakeHub{repo: &hfhub.RepoInfo{
		ID:   "org/model",
		Tags: []string{"gguf", "text-generation"},
		Siblings: []hfhub.Sibling{
			{RFilename: "model.Q4_K_M.gguf", Size: 4096},
			{RFilename: "README.md", Size: 100},
		},
	}}
	var out bytes.Buffer
	if err := showRepoInfo(&out, h, "org/model", false); err != nil {
		t.Fatalf("showRepoInfo: %v", err)
	}
	s := out.String()
	if !strings.Contains(s, "org/model") || !strings.Contains(s, "model.Q4_K_M.gguf") {
		t.Fatalf("missing repo/file: %q", s)
	}
}

func TestShowRepoInfo_JSON(t *testing.T) {
	h := &fakeHub{repo: &hfhub.RepoInfo{ID: "org/model", Siblings: []hfhub.Sibling{{RFilename: "a.gguf", Size: 7}}}}
	var out bytes.Buffer
	if err := showRepoInfo(&out, h, "org/model", true); err != nil {
		t.Fatalf("showRepoInfo: %v", err)
	}
	var v repoInfoView
	if err := json.Unmarshal(out.Bytes(), &v); err != nil {
		t.Fatalf("unmarshal: %v\n%s", err, out.String())
	}
	if v.ID != "org/model" || len(v.Files) != 1 || v.Files[0].Filename != "a.gguf" {
		t.Fatalf("unexpected view: %+v", v)
	}
}
```

- [ ] **Step 2: Run the test to verify it fails**

Run: `go test ./internal/cli/ -run 'TestSearchHub|TestShowRepoInfo' -v`
Expected: compile failure — `searchHub`, `showRepoInfo`, `searchItem`, `repoInfoView` undefined.

- [ ] **Step 3: Implement model_hub.go**

Create `internal/cli/model_hub.go`:

```go
package cli

import (
	"context"
	"errors"
	"fmt"
	"io"
	"strconv"
	"strings"

	"github.com/spf13/cobra"
)

// searchItem is the JSON view of a Hub search result.
type searchItem struct {
	ID           string   `json:"id"`
	Author       string   `json:"author,omitempty"`
	Downloads    int      `json:"downloads"`
	Likes        int      `json:"likes"`
	GGUF         bool     `json:"gguf"`
	PipelineTag  string   `json:"pipelineTag,omitempty"`
	Tags         []string `json:"tags,omitempty"`
}

// repoFile / repoInfoView are the JSON view of repo metadata.
type repoFile struct {
	Filename  string `json:"filename"`
	SizeBytes int64  `json:"sizeBytes"`
}
type repoInfoView struct {
	ID    string     `json:"id"`
	Tags  []string   `json:"tags,omitempty"`
	Files []repoFile `json:"files"`
}

func init() {
	var limit int
	searchCmd := &cobra.Command{
		Use:   "search <query>",
		Short: "Search HuggingFace Hub for models",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return exitOnErr(cmd.ErrOrStderr(),
				searchHub(cmd.OutOrStdout(), buildHFClient(), args[0], limit, jsonOut))
		},
	}
	searchCmd.Flags().IntVar(&limit, "limit", 20, "maximum number of results")
	modelCmd.AddCommand(searchCmd)

	infoCmd := &cobra.Command{
		Use:   "info <repo-id>",
		Short: "Show HuggingFace repo metadata and file listing",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return exitOnErr(cmd.ErrOrStderr(),
				showRepoInfo(cmd.OutOrStdout(), buildHFClient(), args[0], jsonOut))
		},
	}
	modelCmd.AddCommand(infoCmd)
}

func searchHub(out io.Writer, hub hubClient, query string, limit int, asJSON bool) error {
	if strings.TrimSpace(query) == "" {
		return errors.New("search query is required")
	}
	if limit <= 0 {
		limit = 20
	}
	results, err := hub.Search(context.Background(), query, limit)
	if err != nil {
		return fmt.Errorf("search: %w", err)
	}
	if asJSON {
		items := make([]searchItem, 0, len(results))
		for _, r := range results {
			items = append(items, searchItem{
				ID: r.ID, Author: r.Author, Downloads: r.Downloads, Likes: r.Likes,
				GGUF: r.HasGGUFTag(), PipelineTag: r.PipelineTag, Tags: r.Tags,
			})
		}
		return emitJSON(out, items)
	}
	if len(results) == 0 {
		fmt.Fprintln(out, "no results")
		return nil
	}
	rows := make([][]string, 0, len(results))
	for _, r := range results {
		gguf := "-"
		if r.HasGGUFTag() {
			gguf = "yes"
		}
		rows = append(rows, []string{
			clip(r.ID, 48), strconv.Itoa(r.Downloads), strconv.Itoa(r.Likes), gguf, dashOr(r.PipelineTag),
		})
	}
	printTable(out, []string{"ID", "DOWNLOADS", "LIKES", "GGUF", "PIPELINE"}, rows)
	return nil
}

func showRepoInfo(out io.Writer, hub hubClient, repoID string, asJSON bool) error {
	if strings.TrimSpace(repoID) == "" {
		return errors.New("repo id is required")
	}
	info, err := hub.RepoInfo(context.Background(), repoID)
	if err != nil {
		return fmt.Errorf("repo info: %w", err)
	}
	if info == nil {
		return fmt.Errorf("repo not found: %s", repoID)
	}
	if asJSON {
		files := make([]repoFile, 0, len(info.Siblings))
		for _, s := range info.Siblings {
			files = append(files, repoFile{Filename: s.RFilename, SizeBytes: s.Size})
		}
		return emitJSON(out, repoInfoView{ID: info.ID, Tags: info.Tags, Files: files})
	}
	fmt.Fprintf(out, "Repo:  %s\n", info.ID)
	if len(info.Tags) > 0 {
		fmt.Fprintf(out, "Tags:  %s\n", strings.Join(info.Tags, ", "))
	}
	fmt.Fprintln(out)
	if len(info.Siblings) == 0 {
		fmt.Fprintln(out, "no files")
		return nil
	}
	rows := make([][]string, 0, len(info.Siblings))
	for _, s := range info.Siblings {
		rows = append(rows, []string{s.RFilename, humanBytes(s.Size)})
	}
	printTable(out, []string{"FILENAME", "SIZE"}, rows)
	return nil
}
```

- [ ] **Step 4: Run the test to verify it passes**

Run: `go test ./internal/cli/ -run 'TestSearchHub|TestShowRepoInfo' -v`
Expected: PASS (all five).

- [ ] **Step 5: Commit**

```bash
git add internal/cli/model_hub.go internal/cli/model_hub_test.go
git commit -m "feat(cli): add 'model search' and 'model info' HuggingFace commands"
```

---

## Task 4: `model download` — start a download

**Files:**
- Create: `internal/cli/model_download.go`
- Create: `internal/cli/model_download_test.go`

- [ ] **Step 1: Write the failing test**

Create `internal/cli/model_download_test.go`:

```go
package cli

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"

	"github.com/quantmind-br/model-loader/internal/service/downloadmgr"
)

// fakeDLManager implements downloadManager, recording calls and serving canned
// snapshots/events.
type fakeDLManager struct {
	started   []downloadmgr.Spec
	cancelled []downloadmgr.ID
	resumed   []downloadmgr.ID
	snapshot  []downloadmgr.State
	startErr  error
	cancelErr error
	resumeErr error
	events    chan downloadmgr.Event
	nextID    downloadmgr.ID
}

func (f *fakeDLManager) Start(spec downloadmgr.Spec) (downloadmgr.ID, error) {
	if f.startErr != nil {
		return "", f.startErr
	}
	f.started = append(f.started, spec)
	id := f.nextID
	if id == "" {
		id = "id-1"
	}
	return id, nil
}
func (f *fakeDLManager) Cancel(id downloadmgr.ID) error {
	f.cancelled = append(f.cancelled, id)
	return f.cancelErr
}
func (f *fakeDLManager) Resume(id downloadmgr.ID) error {
	f.resumed = append(f.resumed, id)
	return f.resumeErr
}
func (f *fakeDLManager) Snapshot() []downloadmgr.State { return f.snapshot }
func (f *fakeDLManager) Subscribe() <-chan downloadmgr.Event {
	if f.events == nil {
		f.events = make(chan downloadmgr.Event, 4)
	}
	return f.events
}
func (f *fakeDLManager) StartPolling() {}

// tempSearchPath returns an existing temp dir usable as a download root.
func tempSearchPath(t *testing.T) string {
	t.Helper()
	return t.TempDir()
}

func TestStartDownload_SingleFile(t *testing.T) {
	root := tempSearchPath(t)
	mgr := &fakeDLManager{}
	var out bytes.Buffer
	err := startDownload(&out, mgr, &fakeHub{}, root, "org/model", "model.gguf", false, false)
	if err != nil {
		t.Fatalf("startDownload: %v", err)
	}
	if len(mgr.started) != 1 {
		t.Fatalf("expected 1 Start call, got %d", len(mgr.started))
	}
	spec := mgr.started[0]
	if spec.RepoID != "org/model" || spec.Filename != "model.gguf" {
		t.Fatalf("bad spec: %+v", spec)
	}
	if spec.DestFile != filepath.Join(root, "model.gguf") {
		t.Fatalf("bad destFile: %q", spec.DestFile)
	}
	if spec.URL != "https://hf/org/model/model.gguf" {
		t.Fatalf("bad url: %q", spec.URL)
	}
}

func TestStartDownload_Snapshot(t *testing.T) {
	root := tempSearchPath(t)
	mgr := &fakeDLManager{}
	var out bytes.Buffer
	if err := startDownload(&out, mgr, &fakeHub{}, root, "org/model", "file.bin", true, false); err != nil {
		t.Fatalf("startDownload: %v", err)
	}
	want := filepath.Join(root, "org__model", "file.bin")
	if mgr.started[0].DestFile != want {
		t.Fatalf("snapshot destFile = %q, want %q", mgr.started[0].DestFile, want)
	}
}

func TestStartDownload_NoSearchPath(t *testing.T) {
	var out bytes.Buffer
	if err := startDownload(&out, &fakeDLManager{}, &fakeHub{}, "", "org/m", "f.gguf", false, false); err == nil {
		t.Fatal("expected error when search path is empty")
	}
}

func TestStartDownload_AlreadyExists(t *testing.T) {
	root := tempSearchPath(t)
	if err := os.WriteFile(filepath.Join(root, "f.gguf"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	var out bytes.Buffer
	if err := startDownload(&out, &fakeDLManager{}, &fakeHub{}, root, "org/m", "f.gguf", false, false); err == nil {
		t.Fatal("expected error when destination already exists")
	}
}
```

- [ ] **Step 2: Run the test to verify it fails**

Run: `go test ./internal/cli/ -run TestStartDownload -v`
Expected: compile failure — `startDownload` undefined.

- [ ] **Step 3: Implement model_download.go (start only)**

Create `internal/cli/model_download.go`:

```go
package cli

import (
	"fmt"
	"io"

	"github.com/quantmind-br/model-loader/internal/config"
	"github.com/quantmind-br/model-loader/internal/service/downloadmgr"
	"github.com/spf13/cobra"
)

func init() {
	var (
		snapshot bool
		wait     bool
	)
	dlCmd := &cobra.Command{
		Use:   "download <repo-id> <filename>",
		Short: "Download a file from a HuggingFace repository",
		Args:  cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, err := config.Load()
			if err != nil {
				fmt.Fprintf(cmd.ErrOrStderr(), "config error: %v\n", err)
				return &ExitError{Code: 1}
			}
			mgr, err := buildDownloadManager(cfg)
			if err != nil {
				fmt.Fprintf(cmd.ErrOrStderr(), "download manager: %v\n", err)
				return &ExitError{Code: 1}
			}
			defer mgr.Close()
			return exitOnErr(cmd.ErrOrStderr(), startDownload(
				cmd.OutOrStdout(), mgr, buildHFClient(),
				firstPath(cfg.Models.SearchPaths), args[0], args[1], snapshot, wait,
			))
		},
	}
	dlCmd.Flags().BoolVar(&snapshot, "snapshot", false, "place the file under a {org}__{repo} subdirectory (snapshot layout)")
	dlCmd.Flags().BoolVar(&wait, "wait", false, "block until the download finishes")
	modelCmd.AddCommand(dlCmd)
}

// startDownload resolves the destination, enqueues a download, and either
// returns immediately (printing the id) or blocks until the download reaches a
// terminal state when wait is true.
func startDownload(out io.Writer, mgr downloadManager, hub hubClient, searchPath, repoID, filename string, snapshot, wait bool) error {
	if searchPath == "" {
		return fmt.Errorf("no model search path configured (set models.search_paths)")
	}
	destDir, destFile, err := downloadmgr.ResolveDest(searchPath, repoID, filename, snapshot)
	if err != nil {
		return fmt.Errorf("resolve destination: %w", err)
	}
	url := hub.DownloadURL(repoID, filename)

	var sub <-chan downloadmgr.Event
	if wait {
		sub = mgr.Subscribe()
		mgr.StartPolling()
	}

	id, err := mgr.Start(downloadmgr.Spec{
		RepoID:     repoID,
		Filename:   filename,
		URL:        url,
		DestDir:    destDir,
		DestFile:   destFile,
		IsSnapshot: snapshot,
	})
	if err != nil {
		return fmt.Errorf("start download: %w", err)
	}

	if !wait {
		if jsonOut {
			return emitJSON(out, map[string]string{"id": string(id), "status": "started", "dest": destFile})
		}
		fmt.Fprintf(out, "started download %s → %s\n", id, destFile)
		return nil
	}

	for ev := range sub {
		if ev.ID != id || !ev.State.Status.IsTerminal() {
			continue
		}
		if ev.State.Status == downloadmgr.StatusCompleted {
			if jsonOut {
				return emitJSON(out, map[string]string{"id": string(id), "status": "completed", "dest": destFile})
			}
			fmt.Fprintf(out, "completed %s → %s\n", id, destFile)
			return nil
		}
		return fmt.Errorf("download %s ended: %s", id, ev.State.Status)
	}
	return nil
}
```

- [ ] **Step 4: Run the test to verify it passes**

Run: `go test ./internal/cli/ -run TestStartDownload -v`
Expected: PASS (all four).

- [ ] **Step 5: Commit**

```bash
git add internal/cli/model_download.go internal/cli/model_download_test.go
git commit -m "feat(cli): add 'model download' to start a HuggingFace download"
```

---

## Task 5: `model downloads` — list, cancel, resume

**Files:**
- Modify: `internal/cli/model_download.go` (add downloads subtree + pure funcs)
- Modify: `internal/cli/model_download_test.go` (add tests)

- [ ] **Step 1: Write the failing test**

Append to `internal/cli/model_download_test.go`:

```go
func sampleStates() []downloadmgr.State {
	return []downloadmgr.State{
		{ID: "aaa111", Spec: downloadmgr.Spec{RepoID: "org/a", Filename: "a.gguf"}, Status: downloadmgr.StatusActive, Bytes: 512, Total: 1024},
		{ID: "bbb222", Spec: downloadmgr.Spec{RepoID: "org/b", Filename: "b.gguf"}, Status: downloadmgr.StatusCompleted, Bytes: 2048, Total: 2048},
	}
}

func TestListDownloads_Table(t *testing.T) {
	mgr := &fakeDLManager{snapshot: sampleStates()}
	var out bytes.Buffer
	if err := listDownloads(&out, mgr, false); err != nil {
		t.Fatalf("listDownloads: %v", err)
	}
	s := out.String()
	if !strings.Contains(s, "aaa111") || !strings.Contains(s, "active") || !strings.Contains(s, "completed") {
		t.Fatalf("table missing rows: %q", s)
	}
}

func TestListDownloads_Empty(t *testing.T) {
	var out bytes.Buffer
	if err := listDownloads(&out, &fakeDLManager{}, false); err != nil {
		t.Fatalf("listDownloads: %v", err)
	}
	if !strings.Contains(out.String(), "no downloads") {
		t.Fatalf("expected 'no downloads': %q", out.String())
	}
}

func TestResolveDownloadID_Prefix(t *testing.T) {
	mgr := &fakeDLManager{snapshot: sampleStates()}
	id, err := resolveDownloadID(mgr, "aaa")
	if err != nil || id != "aaa111" {
		t.Fatalf("prefix resolve: id=%q err=%v", id, err)
	}
	if _, err := resolveDownloadID(mgr, "zzz"); err == nil {
		t.Fatal("expected not-found error")
	}
}

func TestCancelDownload(t *testing.T) {
	mgr := &fakeDLManager{snapshot: sampleStates()}
	var out bytes.Buffer
	if err := cancelDownload(&out, mgr, "aaa111", false); err != nil {
		t.Fatalf("cancelDownload: %v", err)
	}
	if len(mgr.cancelled) != 1 || mgr.cancelled[0] != "aaa111" {
		t.Fatalf("cancel not recorded: %+v", mgr.cancelled)
	}
}

func TestResumeDownload_NotResumable(t *testing.T) {
	mgr := &fakeDLManager{snapshot: sampleStates(), resumeErr: downloadmgr.ErrNotResumable}
	var out bytes.Buffer
	err := resumeDownload(&out, mgr, "bbb222", false)
	if err == nil || !strings.Contains(err.Error(), "not resumable") {
		t.Fatalf("expected not-resumable error, got %v", err)
	}
}
```

Ensure the test file imports `"strings"` (add it to the import block if not already present).

- [ ] **Step 2: Run the test to verify it fails**

Run: `go test ./internal/cli/ -run 'TestListDownloads|TestResolveDownloadID|TestCancelDownload|TestResumeDownload' -v`
Expected: compile failure — `listDownloads`, `resolveDownloadID`, `cancelDownload`, `resumeDownload` undefined.

- [ ] **Step 3: Add the downloads subtree to model_download.go**

Append to `internal/cli/model_download.go`. First add the imports `"errors"`, `"sort"`, and `"strings"` to its import block:

```go
import (
	"errors"
	"fmt"
	"io"
	"sort"
	"strings"

	"github.com/quantmind-br/model-loader/internal/config"
	"github.com/quantmind-br/model-loader/internal/service/downloadmgr"
	"github.com/spf13/cobra"
)
```

Then append:

```go
// downloadItem is the JSON view of a download's state.
type downloadItem struct {
	ID       string `json:"id"`
	Repo     string `json:"repo,omitempty"`
	Filename string `json:"filename,omitempty"`
	Status   string `json:"status"`
	Bytes    int64  `json:"bytes"`
	Total    int64  `json:"total"`
}

func init() {
	downloadsCmd := &cobra.Command{
		Use:   "downloads",
		Short: "List and manage downloads",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return runWithManager(cmd, func(out io.Writer, mgr *downloadmgr.Manager) error {
				return listDownloads(out, mgr, jsonOut)
			})
		},
	}
	cancelCmd := &cobra.Command{
		Use:   "cancel <id>",
		Short: "Cancel a download",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runWithManager(cmd, func(out io.Writer, mgr *downloadmgr.Manager) error {
				return cancelDownload(out, mgr, args[0], jsonOut)
			})
		},
	}
	resumeCmd := &cobra.Command{
		Use:   "resume <id>",
		Short: "Resume an abandoned or failed download",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runWithManager(cmd, func(out io.Writer, mgr *downloadmgr.Manager) error {
				return resumeDownload(out, mgr, args[0], jsonOut)
			})
		},
	}
	downloadsCmd.AddCommand(cancelCmd)
	downloadsCmd.AddCommand(resumeCmd)
	modelCmd.AddCommand(downloadsCmd)
}

func listDownloads(out io.Writer, mgr downloadManager, asJSON bool) error {
	states := mgr.Snapshot()
	if asJSON {
		items := make([]downloadItem, 0, len(states))
		for _, s := range states {
			items = append(items, downloadItem{
				ID: string(s.ID), Repo: s.Spec.RepoID, Filename: s.Spec.Filename,
				Status: s.Status.String(), Bytes: s.Bytes, Total: s.Total,
			})
		}
		return emitJSON(out, items)
	}
	if len(states) == 0 {
		fmt.Fprintln(out, "no downloads")
		return nil
	}
	rows := make([][]string, 0, len(states))
	for _, s := range states {
		progress := humanBytes(s.Bytes)
		if s.Total > 0 {
			progress = fmt.Sprintf("%s / %s", humanBytes(s.Bytes), humanBytes(s.Total))
		}
		rows = append(rows, []string{
			string(s.ID), dashOr(s.Spec.RepoID), dashOr(s.Spec.Filename), s.Status.String(), progress,
		})
	}
	printTable(out, []string{"ID", "REPO", "FILE", "STATUS", "PROGRESS"}, rows)
	return nil
}

// resolveDownloadID matches ref against a download id exactly, then by unique prefix.
func resolveDownloadID(mgr downloadManager, ref string) (downloadmgr.ID, error) {
	states := mgr.Snapshot()
	var byPrefix []downloadmgr.ID
	for _, s := range states {
		if string(s.ID) == ref {
			return s.ID, nil
		}
		if strings.HasPrefix(string(s.ID), ref) {
			byPrefix = append(byPrefix, s.ID)
		}
	}
	switch len(byPrefix) {
	case 1:
		return byPrefix[0], nil
	case 0:
		return "", fmt.Errorf("download not found: %s", ref)
	default:
		return "", fmt.Errorf("ambiguous download id: %s matches %d downloads", ref, len(byPrefix))
	}
}

func cancelDownload(out io.Writer, mgr downloadManager, ref string, asJSON bool) error {
	id, err := resolveDownloadID(mgr, ref)
	if err != nil {
		return err
	}
	if err := mgr.Cancel(id); err != nil {
		return fmt.Errorf("cancel: %w", err)
	}
	if asJSON {
		return emitJSON(out, map[string]string{"id": string(id), "status": "cancelled"})
	}
	fmt.Fprintf(out, "cancelled %s\n", id)
	return nil
}

func resumeDownload(out io.Writer, mgr downloadManager, ref string, asJSON bool) error {
	id, err := resolveDownloadID(mgr, ref)
	if err != nil {
		return err
	}
	if err := mgr.Resume(id); err != nil {
		if errors.Is(err, downloadmgr.ErrNotResumable) {
			return fmt.Errorf("download not resumable: %s", id)
		}
		return fmt.Errorf("resume: %w", err)
	}
	if asJSON {
		return emitJSON(out, map[string]string{"id": string(id), "status": "resuming"})
	}
	fmt.Fprintf(out, "resuming %s\n", id)
	return nil
}
```

Note: `sort` is imported for forward-compatibility but `listDownloads` relies on `Snapshot()` already being sorted by `StartedAt`. If `go build` reports `sort` as unused, remove it from the import block. (Verify during Step 4.)

- [ ] **Step 4: Run the test to verify it passes**

Run: `go test ./internal/cli/ -run 'TestListDownloads|TestResolveDownloadID|TestCancelDownload|TestResumeDownload' -v`
Expected: PASS (all six).

Run: `go build ./...` — Expected: clean. If `sort` is flagged unused, delete the `"sort"` import line and rebuild.

- [ ] **Step 5: Commit**

```bash
git add internal/cli/model_download.go internal/cli/model_download_test.go
git commit -m "feat(cli): add 'model downloads' list/cancel/resume"
```

---

## Task 6: Full command-tree integration test

**Files:**
- Modify: `internal/cli/model_download_test.go` (add tree assertion + Execute smoke)

- [ ] **Step 1: Write the failing test**

Append to `internal/cli/model_download_test.go`:

```go
func TestModelCommandTree(t *testing.T) {
	model := childByName(rootCmd, "model")
	if model == nil {
		t.Fatal("model command missing")
	}
	for _, name := range []string{"list", "search", "info", "download", "downloads"} {
		if childByName(model, name) == nil {
			t.Errorf("model subcommand missing: %s", name)
		}
	}
	downloads := childByName(model, "downloads")
	if downloads == nil {
		t.Fatal("downloads command missing")
	}
	for _, name := range []string{"cancel", "resume"} {
		if childByName(downloads, name) == nil {
			t.Errorf("downloads subcommand missing: %s", name)
		}
	}
}

func TestModelDownload_ArgValidation(t *testing.T) {
	// 'model download' with too few args must exit non-zero (cobra Args check),
	// without panicking or reaching the network.
	rootCmd.SetArgs([]string{"model", "download", "only-one-arg"})
	t.Cleanup(func() { rootCmd.SetArgs(nil) })
	if code := Execute(); code == 0 {
		t.Fatal("expected non-zero exit for missing filename arg")
	}
}
```

- [ ] **Step 2: Run the test to verify it fails (or passes the tree, fails nothing)**

Run: `go test ./internal/cli/ -run 'TestModelCommandTree|TestModelDownload_ArgValidation' -v`
Expected: both PASS if Tasks 1–5 are complete. If `TestModelCommandTree` fails, a subcommand registration is missing — fix the offending `init()`.

- [ ] **Step 3: No implementation needed**

This task validates wiring. If both tests pass, proceed. If `TestModelDownload_ArgValidation` reaches the network or panics, the cobra `Args: cobra.ExactArgs(2)` guard is missing on the download command — add it (it should already be present from Task 4).

- [ ] **Step 4: Run the full package suite**

Run: `go test ./internal/cli/...`
Expected: PASS. Then run the whole repo:
Run: `go test ./...`
Expected: PASS (no regressions; note the `llamahelp` golden test may fail locally if a real `llama-server` is installed — that is a known pre-existing condition, not caused by this work).

- [ ] **Step 5: Commit**

```bash
git add internal/cli/model_download_test.go
git commit -m "test(cli): assert full model command tree and download arg validation"
```

---

## Self-Review (completed by plan author)

**1. Spec coverage:**
- `model list` (local GGUF scan) → Task 2 ✓
- `model search` → Task 3 ✓
- `model info` → Task 3 ✓
- `model download` (start, single-file + `--snapshot` + `--wait`) → Task 4 ✓
- `model downloads` (list) → Task 5 ✓
- `model downloads cancel` → Task 5 ✓
- `model downloads resume` → Task 5 ✓
- `--json` on every command → covered (each pure fn takes `asJSON`, reads global `jsonOut`) ✓
- Exit codes (0 ok / 1 error) via `exitOnErr` → ✓
- `adapters` → deliberately OUT of scope (no TUI feature exists; confirmed with user) ✓

**2. Placeholder scan:** No TBD/TODO; every code step shows full code. ✓

**3. Type consistency:** `modelListItem`, `searchItem`, `repoInfoView`/`repoFile`, `downloadItem` defined once each; `hubClient`/`downloadManager`/`modelscanner.Scanner` interfaces consistent across tasks; `humanBytes`/`exitOnErr`/`runWithManager`/`firstPath`/`buildScanner`/`buildHFClient`/`buildDownloadManager` defined in Task 1 and reused. ✓

**Known caveat for the implementer:** `model.go` declares helpers (`buildHFClient`, `runWithManager`, etc.) used only by later tasks. Package-level unused funcs are legal in Go and do NOT break the build — do not "fix" them by deleting. Only unused *imports* and unused *local* variables break compilation.
