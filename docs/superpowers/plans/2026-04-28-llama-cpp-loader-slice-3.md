# model-loader — Slice 3 Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Ship `ModelScanner` service, `modelsPage` (browser de GGUFs com filter/rescan), e `ModelPicker` overlay integrado no editor de profiles, encerrando slice 3 do roadmap (design § 11).

**Architecture:** Camada `service/modelscanner` faz walk recursivo dos paths configurados, parseia header GGUF mínimo (magic + version + early metadata) e emite `ScanEvent` via channel. UI consome via streaming `tea.Cmd` re-armado a cada evento. Picker é um componente reutilizável que abre em modal sobre `profilesPage` via `ctrl+p` no field Model.

**Tech Stack:** Go 1.26.2, charmbracelet/bubbletea, charmbracelet/bubbles (list + table), charmbracelet/huh (action menu), encoding/binary (GGUF parser).

**Spec deviation note:** Design § 7.3 F1 step 2 diz "Em `model`, `Enter` abre picker". `huh.NewInput` consome Enter para avançar field — implementar Enter-to-pick exigiria custom field. Compromisso: `ctrl+p` abre picker em qualquer ponto do editor (não só no field Model), com hint inline. Documentar no help da página.

**ScanEvent extension:** Spec § 6.5 define `ScanEvent{Type, File, Error}`. Para per-path status na UI, adicionamos campos `Root string` (atribuição) e `Count int` (Progress payload). Extensão minimamente invasiva, tipos nunca usam JSON wire format então não há quebra de compat.

---

## File Structure

**Domain (types only):**
- `internal/domain/model.go` (NEW) — `ModelFile`, `ScanEvent`, `ScanEventType`

**Service modelscanner:**
- `internal/service/modelscanner/modelscanner.go` (NEW) — `Scanner` interface + package doc
- `internal/service/modelscanner/scanner.go` (NEW) — `fsScanner` walk implementation
- `internal/service/modelscanner/gguf.go` (NEW) — header + metadata parser
- `internal/service/modelscanner/quant.go` (NEW) — filename heuristics (quant + params)
- `internal/service/modelscanner/scanner_test.go` (NEW)
- `internal/service/modelscanner/gguf_test.go` (NEW)
- `internal/service/modelscanner/quant_test.go` (NEW)

**UI components:**
- `internal/ui/components/picker.go` (NEW) — `ModelPicker` overlay
- `internal/ui/components/picker_test.go` (NEW)

**UI pages:**
- `internal/ui/pages/models.go` (NEW) — `ModelsPage` (table + filter + rescan + action menu)
- `internal/ui/pages/models_test.go` (NEW)
- `internal/ui/pages/profiles.go` (MODIFY) — integrar picker via `ctrl+p`, handler `ModelPickedMsg`, handler cross-tab `UseInNewProfileMsg`
- `internal/ui/pages/profiles_editor.go` (MODIFY) — campo `pickerActive` no draft (não, fica no page), small label tweaks
- `internal/ui/pages/profiles_test.go` (MODIFY) — picker integration smoke

**Root + entry:**
- `internal/ui/root.go` (MODIFY) — `WithModelsPage` builder; intercept `UseInNewProfileMsg` para tab-switch + forward
- `cmd/model-loader/main.go` (MODIFY) — construct `fsScanner`, wire em `ModelsPage` + `ProfilesPage`

---

## Conventions for this plan

- **TDD:** cada feature de service tem teste antes da impl (Red → Green → Commit). UI tests podem vir depois da impl quando teatest precisa de fixtures de runtime — segue padrão de slice 2 (unit-level acessando campos não-exportados via `package pages`).
- **Commits:** um commit por task, mensagem no formato `feat(<area>): <ação>` ou `test(<area>): <ação>`.
- **Diretório de trabalho:** `feat/slice-3` branch a partir de `main`. Criar antes de T1.
- **Run tests** com `go test ./...` da raiz do repo após cada task.

---

## Pre-flight

- [x] **Step P1: Criar branch slice-3 a partir de main**

```bash
git checkout main
git pull --ff-only origin main 2>/dev/null || true
git checkout -b feat/slice-3
```

Expected: branch `feat/slice-3` criada e checked out.

- [x] **Step P2: Verificar baseline verde**

Run: `go test ./...`
Expected: PASS em todos os packages (estado pós-merge slice 2).

---

### Task 1: Domain types — `ModelFile`, `ScanEvent`, `ScanEventType`

**Files:**
- Create: `internal/domain/model.go`

- [x] **Step 1: Criar arquivo de tipos**

Conteúdo de `internal/domain/model.go`:

```go
package domain

// ModelFile describes a discovered GGUF file on disk.
// Quant and Params are best-effort: derived from filename heuristics
// (and GGUF metadata when readable). Either may be empty.
type ModelFile struct {
	Path      string // absolute path
	SizeBytes int64
	Name      string // base filename
	Quant     string // e.g. "Q4_K_M", "Q5_K_M", "Q8_0"
	Params    string // e.g. "32B", "7B"
}

// ScanEventType discriminates ScanEvent payloads.
type ScanEventType int

const (
	// ScanEventFile is emitted once per discovered .gguf file.
	ScanEventFile ScanEventType = iota
	// ScanEventProgress is emitted at the start of each root path scan
	// (Count == 0) and again at end (Count == final file count).
	ScanEventProgress
	// ScanEventError is emitted when a root path fails (e.g., ENOENT).
	// Per-file read errors do not abort the scan; they are silently
	// degraded to a ModelFile with empty Quant/Params.
	ScanEventError
	// ScanEventDone is emitted exactly once after all roots are visited.
	ScanEventDone
)

// ScanEvent is the channel payload from ModelScanner.Scan.
// Root is the configured search path the event is attributed to (empty
// for ScanEventDone, which is global).
type ScanEvent struct {
	Type  ScanEventType
	Root  string
	File  *ModelFile
	Count int
	Error error
}
```

- [x] **Step 2: Compilar**

Run: `go build ./internal/domain/...`
Expected: PASS (no test added yet — pure types).

- [x] **Step 3: Commit**

```bash
git add internal/domain/model.go
git commit -m "feat(domain): add ModelFile and ScanEvent types"
```

---

### Task 2: `modelscanner` package skeleton + `Scanner` interface

**Files:**
- Create: `internal/service/modelscanner/modelscanner.go`

- [x] **Step 1: Criar package doc + interface**

Conteúdo de `internal/service/modelscanner/modelscanner.go`:

```go
// Package modelscanner walks configured filesystem paths and emits
// ScanEvent values describing GGUF model files found, with best-effort
// metadata (quant from filename, parameter count from GGUF header).
package modelscanner

import (
	"context"

	"github.com/quantmind-br/model-loader/internal/domain"
)

// Scanner discovers GGUF files under the given root paths.
// Scan returns a buffered channel that the caller must drain until it
// closes. The channel closes after a single ScanEventDone is emitted,
// or earlier if ctx is cancelled.
type Scanner interface {
	Scan(ctx context.Context, paths []string) (<-chan domain.ScanEvent, error)
}
```

- [x] **Step 2: Compilar**

Run: `go build ./internal/service/modelscanner/...`
Expected: PASS.

- [x] **Step 3: Commit**

```bash
git add internal/service/modelscanner/modelscanner.go
git commit -m "feat(modelscanner): introduce Scanner interface skeleton"
```

---

### Task 3: Filename quant heuristic

**Files:**
- Create: `internal/service/modelscanner/quant.go`
- Create: `internal/service/modelscanner/quant_test.go`

- [x] **Step 1: Escrever teste falhando**

Conteúdo de `internal/service/modelscanner/quant_test.go`:

```go
package modelscanner

import "testing"

func TestParseQuant(t *testing.T) {
	cases := []struct {
		name string
		want string
	}{
		{"Qwen2.5-Coder-32B-Instruct-Q5_K_M.gguf", "Q5_K_M"},
		{"llama-3.1-8b-instruct-q4_k_m.gguf", "Q4_K_M"},
		{"Mistral-7B-Instruct-v0.3-Q8_0.gguf", "Q8_0"},
		{"some-model-IQ4_NL.gguf", "IQ4_NL"},
		{"deepseek-coder-6.7B-instruct-Q4_0.gguf", "Q4_0"},
		{"f16-only.gguf", "F16"},
		{"plain-name.gguf", ""},
		{"model-q5_k_s.gguf", "Q5_K_S"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := parseQuant(tc.name)
			if got != tc.want {
				t.Fatalf("parseQuant(%q) = %q, want %q", tc.name, got, tc.want)
			}
		})
	}
}
```

- [x] **Step 2: Run para confirmar falha**

Run: `go test ./internal/service/modelscanner/ -run TestParseQuant -v`
Expected: FAIL — `parseQuant` undefined.

- [x] **Step 3: Implementar**

Conteúdo de `internal/service/modelscanner/quant.go`:

```go
package modelscanner

import (
	"regexp"
	"strings"
)

// quantRe matches well-known llama.cpp quant labels in filenames.
// Order matters: longer (more specific) patterns come first so partial
// matches (e.g. Q4 vs Q4_K_M) yield the more specific result.
var quantRe = regexp.MustCompile(`(?i)(IQ[1-4](?:_[A-Z]+)?|Q[1-8]_K_[MS]|Q[1-8]_K|Q[1-8]_[01]|F16|F32|BF16)`)

// parseQuant extracts the quant tag from a GGUF filename. Returns "" if
// no recognized pattern is found.
func parseQuant(name string) string {
	m := quantRe.FindString(name)
	if m == "" {
		return ""
	}
	return strings.ToUpper(m)
}
```

- [x] **Step 4: Run para confirmar passa**

Run: `go test ./internal/service/modelscanner/ -run TestParseQuant -v`
Expected: PASS, all 8 sub-tests green.

- [x] **Step 5: Commit**

```bash
git add internal/service/modelscanner/quant.go internal/service/modelscanner/quant_test.go
git commit -m "feat(modelscanner): parse quant tag from filename"
```

---

### Task 4: Filename params heuristic

**Files:**
- Modify: `internal/service/modelscanner/quant.go`
- Modify: `internal/service/modelscanner/quant_test.go`

- [x] **Step 1: Adicionar caso de teste falhando**

Append a `internal/service/modelscanner/quant_test.go`:

```go
func TestParseParams(t *testing.T) {
	cases := []struct {
		name string
		want string
	}{
		{"Qwen2.5-Coder-32B-Instruct-Q5_K_M.gguf", "32B"},
		{"llama-3.1-8b-instruct-q4_k_m.gguf", "8B"},
		{"deepseek-coder-6.7B-instruct-Q4_0.gguf", "6.7B"},
		{"Mixtral-8x7B-Instruct-v0.1-Q4_K_M.gguf", "8x7B"},
		{"phi-3.5-mini-3.8B-instruct-Q4_K_M.gguf", "3.8B"},
		{"plain-name.gguf", ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := parseParams(tc.name)
			if got != tc.want {
				t.Fatalf("parseParams(%q) = %q, want %q", tc.name, got, tc.want)
			}
		})
	}
}
```

- [x] **Step 2: Run para confirmar falha**

Run: `go test ./internal/service/modelscanner/ -run TestParseParams -v`
Expected: FAIL — `parseParams` undefined.

- [x] **Step 3: Implementar**

Append a `internal/service/modelscanner/quant.go`:

```go
// paramsRe matches parameter-count labels in filenames:
// "32B", "7B", "6.7B", "3.8B", "8x7B" (mixture-of-experts).
// Captures the number+B with optional decimal and optional NxM prefix.
var paramsRe = regexp.MustCompile(`(?i)(?:^|[-_])(\d+(?:x\d+)?(?:\.\d+)?B)(?:[-_.]|$)`)

// parseParams extracts the parameter-count tag from a GGUF filename.
// Returns "" if no recognizable size label is present.
func parseParams(name string) string {
	m := paramsRe.FindStringSubmatch(name)
	if len(m) < 2 {
		return ""
	}
	return strings.ToUpper(m[1])
}
```

- [x] **Step 4: Run para confirmar passa**

Run: `go test ./internal/service/modelscanner/ -run TestParseParams -v`
Expected: PASS, all 6 sub-tests green.

- [x] **Step 5: Commit**

```bash
git add internal/service/modelscanner/quant.go internal/service/modelscanner/quant_test.go
git commit -m "feat(modelscanner): parse parameter count from filename"
```

---

### Task 5: GGUF magic + version + counts validation

**Files:**
- Create: `internal/service/modelscanner/gguf.go`
- Create: `internal/service/modelscanner/gguf_test.go`

- [x] **Step 1: Escrever teste falhando**

Conteúdo de `internal/service/modelscanner/gguf_test.go`:

```go
package modelscanner

import (
	"bytes"
	"encoding/binary"
	"testing"
)

// buildGGUFHeader returns a minimal valid GGUF header bytestream:
// "GGUF" + version + tensorCount + metaCount.
// Caller appends KV bytes if needed.
func buildGGUFHeader(version uint32, tensorCount, metaCount uint64) []byte {
	var buf bytes.Buffer
	buf.WriteString("GGUF")
	binary.Write(&buf, binary.LittleEndian, version)
	binary.Write(&buf, binary.LittleEndian, tensorCount)
	binary.Write(&buf, binary.LittleEndian, metaCount)
	return buf.Bytes()
}

func TestReadGGUFHeader_ValidMagic(t *testing.T) {
	data := buildGGUFHeader(3, 0, 0)
	hdr, err := readGGUFHeader(bytes.NewReader(data))
	if err != nil {
		t.Fatalf("readGGUFHeader: unexpected err: %v", err)
	}
	if hdr.Version != 3 {
		t.Fatalf("Version = %d, want 3", hdr.Version)
	}
	if hdr.MetadataCount != 0 {
		t.Fatalf("MetadataCount = %d, want 0", hdr.MetadataCount)
	}
}

func TestReadGGUFHeader_BadMagic(t *testing.T) {
	data := []byte("NOPEXXXXXXXXXXXXXXXXXXXXXXXX")
	if _, err := readGGUFHeader(bytes.NewReader(data)); err == nil {
		t.Fatal("expected error on bad magic, got nil")
	}
}

func TestReadGGUFHeader_Truncated(t *testing.T) {
	data := []byte("GGUF\x03\x00") // magic + 2 bytes of version
	if _, err := readGGUFHeader(bytes.NewReader(data)); err == nil {
		t.Fatal("expected error on truncated header, got nil")
	}
}
```

- [x] **Step 2: Run para confirmar falha**

Run: `go test ./internal/service/modelscanner/ -run TestReadGGUFHeader -v`
Expected: FAIL — `readGGUFHeader` and `ggufHeader` undefined.

- [x] **Step 3: Implementar**

Conteúdo de `internal/service/modelscanner/gguf.go`:

```go
package modelscanner

import (
	"encoding/binary"
	"errors"
	"fmt"
	"io"
)

// ggufMagic is the 4-byte file signature.
var ggufMagic = [4]byte{'G', 'G', 'U', 'F'}

// errBadMagic is returned when the first 4 bytes do not match "GGUF".
var errBadMagic = errors.New("modelscanner: not a GGUF file (bad magic)")

// ggufHeader holds the fixed-width prefix every GGUF file starts with.
type ggufHeader struct {
	Version       uint32
	TensorCount   uint64
	MetadataCount uint64
}

// readGGUFHeader reads and validates the GGUF magic + version + counts.
// It does NOT advance into metadata KV pairs.
func readGGUFHeader(r io.Reader) (ggufHeader, error) {
	var magic [4]byte
	if _, err := io.ReadFull(r, magic[:]); err != nil {
		return ggufHeader{}, fmt.Errorf("read magic: %w", err)
	}
	if magic != ggufMagic {
		return ggufHeader{}, errBadMagic
	}
	var hdr ggufHeader
	if err := binary.Read(r, binary.LittleEndian, &hdr.Version); err != nil {
		return ggufHeader{}, fmt.Errorf("read version: %w", err)
	}
	if err := binary.Read(r, binary.LittleEndian, &hdr.TensorCount); err != nil {
		return ggufHeader{}, fmt.Errorf("read tensor count: %w", err)
	}
	if err := binary.Read(r, binary.LittleEndian, &hdr.MetadataCount); err != nil {
		return ggufHeader{}, fmt.Errorf("read metadata count: %w", err)
	}
	return hdr, nil
}
```

- [x] **Step 4: Run para confirmar passa**

Run: `go test ./internal/service/modelscanner/ -run TestReadGGUFHeader -v`
Expected: PASS, 3 sub-tests green.

- [x] **Step 5: Commit**

```bash
git add internal/service/modelscanner/gguf.go internal/service/modelscanner/gguf_test.go
git commit -m "feat(modelscanner): validate GGUF magic and read fixed header"
```

---

### Task 6: GGUF metadata reader — extract `general.parameter_count`

**Files:**
- Modify: `internal/service/modelscanner/gguf.go`
- Modify: `internal/service/modelscanner/gguf_test.go`

- [x] **Step 1: Adicionar testes falhando**

Append a `internal/service/modelscanner/gguf_test.go`:

```go
func TestReadGGUFParams_ParameterCount(t *testing.T) {
	var buf bytes.Buffer
	buf.Write(buildGGUFHeader(3, 0, 1))
	writeGGUFString(&buf, "general.parameter_count")
	binary.Write(&buf, binary.LittleEndian, uint32(10)) // type uint64
	binary.Write(&buf, binary.LittleEndian, uint64(32_000_000_000))

	got, err := readGGUFParams(bytes.NewReader(buf.Bytes()))
	if err != nil {
		t.Fatalf("readGGUFParams: %v", err)
	}
	if got != "32B" {
		t.Fatalf("params = %q, want %q", got, "32B")
	}
}

func TestReadGGUFParams_SizeLabelString(t *testing.T) {
	var buf bytes.Buffer
	buf.Write(buildGGUFHeader(3, 0, 1))
	writeGGUFString(&buf, "general.size_label")
	binary.Write(&buf, binary.LittleEndian, uint32(8)) // type string
	writeGGUFString(&buf, "7B")

	got, err := readGGUFParams(bytes.NewReader(buf.Bytes()))
	if err != nil {
		t.Fatalf("readGGUFParams: %v", err)
	}
	if got != "7B" {
		t.Fatalf("params = %q, want %q", got, "7B")
	}
}

func TestReadGGUFParams_NoMatchReturnsEmpty(t *testing.T) {
	var buf bytes.Buffer
	buf.Write(buildGGUFHeader(3, 0, 1))
	writeGGUFString(&buf, "general.architecture")
	binary.Write(&buf, binary.LittleEndian, uint32(8)) // type string
	writeGGUFString(&buf, "qwen")

	got, err := readGGUFParams(bytes.NewReader(buf.Bytes()))
	if err != nil {
		t.Fatalf("readGGUFParams: %v", err)
	}
	if got != "" {
		t.Fatalf("params = %q, want empty", got)
	}
}

func TestReadGGUFParams_UnsupportedTypeAborts(t *testing.T) {
	var buf bytes.Buffer
	buf.Write(buildGGUFHeader(3, 0, 2))
	writeGGUFString(&buf, "tokenizer.ggml.tokens")
	binary.Write(&buf, binary.LittleEndian, uint32(9)) // type array — unsupported, abort scan
	// Don't bother appending payload; reader should bail out at type check.

	got, err := readGGUFParams(bytes.NewReader(buf.Bytes()))
	if err != nil {
		t.Fatalf("readGGUFParams: %v", err)
	}
	if got != "" {
		t.Fatalf("params = %q, want empty (unsupported type aborts)", got)
	}
}

// writeGGUFString writes the GGUF string format: u64 length + utf8 bytes.
func writeGGUFString(buf *bytes.Buffer, s string) {
	binary.Write(buf, binary.LittleEndian, uint64(len(s)))
	buf.WriteString(s)
}
```

- [x] **Step 2: Run para confirmar falha**

Run: `go test ./internal/service/modelscanner/ -run TestReadGGUFParams -v`
Expected: FAIL — `readGGUFParams` undefined.

- [x] **Step 3: Implementar**

Append a `internal/service/modelscanner/gguf.go`:

```go
// GGUF metadata value type IDs (subset we support).
// Source: github.com/ggerganov/ggml — ggml/include/gguf.h.
const (
	ggufTypeUint8   uint32 = 0
	ggufTypeInt8    uint32 = 1
	ggufTypeUint16  uint32 = 2
	ggufTypeInt16   uint32 = 3
	ggufTypeUint32  uint32 = 4
	ggufTypeInt32   uint32 = 5
	ggufTypeFloat32 uint32 = 6
	ggufTypeBool    uint32 = 7
	ggufTypeString  uint32 = 8
	ggufTypeUint64  uint32 = 10
	ggufTypeInt64   uint32 = 11
	ggufTypeFloat64 uint32 = 12
)

// metadataScanLimit caps how many KV pairs we walk before giving up.
// Real GGUFs have hundreds of KVs; we only need general.parameter_count
// or general.size_label which are usually within the first ~50.
const metadataScanLimit = 128

// readGGUFParams reads the full header and walks metadata KV pairs
// looking for parameter-count signals. Returns "" when neither key is
// present, when an unsupported value type is encountered (we cannot
// safely advance the reader past unknown payloads), or when the file
// is truncated. Caller must seek/wrap the reader to the start.
func readGGUFParams(r io.Reader) (string, error) {
	hdr, err := readGGUFHeader(r)
	if err != nil {
		return "", err
	}
	scan := hdr.MetadataCount
	if scan > metadataScanLimit {
		scan = metadataScanLimit
	}
	for i := uint64(0); i < scan; i++ {
		key, err := readGGUFString(r)
		if err != nil {
			return "", nil
		}
		var typeID uint32
		if err := binary.Read(r, binary.LittleEndian, &typeID); err != nil {
			return "", nil
		}
		switch key {
		case "general.parameter_count":
			if typeID != ggufTypeUint64 {
				return "", nil
			}
			var n uint64
			if err := binary.Read(r, binary.LittleEndian, &n); err != nil {
				return "", nil
			}
			return formatParams(n), nil
		case "general.size_label":
			if typeID != ggufTypeString {
				return "", nil
			}
			s, err := readGGUFString(r)
			if err != nil {
				return "", nil
			}
			return s, nil
		}
		if !skipGGUFValue(r, typeID) {
			return "", nil
		}
	}
	return "", nil
}

// readGGUFString reads a GGUF string (u64 length + utf8 bytes).
// Length is capped at 1 MiB to defend against bogus headers.
func readGGUFString(r io.Reader) (string, error) {
	var n uint64
	if err := binary.Read(r, binary.LittleEndian, &n); err != nil {
		return "", err
	}
	if n > 1<<20 {
		return "", fmt.Errorf("string length %d exceeds limit", n)
	}
	buf := make([]byte, n)
	if _, err := io.ReadFull(r, buf); err != nil {
		return "", err
	}
	return string(buf), nil
}

// skipGGUFValue advances the reader past a metadata value of the given
// type. Returns false for types we cannot skip safely (arrays, unknown).
func skipGGUFValue(r io.Reader, typeID uint32) bool {
	switch typeID {
	case ggufTypeUint8, ggufTypeInt8, ggufTypeBool:
		return discard(r, 1)
	case ggufTypeUint16, ggufTypeInt16:
		return discard(r, 2)
	case ggufTypeUint32, ggufTypeInt32, ggufTypeFloat32:
		return discard(r, 4)
	case ggufTypeUint64, ggufTypeInt64, ggufTypeFloat64:
		return discard(r, 8)
	case ggufTypeString:
		_, err := readGGUFString(r)
		return err == nil
	default:
		// Arrays (9) and unknowns: abort.
		return false
	}
}

func discard(r io.Reader, n int64) bool {
	_, err := io.CopyN(io.Discard, r, n)
	return err == nil
}

// formatParams turns a parameter count like 32_000_000_000 into "32B".
// Below 1B uses M; below 1M uses raw integer; above 1B uses B with one
// decimal when not a round multiple.
func formatParams(n uint64) string {
	switch {
	case n >= 1_000_000_000:
		whole := n / 1_000_000_000
		rem := n % 1_000_000_000
		if rem < 50_000_000 { // round
			return fmt.Sprintf("%dB", whole)
		}
		tenths := (rem + 50_000_000) / 100_000_000
		return fmt.Sprintf("%d.%dB", whole, tenths)
	case n >= 1_000_000:
		return fmt.Sprintf("%dM", n/1_000_000)
	default:
		return fmt.Sprintf("%d", n)
	}
}
```

- [x] **Step 4: Run para confirmar passa**

Run: `go test ./internal/service/modelscanner/ -run TestReadGGUFParams -v`
Expected: PASS, 4 sub-tests green.

- [x] **Step 5: Commit**

```bash
git add internal/service/modelscanner/gguf.go internal/service/modelscanner/gguf_test.go
git commit -m "feat(modelscanner): parse GGUF metadata for parameter count"
```

---

### Task 7: Walk single root path emitting File events

**Files:**
- Create: `internal/service/modelscanner/scanner.go`
- Create: `internal/service/modelscanner/scanner_test.go`

- [x] **Step 1: Escrever teste falhando**

Conteúdo de `internal/service/modelscanner/scanner_test.go`:

```go
package modelscanner

import (
	"bytes"
	"context"
	"encoding/binary"
	"os"
	"path/filepath"
	"testing"

	"github.com/quantmind-br/model-loader/internal/domain"
)

func writeGGUFFile(t *testing.T, path string, paramCount uint64) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	var buf bytes.Buffer
	buf.WriteString("GGUF")
	binary.Write(&buf, binary.LittleEndian, uint32(3))
	binary.Write(&buf, binary.LittleEndian, uint64(0)) // tensors
	binary.Write(&buf, binary.LittleEndian, uint64(1)) // metas
	binary.Write(&buf, binary.LittleEndian, uint64(len("general.parameter_count")))
	buf.WriteString("general.parameter_count")
	binary.Write(&buf, binary.LittleEndian, uint32(10)) // uint64
	binary.Write(&buf, binary.LittleEndian, paramCount)
	if err := os.WriteFile(path, buf.Bytes(), 0o644); err != nil {
		t.Fatal(err)
	}
}

func collect(ch <-chan domain.ScanEvent) []domain.ScanEvent {
	var out []domain.ScanEvent
	for evt := range ch {
		out = append(out, evt)
	}
	return out
}

func TestScanner_FindsSingleGGUF(t *testing.T) {
	dir := t.TempDir()
	writeGGUFFile(t, filepath.Join(dir, "Qwen-32B-Q4_K_M.gguf"), 32_000_000_000)

	s := New()
	ch, err := s.Scan(context.Background(), []string{dir})
	if err != nil {
		t.Fatal(err)
	}
	events := collect(ch)

	var files []*domain.ModelFile
	for _, e := range events {
		if e.Type == domain.ScanEventFile {
			files = append(files, e.File)
		}
	}
	if len(files) != 1 {
		t.Fatalf("got %d files, want 1: %#v", len(files), events)
	}
	got := files[0]
	if got.Name != "Qwen-32B-Q4_K_M.gguf" {
		t.Errorf("Name = %q", got.Name)
	}
	if got.Quant != "Q4_K_M" {
		t.Errorf("Quant = %q, want Q4_K_M", got.Quant)
	}
	if got.Params != "32B" {
		t.Errorf("Params = %q, want 32B", got.Params)
	}
	if got.SizeBytes <= 0 {
		t.Errorf("SizeBytes = %d, want >0", got.SizeBytes)
	}
}
```

- [x] **Step 2: Run para confirmar falha**

Run: `go test ./internal/service/modelscanner/ -run TestScanner_FindsSingleGGUF -v`
Expected: FAIL — `New` undefined.

- [x] **Step 3: Implementar**

Conteúdo de `internal/service/modelscanner/scanner.go`:

```go
package modelscanner

import (
	"context"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"github.com/quantmind-br/model-loader/internal/domain"
)

// New returns a filesystem-backed Scanner.
func New() Scanner {
	return &fsScanner{}
}

type fsScanner struct{}

const eventBuffer = 64

func (s *fsScanner) Scan(ctx context.Context, paths []string) (<-chan domain.ScanEvent, error) {
	ch := make(chan domain.ScanEvent, eventBuffer)
	go func() {
		defer close(ch)
		for _, root := range paths {
			s.scanRoot(ctx, root, ch)
			if ctx.Err() != nil {
				return
			}
		}
		select {
		case ch <- domain.ScanEvent{Type: domain.ScanEventDone}:
		case <-ctx.Done():
		}
	}()
	return ch, nil
}

func (s *fsScanner) scanRoot(ctx context.Context, root string, ch chan<- domain.ScanEvent) {
	count := 0
	walkErr := filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		if err != nil {
			return err
		}
		if d.IsDir() {
			return nil
		}
		if !strings.EqualFold(filepath.Ext(p), ".gguf") {
			return nil
		}
		mf := buildModelFile(p, d)
		count++
		select {
		case ch <- domain.ScanEvent{Type: domain.ScanEventFile, Root: root, File: &mf}:
		case <-ctx.Done():
			return ctx.Err()
		}
		return nil
	})
	if walkErr != nil && ctx.Err() == nil {
		ch <- domain.ScanEvent{Type: domain.ScanEventError, Root: root, Error: walkErr}
		return
	}
	ch <- domain.ScanEvent{Type: domain.ScanEventProgress, Root: root, Count: count}
}

func buildModelFile(path string, d fs.DirEntry) domain.ModelFile {
	name := filepath.Base(path)
	mf := domain.ModelFile{
		Path:  path,
		Name:  name,
		Quant: parseQuant(name),
	}
	if info, err := d.Info(); err == nil {
		mf.SizeBytes = info.Size()
	}
	if params := readParamsFromFile(path); params != "" {
		mf.Params = params
	} else {
		mf.Params = parseParams(name)
	}
	return mf
}

func readParamsFromFile(path string) string {
	f, err := os.Open(path)
	if err != nil {
		return ""
	}
	defer f.Close()
	p, err := readGGUFParams(f)
	if err != nil {
		return ""
	}
	return p
}
```

- [x] **Step 4: Run para confirmar passa**

Run: `go test ./internal/service/modelscanner/ -run TestScanner_FindsSingleGGUF -v`
Expected: PASS.

- [x] **Step 5: Commit**

```bash
git add internal/service/modelscanner/scanner.go internal/service/modelscanner/scanner_test.go
git commit -m "feat(modelscanner): walk root path emitting file events"
```

---

### Task 8: Recursive walk + non-gguf filter coverage

**Files:**
- Modify: `internal/service/modelscanner/scanner_test.go`

- [x] **Step 1: Adicionar teste falhando**

Append a `internal/service/modelscanner/scanner_test.go`:

```go
func TestScanner_RecursiveAndIgnoresNonGGUF(t *testing.T) {
	dir := t.TempDir()
	writeGGUFFile(t, filepath.Join(dir, "top.gguf"), 7_000_000_000)
	writeGGUFFile(t, filepath.Join(dir, "sub", "deep.gguf"), 13_000_000_000)
	writeGGUFFile(t, filepath.Join(dir, "sub", "sub2", "deeper.gguf"), 70_000_000_000)
	if err := os.WriteFile(filepath.Join(dir, "ignore.txt"), []byte("nope"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "sub", "config.json"), []byte("{}"), 0o644); err != nil {
		t.Fatal(err)
	}

	s := New()
	ch, err := s.Scan(context.Background(), []string{dir})
	if err != nil {
		t.Fatal(err)
	}
	events := collect(ch)

	count := 0
	for _, e := range events {
		if e.Type == domain.ScanEventFile {
			count++
		}
	}
	if count != 3 {
		t.Fatalf("file events = %d, want 3 (got events: %#v)", count, events)
	}
}
```

- [x] **Step 2: Run**

Run: `go test ./internal/service/modelscanner/ -run TestScanner_RecursiveAndIgnoresNonGGUF -v`
Expected: PASS (already supported by Task 7's WalkDir + extension filter).

- [x] **Step 3: Commit**

```bash
git add internal/service/modelscanner/scanner_test.go
git commit -m "test(modelscanner): cover recursive walk and non-gguf filter"
```

---

### Task 9: Per-path Progress + Done events

**Files:**
- Modify: `internal/service/modelscanner/scanner_test.go`

- [x] **Step 1: Adicionar teste falhando (verifica que já implementamos corretamente)**

Append a `internal/service/modelscanner/scanner_test.go`:

```go
func TestScanner_EmitsProgressAndDone(t *testing.T) {
	dirA := t.TempDir()
	dirB := t.TempDir()
	writeGGUFFile(t, filepath.Join(dirA, "a.gguf"), 1_000_000_000)
	writeGGUFFile(t, filepath.Join(dirB, "b1.gguf"), 1_000_000_000)
	writeGGUFFile(t, filepath.Join(dirB, "b2.gguf"), 1_000_000_000)

	s := New()
	ch, err := s.Scan(context.Background(), []string{dirA, dirB})
	if err != nil {
		t.Fatal(err)
	}
	events := collect(ch)

	progressByRoot := map[string]int{}
	doneCount := 0
	for _, e := range events {
		switch e.Type {
		case domain.ScanEventProgress:
			progressByRoot[e.Root] = e.Count
		case domain.ScanEventDone:
			doneCount++
		}
	}
	if progressByRoot[dirA] != 1 {
		t.Errorf("progress[dirA] = %d, want 1", progressByRoot[dirA])
	}
	if progressByRoot[dirB] != 2 {
		t.Errorf("progress[dirB] = %d, want 2", progressByRoot[dirB])
	}
	if doneCount != 1 {
		t.Errorf("done events = %d, want 1", doneCount)
	}
}
```

- [x] **Step 2: Run**

Run: `go test ./internal/service/modelscanner/ -run TestScanner_EmitsProgressAndDone -v`
Expected: PASS (Task 7 already implements this).

- [x] **Step 3: Commit**

```bash
git add internal/service/modelscanner/scanner_test.go
git commit -m "test(modelscanner): cover progress and done events"
```

---

### Task 10: Per-path Error event for missing root

**Files:**
- Modify: `internal/service/modelscanner/scanner_test.go`

- [x] **Step 1: Adicionar teste**

Append a `internal/service/modelscanner/scanner_test.go`:

```go
func TestScanner_ErrorOnMissingRoot(t *testing.T) {
	s := New()
	ch, err := s.Scan(context.Background(), []string{"/definitely/does/not/exist/xyz"})
	if err != nil {
		t.Fatal(err)
	}
	events := collect(ch)

	errEvents := 0
	for _, e := range events {
		if e.Type == domain.ScanEventError {
			errEvents++
		}
	}
	if errEvents != 1 {
		t.Fatalf("error events = %d, want 1; got events: %#v", errEvents, events)
	}
}
```

- [x] **Step 2: Run**

Run: `go test ./internal/service/modelscanner/ -run TestScanner_ErrorOnMissingRoot -v`
Expected: PASS (filepath.WalkDir returns error for missing root).

- [x] **Step 3: Commit**

```bash
git add internal/service/modelscanner/scanner_test.go
git commit -m "test(modelscanner): cover root-missing error event"
```

---

### Task 11: Context cancellation aborts scan

**Files:**
- Modify: `internal/service/modelscanner/scanner_test.go`

- [x] **Step 1: Adicionar teste**

Append a `internal/service/modelscanner/scanner_test.go`:

```go
func TestScanner_RespectsContextCancel(t *testing.T) {
	dir := t.TempDir()
	for i := 0; i < 50; i++ {
		writeGGUFFile(t, filepath.Join(dir, "m"+string(rune('a'+i%26))+".gguf"), 1_000_000_000)
	}

	ctx, cancel := context.WithCancel(context.Background())
	s := New()
	ch, err := s.Scan(ctx, []string{dir})
	if err != nil {
		t.Fatal(err)
	}
	// Cancel almost immediately.
	cancel()

	// Drain the channel; we just need it to close without hanging.
	for range ch {
	}
}
```

- [x] **Step 2: Run**

Run: `go test ./internal/service/modelscanner/ -run TestScanner_RespectsContextCancel -v -timeout 10s`
Expected: PASS within 10s (no deadlock).

- [x] **Step 3: Commit**

```bash
git add internal/service/modelscanner/scanner_test.go
git commit -m "test(modelscanner): cover context cancellation"
```

---

### Task 12: ModelsPage skeleton

**Files:**
- Create: `internal/ui/pages/models.go`

- [x] **Step 1: Implementar skeleton**

Conteúdo de `internal/ui/pages/models.go`:

```go
// Package pages holds tab page implementations.
package pages

import (
	"context"
	"fmt"
	"sort"
	"strings"

	"github.com/charmbracelet/bubbles/key"
	"github.com/charmbracelet/bubbles/table"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/quantmind-br/model-loader/internal/domain"
	"github.com/quantmind-br/model-loader/internal/service/modelscanner"
	"github.com/quantmind-br/model-loader/internal/ui/theme"
)

// pathStatus tracks per-root scan progress shown above the table.
type pathStatus struct {
	state string // "scanning" | "scanned" | "error"
	count int
	err   string
}

// ModelsPage browses GGUF files discovered by ModelScanner.
type ModelsPage struct {
	scanner modelscanner.Scanner
	paths   []string
	cancel  context.CancelFunc

	files     []domain.ModelFile
	statusMap map[string]pathStatus

	table      table.Model
	width      int
	height     int
	filter     string
	filterMode bool
	flash      string

	keys modelsKeyMap
}

type modelsKeyMap struct {
	Filter, Rescan, Enter, Cancel key.Binding
}

func defaultModelsKeys() modelsKeyMap {
	return modelsKeyMap{
		Filter: key.NewBinding(key.WithKeys("/"), key.WithHelp("/", "filter")),
		Rescan: key.NewBinding(key.WithKeys("R"), key.WithHelp("R", "rescan")),
		Enter:  key.NewBinding(key.WithKeys("enter"), key.WithHelp("enter", "actions")),
		Cancel: key.NewBinding(key.WithKeys("esc"), key.WithHelp("esc", "clear filter")),
	}
}

// NewModelsPage builds a page wired to a Scanner and configured search paths.
func NewModelsPage(scanner modelscanner.Scanner, paths []string) ModelsPage {
	cols := []table.Column{
		{Title: "Name", Width: 36},
		{Title: "Size", Width: 10},
		{Title: "Quant", Width: 10},
		{Title: "Params", Width: 8},
		{Title: "Path", Width: 40},
	}
	t := table.New(table.WithColumns(cols), table.WithFocused(true), table.WithHeight(12))

	statusMap := make(map[string]pathStatus, len(paths))
	for _, p := range paths {
		statusMap[p] = pathStatus{state: "scanning"}
	}
	return ModelsPage{
		scanner:   scanner,
		paths:     paths,
		statusMap: statusMap,
		table:     t,
		keys:      defaultModelsKeys(),
	}
}
```

- [x] **Step 2: Compilar**

Run: `go build ./internal/ui/pages/...`
Expected: PASS.

- [x] **Step 3: Commit**

```bash
git add internal/ui/pages/models.go
git commit -m "feat(ui/pages/models): page skeleton with table and status map"
```

---

### Task 13: ModelsPage Init starts scan and handles WindowSizeMsg

**Files:**
- Modify: `internal/ui/pages/models.go`

**Streaming pattern note:** Bubbletea's `Init()` value receiver makes mutations to `p` invisible to callers. Pattern adopted: `Init()` returns a Cmd that *creates* the channel + cancel and delivers them via `scanStartedMsg`. State (cancel, status) is set in `Update`. Subsequent events ride via `scanEventMsg` carrying the channel for re-arm.

- [x] **Step 1: Adicionar Init + message types + Update skeleton**

Append a `internal/ui/pages/models.go`:

```go
// scanStartedMsg delivers the channel + cancel handle from a fresh scan
// start. State mutations happen when this message lands in Update.
type scanStartedMsg struct {
	ch     <-chan domain.ScanEvent
	cancel context.CancelFunc
	err    error
}

// scanEventMsg carries one ScanEvent plus the channel for re-arming.
type scanEventMsg struct {
	ch  <-chan domain.ScanEvent
	evt domain.ScanEvent
}

// scanChannelClosedMsg signals the scan goroutine finished and closed
// its channel.
type scanChannelClosedMsg struct{}

func (p ModelsPage) Init() tea.Cmd {
	return startScanCmd(p.scanner, p.paths)
}

// startScanCmd builds a Cmd that creates ctx+cancel, kicks off the
// scanner, and delivers the channel via scanStartedMsg. The Cmd's
// closure owns the cancel until Update captures it.
func startScanCmd(scanner modelscanner.Scanner, paths []string) tea.Cmd {
	return func() tea.Msg {
		ctx, cancel := context.WithCancel(context.Background())
		ch, err := scanner.Scan(ctx, paths)
		if err != nil {
			cancel()
			return scanStartedMsg{err: err}
		}
		return scanStartedMsg{ch: ch, cancel: cancel}
	}
}

func waitForScanEvent(ch <-chan domain.ScanEvent) tea.Cmd {
	return func() tea.Msg {
		evt, ok := <-ch
		if !ok {
			return scanChannelClosedMsg{}
		}
		return scanEventMsg{ch: ch, evt: evt}
	}
}

func (p ModelsPage) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		p.width, p.height = msg.Width, msg.Height
		p.table.SetHeight(msg.Height - 8)
		return p, nil
	case scanStartedMsg:
		if msg.err != nil {
			for _, root := range p.paths {
				p.statusMap[root] = pathStatus{state: "error", err: msg.err.Error()}
			}
			return p, nil
		}
		p.cancel = msg.cancel
		return p, waitForScanEvent(msg.ch)
	case scanEventMsg:
		updated, _ := p.handleScanEvent(msg.evt)
		next := updated.(ModelsPage)
		return next, waitForScanEvent(msg.ch)
	case scanChannelClosedMsg:
		return p, nil
	case tea.KeyMsg:
		return p.handleKey(msg)
	}
	return p, nil
}
```

- [x] **Step 2: Compilar (esperar falha — handlers stub ausentes)**

Run: `go build ./internal/ui/pages/...`
Expected: FAIL — `handleScanEvent` and `handleKey` undefined.

- [x] **Step 3: Adicionar stubs + View provisório**

Append a `internal/ui/pages/models.go`:

```go
func (p ModelsPage) handleScanEvent(evt domain.ScanEvent) (tea.Model, tea.Cmd) {
	// Filled in Task 14.
	return p, nil
}

func (p ModelsPage) handleKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	// Filled in Task 16.
	return p, nil
}

func (p ModelsPage) View() string {
	header := theme.Title.Render("Models")
	return lipgloss.JoinVertical(lipgloss.Left, header, p.table.View())
}
```

- [x] **Step 4: Compilar e rodar testes**

Run: `go build ./... && go test ./...`
Expected: PASS (no new tests yet; just compilation).

- [x] **Step 5: Commit**

```bash
git add internal/ui/pages/models.go
git commit -m "feat(ui/pages/models): wire init + scan cmd + event plumbing stubs"
```

---

### Task 14: Process scan events into rows + status

**Files:**
- Modify: `internal/ui/pages/models.go`

- [x] **Step 1: Substituir `handleScanEvent` stub pela implementação**

Em `internal/ui/pages/models.go`, substituir:

```go
func (p ModelsPage) handleScanEvent(evt domain.ScanEvent) (tea.Model, tea.Cmd) {
	// Filled in next task.
	return p, nil
}
```

por:

```go
func (p ModelsPage) handleScanEvent(evt domain.ScanEvent) (tea.Model, tea.Cmd) {
	switch evt.Type {
	case domain.ScanEventFile:
		if evt.File != nil {
			p.files = append(p.files, *evt.File)
			p.refreshRows()
		}
	case domain.ScanEventProgress:
		st := p.statusMap[evt.Root]
		st.count = evt.Count
		st.state = "scanned"
		p.statusMap[evt.Root] = st
	case domain.ScanEventError:
		st := p.statusMap[evt.Root]
		st.state = "error"
		if evt.Error != nil {
			st.err = evt.Error.Error()
		}
		p.statusMap[evt.Root] = st
	case domain.ScanEventDone:
		// Channel will close right after; nothing to do.
	}
	// Re-arm: wait for the next event from the same channel. Since we
	// don't keep the channel handle, we encode the next read via a
	// sentinel: if the underlying channel is already closed, the
	// previously-issued waitForScanEvent will have produced
	// scanChannelClosedMsg which is handled in Update.
	return p, nil
}

// refreshRows rebuilds table rows from p.files honoring the current
// filter. Sorted by name for stable display.
func (p *ModelsPage) refreshRows() {
	files := p.files
	if p.filter != "" {
		q := strings.ToLower(p.filter)
		filtered := make([]domain.ModelFile, 0, len(files))
		for _, f := range files {
			if strings.Contains(strings.ToLower(f.Name), q) {
				filtered = append(filtered, f)
			}
		}
		files = filtered
	}
	sort.Slice(files, func(i, j int) bool { return files[i].Name < files[j].Name })
	rows := make([]table.Row, 0, len(files))
	for _, f := range files {
		rows = append(rows, table.Row{
			truncate(f.Name, 36),
			humanSize(f.SizeBytes),
			f.Quant,
			f.Params,
			truncate(f.Path, 40),
		})
	}
	p.table.SetRows(rows)
}

// humanSize formats bytes as "X.YG" / "X.YM".
func humanSize(n int64) string {
	switch {
	case n >= 1<<30:
		return fmt.Sprintf("%.1fG", float64(n)/float64(1<<30))
	case n >= 1<<20:
		return fmt.Sprintf("%.1fM", float64(n)/float64(1<<20))
	case n >= 1<<10:
		return fmt.Sprintf("%.1fK", float64(n)/float64(1<<10))
	default:
		return fmt.Sprintf("%dB", n)
	}
}
```

- [x] **Step 2: Compilar**

Run: `go build ./...`
Expected: PASS.

- [x] **Step 3: Commit**

```bash
git add internal/ui/pages/models.go
git commit -m "feat(ui/pages/models): process scan events, render rows"
```

---

### Task 15: Render status header per path + filter line

**Files:**
- Modify: `internal/ui/pages/models.go`

- [x] **Step 1: Substituir `View`**

Substituir o método `View` em `internal/ui/pages/models.go`:

```go
func (p ModelsPage) View() string {
	header := theme.Title.Render("Models")
	statusLine := p.renderStatus()
	filterLine := ""
	if p.filterMode || p.filter != "" {
		filterLine = theme.Subtitle.Render(fmt.Sprintf("filter: %q", p.filter))
	}
	help := theme.Subtitle.Render("[/] filter  [R] rescan  [enter] actions  [esc] clear")
	footer := ""
	if p.flash != "" {
		footer = theme.Subtitle.Render(p.flash)
	}
	return lipgloss.JoinVertical(lipgloss.Left, header, statusLine, p.table.View(), filterLine, help, footer)
}

func (p ModelsPage) renderStatus() string {
	if len(p.paths) == 0 {
		return theme.Subtitle.Render("no search paths configured")
	}
	parts := make([]string, 0, len(p.paths))
	for _, root := range p.paths {
		st := p.statusMap[root]
		var label string
		switch st.state {
		case "scanning":
			label = theme.Subtitle.Render(fmt.Sprintf("%s [scanning]", root))
		case "scanned":
			label = theme.OK.Render(fmt.Sprintf("%s [%d]", root, st.count))
		case "error":
			label = theme.Error.Render(fmt.Sprintf("%s [error: %s]", root, st.err))
		default:
			label = theme.Subtitle.Render(root)
		}
		parts = append(parts, label)
	}
	return strings.Join(parts, "  ")
}
```

- [x] **Step 2: Compilar**

Run: `go build ./...`
Expected: PASS.

- [x] **Step 3: Commit**

```bash
git add internal/ui/pages/models.go
git commit -m "feat(ui/pages/models): render per-path status header and filter line"
```

---

### Task 16: Filter `/` mode + rescan `R`

**Files:**
- Modify: `internal/ui/pages/models.go`

- [x] **Step 1: Substituir `handleKey` stub pela implementação**

Substituir:

```go
func (p ModelsPage) handleKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	// Filled in later task.
	return p, nil
}
```

por:

```go
func (p ModelsPage) handleKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch {
	case key.Matches(msg, p.keys.Filter):
		p.filterMode = !p.filterMode
		return p, nil
	case key.Matches(msg, p.keys.Cancel):
		if p.filterMode || p.filter != "" {
			p.filterMode = false
			p.filter = ""
			p.refreshRows()
			return p, nil
		}
	case key.Matches(msg, p.keys.Rescan):
		if p.cancel != nil {
			p.cancel()
			p.cancel = nil
		}
		p.flash = "rescan started"
		p.files = nil
		for _, root := range p.paths {
			p.statusMap[root] = pathStatus{state: "scanning"}
		}
		p.refreshRows()
		return p, startScanCmd(p.scanner, p.paths)
	}

	if p.filterMode {
		switch msg.String() {
		case "backspace":
			if len(p.filter) > 0 {
				p.filter = p.filter[:len(p.filter)-1]
				p.refreshRows()
			}
			return p, nil
		}
		if len(msg.Runes) == 1 {
			p.filter += string(msg.Runes)
			p.refreshRows()
			return p, nil
		}
	}

	t, cmd := p.table.Update(msg)
	p.table = t
	return p, cmd
}
```

- [x] **Step 2: Compilar**

Run: `go build ./...`
Expected: PASS.

- [x] **Step 3: Commit**

```bash
git add internal/ui/pages/models.go
git commit -m "feat(ui/pages/models): filter and rescan keybindings"
```

---

### Task 17: ModelsPage smoke test

**Files:**
- Create: `internal/ui/pages/models_test.go`

- [x] **Step 1: Escrever teste**

Conteúdo de `internal/ui/pages/models_test.go`:

```go
package pages

import (
	"context"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/quantmind-br/model-loader/internal/domain"
)

// fakeScanner emits a fixed sequence of events for tests.
type fakeScanner struct {
	events []domain.ScanEvent
}

func (f *fakeScanner) Scan(ctx context.Context, paths []string) (<-chan domain.ScanEvent, error) {
	ch := make(chan domain.ScanEvent, len(f.events)+1)
	go func() {
		defer close(ch)
		for _, e := range f.events {
			select {
			case ch <- e:
			case <-ctx.Done():
				return
			}
		}
	}()
	return ch, nil
}

func TestModelsPage_LoadsFilesIntoTable(t *testing.T) {
	mf := domain.ModelFile{
		Path:      "/tmp/models/qwen-32b.gguf",
		SizeBytes: 16_000_000_000,
		Name:      "qwen-32b.gguf",
		Quant:     "Q4_K_M",
		Params:    "32B",
	}
	scanner := &fakeScanner{events: []domain.ScanEvent{
		{Type: domain.ScanEventFile, Root: "/tmp/models", File: &mf},
		{Type: domain.ScanEventProgress, Root: "/tmp/models", Count: 1},
		{Type: domain.ScanEventDone},
	}}

	page := NewModelsPage(scanner, []string{"/tmp/models"})
	model := tea.Model(page)

	cmd := model.Init()
	for cmd != nil {
		msg := cmd()
		if msg == nil {
			break
		}
		var c tea.Cmd
		model, c = model.Update(msg)
		cmd = c
	}

	mp := model.(ModelsPage)
	if len(mp.files) != 1 {
		t.Fatalf("files = %d, want 1", len(mp.files))
	}
	if mp.files[0].Name != "qwen-32b.gguf" {
		t.Errorf("Name = %q", mp.files[0].Name)
	}
	if mp.statusMap["/tmp/models"].state != "scanned" {
		t.Errorf("status state = %q, want scanned", mp.statusMap["/tmp/models"].state)
	}
	if mp.statusMap["/tmp/models"].count != 1 {
		t.Errorf("status count = %d, want 1", mp.statusMap["/tmp/models"].count)
	}
}

func TestModelsPage_FilterModeReducesRows(t *testing.T) {
	files := []domain.ModelFile{
		{Path: "/m/a.gguf", Name: "alpha.gguf", Quant: "Q4_K_M"},
		{Path: "/m/b.gguf", Name: "beta.gguf", Quant: "Q5_K_M"},
		{Path: "/m/c.gguf", Name: "gamma.gguf", Quant: "Q8_0"},
	}
	scanner := &fakeScanner{}
	page := NewModelsPage(scanner, nil)
	page.files = files
	page.refreshRows()
	if got := len(page.table.Rows()); got != 3 {
		t.Fatalf("rows pre-filter = %d, want 3", got)
	}

	page.filter = "alpha"
	page.refreshRows()
	if got := len(page.table.Rows()); got != 1 {
		t.Fatalf("rows post-filter = %d, want 1", got)
	}
}
```

- [x] **Step 2: Run**

Run: `go test ./internal/ui/pages/ -run TestModelsPage -v`
Expected: PASS.

- [x] **Step 3: Commit**

```bash
git add internal/ui/pages/models_test.go
git commit -m "test(ui/pages/models): smoke load + filter coverage"
```

---

### Task 18: ModelPicker overlay component skeleton

**Files:**
- Create: `internal/ui/components/picker.go`

- [x] **Step 1: Implementar componente**

Conteúdo de `internal/ui/components/picker.go`:

```go
// Package components contains reusable UI building blocks.
package components

import (
	"context"
	"fmt"
	"sort"
	"strings"

	"github.com/charmbracelet/bubbles/key"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/quantmind-br/model-loader/internal/domain"
	"github.com/quantmind-br/model-loader/internal/ui/theme"
)

// ModelScanner is the minimal Scanner contract the picker depends on.
// Mirrors modelscanner.Scanner; redeclared locally to avoid the
// components package importing service/.
type ModelScanner interface {
	Scan(ctx context.Context, paths []string) (<-chan domain.ScanEvent, error)
}

// ModelPickedMsg is emitted when the user selects a model.
type ModelPickedMsg struct {
	Path string
}

// ModelPickerCancelledMsg is emitted on Esc / cancel.
type ModelPickerCancelledMsg struct{}

// ModelPicker is a modal overlay listing GGUF files.
// Streaming pattern mirrors ModelsPage: Init returns a Cmd that creates
// ctx+channel+cancel and delivers them via PickerScanStartedMsg.
type ModelPicker struct {
	scanner ModelScanner
	paths   []string
	cancel  context.CancelFunc

	files       []domain.ModelFile
	filtered    []domain.ModelFile
	cursor      int
	filter      string
	filterMode  bool
	scanning    bool

	width  int
	height int
}

// NewModelPicker constructs a picker bound to a Scanner and search paths.
func NewModelPicker(scanner ModelScanner, paths []string) ModelPicker {
	return ModelPicker{scanner: scanner, paths: paths, scanning: true}
}

// PickerScanStartedMsg delivers the scan channel + cancel handle.
type PickerScanStartedMsg struct {
	Ch     <-chan domain.ScanEvent
	Cancel context.CancelFunc
	Err    error
}

// PickerScanEventMsg carries one ScanEvent + the channel for re-arm.
type PickerScanEventMsg struct {
	Ch  <-chan domain.ScanEvent
	Evt domain.ScanEvent
}

// PickerScanClosedMsg signals the scan channel closed.
type PickerScanClosedMsg struct{}

// Init returns a Cmd that starts the scan goroutine and emits
// PickerScanStartedMsg.
func (m ModelPicker) Init() tea.Cmd {
	scanner := m.scanner
	paths := m.paths
	return func() tea.Msg {
		ctx, cancel := context.WithCancel(context.Background())
		ch, err := scanner.Scan(ctx, paths)
		if err != nil {
			cancel()
			return PickerScanStartedMsg{Err: err}
		}
		return PickerScanStartedMsg{Ch: ch, Cancel: cancel}
	}
}

func pickerWaitForEvent(ch <-chan domain.ScanEvent) tea.Cmd {
	return func() tea.Msg {
		evt, ok := <-ch
		if !ok {
			return PickerScanClosedMsg{}
		}
		return PickerScanEventMsg{Ch: ch, Evt: evt}
	}
}

// pickerKeys for the modal.
type pickerKeys struct {
	Up, Down, Enter, Esc, Filter, Backspace key.Binding
}

func defaultPickerKeys() pickerKeys {
	return pickerKeys{
		Up:        key.NewBinding(key.WithKeys("up", "k")),
		Down:      key.NewBinding(key.WithKeys("down", "j")),
		Enter:     key.NewBinding(key.WithKeys("enter")),
		Esc:       key.NewBinding(key.WithKeys("esc")),
		Filter:    key.NewBinding(key.WithKeys("/")),
		Backspace: key.NewBinding(key.WithKeys("backspace")),
	}
}
```

- [x] **Step 2: Compilar**

Run: `go build ./...`
Expected: PASS (no Update/View yet but builds because no callers).

- [x] **Step 3: Commit**

```bash
git add internal/ui/components/picker.go
git commit -m "feat(ui/components/picker): skeleton with scan plumbing"
```

---

### Task 19: ModelPicker Update + View + selection

**Files:**
- Modify: `internal/ui/components/picker.go`

- [x] **Step 1: Adicionar Update + View + helpers**

Append a `internal/ui/components/picker.go`:

```go
// Update handles keyboard input and streaming scan events.
func (m ModelPicker) Update(msg tea.Msg) (ModelPicker, tea.Cmd) {
	keys := defaultPickerKeys()
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width, m.height = msg.Width, msg.Height
		return m, nil
	case PickerScanStartedMsg:
		if msg.Err != nil {
			m.scanning = false
			return m, nil
		}
		m.cancel = msg.Cancel
		return m, pickerWaitForEvent(msg.Ch)
	case PickerScanEventMsg:
		switch msg.Evt.Type {
		case domain.ScanEventFile:
			if msg.Evt.File != nil {
				m.files = append(m.files, *msg.Evt.File)
				m.applyFilter()
			}
		case domain.ScanEventDone:
			m.scanning = false
		}
		return m, pickerWaitForEvent(msg.Ch)
	case PickerScanClosedMsg:
		m.scanning = false
		return m, nil
	case tea.KeyMsg:
		if key.Matches(msg, keys.Esc) {
			if m.cancel != nil {
				m.cancel()
			}
			return m, func() tea.Msg { return ModelPickerCancelledMsg{} }
		}
		if key.Matches(msg, keys.Enter) {
			if len(m.filtered) == 0 {
				return m, nil
			}
			path := m.filtered[m.cursor].Path
			if m.cancel != nil {
				m.cancel()
			}
			return m, func() tea.Msg { return ModelPickedMsg{Path: path} }
		}
		if key.Matches(msg, keys.Filter) {
			m.filterMode = !m.filterMode
			return m, nil
		}
		if m.filterMode {
			if key.Matches(msg, keys.Backspace) {
				if len(m.filter) > 0 {
					m.filter = m.filter[:len(m.filter)-1]
					m.applyFilter()
				}
				return m, nil
			}
			if len(msg.Runes) == 1 {
				m.filter += string(msg.Runes)
				m.applyFilter()
				return m, nil
			}
		}
		if key.Matches(msg, keys.Up) {
			if m.cursor > 0 {
				m.cursor--
			}
			return m, nil
		}
		if key.Matches(msg, keys.Down) {
			if m.cursor < len(m.filtered)-1 {
				m.cursor++
			}
			return m, nil
		}
	}
	return m, nil
}

func (m *ModelPicker) applyFilter() {
	if m.filter == "" {
		m.filtered = append(m.filtered[:0], m.files...)
	} else {
		q := strings.ToLower(m.filter)
		m.filtered = m.filtered[:0]
		for _, f := range m.files {
			if strings.Contains(strings.ToLower(f.Name), q) {
				m.filtered = append(m.filtered, f)
			}
		}
	}
	sort.Slice(m.filtered, func(i, j int) bool { return m.filtered[i].Name < m.filtered[j].Name })
	if m.cursor >= len(m.filtered) {
		m.cursor = 0
	}
}

// View renders the picker as a centered overlay box.
func (m ModelPicker) View() string {
	title := theme.Title.Render("Pick a model")
	hint := "[↑↓] move  [/] filter  [enter] select  [esc] cancel"
	if m.filterMode {
		hint = "[type] add  [backspace] del  [/] exit filter  [enter] select"
	}
	statusLine := ""
	if m.scanning {
		statusLine = theme.Subtitle.Render(fmt.Sprintf("scanning… %d found", len(m.files)))
	} else {
		statusLine = theme.Subtitle.Render(fmt.Sprintf("%d models", len(m.files)))
	}
	filterLine := ""
	if m.filterMode || m.filter != "" {
		filterLine = theme.Subtitle.Render(fmt.Sprintf("filter: %q", m.filter))
	}
	rows := make([]string, 0, len(m.filtered))
	for i, f := range m.filtered {
		row := fmt.Sprintf("%-36s  %-8s  %-6s  %s", truncatePath(f.Name, 36), f.Quant, f.Params, truncatePath(f.Path, 60))
		if i == m.cursor {
			row = theme.Selected.Render(row)
		}
		rows = append(rows, row)
	}
	body := strings.Join(rows, "\n")
	help := theme.Subtitle.Render(hint)
	box := lipgloss.JoinVertical(lipgloss.Left, title, statusLine, filterLine, body, help)
	return theme.Pane.Width(80).Render(box)
}

func truncatePath(s string, max int) string {
	if len(s) <= max {
		return s
	}
	return s[:max-1] + "…"
}
```

- [x] **Step 2: Compilar**

Run: `go build ./...`
Expected: PASS.

- [x] **Step 3: Commit**

```bash
git add internal/ui/components/picker.go
git commit -m "feat(ui/components/picker): update, view, selection messages"
```

---

### Task 20: ModelPicker test

**Files:**
- Create: `internal/ui/components/picker_test.go`

- [x] **Step 1: Escrever teste**

Conteúdo de `internal/ui/components/picker_test.go`:

```go
package components

import (
	"context"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/quantmind-br/model-loader/internal/domain"
)

type fakePickerScanner struct {
	events []domain.ScanEvent
}

func (f *fakePickerScanner) Scan(ctx context.Context, paths []string) (<-chan domain.ScanEvent, error) {
	ch := make(chan domain.ScanEvent, len(f.events)+1)
	go func() {
		defer close(ch)
		for _, e := range f.events {
			select {
			case ch <- e:
			case <-ctx.Done():
				return
			}
		}
	}()
	return ch, nil
}

// drainScan walks the picker through Init → ScanStarted → all events
// until the channel closes, returning the final picker state.
func drainScan(t *testing.T, p ModelPicker) ModelPicker {
	t.Helper()
	cmd := p.Init()
	if cmd == nil {
		return p
	}
	for cmd != nil {
		msg := cmd()
		if msg == nil {
			break
		}
		var c tea.Cmd
		p, c = p.Update(msg)
		cmd = c
	}
	return p
}

func TestModelPicker_SelectEmitsModelPickedMsg(t *testing.T) {
	mf := domain.ModelFile{Path: "/m/a.gguf", Name: "a.gguf"}
	scanner := &fakePickerScanner{events: []domain.ScanEvent{
		{Type: domain.ScanEventFile, File: &mf},
		{Type: domain.ScanEventDone},
	}}

	p := NewModelPicker(scanner, []string{"/m"})
	p = drainScan(t, p)

	// Trigger Enter and capture the resulting Cmd's message.
	_, c := p.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if c == nil {
		t.Fatal("expected ModelPickedMsg cmd, got nil")
	}
	got := c()
	picked, ok := got.(ModelPickedMsg)
	if !ok {
		t.Fatalf("msg type = %T, want ModelPickedMsg", got)
	}
	if picked.Path != "/m/a.gguf" {
		t.Fatalf("path = %q", picked.Path)
	}
}

func TestModelPicker_EscEmitsCancel(t *testing.T) {
	scanner := &fakePickerScanner{events: []domain.ScanEvent{{Type: domain.ScanEventDone}}}
	p := NewModelPicker(scanner, nil)
	p = drainScan(t, p)

	_, c := p.Update(tea.KeyMsg{Type: tea.KeyEsc})
	if c == nil {
		t.Fatal("expected ModelPickerCancelledMsg cmd")
	}
	got := c()
	if _, ok := got.(ModelPickerCancelledMsg); !ok {
		t.Fatalf("msg type = %T, want ModelPickerCancelledMsg", got)
	}
}

func TestModelPicker_FilterReducesList(t *testing.T) {
	files := []domain.ModelFile{
		{Path: "/m/qwen.gguf", Name: "qwen.gguf"},
		{Path: "/m/llama.gguf", Name: "llama.gguf"},
	}
	scanner := &fakePickerScanner{}
	p := NewModelPicker(scanner, nil)
	p.files = files
	p.applyFilter()
	if len(p.filtered) != 2 {
		t.Fatalf("filtered = %d", len(p.filtered))
	}
	p.filter = "qwen"
	p.applyFilter()
	if len(p.filtered) != 1 {
		t.Fatalf("filtered after 'qwen' = %d", len(p.filtered))
	}
	if p.filtered[0].Name != "qwen.gguf" {
		t.Fatalf("filtered[0] = %q", p.filtered[0].Name)
	}
}
```

- [x] **Step 2: Run**

Run: `go test ./internal/ui/components/ -run TestModelPicker -v`
Expected: PASS, 3 sub-tests green.

- [x] **Step 3: Commit**

```bash
git add internal/ui/components/picker_test.go
git commit -m "test(ui/components/picker): selection, cancel, and filter coverage"
```

---

### Task 21: ProfilesPage `WithModelScanner` + `ctrl+p` opens picker

**Files:**
- Modify: `internal/ui/pages/profiles.go`

- [x] **Step 1: Adicionar campos e builder**

Em `internal/ui/pages/profiles.go`, dentro do struct `ProfilesPage`, adicionar:

```go
	// Picker overlay.
	pickerActive bool
	picker       components.ModelPicker
	scanner      components.ModelScanner
	scanPaths    []string
```

E adicionar import:

```go
	"github.com/quantmind-br/model-loader/internal/ui/components"
```

E após `NewProfilesPage`, adicionar builder:

```go
// WithModelScanner enables the ctrl+p model picker overlay in the editor.
func (p ProfilesPage) WithModelScanner(scanner components.ModelScanner, paths []string) ProfilesPage {
	p.scanner = scanner
	p.scanPaths = paths
	return p
}
```

- [x] **Step 2: Adicionar `updatePicker` helper**

Em `internal/ui/pages/profiles.go`, adicionar:

```go
func (p ProfilesPage) updatePicker(msg tea.Msg) (tea.Model, tea.Cmd) {
	picker, cmd := p.picker.Update(msg)
	p.picker = picker
	return p, cmd
}
```

- [x] **Step 3: Rotear mensagens do picker e ctrl+p no `Update` principal**

Em `internal/ui/pages/profiles.go`, no método `Update`, adicionar antes do `case tea.KeyMsg`:

```go
	case components.PickerScanStartedMsg, components.PickerScanEventMsg, components.PickerScanClosedMsg:
		if p.pickerActive {
			return p.updatePicker(msg)
		}
		return p, nil

	case components.ModelPickedMsg:
		p.draft.Model = msg.Path
		p.pickerActive = false
		if p.picker.Cancel() != nil {
			// Cancel scan goroutine if still running.
			p.picker.Cancel()
		}
		// Rebuild form so the new Model value is shown if user is on
		// essentials sub-tab.
		p.form = buildEditorForm(&p.draft, p.schema)
		return p, p.form.Init()

	case components.ModelPickerCancelledMsg:
		p.pickerActive = false
		return p, nil
```

E em `updateForm`, no início (antes do `if msg.String() == "esc"`):

```go
	if p.pickerActive {
		return p.updatePicker(msg)
	}
	if msg.String() == "ctrl+p" && p.scanner != nil {
		p.picker = components.NewModelPicker(p.scanner, p.scanPaths)
		p.pickerActive = true
		return p, p.picker.Init()
	}
```

- [x] **Step 4: Adicionar `Cancel()` accessor no picker**

Em `internal/ui/components/picker.go`, adicionar método:

```go
// Cancel returns the cancel func for the in-flight scan (or nil).
// Callers may invoke it to abort the scan when closing the picker.
func (m ModelPicker) Cancel() context.CancelFunc {
	return m.cancel
}
```

Substituir o handler de `ModelPickedMsg` em profiles.go para chamar `Cancel()` corretamente:

```go
	case components.ModelPickedMsg:
		p.draft.Model = msg.Path
		p.pickerActive = false
		if c := p.picker.Cancel(); c != nil {
			c()
		}
		p.form = buildEditorForm(&p.draft, p.schema)
		return p, p.form.Init()
```

(Substituir o handler do Step 3 por este — o do Step 3 referenciava `p.picker.Cancel()` como método null check, que era inválido.)

- [x] **Step 5: Render picker overlay no `View`**

Em `internal/ui/pages/profiles.go`, no método `View`, ANTES do bloco `if p.editing`:

```go
	if p.pickerActive {
		return p.picker.View()
	}
```

- [x] **Step 6: Atualizar hint do header**

Em `internal/ui/pages/profiles.go`, substituir:

```go
		header := theme.Title.Render(fmt.Sprintf("Editor — [%s]   ctrl+t to switch", p.subTab))
```

por:

```go
		header := theme.Title.Render(fmt.Sprintf("Editor — [%s]   ctrl+t to switch  ctrl+p to pick model", p.subTab))
```

- [x] **Step 7: Compilar e rodar testes existentes**

Run: `go build ./... && go test ./...`
Expected: PASS (existing profiles_test still passes; picker not yet exercised in tests).

- [x] **Step 8: Commit**

```bash
git add internal/ui/pages/profiles.go internal/ui/pages/profiles_editor.go
git commit -m "feat(ui/pages/profiles): integrate model picker via ctrl+p"
```

---

### Task 22: ProfilesPage picker integration test

**Files:**
- Modify: `internal/ui/pages/profiles_test.go`

- [x] **Step 1: Adicionar teste**

Primeiro, **substituir o import block** no topo de `internal/ui/pages/profiles_test.go` para incluir `context` e `components`. Bloco final:

```go
import (
	"context"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/exp/teatest"

	"github.com/quantmind-br/model-loader/internal/domain"
	"github.com/quantmind-br/model-loader/internal/service/profilestore"
	"github.com/quantmind-br/model-loader/internal/ui/components"
)
```

Em seguida, **append** ao final de `internal/ui/pages/profiles_test.go`:

```go
type stubScanner struct{}

func (stubScanner) Scan(ctx context.Context, paths []string) (<-chan domain.ScanEvent, error) {
	ch := make(chan domain.ScanEvent, 1)
	close(ch)
	return ch, nil
}

func TestProfilesPage_PickerWritesDraftModel(t *testing.T) {
	dir := t.TempDir()
	store, err := profilestore.NewFSStore(dir)
	if err != nil {
		t.Fatal(err)
	}
	page := NewProfilesPage(store, domain.FlagSchema{}).WithModelScanner(stubScanner{}, nil)

	// Start a new draft so editing is active.
	model, _ := page.startNew()
	page = model.(ProfilesPage)
	page.editing = true

	// Simulate ModelPickedMsg landing in Update.
	updated, _ := page.Update(components.ModelPickedMsg{Path: "/picked/model.gguf"})
	page = updated.(ProfilesPage)

	if page.draft.Model != "/picked/model.gguf" {
		t.Fatalf("draft.Model = %q, want /picked/model.gguf", page.draft.Model)
	}
	if page.pickerActive {
		t.Errorf("pickerActive = true, want false after pick")
	}
}
```

- [x] **Step 2: Run**

Run: `go test ./internal/ui/pages/ -run TestProfilesPage_PickerWritesDraftModel -v`
Expected: PASS.

- [x] **Step 3: Commit**

```bash
git add internal/ui/pages/profiles_test.go
git commit -m "test(ui/pages/profiles): picker selection writes draft model"
```

---

### Task 23: ModelsPage Enter → action submenu

**Files:**
- Modify: `internal/ui/pages/models.go`

- [x] **Step 1: Adicionar action submenu**

Adicionar campos a `ModelsPage` struct:

```go
	actionForm   *huh.Form
	actionAnswer *string
	actionPath   string // path of the file the action submenu targets
```

Adicionar import: `"github.com/charmbracelet/huh"`.

Adicionar handler `enter`:

Em `handleKey`, antes do `default` final (o `t, cmd := p.table.Update(msg)`), adicionar:

```go
	case key.Matches(msg, p.keys.Enter):
		if len(p.table.Rows()) == 0 {
			return p, nil
		}
		idx := p.table.Cursor()
		if idx < 0 {
			return p, nil
		}
		// Map row idx to file via filtered ordering. Recompute filtered
		// list to match what's displayed.
		visible := p.visibleFiles()
		if idx >= len(visible) {
			return p, nil
		}
		selected := visible[idx]
		answer := ""
		p.actionAnswer = &answer
		p.actionPath = selected.Path
		p.actionForm = huh.NewForm(huh.NewGroup(
			huh.NewSelect[string]().
				Title("Action for "+selected.Name).
				Options(
					huh.NewOption("Use in new profile", "new"),
					huh.NewOption("Reveal path", "reveal"),
				).
				Value(p.actionAnswer),
		)).WithShowHelp(false).WithShowErrors(false)
		return p, p.actionForm.Init()
```

E adicionar helper:

```go
func (p ModelsPage) visibleFiles() []domain.ModelFile {
	files := p.files
	if p.filter != "" {
		q := strings.ToLower(p.filter)
		filtered := make([]domain.ModelFile, 0, len(files))
		for _, f := range files {
			if strings.Contains(strings.ToLower(f.Name), q) {
				filtered = append(filtered, f)
			}
		}
		files = filtered
	}
	sort.Slice(files, func(i, j int) bool { return files[i].Name < files[j].Name })
	return files
}
```

E refatorar `refreshRows` para usar `visibleFiles`:

```go
func (p *ModelsPage) refreshRows() {
	files := p.visibleFiles()
	rows := make([]table.Row, 0, len(files))
	for _, f := range files {
		rows = append(rows, table.Row{
			truncate(f.Name, 36),
			humanSize(f.SizeBytes),
			f.Quant,
			f.Params,
			truncate(f.Path, 40),
		})
	}
	p.table.SetRows(rows)
}
```

- [x] **Step 2: Compilar (não vai passar pois actionForm não tem driver)**

Run: `go build ./...`
Expected: PASS.

- [x] **Step 3: Commit (parcial — driver vem na Task 24)**

```bash
git add internal/ui/pages/models.go
git commit -m "feat(ui/pages/models): action submenu on row enter"
```

---

### Task 24: Drive action submenu + emit `UseInNewProfileMsg`

**Files:**
- Modify: `internal/ui/pages/models.go`

- [x] **Step 1: Definir mensagem cross-tab + drive form**

Append a `internal/ui/pages/models.go`:

```go
// UseInNewProfileMsg requests creating a new profile pre-filled with Path.
// Root catches this message, switches to the Profiles tab, and forwards
// it so ProfilesPage starts a new draft.
type UseInNewProfileMsg struct {
	Path string
}
```

Modificar `Update` para drenar form em curso:

Em `Update`, antes do `case scanEventMsg`:

```go
	case actionFormDoneMsg:
		// Action form completed; consume answer.
		path := p.actionPath
		ans := ""
		if p.actionAnswer != nil {
			ans = *p.actionAnswer
		}
		p.actionForm = nil
		p.actionAnswer = nil
		p.actionPath = ""
		switch ans {
		case "new":
			return p, func() tea.Msg { return UseInNewProfileMsg{Path: path} }
		case "reveal":
			p.flash = path
			return p, nil
		}
		return p, nil
```

Forward keystrokes to action form. Em `Update`, dentro do `case tea.KeyMsg` (no início do método handleKey ou antes do early-returns), adicionar:

Modificar `Update`:

```go
	case tea.KeyMsg:
		if p.actionForm != nil {
			if msg.String() == "esc" {
				p.actionForm = nil
				p.actionAnswer = nil
				p.actionPath = ""
				return p, nil
			}
			updated, cmd := p.actionForm.Update(msg)
			if f, ok := updated.(*huh.Form); ok {
				p.actionForm = f
			}
			if p.actionForm != nil && p.actionForm.State == huh.StateCompleted {
				return p.Update(actionFormDoneMsg{})
			}
			return p, cmd
		}
		return p.handleKey(msg)
```

E definir o sentinel:

```go
type actionFormDoneMsg struct{}
```

Renderizar formulário no `View` quando ativo:

Em `View`, no início:

```go
	if p.actionForm != nil {
		return p.actionForm.View()
	}
```

- [x] **Step 2: Compilar**

Run: `go build ./...`
Expected: PASS.

- [x] **Step 3: Adicionar teste**

Append a `internal/ui/pages/models_test.go`:

```go
func TestModelsPage_ActionUseInNewProfileEmitsMsg(t *testing.T) {
	mf := domain.ModelFile{Path: "/m/q.gguf", Name: "q.gguf"}
	page := NewModelsPage(&fakeScanner{}, []string{"/m"})
	page.files = []domain.ModelFile{mf}
	page.refreshRows()

	// Inject "new" answer and call the form-done handler directly.
	answer := "new"
	page.actionAnswer = &answer
	page.actionPath = mf.Path

	updated, cmd := page.Update(actionFormDoneMsg{})
	_ = updated
	if cmd == nil {
		t.Fatal("expected UseInNewProfileMsg cmd, got nil")
	}
	got := cmd()
	useMsg, ok := got.(UseInNewProfileMsg)
	if !ok {
		t.Fatalf("msg type = %T, want UseInNewProfileMsg", got)
	}
	if useMsg.Path != "/m/q.gguf" {
		t.Fatalf("Path = %q", useMsg.Path)
	}
}
```

- [x] **Step 4: Run**

Run: `go test ./internal/ui/pages/ -run TestModelsPage_ActionUseInNewProfileEmitsMsg -v`
Expected: PASS.

- [x] **Step 5: Commit**

```bash
git add internal/ui/pages/models.go internal/ui/pages/models_test.go
git commit -m "feat(ui/pages/models): drive action submenu, emit cross-tab msg"
```

---

### Task 25: ProfilesPage handles `UseInNewProfileMsg`

**Files:**
- Modify: `internal/ui/pages/profiles.go`
- Modify: `internal/ui/pages/profiles_test.go`

- [x] **Step 1: Adicionar handler no Update**

Em `internal/ui/pages/profiles.go`, no método `Update`, adicionar antes do `case components.ModelPickedMsg`:

```go
	case UseInNewProfileMsg:
		// Open a new draft pre-filled with the selected model path.
		p.draft = profileDraft{
			ID:         "",
			Name:       "New Profile",
			Model:      msg.Path,
			NGL:        "99",
			CtxSize:    "8192",
			BatchSize:  "2048",
			UBatchSize: "512",
			Port:       "8080",
			FlashAttn:  "auto",
			CacheTypeK: "q8_0",
			CacheTypeV: "q8_0",
			isNew:      true,
		}
		p.form = buildEditorForm(&p.draft, p.schema)
		p.editing = true
		p.flash = "new profile prefilled with picked model"
		return p, p.form.Init()
```

- [x] **Step 2: Adicionar teste**

Append a `internal/ui/pages/profiles_test.go`:

```go
func TestProfilesPage_UseInNewProfilePrefillsDraft(t *testing.T) {
	dir := t.TempDir()
	store, err := profilestore.NewFSStore(dir)
	if err != nil {
		t.Fatal(err)
	}
	page := NewProfilesPage(store, domain.FlagSchema{})

	updated, _ := page.Update(UseInNewProfileMsg{Path: "/foo/bar.gguf"})
	page = updated.(ProfilesPage)

	if !page.editing {
		t.Fatal("page.editing = false, want true")
	}
	if page.draft.Model != "/foo/bar.gguf" {
		t.Fatalf("draft.Model = %q", page.draft.Model)
	}
	if !page.draft.isNew {
		t.Errorf("isNew = false, want true")
	}
}
```

- [x] **Step 3: Run**

Run: `go test ./internal/ui/pages/ -run TestProfilesPage_UseInNewProfilePrefillsDraft -v`
Expected: PASS.

- [x] **Step 4: Commit**

```bash
git add internal/ui/pages/profiles.go internal/ui/pages/profiles_test.go
git commit -m "feat(ui/pages/profiles): handle UseInNewProfileMsg cross-tab"
```

---

### Task 26: Root tab routing for `UseInNewProfileMsg`

**Files:**
- Modify: `internal/ui/root.go`

- [x] **Step 1: Adicionar `WithModelsPage` builder + intercept message**

Em `internal/ui/root.go`, adicionar builder após `WithProfilesPage`:

```go
// WithModelsPage replaces the placeholder Models tab with a real model.
func (m RootModel) WithModelsPage(p tea.Model) RootModel {
	m.pages[TabModels] = p
	return m
}
```

E em `Update`, antes do `case tea.KeyMsg`:

```go
	case pages.UseInNewProfileMsg:
		// Switch to Profiles tab and forward the message so the page
		// opens a pre-filled new draft.
		m.active = TabProfiles
		updated, cmd := m.pages[TabProfiles].Update(msg)
		m.pages[TabProfiles] = updated
		return m, cmd
```

- [x] **Step 2: Compilar**

Run: `go build ./...`
Expected: PASS.

- [x] **Step 3: Adicionar teste**

`internal/ui/root_test.go` já existe com imports para teatest. **Substituir o import block existente** pelo seguinte (mesclando):

```go
import (
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/exp/teatest"

	"github.com/quantmind-br/model-loader/internal/domain"
	"github.com/quantmind-br/model-loader/internal/service/profilestore"
	"github.com/quantmind-br/model-loader/internal/ui/pages"
)
```

Em seguida **append** ao final de `internal/ui/root_test.go`:

```go
func TestRoot_UseInNewProfileSwitchesTab(t *testing.T) {
	dir := t.TempDir()
	store, err := profilestore.NewFSStore(dir)
	if err != nil {
		t.Fatal(err)
	}
	root := NewRoot(TabModels).
		WithProfilesPage(pages.NewProfilesPage(store, domain.FlagSchema{}))

	updated, _ := root.Update(pages.UseInNewProfileMsg{Path: "/x.gguf"})
	r := updated.(RootModel)

	if r.active != TabProfiles {
		t.Errorf("active = %d, want TabProfiles=%d", r.active, TabProfiles)
	}
}
```

- [x] **Step 4: Run**

Run: `go test ./internal/ui/ -run TestRoot_UseInNewProfileSwitchesTab -v`
Expected: PASS.

- [x] **Step 5: Commit**

```bash
git add internal/ui/root.go internal/ui/root_test.go
git commit -m "feat(ui/root): WithModelsPage builder and UseInNewProfile routing"
```

---

### Task 27: Wire scanner in main.go

**Files:**
- Modify: `cmd/model-loader/main.go`

- [x] **Step 1: Construir scanner e injetar em ambas as pages**

Substituir o body de `main()` em `cmd/model-loader/main.go`:

```go
func main() {
	cfg, err := config.Load()
	if err != nil {
		fmt.Fprintf(os.Stderr, "config error: %v\n", err)
		os.Exit(1)
	}

	store, err := profilestore.NewFSStore(cfg.Paths.ProfilesDir)
	if err != nil {
		fmt.Fprintf(os.Stderr, "profile store: %v\n", err)
		os.Exit(1)
	}

	schema, schemaWarn := loadSchema()
	scanner := modelscanner.New()

	profilesPage := pages.NewProfilesPage(store, schema).
		WithModelScanner(scanner, cfg.Models.SearchPaths)
	modelsPage := pages.NewModelsPage(scanner, cfg.Models.SearchPaths)

	root := ui.NewRoot(parseTab(cfg.UI.DefaultTab)).
		WithProfilesPage(profilesPage).
		WithModelsPage(modelsPage)
	if schemaWarn != "" {
		root = root.WithStatusWarn(schemaWarn)
	}

	prog := tea.NewProgram(root, tea.WithAltScreen())
	if _, err := prog.Run(); err != nil {
		fmt.Fprintf(os.Stderr, "tui error: %v\n", err)
		os.Exit(1)
	}
}
```

E adicionar import:

```go
	"github.com/quantmind-br/model-loader/internal/service/modelscanner"
```

- [x] **Step 2: Compilar e rodar full suite**

Run: `go build ./... && go test ./...`
Expected: PASS — all packages build, all tests green.

- [x] **Step 3: Commit**

```bash
git add cmd/model-loader/main.go
git commit -m "feat(cmd): wire ModelScanner into ProfilesPage and ModelsPage"
```

---

### Task 28: End-to-end smoke

**Files:** none (manual verification + final test sweep)

- [x] **Step 1: Rodar suíte completa**

Run: `go test ./... -count=1`
Expected: PASS in all packages, no skipped tests, no race warnings.

- [x] **Step 2: Verificar compilação do binário**

Run: `go build -o /tmp/model-loader ./cmd/model-loader && ls -la /tmp/model-loader`
Expected: binary built successfully.

- [ ] **Step 3: Smoke launch (opcional, manual)** *(skipped — optional sanity-check, automated suite already green)*

Run: `/tmp/model-loader` from a terminal — confirm:
- Tab "Models" visível
- Models tab mostra paths configurados com status
- Em Profiles, criar new (n), ctrl+p abre picker
- Picker com esc volta para editor
- Em Models, enter abre menu de ações

(If running headless, skip; CI cannot drive a TTY. The unit + page tests cover state transitions.)

- [x] **Step 4: Cleanup binary**

Run: `rm -f /tmp/model-loader`
Expected: file removed.

(No commit — this task is a verification gate.)

---

## Self-Review Checklist (run after writing the plan)

**1. Spec coverage:**

| Spec § | Requirement | Task |
|--------|-------------|------|
| 6.5 | `Scanner.Scan(ctx, paths) → channel` | T2, T7 |
| 6.5 | Walk recursivo + .gguf filter | T7, T8 |
| 6.5 | Parse mínimo GGUF header (magic + version + first metadata) | T5, T6 |
| 6.5 | Header inválido → ModelFile só com path/size/nome | T7 (`readParamsFromFile` retorna "" em erro) |
| 6.5 | ctx cancel | T11 |
| 7.2 modelsPage | Top: status paths configurados | T15 |
| 7.2 modelsPage | Tabela name/size/quant/params/path | T12, T14 |
| 7.2 modelsPage | Filtro `/` | T16 |
| 7.2 modelsPage | Enter ações: New profile / Reveal path | T23, T24 |
| 7.2 modelsPage | `R` rescan | T16 |
| 7.3 F1 step 2 | Picker integrado no editor | T18, T19, T21 (com deviation: ctrl+p) |

Spec deviation: "Use in existing → pick" não está implementado. Justificativa: requer profile-picker overlay (yet another picker component) + edit-flow rewrite — escopo demasiado para slice 3. Documentado como follow-up para slice 4 polish ou slice 6.

**2. Placeholder scan:** Nenhum "TBD"/"implement later". Cada step contém código completo ou comando exato.

**3. Type consistency:**
- `ModelFile{Path, SizeBytes, Name, Quant, Params}` consistente em T1, T7, T12, T14.
- `ScanEvent{Type, Root, File, Count, Error}` consistente em T1, T7, T9, T10, T13, T14.
- `ModelPickedMsg{Path}`, `ModelPickerCancelledMsg{}`, `UseInNewProfileMsg{Path}` consistentes em T18-T26.
- `ModelsPage.refreshRows()` e `ModelsPage.visibleFiles()` definidos em T14 e refatorados em T23 — conferido OK.

---

## Execution Handoff

Plan complete and saved to `docs/superpowers/plans/2026-04-28-model-loader-slice-3.md`.

**Two execution options:**

**1. Subagent-Driven (recommended)** — Eu despacho um implementer subagent por task com revisão de spec compliance + code quality entre tasks. Mesmo fluxo do slice 2.

**2. Inline Execution** — Executo as tasks nesta sessão em batches com checkpoints.

Which approach?
