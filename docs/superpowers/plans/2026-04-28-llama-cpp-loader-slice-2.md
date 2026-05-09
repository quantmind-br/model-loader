# llama.cpp-loader — Plano Slice 2 (Help Parser + Validator + Editor Tabs)

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Tornar o editor de profiles version-aware. Parser de `llama-server --help` popula `domain.FlagSchema` em memória; quando indisponível, schema embutido garante operação. `Validator` aplica regras tipo/range/cross-field e renderiza erros/warnings inline no editor. `profilesPage` ganha sub-tabs `Essentials` (13 campos curados) e `Advanced` (tabela completa do schema com filtro). Ao final do slice 2, criar/editar profile mostra feedback de validação em tempo real e o usuário consegue tunar qualquer flag do binário instalado sem editar JSON manualmente.

**Architecture:** Mantém a hierarquia `cmd → ui → service → domain` estabelecida no slice 0+1. Acrescenta dois novos services puros (`llamahelp`, `validator`) e duas componentes de UI (sub-tabs Essentials/Advanced). O parser tem duas faces: `ParseHelp([]byte) FlagSchema` é puro (testável com fixture), `ExecParser` envolve o pure-parser em `os/exec`. Fallback embutido vive em `embedded.go` e cobre apenas os 13 essentials, o suficiente para criar/editar profile mesmo sem binário.

**Tech Stack:** Mesmo do slice 0+1. Sem deps novas — tudo `regexp`, `strings`, `bufio`, `os/exec` da std lib + bibliotecas já presentes (huh, bubbles, lipgloss).

**Convenções importantes para o executor:**

- TDD obrigatório nos services puros (`llamahelp`, `validator`). UI tem teatest seletivo.
- Commits frequentes — um por task quando verde. Mensagem em inglês: `feat:`/`refactor:`/`test:`/`chore:`/`docs:`.
- Idioma do código e strings de UI: **en-US**. Plano e spec em pt-BR.
- Working directory: `/home/diogo/dev/llama.cpp-loader`.
- `go build ./...` e `go test ./...` antes de cada commit. Se a task só toca um package, basta `go test ./internal/service/llamahelp/...`.
- Não modifique tasks fora da sua. Se encontrar bug em código de slice anterior, anote no commit message mas não corrija fora do escopo.
- Convenção de chave em `Profile.Args`: o editor essentials reusa as chaves já gravadas no slice 1 (`ngl`, `ctx-size`, `port`, `flash-attn`). O launcher (slice 4) será responsável por traduzir chave → flag CLI usando o `FlagSchema`. Não é objetivo deste slice migrar profiles existentes.

---

## File Structure

```
llama.cpp-loader/
├── testdata/
│   ├── help-v7376.txt                                  (T1, fixture)
│   ├── help-v7376.golden.json                          (T11, golden)
│   └── fake-llama-help.sh                              (T12, exec test)
├── internal/
│   ├── domain/
│   │   └── flag_schema.go                              (T2, enriquecer)
│   ├── service/
│   │   ├── llamahelp/
│   │   │   ├── llamahelp.go                            (T2, types+interface)
│   │   │   ├── parser.go                               (T3-T11, pure parser)
│   │   │   ├── parser_test.go                          (T3-T11)
│   │   │   ├── exec_parser.go                          (T12)
│   │   │   ├── exec_parser_test.go                     (T12)
│   │   │   ├── embedded.go                             (T13)
│   │   │   └── embedded_test.go                        (T13)
│   │   └── validator/
│   │       ├── validator.go                            (T14)
│   │       ├── validator_test.go                       (T14-T17)
│   │       └── rules.go                                (T15-T17)
│   ├── ui/
│   │   ├── pages/
│   │   │   ├── profiles.go                             (T18-T25, modificado)
│   │   │   ├── profiles_editor.go                      (T18, extracted)
│   │   │   ├── profiles_editor_test.go                 (T20-T22)
│   │   │   └── profiles_test.go                        (T25, atualizado)
│   │   └── root.go                                     (T25, schema thread-through)
│   └── service/profilestore/                            (sem alterações)
└── cmd/model-loader/
    └── main.go                                          (T26, parser+fallback boot)
```

**Princípios de boundary:**

- `parser.go` é puro: recebe `[]byte` ou `string`, devolve `FlagSchema`. Sem `os/exec`.
- `embedded.go` é compile-time literal — sem ler arquivo no runtime.
- `validator.go` é puro: recebe `Profile` + `FlagSchema`, devolve `Report`. Sem I/O exceto `os.Stat` para path do model em `rules.go` (limite explícito documentado).
- UI nunca chama `os/exec`/`os.Stat` direto: tudo via service injetado no boot.

---

## Phase A — Help parser + embedded fallback (12 tasks)

### Task 1: Capturar fixture `--help` real

**Files:**
- Create: `testdata/help-v7376.txt`

- [ ] **Step 1: Capturar saída real de `llama-server --help`**

```bash
mkdir -p testdata
llama-server --help 2>&1 > testdata/help-v7376.txt
```

- [ ] **Step 2: Verificar que o arquivo tem >400 linhas e contém section headers esperados**

```bash
wc -l testdata/help-v7376.txt
grep -E "^----- " testdata/help-v7376.txt
```

Esperado: ≥400 linhas; ao menos 3 headers (`common params`, `sampling params`, `example-specific params`).

- [ ] **Step 3: Commit**

```bash
git add testdata/help-v7376.txt
git commit -m "test(llamahelp): capture llama-server --help fixture for v7376"
```

---

### Task 2: Enriquecer `domain.FlagSchema` + skeleton do package `llamahelp`

**Files:**
- Modify: `internal/domain/flag_schema.go`
- Create: `internal/service/llamahelp/llamahelp.go`

- [ ] **Step 1: Adicionar `Aliases` ao `FlagSpec` e helper `Lookup` ao `FlagSchema`**

Substitua `internal/domain/flag_schema.go` por:

```go
package domain

// FlagType enumerates the supported llama-server flag value types.
type FlagType int

const (
	FlagTypeBool FlagType = iota
	FlagTypeInt
	FlagTypeFloat
	FlagTypeString
	FlagTypeEnum
)

// FlagSpec describes a single llama-server flag.
type FlagSpec struct {
	Long       string   // canonical long name (without leading --), e.g. "ctx-size"
	Short      string   // first short alias (without leading -), e.g. "c"; "" if absent
	Aliases    []string // additional long aliases (without leading --)
	Type       FlagType
	EnumValues []string
	Default    any
	HelpText   string
	Group      string // "common" | "sampling" | "example-specific" | "embedded"
}

// FlagSchema is the parsed --help output keyed by long name.
type FlagSchema struct {
	Version string
	Flags   map[string]FlagSpec
}

// Lookup resolves a name (long, alias, or short) to a FlagSpec.
// Returns the spec and true on hit.
func (s FlagSchema) Lookup(name string) (FlagSpec, bool) {
	if spec, ok := s.Flags[name]; ok {
		return spec, true
	}
	for _, spec := range s.Flags {
		if spec.Short == name {
			return spec, true
		}
		for _, alias := range spec.Aliases {
			if alias == name {
				return spec, true
			}
		}
	}
	return FlagSpec{}, false
}
```

- [ ] **Step 2: Criar package skeleton `internal/service/llamahelp/llamahelp.go`**

```go
// Package llamahelp parses llama-server --help into a FlagSchema and supplies
// an embedded fallback when the binary is unavailable.
package llamahelp

import (
	"context"

	"github.com/quantmind-br/model-loader/internal/domain"
)

// Parser exposes schema discovery against a real llama-server binary.
type Parser interface {
	Parse(ctx context.Context) (domain.FlagSchema, error)
	DetectVersion(ctx context.Context) (string, error)
}
```

- [ ] **Step 3: Build**

```bash
go build ./...
```

Esperado: zero erros.

- [ ] **Step 4: Commit**

```bash
git add internal/domain/flag_schema.go internal/service/llamahelp/llamahelp.go
git commit -m "feat(domain,llamahelp): add Aliases + Lookup; add llamahelp package skeleton"
```

---

### Task 3: Pure parser — section headers

**Files:**
- Create: `internal/service/llamahelp/parser.go`
- Create: `internal/service/llamahelp/parser_test.go`

- [ ] **Step 1: Escrever teste table-driven para `parseSectionHeader`**

```go
package llamahelp

import "testing"

func TestParseSectionHeader(t *testing.T) {
	cases := []struct {
		line string
		want string
	}{
		{"----- common params -----", "common"},
		{"----- sampling params -----", "sampling"},
		{"----- example-specific params -----", "example-specific"},
		{"   ----- weird params -----   ", "weird"},
		{"-c,    --ctx-size N", ""},
		{"", ""},
		{"-----", ""},
	}
	for _, tc := range cases {
		t.Run(tc.line, func(t *testing.T) {
			got := parseSectionHeader(tc.line)
			if got != tc.want {
				t.Fatalf("parseSectionHeader(%q) = %q, want %q", tc.line, got, tc.want)
			}
		})
	}
}
```

- [ ] **Step 2: Rodar — falha esperada (`parseSectionHeader undefined`)**

```bash
go test ./internal/service/llamahelp/ -run TestParseSectionHeader
```

- [ ] **Step 3: Implementar em `parser.go`**

```go
package llamahelp

import (
	"regexp"
	"strings"
)

var sectionHeaderRe = regexp.MustCompile(`^-{5}\s+(.+?)\s+params\s+-{5}$`)

// parseSectionHeader returns the section name (e.g., "common") for a header
// line, or "" if the line is not a section header.
func parseSectionHeader(line string) string {
	trimmed := strings.TrimSpace(line)
	m := sectionHeaderRe.FindStringSubmatch(trimmed)
	if m == nil {
		return ""
	}
	return m[1]
}
```

- [ ] **Step 4: Rodar testes — verde**

```bash
go test ./internal/service/llamahelp/ -run TestParseSectionHeader -v
```

- [ ] **Step 5: Commit**

```bash
git add internal/service/llamahelp/parser.go internal/service/llamahelp/parser_test.go
git commit -m "feat(llamahelp): parse section headers"
```

---

### Task 4: Pure parser — flag line: short + long + placeholder

**Files:**
- Modify: `internal/service/llamahelp/parser.go`
- Modify: `internal/service/llamahelp/parser_test.go`

Estratégia: uma função `parseFlagLine(line string) (FlagSpec, bool)` table-driven cobrindo todas as variações de entrada. Esta task adiciona o caso mais comum: `-x, --long PLACEHOLDER  description (default: ...)`.

- [ ] **Step 1: Adicionar teste para flag line canônica**

Acrescente em `parser_test.go`:

```go
import (
	"reflect"

	"github.com/quantmind-br/model-loader/internal/domain"
)

func TestParseFlagLine_ShortLongPlaceholder(t *testing.T) {
	cases := []struct {
		name string
		line string
		want domain.FlagSpec
	}{
		{
			name: "ctx-size with N placeholder and default",
			line: "-c,    --ctx-size N                     size of the prompt context (default: 4096, 0 = loaded from model)",
			want: domain.FlagSpec{
				Long:     "ctx-size",
				Short:    "c",
				Type:     domain.FlagTypeInt,
				Default:  4096,
				HelpText: "size of the prompt context (default: 4096, 0 = loaded from model)",
			},
		},
		{
			name: "batch-size",
			line: "-b,    --batch-size N                   logical maximum batch size (default: 2048)",
			want: domain.FlagSpec{
				Long:     "batch-size",
				Short:    "b",
				Type:     domain.FlagTypeInt,
				Default:  2048,
				HelpText: "logical maximum batch size (default: 2048)",
			},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, ok := parseFlagLine(tc.line)
			if !ok {
				t.Fatalf("parseFlagLine returned !ok for %q", tc.line)
			}
			if !reflect.DeepEqual(got, tc.want) {
				t.Fatalf("got %+v, want %+v", got, tc.want)
			}
		})
	}
}
```

- [ ] **Step 2: Rodar — falha esperada (`parseFlagLine undefined`)**

```bash
go test ./internal/service/llamahelp/ -run TestParseFlagLine_ShortLongPlaceholder
```

- [ ] **Step 3: Implementar `parseFlagLine` em `parser.go`**

Acrescente:

```go
// flagLineRe matches the canonical "<aliases>  <description>" layout.
// Group 1 = alias chunk (left), Group 2 = description chunk (right).
// Two or more spaces separate the alias chunk from the description.
var flagLineRe = regexp.MustCompile(`^(-[^ ]+(?:,\s*-[^ ]+)*)\s{2,}(\S.*)$`)

// defaultRe extracts "(default: X)" — first occurrence wins.
var defaultRe = regexp.MustCompile(`\(default:\s*([^),]+)\)`)

// parseFlagLine parses a single help line into a FlagSpec. Returns false when
// the line is not a flag definition (header, blank, continuation).
func parseFlagLine(line string) (domain.FlagSpec, bool) {
	if strings.HasPrefix(strings.TrimSpace(line), "(env:") {
		return domain.FlagSpec{}, false
	}
	m := flagLineRe.FindStringSubmatch(strings.TrimRight(line, " "))
	if m == nil {
		return domain.FlagSpec{}, false
	}
	aliasChunk, descChunk := m[1], strings.TrimSpace(m[2])

	short, longs, placeholder := splitAliases(aliasChunk)
	if len(longs) == 0 {
		return domain.FlagSpec{}, false
	}

	spec := domain.FlagSpec{
		Long:     longs[len(longs)-1],
		Short:    short,
		HelpText: descChunk,
	}
	if len(longs) > 1 {
		spec.Aliases = longs[:len(longs)-1]
	}
	spec.Type = inferType(placeholder)
	if d := extractDefault(descChunk); d != nil {
		spec.Default = coerceDefault(spec.Type, d)
	}
	return spec, true
}

// splitAliases parses "-c, --ctx-size N" → short="c", longs=["ctx-size"], placeholder="N".
// Multi-alias: "-ngl, --gpu-layers, --n-gpu-layers N" → short="ngl",
// longs=["gpu-layers","n-gpu-layers"], placeholder="N".
// The last whitespace-delimited token in the alias chunk may be a placeholder
// such as "N", "TYPE", "[on|off|auto]", or "{a,b,c}". If the last token starts
// with '-', there is no placeholder.
func splitAliases(chunk string) (short string, longs []string, placeholder string) {
	parts := strings.Fields(chunk)
	if len(parts) == 0 {
		return "", nil, ""
	}
	// Detect placeholder: last token without leading '-'.
	last := parts[len(parts)-1]
	if !strings.HasPrefix(last, "-") {
		placeholder = last
		parts = parts[:len(parts)-1]
	}
	for _, p := range parts {
		p = strings.TrimSuffix(p, ",")
		if strings.HasPrefix(p, "--") {
			longs = append(longs, strings.TrimPrefix(p, "--"))
		} else if strings.HasPrefix(p, "-") && short == "" {
			short = strings.TrimPrefix(p, "-")
		}
	}
	return short, longs, placeholder
}

// inferType maps the placeholder token to a FlagType. Defaults to FlagTypeBool
// when the placeholder is empty (no value expected).
func inferType(placeholder string) domain.FlagType {
	if placeholder == "" {
		return domain.FlagTypeBool
	}
	// Will be enriched in later tasks (enums, floats, special tokens).
	return domain.FlagTypeInt
}

// extractDefault pulls the first "(default: X)" payload from the description.
// Returns nil if absent.
func extractDefault(desc string) any {
	m := defaultRe.FindStringSubmatch(desc)
	if m == nil {
		return nil
	}
	return strings.TrimSpace(m[1])
}

// coerceDefault converts the raw default string to the FlagSpec's typed value.
// Falls back to the original string on parse failure.
func coerceDefault(t domain.FlagType, raw any) any {
	s, ok := raw.(string)
	if !ok {
		return raw
	}
	switch t {
	case domain.FlagTypeInt:
		var n int
		if _, err := fmt.Sscanf(s, "%d", &n); err == nil {
			return n
		}
	case domain.FlagTypeBool:
		switch strings.ToLower(s) {
		case "true", "yes", "1":
			return true
		case "false", "no", "0":
			return false
		}
	}
	return s
}
```

Adicione `"fmt"` ao bloco de imports de `parser.go`.

- [ ] **Step 4: Rodar — verde**

```bash
go test ./internal/service/llamahelp/ -v
```

- [ ] **Step 5: Commit**

```bash
git add internal/service/llamahelp/parser.go internal/service/llamahelp/parser_test.go
git commit -m "feat(llamahelp): parse short+long flag lines with int placeholders"
```

---

### Task 5: Pure parser — long-only flag with placeholder

**Files:**
- Modify: `internal/service/llamahelp/parser_test.go`

Padrão: `--port PORT                             port to listen (default: 8080)`.

- [ ] **Step 1: Adicionar teste**

Acrescente em `TestParseFlagLine_ShortLongPlaceholder` (ou crie nova função):

```go
func TestParseFlagLine_LongOnlyPlaceholder(t *testing.T) {
	cases := []struct {
		name string
		line string
		want domain.FlagSpec
	}{
		{
			name: "port long-only",
			line: "--port PORT                             port to listen (default: 8080)",
			want: domain.FlagSpec{
				Long:     "port",
				Type:     domain.FlagTypeInt,
				Default:  8080,
				HelpText: "port to listen (default: 8080)",
			},
		},
		{
			name: "keep long-only",
			line: "--keep N                                number of tokens to keep from the initial prompt (default: 0, -1 = all)",
			want: domain.FlagSpec{
				Long:     "keep",
				Type:     domain.FlagTypeInt,
				Default:  0,
				HelpText: "number of tokens to keep from the initial prompt (default: 0, -1 = all)",
			},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, ok := parseFlagLine(tc.line)
			if !ok {
				t.Fatalf("parseFlagLine returned !ok for %q", tc.line)
			}
			if !reflect.DeepEqual(got, tc.want) {
				t.Fatalf("got %+v, want %+v", got, tc.want)
			}
		})
	}
}
```

- [ ] **Step 2: Rodar — espera passar (regex já tolera ausência de short)**

```bash
go test ./internal/service/llamahelp/ -run TestParseFlagLine_LongOnlyPlaceholder -v
```

Se passar de primeira: ótimo, o regex `^(-[^ ]+(?:,\s*-[^ ]+)*)` já casa `--port` (que começa com `-`). Se falhar: ajustar regex para permitir o caso long-only sem vírgula.

- [ ] **Step 3: Commit**

```bash
git add internal/service/llamahelp/parser_test.go
git commit -m "test(llamahelp): cover long-only flag lines"
```

---

### Task 6: Pure parser — bool flag (no placeholder)

**Files:**
- Modify: `internal/service/llamahelp/parser_test.go`
- Modify: `internal/service/llamahelp/parser.go`

Padrão: `--mlock                                 force system to keep model in RAM rather than swapping or compressing`.

- [ ] **Step 1: Adicionar teste**

```go
func TestParseFlagLine_BoolNoPlaceholder(t *testing.T) {
	cases := []struct {
		name string
		line string
		want domain.FlagSpec
	}{
		{
			name: "mlock",
			line: "--mlock                                 force system to keep model in RAM rather than swapping or compressing",
			want: domain.FlagSpec{
				Long:     "mlock",
				Type:     domain.FlagTypeBool,
				HelpText: "force system to keep model in RAM rather than swapping or compressing",
			},
		},
		{
			name: "swa-full bool",
			line: "--swa-full                              use full-size SWA cache (default: false)",
			want: domain.FlagSpec{
				Long:     "swa-full",
				Type:     domain.FlagTypeBool,
				Default:  false,
				HelpText: "use full-size SWA cache (default: false)",
			},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, ok := parseFlagLine(tc.line)
			if !ok {
				t.Fatalf("!ok for %q", tc.line)
			}
			if !reflect.DeepEqual(got, tc.want) {
				t.Fatalf("got %+v, want %+v", got, tc.want)
			}
		})
	}
}
```

- [ ] **Step 2: Rodar — espera falhar (`inferType("")` retorna Bool já, mas o `coerceDefault` para `swa-full` precisa funcionar: `(default: false)` é Bool, e o coerceDefault já trata)**

```bash
go test ./internal/service/llamahelp/ -run TestParseFlagLine_BoolNoPlaceholder -v
```

Provável falha: `splitAliases` para `--mlock` sem placeholder retorna `placeholder=""`, mas a regex anterior só casa quando existe whitespace ≥2 antes da descrição. Linhas bool têm muito espaço antes da descrição → regex já casa. Se passar: ótimo.

- [ ] **Step 3: Se algum caso falhar, ajustar**

Caso 1 — `--mlock`: o `inferType("")` retorna `FlagTypeBool` (correto). Caso `--swa-full`: idem, e `coerceDefault(Bool, "false")` retorna `false` (correto).

Se ambos passam de primeira, vá direto pro commit.

- [ ] **Step 4: Commit**

```bash
git add internal/service/llamahelp/parser_test.go
git commit -m "test(llamahelp): cover bool flags with no placeholder"
```

---

### Task 7: Pure parser — enum placeholders `[a|b|c]` e `{a,b,c}`

**Files:**
- Modify: `internal/service/llamahelp/parser.go`
- Modify: `internal/service/llamahelp/parser_test.go`

Padrões:
- `-fa,   --flash-attn [on|off|auto]       set Flash Attention use ('on', 'off', or 'auto', default: 'auto')`
- `-sm,   --split-mode {none,layer,row}    how to split...`

- [ ] **Step 1: Adicionar teste**

```go
func TestParseFlagLine_EnumPlaceholders(t *testing.T) {
	cases := []struct {
		name string
		line string
		want domain.FlagSpec
	}{
		{
			name: "flash-attn pipe enum",
			line: "-fa,   --flash-attn [on|off|auto]       set Flash Attention use ('on', 'off', or 'auto', default: 'auto')",
			want: domain.FlagSpec{
				Long:       "flash-attn",
				Short:      "fa",
				Type:       domain.FlagTypeEnum,
				EnumValues: []string{"on", "off", "auto"},
				Default:    "auto",
				HelpText:   "set Flash Attention use ('on', 'off', or 'auto', default: 'auto')",
			},
		},
		{
			name: "split-mode brace enum",
			line: "-sm,   --split-mode {none,layer,row}    how to split the model across multiple GPUs",
			want: domain.FlagSpec{
				Long:       "split-mode",
				Short:      "sm",
				Type:       domain.FlagTypeEnum,
				EnumValues: []string{"none", "layer", "row"},
				HelpText:   "how to split the model across multiple GPUs",
			},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, ok := parseFlagLine(tc.line)
			if !ok {
				t.Fatalf("!ok for %q", tc.line)
			}
			if !reflect.DeepEqual(got, tc.want) {
				t.Fatalf("got %+v, want %+v", got, tc.want)
			}
		})
	}
}
```

- [ ] **Step 2: Rodar — espera falhar (placeholder atual não detecta enum, e coerce de `'auto'` mantém aspas)**

```bash
go test ./internal/service/llamahelp/ -run TestParseFlagLine_EnumPlaceholders -v
```

- [ ] **Step 3: Atualizar `inferType` e adicionar `parseEnumPlaceholder`**

Em `parser.go`, substitua `inferType` e adicione `parseEnumPlaceholder`:

```go
var (
	bracketEnumRe = regexp.MustCompile(`^\[([^\]]+)\]$`)
	braceEnumRe   = regexp.MustCompile(`^\{([^}]+)\}$`)
)

// parseEnumPlaceholder returns the enum values when placeholder is "[a|b|c]"
// or "{a,b,c}". Otherwise returns nil.
func parseEnumPlaceholder(placeholder string) []string {
	if m := bracketEnumRe.FindStringSubmatch(placeholder); m != nil {
		return splitAndTrim(m[1], "|")
	}
	if m := braceEnumRe.FindStringSubmatch(placeholder); m != nil {
		return splitAndTrim(m[1], ",")
	}
	return nil
}

func splitAndTrim(s, sep string) []string {
	parts := strings.Split(s, sep)
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		out = append(out, strings.TrimSpace(p))
	}
	return out
}

// inferType maps the placeholder token to a FlagType.
func inferType(placeholder string) domain.FlagType {
	switch {
	case placeholder == "":
		return domain.FlagTypeBool
	case parseEnumPlaceholder(placeholder) != nil:
		return domain.FlagTypeEnum
	}
	// Fallback: scalar. Distinguishing int vs float vs string is best-effort
	// using common llama-server placeholders.
	switch placeholder {
	case "N", "INDEX", "PORT":
		return domain.FlagTypeInt
	case "F", "RATE":
		return domain.FlagTypeFloat
	}
	return domain.FlagTypeString
}
```

Em `parseFlagLine`, depois de calcular `spec.Type`, popule `EnumValues`:

```go
	spec.Type = inferType(placeholder)
	if spec.Type == domain.FlagTypeEnum {
		spec.EnumValues = parseEnumPlaceholder(placeholder)
	}
```

E em `coerceDefault`, adicionar caso enum (strip aspas simples):

```go
	case domain.FlagTypeEnum:
		return strings.Trim(s, "'\"")
```

- [ ] **Step 4: Rodar — verde**

```bash
go test ./internal/service/llamahelp/ -v
```

- [ ] **Step 5: Commit**

```bash
git add internal/service/llamahelp/parser.go internal/service/llamahelp/parser_test.go
git commit -m "feat(llamahelp): detect enum placeholders [a|b|c] and {a,b,c}"
```

---

### Task 8: Pure parser — TYPE placeholder com enum hardcoded (cache-type)

**Files:**
- Modify: `internal/service/llamahelp/parser.go`
- Modify: `internal/service/llamahelp/parser_test.go`

Padrão: `-ctk,  --cache-type-k TYPE              KV cache data type for K`. O help diz `TYPE`, mas o usuário precisa ver as opções. Hardcode após-parse.

- [ ] **Step 1: Adicionar teste**

```go
func TestParseFlagLine_CacheTypeHardcodedEnum(t *testing.T) {
	cases := []struct {
		name string
		line string
		want domain.FlagSpec
	}{
		{
			name: "cache-type-k",
			line: "-ctk,  --cache-type-k TYPE              KV cache data type for K",
			want: domain.FlagSpec{
				Long:       "cache-type-k",
				Short:      "ctk",
				Type:       domain.FlagTypeEnum,
				EnumValues: cacheTypeEnum,
				HelpText:   "KV cache data type for K",
			},
		},
		{
			name: "cache-type-v",
			line: "-ctv,  --cache-type-v TYPE              KV cache data type for V",
			want: domain.FlagSpec{
				Long:       "cache-type-v",
				Short:      "ctv",
				Type:       domain.FlagTypeEnum,
				EnumValues: cacheTypeEnum,
				HelpText:   "KV cache data type for V",
			},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, ok := parseFlagLine(tc.line)
			if !ok {
				t.Fatalf("!ok for %q", tc.line)
			}
			if !reflect.DeepEqual(got, tc.want) {
				t.Fatalf("got %+v, want %+v", got, tc.want)
			}
		})
	}
}
```

- [ ] **Step 2: Rodar — falha esperada (`cacheTypeEnum undefined`)**

```bash
go test ./internal/service/llamahelp/ -run TestParseFlagLine_CacheTypeHardcodedEnum
```

- [ ] **Step 3: Implementar override hardcoded**

Em `parser.go`, adicione no topo (após imports):

```go
// cacheTypeEnum lists the KV-cache quant types accepted by --cache-type-{k,v}.
// The --help output renders these as TYPE; we hardcode the well-known set so
// the editor can offer a select.
var cacheTypeEnum = []string{"f32", "f16", "bf16", "q8_0", "q4_0", "q4_1", "iq4_nl", "q5_0", "q5_1"}

// hardcodedFlagOverrides applies post-parse fixes for flags whose --help
// representation does not expose enum values.
func hardcodedFlagOverrides(spec domain.FlagSpec) domain.FlagSpec {
	switch spec.Long {
	case "cache-type-k", "cache-type-v", "cache-type-k-draft", "cache-type-v-draft":
		spec.Type = domain.FlagTypeEnum
		spec.EnumValues = cacheTypeEnum
	}
	return spec
}
```

E em `parseFlagLine`, antes do `return`:

```go
	spec = hardcodedFlagOverrides(spec)
	return spec, true
```

- [ ] **Step 4: Rodar — verde**

```bash
go test ./internal/service/llamahelp/ -v
```

- [ ] **Step 5: Commit**

```bash
git add internal/service/llamahelp/parser.go internal/service/llamahelp/parser_test.go
git commit -m "feat(llamahelp): hardcode cache-type-{k,v} enum values"
```

---

### Task 9: Pure parser — multi-alias (`-ngl, --gpu-layers, --n-gpu-layers N`)

**Files:**
- Modify: `internal/service/llamahelp/parser_test.go`

A `splitAliases` já lida com múltiplos longs (acumula em `longs`). Esta task confirma que a canonical é o último e que o resto vai pra `Aliases`.

- [ ] **Step 1: Adicionar teste**

```go
func TestParseFlagLine_MultiAlias(t *testing.T) {
	line := "-ngl,  --gpu-layers, --n-gpu-layers N   max. number of layers to store in VRAM (default: -1)"
	got, ok := parseFlagLine(line)
	if !ok {
		t.Fatalf("!ok for %q", line)
	}
	want := domain.FlagSpec{
		Long:     "n-gpu-layers",
		Short:    "ngl",
		Aliases:  []string{"gpu-layers"},
		Type:     domain.FlagTypeInt,
		Default:  -1,
		HelpText: "max. number of layers to store in VRAM (default: -1)",
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %+v, want %+v", got, want)
	}
}
```

- [ ] **Step 2: Rodar — espera passar (lógica já implementada na T4)**

```bash
go test ./internal/service/llamahelp/ -run TestParseFlagLine_MultiAlias -v
```

- [ ] **Step 3: Se passar: commit. Se falhar: investigar `splitAliases` (especialmente vírgula após short e tratamento de negative defaults)**

Caso o `default: -1` não case com `defaultRe`, ajuste `defaultRe` para `\(default:\s*([^),]+)\)` (já é assim) — o `-1` é capturado, e `coerceDefault` com `Sscanf("%d")` vai parsear corretamente (Sscanf aceita negativos).

- [ ] **Step 4: Commit**

```bash
git add internal/service/llamahelp/parser_test.go
git commit -m "test(llamahelp): cover multi-alias flags (ngl/gpu-layers/n-gpu-layers)"
```

---

### Task 10: Pure parser — função de alto nível `ParseHelp`

**Files:**
- Modify: `internal/service/llamahelp/parser.go`
- Modify: `internal/service/llamahelp/parser_test.go`

Compõe header detection + flag-line scanning, ignora linhas de continuação (`(env: ...)` e indentação maior que a esperada).

- [ ] **Step 1: Adicionar teste**

```go
func TestParseHelp_SmokeOnFixture(t *testing.T) {
	data, err := os.ReadFile("../../../testdata/help-v7376.txt")
	if err != nil {
		t.Fatalf("read fixture: %v", err)
	}
	schema, err := ParseHelp(data)
	if err != nil {
		t.Fatalf("ParseHelp: %v", err)
	}
	// Sanity: well-known flags must be present.
	want := []string{"ctx-size", "batch-size", "ubatch-size", "flash-attn", "port", "mlock"}
	for _, name := range want {
		if _, ok := schema.Flags[name]; !ok {
			t.Errorf("missing flag %q in parsed schema", name)
		}
	}
	// At least 50 flags expected from a real help dump.
	if len(schema.Flags) < 50 {
		t.Errorf("expected ≥50 flags parsed, got %d", len(schema.Flags))
	}
	// Group should be set for at least one common-section flag.
	if spec, ok := schema.Flags["ctx-size"]; ok && spec.Group != "common" {
		t.Errorf("ctx-size group=%q, want %q", spec.Group, "common")
	}
}
```

Adicione `"os"` aos imports de `parser_test.go`.

- [ ] **Step 2: Rodar — falha esperada (`ParseHelp undefined`)**

```bash
go test ./internal/service/llamahelp/ -run TestParseHelp_SmokeOnFixture
```

- [ ] **Step 3: Implementar `ParseHelp`**

Em `parser.go`, adicione:

```go
import (
	"bufio"
	"bytes"
	// existing imports...
)

// ParseHelp scans the full --help output and returns a FlagSchema.
// Lines before the first section header are skipped (CUDA banner etc).
// Continuation lines are ignored; only the first line of each flag is parsed.
func ParseHelp(data []byte) (domain.FlagSchema, error) {
	schema := domain.FlagSchema{Flags: make(map[string]domain.FlagSpec)}
	scanner := bufio.NewScanner(bytes.NewReader(data))
	scanner.Buffer(make([]byte, 64*1024), 1024*1024)
	currentGroup := ""
	for scanner.Scan() {
		line := scanner.Text()
		if header := parseSectionHeader(line); header != "" {
			currentGroup = header
			continue
		}
		if currentGroup == "" {
			continue
		}
		spec, ok := parseFlagLine(line)
		if !ok {
			continue
		}
		spec.Group = currentGroup
		schema.Flags[spec.Long] = spec
	}
	if err := scanner.Err(); err != nil {
		return domain.FlagSchema{}, err
	}
	return schema, nil
}
```

- [ ] **Step 4: Rodar — verde**

```bash
go test ./internal/service/llamahelp/ -v
```

Esperado: smoke teste passa, ≥50 flags, todas as comuns presentes.

- [ ] **Step 5: Commit**

```bash
git add internal/service/llamahelp/parser.go internal/service/llamahelp/parser_test.go
git commit -m "feat(llamahelp): top-level ParseHelp scans full --help output"
```

---

### Task 11: Golden file do schema parseado

**Files:**
- Create: `testdata/help-v7376.golden.json`
- Modify: `internal/service/llamahelp/parser_test.go`

- [ ] **Step 1: Gerar golden via test helper**

Crie um teste write-on-update temporário para gerar o golden:

```go
// TestGenerateGolden runs only with -update flag; commits the parsed schema
// so future regressions show up as JSON diff.
var updateGolden = flag.Bool("update", false, "regenerate golden files")

func TestParseHelp_Golden(t *testing.T) {
	data, err := os.ReadFile("../../../testdata/help-v7376.txt")
	if err != nil {
		t.Fatalf("read fixture: %v", err)
	}
	schema, err := ParseHelp(data)
	if err != nil {
		t.Fatalf("ParseHelp: %v", err)
	}
	got, err := json.MarshalIndent(schema, "", "  ")
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	goldenPath := "../../../testdata/help-v7376.golden.json"
	if *updateGolden {
		if err := os.WriteFile(goldenPath, got, 0o644); err != nil {
			t.Fatalf("write golden: %v", err)
		}
		t.Log("golden updated")
		return
	}
	want, err := os.ReadFile(goldenPath)
	if err != nil {
		t.Fatalf("read golden (run with -update first): %v", err)
	}
	if !bytes.Equal(got, want) {
		t.Fatalf("golden mismatch.\nDiff: run `go test ./internal/service/llamahelp -update` and inspect the diff with `git diff`.")
	}
}
```

Adicione imports: `"flag"`, `"encoding/json"`, `"bytes"`.

- [ ] **Step 2: Gerar o golden**

```bash
go test ./internal/service/llamahelp/ -run TestParseHelp_Golden -update -v
```

Esperado: `golden updated`.

- [ ] **Step 3: Verificar golden tem flags esperadas**

```bash
grep -E '"long":\s*"(ctx-size|batch-size|ubatch-size|flash-attn|port|mlock|n-gpu-layers|cache-type-k)"' testdata/help-v7376.golden.json | wc -l
```

Esperado: ≥7 matches.

- [ ] **Step 4: Re-rodar sem -update — verde**

```bash
go test ./internal/service/llamahelp/ -run TestParseHelp_Golden -v
```

- [ ] **Step 5: Commit**

```bash
git add testdata/help-v7376.golden.json internal/service/llamahelp/parser_test.go
git commit -m "test(llamahelp): add golden schema for v7376 help fixture"
```

---

### Task 12: ExecParser — wrapper que invoca `llama-server` real

**Files:**
- Create: `internal/service/llamahelp/exec_parser.go`
- Create: `internal/service/llamahelp/exec_parser_test.go`
- Create: `testdata/fake-llama-help.sh`

- [ ] **Step 1: Criar fake-llama-help.sh**

```bash
cat > testdata/fake-llama-help.sh <<'SH'
#!/usr/bin/env bash
# Test fake of llama-server: emits canned --help / --version output for
# llamahelp.ExecParser tests. Real binary is replaced via PATH override.
case "$1" in
  --help|--usage)
    cat "$(dirname "$0")/help-v7376.txt"
    exit 0
    ;;
  --version)
    echo "version: 7376 (380b4c9)"
    exit 0
    ;;
  *)
    echo "fake-llama-server: unknown args: $*" 1>&2
    exit 2
    ;;
esac
SH
chmod +x testdata/fake-llama-help.sh
```

- [ ] **Step 2: Escrever teste**

```go
package llamahelp

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestExecParser_ParseUsesPATH(t *testing.T) {
	// Symlink fake script as `llama-server` in a temp dir, prepend to PATH.
	tmp := t.TempDir()
	src, err := filepath.Abs("../../../testdata/fake-llama-help.sh")
	if err != nil {
		t.Fatalf("abs: %v", err)
	}
	dst := filepath.Join(tmp, "llama-server")
	if err := os.Symlink(src, dst); err != nil {
		t.Fatalf("symlink: %v", err)
	}
	t.Setenv("PATH", tmp+string(os.PathListSeparator)+os.Getenv("PATH"))

	// Sanity: exec.LookPath resolves to our fake.
	if got, err := exec.LookPath("llama-server"); err != nil || !strings.HasPrefix(got, tmp) {
		t.Fatalf("LookPath = %q, err=%v; want path under %q", got, err, tmp)
	}

	parser := NewExecParser()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	schema, err := parser.Parse(ctx)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if _, ok := schema.Flags["ctx-size"]; !ok {
		t.Errorf("missing ctx-size in schema parsed via fake binary")
	}
	if v, err := parser.DetectVersion(ctx); err != nil || !strings.Contains(v, "7376") {
		t.Errorf("DetectVersion = %q, err=%v; want substring 7376", v, err)
	}
}
```

- [ ] **Step 3: Rodar — falha esperada (`NewExecParser undefined`)**

```bash
go test ./internal/service/llamahelp/ -run TestExecParser_ParseUsesPATH
```

- [ ] **Step 4: Implementar `exec_parser.go`**

```go
package llamahelp

import (
	"bytes"
	"context"
	"fmt"
	"os/exec"
	"strings"

	"github.com/quantmind-br/model-loader/internal/domain"
)

// ExecParser invokes llama-server in PATH to capture --help and --version.
type ExecParser struct {
	binary string
}

// NewExecParser returns an ExecParser that resolves "llama-server" via PATH.
func NewExecParser() *ExecParser {
	return &ExecParser{binary: "llama-server"}
}

func (p *ExecParser) Parse(ctx context.Context) (domain.FlagSchema, error) {
	cmd := exec.CommandContext(ctx, p.binary, "--help")
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return domain.FlagSchema{}, fmt.Errorf("%s --help: %w (stderr: %s)", p.binary, err, stderr.String())
	}
	combined := append(stdout.Bytes(), stderr.Bytes()...)
	schema, err := ParseHelp(combined)
	if err != nil {
		return domain.FlagSchema{}, err
	}
	if v, verr := p.DetectVersion(ctx); verr == nil {
		schema.Version = v
	}
	return schema, nil
}

func (p *ExecParser) DetectVersion(ctx context.Context) (string, error) {
	cmd := exec.CommandContext(ctx, p.binary, "--version")
	var out bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = &out
	if err := cmd.Run(); err != nil {
		return "", fmt.Errorf("%s --version: %w", p.binary, err)
	}
	return strings.TrimSpace(out.String()), nil
}
```

- [ ] **Step 5: Rodar testes — verde**

```bash
go test ./internal/service/llamahelp/ -v
```

- [ ] **Step 6: Commit**

```bash
git add internal/service/llamahelp/exec_parser.go internal/service/llamahelp/exec_parser_test.go testdata/fake-llama-help.sh
git commit -m "feat(llamahelp): ExecParser wraps llama-server --help/--version"
```

---

### Task 13: Schema embutido (fallback)

**Files:**
- Create: `internal/service/llamahelp/embedded.go`
- Create: `internal/service/llamahelp/embedded_test.go`

Cobre apenas os 13 essentials da spec — o suficiente para o editor abrir mesmo sem binário.

- [ ] **Step 1: Escrever teste**

```go
package llamahelp

import (
	"testing"

	"github.com/quantmind-br/model-loader/internal/domain"
)

func TestEmbedded_HasAllEssentials(t *testing.T) {
	schema := EmbeddedSchema()
	want := []string{
		"model", "n-gpu-layers", "ctx-size", "batch-size", "ubatch-size",
		"flash-attn", "threads", "parallel", "mlock",
		"cache-type-k", "cache-type-v", "split-mode", "tensor-split",
	}
	for _, name := range want {
		if _, ok := schema.Lookup(name); !ok {
			t.Errorf("embedded missing essential flag %q", name)
		}
	}
	if schema.Version == "" {
		t.Error("embedded schema must report a version label")
	}
}

func TestEmbedded_FlashAttnEnumValues(t *testing.T) {
	schema := EmbeddedSchema()
	spec, ok := schema.Lookup("flash-attn")
	if !ok {
		t.Fatal("flash-attn missing")
	}
	if spec.Type != domain.FlagTypeEnum {
		t.Errorf("flash-attn type = %v, want enum", spec.Type)
	}
	wantSet := map[string]bool{"on": false, "off": false, "auto": false}
	for _, v := range spec.EnumValues {
		wantSet[v] = true
	}
	for v, found := range wantSet {
		if !found {
			t.Errorf("flash-attn enum missing %q", v)
		}
	}
}
```

- [ ] **Step 2: Rodar — falha esperada**

```bash
go test ./internal/service/llamahelp/ -run TestEmbedded
```

- [ ] **Step 3: Implementar `embedded.go`**

```go
package llamahelp

import "github.com/quantmind-br/model-loader/internal/domain"

// EmbeddedSchema returns the compile-time fallback schema covering the curated
// essentials. Used when llama-server --help cannot be invoked. Version label
// reflects the upstream build the embedded data was pinned against.
func EmbeddedSchema() domain.FlagSchema {
	flags := map[string]domain.FlagSpec{
		"model": {
			Long:     "model",
			Short:    "m",
			Type:     domain.FlagTypeString,
			HelpText: "model path (.gguf)",
			Group:    "embedded",
		},
		"n-gpu-layers": {
			Long:     "n-gpu-layers",
			Short:    "ngl",
			Aliases:  []string{"gpu-layers"},
			Type:     domain.FlagTypeInt,
			Default:  -1,
			HelpText: "max number of layers to store in VRAM",
			Group:    "embedded",
		},
		"ctx-size": {
			Long:     "ctx-size",
			Short:    "c",
			Type:     domain.FlagTypeInt,
			Default:  4096,
			HelpText: "size of the prompt context",
			Group:    "embedded",
		},
		"batch-size": {
			Long:     "batch-size",
			Short:    "b",
			Type:     domain.FlagTypeInt,
			Default:  2048,
			HelpText: "logical maximum batch size",
			Group:    "embedded",
		},
		"ubatch-size": {
			Long:     "ubatch-size",
			Short:    "ub",
			Type:     domain.FlagTypeInt,
			Default:  512,
			HelpText: "physical maximum batch size",
			Group:    "embedded",
		},
		"flash-attn": {
			Long:       "flash-attn",
			Short:      "fa",
			Type:       domain.FlagTypeEnum,
			EnumValues: []string{"on", "off", "auto"},
			Default:    "auto",
			HelpText:   "Flash Attention mode",
			Group:      "embedded",
		},
		"threads": {
			Long:     "threads",
			Short:    "t",
			Type:     domain.FlagTypeInt,
			Default:  -1,
			HelpText: "CPU threads",
			Group:    "embedded",
		},
		"parallel": {
			Long:     "parallel",
			Short:    "np",
			Type:     domain.FlagTypeInt,
			Default:  1,
			HelpText: "number of parallel sequences to decode",
			Group:    "embedded",
		},
		"mlock": {
			Long:     "mlock",
			Type:     domain.FlagTypeBool,
			HelpText: "lock model in RAM (no swap)",
			Group:    "embedded",
		},
		"cache-type-k": {
			Long:       "cache-type-k",
			Short:      "ctk",
			Type:       domain.FlagTypeEnum,
			EnumValues: cacheTypeEnum,
			HelpText:   "KV cache data type for K",
			Group:      "embedded",
		},
		"cache-type-v": {
			Long:       "cache-type-v",
			Short:      "ctv",
			Type:       domain.FlagTypeEnum,
			EnumValues: cacheTypeEnum,
			HelpText:   "KV cache data type for V",
			Group:      "embedded",
		},
		"split-mode": {
			Long:       "split-mode",
			Short:      "sm",
			Type:       domain.FlagTypeEnum,
			EnumValues: []string{"none", "layer", "row"},
			HelpText:   "how to split model across multiple GPUs",
			Group:      "embedded",
		},
		"tensor-split": {
			Long:     "tensor-split",
			Short:    "ts",
			Type:     domain.FlagTypeString,
			HelpText: "fraction of model offloaded to each GPU (comma-separated)",
			Group:    "embedded",
		},
	}
	return domain.FlagSchema{
		Version: "embedded-v7376",
		Flags:   flags,
	}
}
```

- [ ] **Step 4: Rodar — verde**

```bash
go test ./internal/service/llamahelp/ -v
```

- [ ] **Step 5: Commit**

```bash
git add internal/service/llamahelp/embedded.go internal/service/llamahelp/embedded_test.go
git commit -m "feat(llamahelp): embedded fallback schema covering 13 essentials"
```

---

## Phase B — Validator (4 tasks)

### Task 14: Validator package + types + skeleton

**Files:**
- Create: `internal/service/validator/validator.go`
- Create: `internal/service/validator/validator_test.go`

- [ ] **Step 1: Teste smoke**

```go
package validator

import (
	"testing"

	"github.com/quantmind-br/model-loader/internal/domain"
)

func TestValidator_EmptySchemaProducesNoTypeIssues(t *testing.T) {
	v := New()
	p := domain.Profile{
		ID:    "x",
		Name:  "X",
		Model: "/tmp/nonexistent.gguf", // expected to surface in the model-existence rule (added later).
		Args:  map[string]any{},
	}
	rep := v.Validate(p, domain.FlagSchema{Flags: map[string]domain.FlagSpec{}})
	// At this stage, with no schema and no rules wired, Errors should be empty.
	if len(rep.Errors) != 0 {
		t.Errorf("Errors=%v, want empty", rep.Errors)
	}
}
```

- [ ] **Step 2: Falha esperada**

```bash
go test ./internal/service/validator/ -v
```

- [ ] **Step 3: Implementar skeleton**

```go
// Package validator checks profiles against a FlagSchema and fixed rules.
package validator

import "github.com/quantmind-br/model-loader/internal/domain"

// Severity grades a FieldIssue.
type Severity int

const (
	SeverityWarning Severity = iota
	SeverityError
)

// FieldIssue is a single rule violation, scoped to one Profile field.
type FieldIssue struct {
	Field    string
	Message  string
	Severity Severity
}

// Report aggregates issues from all rules.
type Report struct {
	Errors   []FieldIssue
	Warnings []FieldIssue
}

// HasBlockingErrors returns true when at least one issue is SeverityError.
func (r Report) HasBlockingErrors() bool {
	return len(r.Errors) > 0
}

// Validator runs all configured rules on a Profile.
type Validator interface {
	Validate(p domain.Profile, schema domain.FlagSchema) Report
}

// New returns a default Validator with the standard rule set.
func New() Validator {
	return defaultValidator{}
}

type defaultValidator struct{}

func (v defaultValidator) Validate(p domain.Profile, schema domain.FlagSchema) Report {
	rep := Report{}
	rep = applyTypeRules(p, schema, rep)
	rep = applyCrossFieldRules(p, rep)
	rep = applyExistenceRules(p, rep)
	return rep
}

func appendIssue(rep Report, issue FieldIssue) Report {
	if issue.Severity == SeverityError {
		rep.Errors = append(rep.Errors, issue)
	} else {
		rep.Warnings = append(rep.Warnings, issue)
	}
	return rep
}
```

- [ ] **Step 4: Criar `rules.go` com stubs**

```go
package validator

import "github.com/quantmind-br/model-loader/internal/domain"

func applyTypeRules(p domain.Profile, schema domain.FlagSchema, rep Report) Report {
	return rep
}

func applyCrossFieldRules(p domain.Profile, rep Report) Report {
	return rep
}

func applyExistenceRules(p domain.Profile, rep Report) Report {
	return rep
}
```

- [ ] **Step 5: Rodar — verde**

```bash
go test ./internal/service/validator/ -v
```

- [ ] **Step 6: Commit**

```bash
git add internal/service/validator/validator.go internal/service/validator/validator_test.go internal/service/validator/rules.go
git commit -m "feat(validator): package skeleton with rule pipeline stubs"
```

---

### Task 15: Validator — type rule (int parse, enum match, bool coercion)

**Files:**
- Modify: `internal/service/validator/rules.go`
- Modify: `internal/service/validator/validator_test.go`

- [ ] **Step 1: Teste table-driven**

```go
func TestValidator_TypeRule(t *testing.T) {
	schema := domain.FlagSchema{Flags: map[string]domain.FlagSpec{
		"ctx-size":   {Long: "ctx-size", Type: domain.FlagTypeInt},
		"flash-attn": {Long: "flash-attn", Type: domain.FlagTypeEnum, EnumValues: []string{"on", "off", "auto"}},
		"mlock":      {Long: "mlock", Type: domain.FlagTypeBool},
	}}
	cases := []struct {
		name      string
		args      map[string]any
		wantErrs  int
		wantField string
	}{
		{"int ok as float64", map[string]any{"ctx-size": float64(4096)}, 0, ""},
		{"int ok as int", map[string]any{"ctx-size": 4096}, 0, ""},
		{"int rejects string", map[string]any{"ctx-size": "abc"}, 1, "ctx-size"},
		{"enum ok", map[string]any{"flash-attn": "on"}, 0, ""},
		{"enum rejects unknown", map[string]any{"flash-attn": "maybe"}, 1, "flash-attn"},
		{"bool ok", map[string]any{"mlock": true}, 0, ""},
		{"bool rejects string", map[string]any{"mlock": "yes"}, 1, "mlock"},
		{"unknown flag is ignored", map[string]any{"unheard-of": 1}, 0, ""},
	}
	v := New()
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			p := domain.Profile{ID: "x", Args: tc.args}
			rep := v.Validate(p, schema)
			if got := len(rep.Errors); got != tc.wantErrs {
				t.Fatalf("Errors=%d (%v), want %d", got, rep.Errors, tc.wantErrs)
			}
			if tc.wantErrs > 0 && rep.Errors[0].Field != tc.wantField {
				t.Errorf("Errors[0].Field=%q, want %q", rep.Errors[0].Field, tc.wantField)
			}
		})
	}
}
```

- [ ] **Step 2: Falha esperada**

```bash
go test ./internal/service/validator/ -run TestValidator_TypeRule
```

- [ ] **Step 3: Implementar `applyTypeRules`**

```go
package validator

import (
	"fmt"

	"github.com/quantmind-br/model-loader/internal/domain"
)

func applyTypeRules(p domain.Profile, schema domain.FlagSchema, rep Report) Report {
	for key, val := range p.Args {
		spec, ok := schema.Lookup(key)
		if !ok {
			continue // unknown flags are not type-checked here
		}
		if msg := checkType(spec, val); msg != "" {
			rep = appendIssue(rep, FieldIssue{Field: key, Message: msg, Severity: SeverityError})
		}
	}
	return rep
}

func checkType(spec domain.FlagSpec, val any) string {
	switch spec.Type {
	case domain.FlagTypeInt:
		switch val.(type) {
		case int, int32, int64, float64, float32:
			return ""
		}
		return fmt.Sprintf("expected int, got %T", val)
	case domain.FlagTypeFloat:
		switch val.(type) {
		case float32, float64, int, int32, int64:
			return ""
		}
		return fmt.Sprintf("expected float, got %T", val)
	case domain.FlagTypeBool:
		if _, ok := val.(bool); ok {
			return ""
		}
		return fmt.Sprintf("expected bool, got %T", val)
	case domain.FlagTypeString:
		if _, ok := val.(string); ok {
			return ""
		}
		return fmt.Sprintf("expected string, got %T", val)
	case domain.FlagTypeEnum:
		s, ok := val.(string)
		if !ok {
			return fmt.Sprintf("expected one of %v, got %T", spec.EnumValues, val)
		}
		for _, v := range spec.EnumValues {
			if v == s {
				return ""
			}
		}
		return fmt.Sprintf("%q not in %v", s, spec.EnumValues)
	}
	return ""
}
```

- [ ] **Step 4: Rodar — verde**

```bash
go test ./internal/service/validator/ -v
```

- [ ] **Step 5: Commit**

```bash
git add internal/service/validator/rules.go internal/service/validator/validator_test.go
git commit -m "feat(validator): type rule (int/float/bool/string/enum)"
```

---

### Task 16: Validator — cross-field rules

**Files:**
- Modify: `internal/service/validator/rules.go`
- Modify: `internal/service/validator/validator_test.go`

Regras (spec 6.3):
1. `ubatch-size > batch-size` → erro.
2. `flash-attn=on` com cache-type-k ou cache-type-v = `f16` → warning.
3. `ctx-size > 32768` E `n-gpu-layers < 99` (ou ngl < 99) → warning.

- [ ] **Step 1: Teste**

```go
func TestValidator_CrossFieldRules(t *testing.T) {
	schema := domain.FlagSchema{Flags: map[string]domain.FlagSpec{
		"ctx-size":     {Long: "ctx-size", Type: domain.FlagTypeInt},
		"batch-size":   {Long: "batch-size", Type: domain.FlagTypeInt},
		"ubatch-size":  {Long: "ubatch-size", Type: domain.FlagTypeInt},
		"flash-attn":   {Long: "flash-attn", Type: domain.FlagTypeEnum, EnumValues: []string{"on", "off", "auto"}},
		"cache-type-k": {Long: "cache-type-k", Type: domain.FlagTypeEnum, EnumValues: cacheTypes()},
		"cache-type-v": {Long: "cache-type-v", Type: domain.FlagTypeEnum, EnumValues: cacheTypes()},
		"ngl":          {Long: "n-gpu-layers", Short: "ngl", Type: domain.FlagTypeInt},
	}}
	v := New()
	cases := []struct {
		name        string
		args        map[string]any
		wantErrs    int
		wantWarns   int
		wantField   string
	}{
		{
			name:     "ubatch > batch is error",
			args:     map[string]any{"batch-size": 2048, "ubatch-size": 4096},
			wantErrs: 1, wantField: "ubatch-size",
		},
		{
			name:     "ubatch == batch is fine",
			args:     map[string]any{"batch-size": 2048, "ubatch-size": 2048},
			wantErrs: 0,
		},
		{
			name:      "flash-attn on with f16 cache warns",
			args:      map[string]any{"flash-attn": "on", "cache-type-k": "f16", "cache-type-v": "f16"},
			wantWarns: 1,
		},
		{
			name:      "ctx>32k with ngl<99 warns",
			args:      map[string]any{"ctx-size": 65536, "ngl": 50},
			wantWarns: 1,
		},
		{
			name:      "ctx>32k with ngl=99 no warn",
			args:      map[string]any{"ctx-size": 65536, "ngl": 99},
			wantWarns: 0,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			p := domain.Profile{ID: "x", Args: tc.args}
			rep := v.Validate(p, schema)
			if got := len(rep.Errors); got != tc.wantErrs {
				t.Errorf("Errors=%d (%v), want %d", got, rep.Errors, tc.wantErrs)
			}
			if got := len(rep.Warnings); got != tc.wantWarns {
				t.Errorf("Warnings=%d (%v), want %d", got, rep.Warnings, tc.wantWarns)
			}
			if tc.wantField != "" && len(rep.Errors) > 0 && rep.Errors[0].Field != tc.wantField {
				t.Errorf("Errors[0].Field=%q, want %q", rep.Errors[0].Field, tc.wantField)
			}
		})
	}
}

func cacheTypes() []string {
	return []string{"f32", "f16", "bf16", "q8_0", "q4_0"}
}
```

- [ ] **Step 2: Falha esperada**

```bash
go test ./internal/service/validator/ -run TestValidator_CrossFieldRules
```

- [ ] **Step 3: Implementar `applyCrossFieldRules`**

Substitua a função stub em `rules.go`:

```go
func applyCrossFieldRules(p domain.Profile, rep Report) Report {
	if batch, ok := intArg(p.Args, "batch-size"); ok {
		if ubatch, ok := intArg(p.Args, "ubatch-size"); ok && ubatch > batch {
			rep = appendIssue(rep, FieldIssue{
				Field:    "ubatch-size",
				Message:  fmt.Sprintf("ubatch-size (%d) must be ≤ batch-size (%d)", ubatch, batch),
				Severity: SeverityError,
			})
		}
	}
	if fa, ok := stringArg(p.Args, "flash-attn"); ok && fa == "on" {
		if k, _ := stringArg(p.Args, "cache-type-k"); k == "f16" {
			rep = appendIssue(rep, FieldIssue{
				Field:    "cache-type-k",
				Message:  "flash-attn=on with f16 KV cache is suboptimal; consider q8_0",
				Severity: SeverityWarning,
			})
		} else if v, _ := stringArg(p.Args, "cache-type-v"); v == "f16" {
			rep = appendIssue(rep, FieldIssue{
				Field:    "cache-type-v",
				Message:  "flash-attn=on with f16 KV cache is suboptimal; consider q8_0",
				Severity: SeverityWarning,
			})
		}
	}
	if ctx, ok := intArg(p.Args, "ctx-size"); ok && ctx > 32768 {
		ngl, hasNGL := intArg(p.Args, "ngl")
		if !hasNGL {
			ngl, hasNGL = intArg(p.Args, "n-gpu-layers")
		}
		if hasNGL && ngl < 99 {
			rep = appendIssue(rep, FieldIssue{
				Field:    "ngl",
				Message:  fmt.Sprintf("ctx-size %d with ngl %d may force CPU offload", ctx, ngl),
				Severity: SeverityWarning,
			})
		}
	}
	return rep
}

func intArg(args map[string]any, key string) (int, bool) {
	v, ok := args[key]
	if !ok {
		return 0, false
	}
	switch n := v.(type) {
	case int:
		return n, true
	case int32:
		return int(n), true
	case int64:
		return int(n), true
	case float64:
		return int(n), true
	case float32:
		return int(n), true
	}
	return 0, false
}

func stringArg(args map[string]any, key string) (string, bool) {
	v, ok := args[key]
	if !ok {
		return "", false
	}
	s, ok := v.(string)
	return s, ok
}
```

- [ ] **Step 4: Rodar — verde**

```bash
go test ./internal/service/validator/ -v
```

- [ ] **Step 5: Commit**

```bash
git add internal/service/validator/rules.go internal/service/validator/validator_test.go
git commit -m "feat(validator): cross-field rules (ubatch≤batch, fa+f16 warn, ctx+ngl warn)"
```

---

### Task 17: Validator — model path existence

**Files:**
- Modify: `internal/service/validator/rules.go`
- Modify: `internal/service/validator/validator_test.go`

- [ ] **Step 1: Teste**

```go
func TestValidator_ModelExistence(t *testing.T) {
	tmp := t.TempDir()
	existing := tmp + "/m.gguf"
	if err := os.WriteFile(existing, []byte("g"), 0o644); err != nil {
		t.Fatal(err)
	}
	v := New()
	cases := []struct {
		name     string
		model    string
		wantErrs int
	}{
		{"empty path no error (rule only applies when set)", "", 0},
		{"existing path no error", existing, 0},
		{"missing path errors", tmp + "/nope.gguf", 1},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			p := domain.Profile{ID: "x", Model: tc.model}
			rep := v.Validate(p, domain.FlagSchema{})
			if got := len(rep.Errors); got != tc.wantErrs {
				t.Errorf("Errors=%d (%v), want %d", got, rep.Errors, tc.wantErrs)
			}
		})
	}
}
```

Adicione `"os"` aos imports do test.

- [ ] **Step 2: Falha esperada**

```bash
go test ./internal/service/validator/ -run TestValidator_ModelExistence
```

- [ ] **Step 3: Implementar `applyExistenceRules`**

```go
import "os"

func applyExistenceRules(p domain.Profile, rep Report) Report {
	if p.Model == "" {
		return rep
	}
	if _, err := os.Stat(p.Model); err != nil {
		if os.IsNotExist(err) {
			return appendIssue(rep, FieldIssue{
				Field:    "model",
				Message:  "model file does not exist",
				Severity: SeverityError,
			})
		}
		return appendIssue(rep, FieldIssue{
			Field:    "model",
			Message:  "model path stat failed: " + err.Error(),
			Severity: SeverityError,
		})
	}
	return rep
}
```

- [ ] **Step 4: Rodar — verde**

```bash
go test ./internal/service/validator/ -v
```

- [ ] **Step 5: Commit**

```bash
git add internal/service/validator/rules.go internal/service/validator/validator_test.go
git commit -m "feat(validator): model path existence rule"
```

---

## Phase C — Editor expansion (8 tasks)

### Task 18: Refactor — extrair editor para `profiles_editor.go`

**Files:**
- Create: `internal/ui/pages/profiles_editor.go`
- Modify: `internal/ui/pages/profiles.go`

Sem mudança de comportamento — apenas mover `profileDraft`, `buildEditorForm`, `argString`, `argBool` para arquivo dedicado, e introduzir um struct `editor` que encapsula o estado.

- [ ] **Step 1: Criar `profiles_editor.go`**

```go
package pages

import (
	"fmt"
	"strconv"

	"github.com/charmbracelet/huh"
)

// profileDraft is the editor's mutable state, mapped from huh form back to a Profile on save.
type profileDraft struct {
	ID          string // immutable once created
	Name        string
	Description string
	Model       string
	NGL         string
	CtxSize     string
	Port        string
	FlashAttn   bool
	isNew       bool
}

func argString(v any) string {
	switch t := v.(type) {
	case nil:
		return ""
	case string:
		return t
	case float64:
		return strconv.FormatFloat(t, 'f', -1, 64)
	case int:
		return strconv.Itoa(t)
	default:
		return fmt.Sprintf("%v", t)
	}
}

func argBool(v any) bool {
	b, _ := v.(bool)
	return b
}

func buildEditorForm(d *profileDraft) *huh.Form {
	return huh.NewForm(
		huh.NewGroup(
			huh.NewInput().Title("Name").Value(&d.Name),
			huh.NewInput().Title("Description").Value(&d.Description),
			huh.NewInput().Title("Model path").Value(&d.Model),
		),
		huh.NewGroup(
			huh.NewInput().Title("ngl (gpu layers)").Value(&d.NGL),
			huh.NewInput().Title("ctx-size").Value(&d.CtxSize),
			huh.NewInput().Title("port").Value(&d.Port),
			huh.NewConfirm().Title("flash-attn?").Value(&d.FlashAttn).Affirmative("Yes").Negative("No"),
		),
	).WithShowHelp(true)
}
```

- [ ] **Step 2: Remover as definições duplicadas de `profiles.go`**

Em `internal/ui/pages/profiles.go`, remova `profileDraft`, `argString`, `argBool` e `buildEditorForm`. Deixe imports ajustados.

- [ ] **Step 3: Build e testes existentes**

```bash
go build ./...
go test ./...
```

Esperado: tudo passa, comportamento idêntico ao slice 1.

- [ ] **Step 4: Commit**

```bash
git add internal/ui/pages/profiles.go internal/ui/pages/profiles_editor.go
git commit -m "refactor(ui/pages/profiles): extract editor draft and form builder"
```

---

### Task 19: Editor — estado de sub-tab Essentials/Advanced

**Files:**
- Modify: `internal/ui/pages/profiles.go`
- Modify: `internal/ui/pages/profiles_editor.go`

- [ ] **Step 1: Adicionar enum sub-tab e key binding**

Em `profiles_editor.go`, no topo:

```go
type subTab int

const (
	subTabEssentials subTab = iota
	subTabAdvanced
)

func (s subTab) String() string {
	if s == subTabEssentials {
		return "Essentials"
	}
	return "Advanced"
}
```

- [ ] **Step 2: Adicionar campo `subTab` ao `ProfilesPage`**

Em `profiles.go`, no struct `ProfilesPage`, adicione após `editing`:

```go
	subTab        subTab
```

E em `defaultProfilesKeys`, adicione:

```go
		Tab: key.NewBinding(key.WithKeys("ctrl+t"), key.WithHelp("ctrl+t", "tab editor")),
```

(Use `ctrl+t` para evitar conflito com `Tab` global do root.)

E em `profilesKeyMap`:

```go
type profilesKeyMap struct {
	New, Save, Duplicate, Delete, Edit, Cancel, Tab key.Binding
}
```

- [ ] **Step 3: Handle no `updateForm`**

Substitua `updateForm`:

```go
func (p ProfilesPage) updateForm(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	if msg.String() == "esc" {
		p.editing = false
		p.form = nil
		return p, nil
	}
	if key.Matches(msg, p.listKeys.Tab) {
		if p.subTab == subTabEssentials {
			p.subTab = subTabAdvanced
		} else {
			p.subTab = subTabEssentials
		}
		return p, nil
	}

	updated, cmd := p.form.Update(msg)
	if f, ok := updated.(*huh.Form); ok {
		p.form = f
	}
	if p.form != nil && p.form.State == huh.StateCompleted {
		return p.commitDraft()
	}
	return p, cmd
}
```

- [ ] **Step 4: Mostrar header com sub-tab no `View`**

Em `profiles.go` `View()`, no ramo `editing`:

```go
	if p.editing && p.form != nil {
		header := theme.Title.Render(fmt.Sprintf("Editor — [%s]   ctrl+t to switch", p.subTab))
		return lipgloss.JoinVertical(lipgloss.Left, header, p.form.View())
	}
```

- [ ] **Step 5: Build + testes**

```bash
go build ./...
go test ./...
```

Comportamento esperado: ainda só Essentials renderiza form (Advanced placeholder até T22). Switch via ctrl+t não quebra nada.

- [ ] **Step 6: Commit**

```bash
git add internal/ui/pages/profiles.go internal/ui/pages/profiles_editor.go
git commit -m "feat(ui/pages/profiles): editor sub-tab toggle (Essentials/Advanced)"
```

---

### Task 20: Editor Essentials — usar FlagSchema para help/default hints

**Files:**
- Modify: `internal/ui/pages/profiles_editor.go`
- Modify: `internal/ui/pages/profiles.go`

Adapta `buildEditorForm` para receber o schema e enriquecer labels com help text + defaults vindos do schema. Mantém os 7 campos atuais mais inclusões básicas: `batch-size`, `ubatch-size`, `cache-type-k`, `cache-type-v`. Total: 11 essentials cobertos no editor (model + 6 já existentes + 4 novos).

- [ ] **Step 1: Adicionar campos ao `profileDraft`**

```go
type profileDraft struct {
	ID          string
	Name        string
	Description string
	Model       string
	NGL         string
	CtxSize     string
	BatchSize   string
	UBatchSize  string
	Port        string
	FlashAttn   bool
	CacheTypeK  string
	CacheTypeV  string
	isNew       bool
}
```

- [ ] **Step 2: Atualizar `buildEditorForm` para receber schema**

```go
import (
	"github.com/quantmind-br/model-loader/internal/domain"
)

func buildEditorForm(d *profileDraft, schema domain.FlagSchema) *huh.Form {
	cacheOpts := selectOptions(schema, "cache-type-k", []string{"f16", "q8_0", "q4_0"})
	return huh.NewForm(
		huh.NewGroup(
			huh.NewInput().Title("Name").Value(&d.Name),
			huh.NewInput().Title("Description").Value(&d.Description),
			huh.NewInput().Title("Model path (.gguf)").Value(&d.Model),
		),
		huh.NewGroup(
			huh.NewInput().Title(labelWithHelp(schema, "n-gpu-layers", "ngl (gpu layers)")).Value(&d.NGL),
			huh.NewInput().Title(labelWithHelp(schema, "ctx-size", "ctx-size")).Value(&d.CtxSize),
			huh.NewInput().Title(labelWithHelp(schema, "batch-size", "batch-size")).Value(&d.BatchSize),
			huh.NewInput().Title(labelWithHelp(schema, "ubatch-size", "ubatch-size")).Value(&d.UBatchSize),
			huh.NewInput().Title(labelWithHelp(schema, "port", "port")).Value(&d.Port),
			huh.NewConfirm().Title(labelWithHelp(schema, "flash-attn", "flash-attn?")).Value(&d.FlashAttn).Affirmative("Yes").Negative("No"),
			huh.NewSelect[string]().Title("cache-type-k").Options(toOptions(cacheOpts)...).Value(&d.CacheTypeK),
			huh.NewSelect[string]().Title("cache-type-v").Options(toOptions(cacheOpts)...).Value(&d.CacheTypeV),
		),
	).WithShowHelp(true)
}

func labelWithHelp(schema domain.FlagSchema, name, fallback string) string {
	if spec, ok := schema.Lookup(name); ok && spec.HelpText != "" {
		if spec.Default != nil {
			return fmt.Sprintf("%s — %s (default %v)", fallback, spec.HelpText, spec.Default)
		}
		return fmt.Sprintf("%s — %s", fallback, spec.HelpText)
	}
	return fallback
}

func selectOptions(schema domain.FlagSchema, name string, fallback []string) []string {
	if spec, ok := schema.Lookup(name); ok && len(spec.EnumValues) > 0 {
		return spec.EnumValues
	}
	return fallback
}

func toOptions(values []string) []huh.Option[string] {
	out := make([]huh.Option[string], 0, len(values))
	for _, v := range values {
		out = append(out, huh.NewOption(v, v))
	}
	return out
}
```

- [ ] **Step 3: Atualizar `ProfilesPage` para guardar `schema`**

Em `profiles.go`:

```go
type ProfilesPage struct {
	store  profilestore.Store
	schema domain.FlagSchema
	// ... existing fields
}
```

E ajustar `NewProfilesPage`:

```go
func NewProfilesPage(store profilestore.Store, schema domain.FlagSchema) ProfilesPage {
	delegate := list.NewDefaultDelegate()
	l := list.New(nil, delegate, 0, 0)
	l.Title = "Profiles"
	l.SetShowHelp(false)
	l.SetShowStatusBar(false)
	return ProfilesPage{
		store:    store,
		schema:   schema,
		list:     l,
		listKeys: defaultProfilesKeys(),
	}
}
```

Atualize chamadas a `buildEditorForm(&p.draft)` para `buildEditorForm(&p.draft, p.schema)`.

- [ ] **Step 4: Atualizar `startNew` e `startEditSelected` para popular novos campos**

Em `startNew`:

```go
	p.draft = profileDraft{
		ID:         "",
		Name:       "New Profile",
		NGL:        "99",
		CtxSize:    "8192",
		BatchSize:  "2048",
		UBatchSize: "512",
		Port:       "8080",
		FlashAttn:  true,
		CacheTypeK: "q8_0",
		CacheTypeV: "q8_0",
		isNew:      true,
	}
```

Em `startEditSelected`:

```go
		BatchSize:  argString(pr.Args["batch-size"]),
		UBatchSize: argString(pr.Args["ubatch-size"]),
		CacheTypeK: argString(pr.Args["cache-type-k"]),
		CacheTypeV: argString(pr.Args["cache-type-v"]),
```

- [ ] **Step 5: Atualizar `commitDraft` para persistir os novos args**

Substitua o bloco `Args:` em `commitDraft` por:

```go
	args := map[string]any{
		"ngl":        float64(ngl),
		"ctx-size":   float64(ctx),
		"port":       float64(port),
		"flash-attn": d.FlashAttn,
	}
	if v, err := strconv.Atoi(d.BatchSize); err == nil {
		args["batch-size"] = float64(v)
	}
	if v, err := strconv.Atoi(d.UBatchSize); err == nil {
		args["ubatch-size"] = float64(v)
	}
	if d.CacheTypeK != "" {
		args["cache-type-k"] = d.CacheTypeK
	}
	if d.CacheTypeV != "" {
		args["cache-type-v"] = d.CacheTypeV
	}
	pr := domain.Profile{
		ID:          d.ID,
		Name:        d.Name,
		Description: d.Description,
		Model:       d.Model,
		Args:        args,
		Launch:      domain.LaunchConfig{DefaultBackground: true},
	}
```

- [ ] **Step 6: Atualizar callers em `cmd/model-loader/main.go`**

Em `main.go`, substituir:

```go
	root := ui.NewRoot(parseTab(cfg.UI.DefaultTab)).
		WithProfilesPage(pages.NewProfilesPage(store))
```

por (provisoriamente — wiring schema completo virá em T25):

```go
	root := ui.NewRoot(parseTab(cfg.UI.DefaultTab)).
		WithProfilesPage(pages.NewProfilesPage(store, llamahelp.EmbeddedSchema()))
```

E adicionar import: `"github.com/quantmind-br/model-loader/internal/service/llamahelp"`.

- [ ] **Step 7: Atualizar testes existentes em `profiles_test.go`**

Substitua `pages.NewProfilesPage(store)` por `pages.NewProfilesPage(store, domain.FlagSchema{})` (ou `llamahelp.EmbeddedSchema()`). Importe `domain` se necessário.

- [ ] **Step 8: Build + testes**

```bash
go build ./...
go test ./...
```

- [ ] **Step 9: Commit**

```bash
git add internal/ui/pages/profiles_editor.go internal/ui/pages/profiles.go internal/ui/pages/profiles_test.go cmd/model-loader/main.go
git commit -m "feat(ui/pages/profiles): essentials form pulls labels/defaults from FlagSchema"
```

---

### Task 21: Editor — view Advanced (tabela read-only)

**Files:**
- Modify: `internal/ui/pages/profiles_editor.go`
- Modify: `internal/ui/pages/profiles.go`

Read-only nesta task; edição inline fica fora do escopo do slice 2 (slice 6 polishes). Tabela mostra `long | type | default | help` ordenado por `long`.

- [ ] **Step 1: Adicionar componente em `profiles_editor.go`**

```go
import (
	"sort"
	"strings"

	"github.com/charmbracelet/bubbles/table"
)

func newAdvancedTable(schema domain.FlagSchema, width, height int) table.Model {
	cols := []table.Column{
		{Title: "Flag", Width: 24},
		{Title: "Type", Width: 8},
		{Title: "Default", Width: 12},
		{Title: "Help", Width: width - 24 - 8 - 12 - 8},
	}
	rows := schemaRows(schema)
	t := table.New(table.WithColumns(cols), table.WithRows(rows), table.WithFocused(true), table.WithHeight(height))
	return t
}

func schemaRows(schema domain.FlagSchema) []table.Row {
	names := make([]string, 0, len(schema.Flags))
	for k := range schema.Flags {
		names = append(names, k)
	}
	sort.Strings(names)
	rows := make([]table.Row, 0, len(names))
	for _, name := range names {
		spec := schema.Flags[name]
		rows = append(rows, table.Row{
			spec.Long,
			typeLabel(spec.Type),
			fmt.Sprintf("%v", spec.Default),
			truncate(spec.HelpText, 80),
		})
	}
	return rows
}

func typeLabel(t domain.FlagType) string {
	switch t {
	case domain.FlagTypeBool:
		return "bool"
	case domain.FlagTypeInt:
		return "int"
	case domain.FlagTypeFloat:
		return "float"
	case domain.FlagTypeEnum:
		return "enum"
	default:
		return "string"
	}
}

func truncate(s string, max int) string {
	if len(s) <= max {
		return s
	}
	return s[:max-1] + "…"
}

// filterRows returns rows whose Flag column contains q (case-insensitive).
func filterRows(all []table.Row, q string) []table.Row {
	if q == "" {
		return all
	}
	q = strings.ToLower(q)
	out := make([]table.Row, 0, len(all))
	for _, r := range all {
		if strings.Contains(strings.ToLower(r[0]), q) {
			out = append(out, r)
		}
	}
	return out
}
```

- [ ] **Step 2: Adicionar tabela ao state e chave de filtro**

Em `profiles.go` `ProfilesPage`:

```go
	advanced       table.Model
	advancedAll    []table.Row
	advancedFilter string
	filterMode     bool
```

Em `NewProfilesPage`, antes de `return`:

```go
	tbl := newAdvancedTable(schema, 100, 12)
```

E:

```go
	return ProfilesPage{
		store:       store,
		schema:      schema,
		advanced:    tbl,
		advancedAll: tbl.Rows(),
		// ... existing
	}
```

- [ ] **Step 3: Renderizar Advanced no `View`**

Substitua o bloco `editing`:

```go
	if p.editing && p.form != nil {
		header := theme.Title.Render(fmt.Sprintf("Editor — [%s]   ctrl+t to switch", p.subTab))
		var body string
		if p.subTab == subTabEssentials {
			body = p.form.View()
		} else {
			body = p.advanced.View()
		}
		filterLine := ""
		if p.subTab == subTabAdvanced {
			filterLine = theme.Subtitle.Render(fmt.Sprintf("filter: %q  (/ to edit, esc to clear)", p.advancedFilter))
		}
		return lipgloss.JoinVertical(lipgloss.Left, header, body, filterLine)
	}
```

- [ ] **Step 4: Routing de keys da tabela**

Em `updateForm`, antes do dispatch para `p.form.Update`:

```go
	if p.subTab == subTabAdvanced {
		switch msg.String() {
		case "/":
			p.filterMode = !p.filterMode
			return p, nil
		case "backspace":
			if p.filterMode && len(p.advancedFilter) > 0 {
				p.advancedFilter = p.advancedFilter[:len(p.advancedFilter)-1]
				p.advanced.SetRows(filterRows(p.advancedAll, p.advancedFilter))
			}
			return p, nil
		}
		if p.filterMode && len(msg.Runes) == 1 {
			p.advancedFilter += string(msg.Runes)
			p.advanced.SetRows(filterRows(p.advancedAll, p.advancedFilter))
			return p, nil
		}
		t, cmd := p.advanced.Update(msg)
		p.advanced = t
		return p, cmd
	}
```

- [ ] **Step 5: Build + testes**

```bash
go build ./...
go test ./...
```

- [ ] **Step 6: Commit**

```bash
git add internal/ui/pages/profiles.go internal/ui/pages/profiles_editor.go
git commit -m "feat(ui/pages/profiles): advanced sub-tab renders schema table with / filter"
```

---

### Task 22: Editor — render inline `ValidationReport`

**Files:**
- Modify: `internal/ui/pages/profiles.go`

`theme.Error` e `theme.Warn` já existem em `theme.go` (slice 0). Reusar.

- [ ] **Step 1: Validar live em `previewProfile` (puro)**

Em `profiles.go`, adicione:

```go
import (
	"github.com/quantmind-br/model-loader/internal/service/validator"
)

// previewProfile builds a Profile from the current draft (without saving) for
// the validator. Mirrors commitDraft's mapping but is allocation-only.
func (p ProfilesPage) previewProfile() domain.Profile {
	d := p.draft
	args := map[string]any{
		"flash-attn": d.FlashAttn,
	}
	if v, err := strconv.Atoi(d.NGL); err == nil {
		args["ngl"] = float64(v)
	}
	if v, err := strconv.Atoi(d.CtxSize); err == nil {
		args["ctx-size"] = float64(v)
	}
	if v, err := strconv.Atoi(d.BatchSize); err == nil {
		args["batch-size"] = float64(v)
	}
	if v, err := strconv.Atoi(d.UBatchSize); err == nil {
		args["ubatch-size"] = float64(v)
	}
	if v, err := strconv.Atoi(d.Port); err == nil {
		args["port"] = float64(v)
	}
	if d.CacheTypeK != "" {
		args["cache-type-k"] = d.CacheTypeK
	}
	if d.CacheTypeV != "" {
		args["cache-type-v"] = d.CacheTypeV
	}
	return domain.Profile{
		ID:    p.draft.ID,
		Model: d.Model,
		Args:  args,
	}
}
```

Adicione `validator` field ao struct:

```go
type ProfilesPage struct {
	// ... existing
	validator validator.Validator
}
```

E em `NewProfilesPage`:

```go
	return ProfilesPage{
		// ...
		validator: validator.New(),
	}
```

- [ ] **Step 2: Renderizar report no `View` quando editando**

Use `theme.Error` e `theme.Warn` já existentes (não criar styles novos):

```go
	if p.editing && p.form != nil {
		header := theme.Title.Render(fmt.Sprintf("Editor — [%s]   ctrl+t to switch", p.subTab))
		var body string
		if p.subTab == subTabEssentials {
			body = p.form.View()
		} else {
			body = p.advanced.View()
		}
		report := p.validator.Validate(p.previewProfile(), p.schema)
		var lines []string
		for _, e := range report.Errors {
			lines = append(lines, theme.Error.Render("✗ "+e.Field+": "+e.Message))
		}
		for _, w := range report.Warnings {
			lines = append(lines, theme.Warn.Render("! "+w.Field+": "+w.Message))
		}
		filterLine := ""
		if p.subTab == subTabAdvanced {
			filterLine = theme.Subtitle.Render(fmt.Sprintf("filter: %q", p.advancedFilter))
		}
		footer := strings.Join(lines, "\n")
		return lipgloss.JoinVertical(lipgloss.Left, header, body, filterLine, footer)
	}
```

Adicione `"strings"` aos imports de `profiles.go`.

- [ ] **Step 3: Build + testes**

```bash
go build ./...
go test ./...
```

- [ ] **Step 4: Commit**

```bash
git add internal/ui/theme/theme.go internal/ui/pages/profiles.go
git commit -m "feat(ui/pages/profiles): inline validation report under editor"
```

---

### Task 23: teatest smoke — validation aparece quando ubatch > batch

**Files:**
- Modify: `internal/ui/pages/profiles_test.go`

- [ ] **Step 1: Adicionar teste**

```go
func TestProfilesPage_ValidationShowsUbatchError(t *testing.T) {
	tmp := t.TempDir()
	store := profilestore.NewFSStore(tmp)
	page := pages.NewProfilesPage(store, llamahelp.EmbeddedSchema())

	tm := teatest.NewTestModel(t, page, teatest.WithInitialTermSize(120, 40))
	tm.Send(tea.WindowSizeMsg{Width: 120, Height: 40})

	// Open new profile editor.
	tm.Send(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'n'}})

	// Tab to ubatch field. Default form: name → desc → model → ngl → ctx → batch (2048) → ubatch (512).
	// Override ubatch to 4096 by typing into the input.
	// huh InputModel responds to typed runes; we move via tab.
	for i := 0; i < 6; i++ {
		tm.Send(tea.KeyMsg{Type: tea.KeyTab})
	}
	// Clear current ubatch value.
	for i := 0; i < 4; i++ {
		tm.Send(tea.KeyMsg{Type: tea.KeyBackspace})
	}
	tm.Send(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("4096")})

	teatest.WaitFor(t, tm.Output(), func(out []byte) bool {
		return bytes.Contains(out, []byte("ubatch-size")) && bytes.Contains(out, []byte("must be"))
	}, teatest.WithDuration(2*time.Second))

	tm.Send(tea.KeyMsg{Type: tea.KeyEsc})
	tm.Send(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'q'}})
	tm.WaitFinished(t, teatest.WithFinalTimeout(2*time.Second))
}
```

Adicione imports faltantes: `bytes`, `time`, `llamahelp`, etc.

- [ ] **Step 2: Rodar — esperar passar (depende da T20+T22 já wired)**

```bash
go test ./internal/ui/pages/ -run TestProfilesPage_ValidationShowsUbatchError -v
```

Se a teatest sequence falhar por causa de tab navigation no huh ou de valores não persistirem antes de re-render: investigar com `go test -v` e snapshot do `tm.Output()`. Pode ser necessário ajustar o número de Tabs ou usar `huh.Form`'s API direta.

- [ ] **Step 3: Commit**

```bash
git add internal/ui/pages/profiles_test.go
git commit -m "test(ui/pages/profiles): smoke validation surfaces ubatch>batch error"
```

---

### Task 24: Adicionar `WithStatusWarn` ao `RootModel`

**Files:**
- Modify: `internal/ui/root.go`

Schema é injetado direto na `ProfilesPage` em main (T20 já fez isso). `RootModel` não precisa de campo `schema`. O que precisa entrar agora é o builder `WithStatusWarn` para que main exiba o warn de fallback do parser (T25).

- [ ] **Step 1: Adicionar builder em `root.go`**

```go
// WithStatusWarn sets a warning message on the status bar (used at boot to
// surface schema fallback notices).
func (m RootModel) WithStatusWarn(msg string) RootModel {
	m.status.SetMessage(components.StatusWarn, msg)
	return m
}
```

- [ ] **Step 2: Build**

```bash
go build ./...
```

- [ ] **Step 3: Commit**

```bash
git add internal/ui/root.go
git commit -m "feat(ui/root): add WithStatusWarn builder for boot warnings"
```

---

## Phase D — Boot wiring (3 tasks)

### Task 25: `main.go` — boot do parser com fallback

**Files:**
- Modify: `cmd/model-loader/main.go`

- [ ] **Step 1: Reescrever `main.go` com parser boot + fallback**

Substitua o conteúdo de `cmd/model-loader/main.go` por:

```go
// Command model-loader launches the TUI for managing llama.cpp profiles.
package main

import (
	"context"
	"fmt"
	"os"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/quantmind-br/model-loader/internal/config"
	"github.com/quantmind-br/model-loader/internal/domain"
	"github.com/quantmind-br/model-loader/internal/service/llamahelp"
	"github.com/quantmind-br/model-loader/internal/service/profilestore"
	"github.com/quantmind-br/model-loader/internal/ui"
	"github.com/quantmind-br/model-loader/internal/ui/pages"
)

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

	root := ui.NewRoot(parseTab(cfg.UI.DefaultTab)).
		WithProfilesPage(pages.NewProfilesPage(store, schema))
	if schemaWarn != "" {
		root = root.WithStatusWarn(schemaWarn)
	}

	prog := tea.NewProgram(root, tea.WithAltScreen())
	if _, err := prog.Run(); err != nil {
		fmt.Fprintf(os.Stderr, "tui error: %v\n", err)
		os.Exit(1)
	}
}

// loadSchema attempts to parse llama-server --help. On failure (binary absent,
// timeout, parse error) it returns the embedded fallback and a warning string
// suitable for the status bar.
func loadSchema() (domain.FlagSchema, string) {
	parser := llamahelp.NewExecParser()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	schema, err := parser.Parse(ctx)
	if err != nil {
		return llamahelp.EmbeddedSchema(), fmt.Sprintf("schema fallback: %v", err)
	}
	return schema, ""
}

func parseTab(name string) ui.Tab {
	switch name {
	case "launcher":
		return ui.TabLauncher
	case "monitor":
		return ui.TabMonitor
	case "models":
		return ui.TabModels
	default:
		return ui.TabProfiles
	}
}
```

- [ ] **Step 2: Build + run smoke**

```bash
go build ./...
go run ./cmd/model-loader
```

Esperado: app abre, statusbar limpa (binário presente). Quitar com `q`.

- [ ] **Step 3: Smoke fallback**

```bash
env -i HOME="$HOME" PATH="/nonexistent" go run ./cmd/model-loader
```

Esperado: app abre, statusbar mostra warning de fallback. Quitar com `q`.

- [ ] **Step 4: Commit**

```bash
git add cmd/model-loader/main.go
git commit -m "feat(cmd): boot llamahelp parser with embedded fallback warn"
```

---

### Task 26: Documentar versionamento do fallback embutido

**Files:**
- Modify: `internal/service/llamahelp/embedded.go`

- [ ] **Step 1: Adicionar comentário descrevendo a versão pinada e como atualizar**

No topo do `embedded.go`, antes da função:

```go
// EmbeddedSchema returns the compile-time fallback FlagSchema covering the
// curated essential flags listed in the design spec. The schema is pinned to
// llama.cpp build "v7376 (380b4c9)" — last validated on 2026-04-28.
//
// To refresh against a newer llama.cpp build:
//   1. capture the help into testdata: llama-server --help > testdata/help-vXXXX.txt
//   2. re-run the golden: go test ./internal/service/llamahelp -update
//   3. eyeball flag types/defaults; only update embedded.go if essentials drift
//   4. bump the Version field below to "embedded-vXXXX"
```

- [ ] **Step 2: Build sanity**

```bash
go build ./...
```

- [ ] **Step 3: Commit**

```bash
git add internal/service/llamahelp/embedded.go
git commit -m "docs(llamahelp): document embedded schema refresh procedure"
```

---

### Task 27: Final sweep — testes integrados e build

**Files:**
- (sem mudanças de código; verificação)

- [ ] **Step 1: Rodar suite completa**

```bash
go test ./... -count=1
```

Esperado: todos os pacotes passam. Slice 2 estável.

- [ ] **Step 2: `go vet` e tidy**

```bash
go vet ./...
go mod tidy
git diff go.mod go.sum
```

Se `go.sum` tiver mudanças mínimas (hash de `bubbles/table`), commit junto. Se mudanças forem grandes (deps removidas/adicionadas inadvertidamente): investigar antes de commitar.

- [ ] **Step 3: Smoke manual final**

```bash
go run ./cmd/model-loader
```

Verificações manuais:
- App abre, navega entre 4 tabs.
- Em Profiles, `n` abre editor com Essentials.
- `ctrl+t` alterna pra Advanced; tabela renderiza ≥50 linhas; `/` filtra.
- Volta para Essentials; preenche Name + Model path com `/tmp/foo.gguf` (não existe), valida que aparece error inline `model: model file does not exist`.
- Salvar (`enter` no fim do form) cria arquivo em `~/.config/model-loader/profiles/`.
- Quit com `q`.

- [ ] **Step 4: Commit do tidy (se houver)**

```bash
git add go.mod go.sum
git commit -m "chore: tidy after slice 2 (bubbles/table)"
```

(Se sem alterações, pular.)

---

## Final do plano

Ao terminar a Task 27 esta slice está pronta para review/merge. Próximo slice (3) cobrirá `ModelScanner` + picker integrado no editor — substitui o input livre `Model path (.gguf)` da Task 20 por um modal de seleção.

**Arquivos-chave gerados/modificados nesta slice:**

- `testdata/help-v7376.txt` (T1) — fixture viva
- `testdata/help-v7376.golden.json` (T11) — golden do parser
- `testdata/fake-llama-help.sh` (T12) — fake binário p/ exec test
- `internal/domain/flag_schema.go` (T2) — FlagSpec.Aliases + Schema.Lookup
- `internal/service/llamahelp/{llamahelp,parser,exec_parser,embedded}.go` (T2-T13)
- `internal/service/validator/{validator,rules}.go` (T14-T17)
- `internal/ui/pages/profiles_editor.go` (T18-T22) — Essentials + Advanced + filter
- `internal/ui/pages/profiles.go` (T18-T24) — schema/validator injection, render report
- `internal/ui/root.go` (T24-T25) — schema thread-through, WithStatusWarn
- `cmd/model-loader/main.go` (T25) — exec parser boot + fallback

**Riscos conhecidos:**

- Parser de `--help` é frágil: se o llama.cpp mudar o layout (e.g., colunas, header style), rerodar fixture/golden e ajustar regex. Cobertura via golden file mitiga (diff visível).
- `teatest` com huh.Form pode ter timing-sensitive (Tab navigation entre fields). Se T23 ficar flaky, considere assertar via render direto do `previewProfile()` + `validator.Validate()` em vez de teatest.
- `WithStatusWarn` precisa que `StatusBar.Warn` retorne um novo `StatusBar` (imutável). Verificar API atual antes de implementar T25.
