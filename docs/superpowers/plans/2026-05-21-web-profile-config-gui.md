# Web Profile Configuration GUI — Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Replace the in-TUI `huh` profile editor with an on-demand, in-process web GUI that renders a per-backend, schema-driven form and lets the user edit the backend's validation criteria, fields, highlights, and cross-field rules from the browser.

**Architecture:** New `internal/service/configweb` package runs an `http.Server` on `127.0.0.1:0` in a goroutine launched by the TUI Profiles tab; it persists profiles via `profilestore` and backend edits via `backendcatalog.SchemaStore`, then signals the TUI through a result channel and shuts down. A single schema-driven render engine builds the form from `BackendValidationSchema.Flags` + a new editable `Presentation` block; cross-field `Rules` are evaluated by an extended validator.

**Tech Stack:** Go 1.26, `net/http`, `html/template`, `embed.FS`, vendored HTMX + Alpine.js + hand-written CSS. No Node toolchain.

**Spec:** `docs/superpowers/specs/2026-05-21-web-profile-config-gui-design.md`

---

## File Structure

**Domain (data model):**
- Modify `internal/domain/flag_schema.go` — add `Required` to `FlagSpec`; add `Rules` to `FlagSchema`.
- Modify `internal/domain/backend_schema.go` — add `Presentation`, `PresentationGroup`, `CrossFieldRule`, `Cond`, `Effect`; extend envelope + `ToFlagSchema`/`FlagSchemaToBackend`.

**Validator:**
- Modify `internal/service/validator/rules.go` — add `applyRequiredRules`, `applyCrossFieldRules`.
- Modify `internal/service/validator/validator.go` — wire the new steps into `Validate`.

**Presentation seed + migration:**
- Create `internal/service/backendschema/presentation.go` — per-backend essentials seed + `BuildPresentation`.
- Create `internal/service/backendschema/presentation_test.go`.
- Modify `internal/service/migration/migration.go` — `ensurePresentations` step.

**Web server (no UI yet):**
- Create `internal/service/configweb/session.go` — session lifecycle + result channel.
- Create `internal/service/configweb/viewmodel.go` — build render model from schema + presentation + draft.
- Create `internal/service/configweb/server.go` — routes, handlers.
- Create `internal/service/configweb/draft.go` — web `Draft` ⇄ `domain.Profile` mapping.
- Tests alongside each.

**Frontend assets:**
- Create `internal/service/configweb/assets/` — `embed.go`, `templates/*.gohtml`, `static/app.css`, `static/htmx.min.js`, `static/alpine.min.js`.

**TUI integration:**
- Create `internal/ui/pages/profiles_webedit.go` — launch cmd, modal, result handling.
- Modify `internal/ui/pages/profiles.go` / `profiles_update.go` / `profiles_crud.go` — replace `huh` editor launch; `IsCapturingInput`.
- Modify `internal/ui/root.go` (only if a new shortcut is added — none planned).
- Delete `internal/ui/pages/profile_editor/` after wiring.

**Docs/goldens:**
- Modify `docs/profile-schema.json` (if any persisted profile shape changes — none planned; envelope change only).
- Update backend schema golden fixtures via `go test ./... -update`.
- Update `CLAUDE.md` notes.

---

## Phase 1 — Domain model

### Task 1: Add `Required` to FlagSpec

**Files:**
- Modify: `internal/domain/flag_schema.go`
- Test: `internal/domain/flag_schema_test.go` (create if absent)

- [ ] **Step 1: Write the failing test**

```go
package domain

import (
	"encoding/json"
	"testing"
)

func TestFlagSpec_RequiredRoundTrip(t *testing.T) {
	in := FlagSpec{Long: "model", Type: FlagTypeString, Required: true}
	b, err := json.Marshal(in)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var out FlagSpec
	if err := json.Unmarshal(b, &out); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if !out.Required {
		t.Fatalf("Required not preserved: %+v", out)
	}
}

func TestFlagSpec_RequiredOmittedWhenFalse(t *testing.T) {
	b, _ := json.Marshal(FlagSpec{Long: "x", Type: FlagTypeString})
	if string(b) == "" || contains(string(b), "required") {
		t.Fatalf("required should be omitted when false, got %s", b)
	}
}

func contains(s, sub string) bool { return len(s) >= len(sub) && (func() bool {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
})() }
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/domain/ -run TestFlagSpec_Required -v`
Expected: FAIL — `out.Required` undefined / field missing.

- [ ] **Step 3: Add the field**

In `internal/domain/flag_schema.go`, add to `FlagSpec` (after `IsPort`):

```go
	IsPort   bool     `json:"isPort,omitempty"`
	Required bool     `json:"required,omitempty"`
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/domain/ -run TestFlagSpec_Required -v`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add internal/domain/flag_schema.go internal/domain/flag_schema_test.go
git commit -m "feat(domain): add Required to FlagSpec"
```

### Task 2: Add Presentation types

**Files:**
- Modify: `internal/domain/backend_schema.go`
- Test: `internal/domain/backend_schema_test.go` (create if absent)

- [ ] **Step 1: Write the failing test**

```go
package domain

import (
	"encoding/json"
	"testing"
)

func TestPresentation_RoundTrip(t *testing.T) {
	in := Presentation{Groups: []PresentationGroup{
		{Name: "Essenciais", Highlighted: true, Flags: []string{"ctx-size", "port"}},
		{Name: "Sampling", Flags: []string{"temp"}},
	}}
	b, err := json.Marshal(in)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var out Presentation
	if err := json.Unmarshal(b, &out); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if len(out.Groups) != 2 || out.Groups[0].Name != "Essenciais" || !out.Groups[0].Highlighted {
		t.Fatalf("groups not preserved: %+v", out)
	}
	if len(out.Groups[0].Flags) != 2 || out.Groups[0].Flags[1] != "port" {
		t.Fatalf("flag order not preserved: %+v", out.Groups[0])
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/domain/ -run TestPresentation -v`
Expected: FAIL — `Presentation` undefined.

- [ ] **Step 3: Add the types**

Append to `internal/domain/backend_schema.go`:

```go
// Presentation is the editable per-backend UI layout for the config web GUI.
// It controls which flags are shown, in what groups, and in what order. The
// first group with Highlighted=true renders at the top as "Essentials".
type Presentation struct {
	Groups []PresentationGroup `json:"groups"`
}

// PresentationGroup is an ordered set of flags rendered together.
type PresentationGroup struct {
	Name        string   `json:"name"`
	Highlighted bool     `json:"highlighted,omitempty"`
	Flags       []string `json:"flags"` // ordered flag long-names
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/domain/ -run TestPresentation -v`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add internal/domain/backend_schema.go internal/domain/backend_schema_test.go
git commit -m "feat(domain): add Presentation/PresentationGroup types"
```

### Task 3: Add CrossFieldRule types + extend envelope

**Files:**
- Modify: `internal/domain/backend_schema.go`
- Modify: `internal/domain/flag_schema.go` (add `Rules` to `FlagSchema`)
- Test: `internal/domain/backend_schema_test.go`

- [ ] **Step 1: Write the failing test**

```go
func TestBackendValidationSchema_EnvelopeRoundTrip(t *testing.T) {
	in := BackendValidationSchema{
		SchemaVersion: 1,
		Kind:          ValidationSchemaCLIFlagsV1,
		BackendKind:   BackendKindLlamaServer,
		BackendID:     "llama",
		Flags:         map[string]FlagSpec{"ctx-size": {Long: "ctx-size", Type: FlagTypeInt}},
		Presentation:  &Presentation{Groups: []PresentationGroup{{Name: "Essenciais", Highlighted: true, Flags: []string{"ctx-size"}}}},
		Rules: []CrossFieldRule{{
			ID:       "r1",
			When:     Cond{Flag: "flash-attn", Op: "eq", Value: "on"},
			Then:     Effect{Kind: "limit", Flag: "ctx-size", Op: "le", Value: "32768"},
			Severity: "warning",
		}},
	}
	b, err := json.Marshal(in)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var out BackendValidationSchema
	if err := json.Unmarshal(b, &out); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if out.Presentation == nil || len(out.Rules) != 1 {
		t.Fatalf("envelope not preserved: %+v", out)
	}
	if out.Rules[0].When.Flag != "flash-attn" || out.Rules[0].Then.Value != "32768" {
		t.Fatalf("rule not preserved: %+v", out.Rules[0])
	}
}

func TestToFlagSchema_CarriesRules(t *testing.T) {
	in := BackendValidationSchema{
		BackendKind: BackendKindLlamaServer,
		Rules:       []CrossFieldRule{{ID: "r1", Severity: "error"}},
	}
	fs := in.ToFlagSchema()
	if len(fs.Rules) != 1 {
		t.Fatalf("ToFlagSchema dropped rules: %+v", fs)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/domain/ -run 'EnvelopeRoundTrip|CarriesRules' -v`
Expected: FAIL — `CrossFieldRule`/`Presentation`/`Rules` undefined.

- [ ] **Step 3: Add the types and extend the envelope**

Append to `internal/domain/backend_schema.go`:

```go
// CrossFieldRule is a declarative validation rule spanning multiple flags.
type CrossFieldRule struct {
	ID       string `json:"id"`
	When     Cond   `json:"when"`
	Then     Effect `json:"then"`
	Severity string `json:"severity"` // "warning" | "error"
}

// Cond is a rule precondition. Op is one of: eq, ne, le, ge.
type Cond struct {
	Flag  string `json:"flag"`
	Op    string `json:"op"`
	Value string `json:"value"`
}

// Effect is a rule consequence. Kind is one of: limit, require, message.
type Effect struct {
	Kind    string `json:"kind"`
	Flag    string `json:"flag,omitempty"`
	Op      string `json:"op,omitempty"`
	Value   string `json:"value,omitempty"`
	Message string `json:"message,omitempty"`
}
```

In the `BackendValidationSchema` struct, add after `Flags`:

```go
	Flags        map[string]FlagSpec `json:"flags,omitempty"`
	Presentation *Presentation       `json:"presentation,omitempty"`
	Rules        []CrossFieldRule    `json:"rules,omitempty"`
```

Update `ToFlagSchema` to copy rules — change its return to include `Rules: s.Rules`:

```go
	return FlagSchema{
		Version:     version,
		BackendKind: s.BackendKind,
		Flags:       s.Flags,
		Rules:       s.Rules,
	}
```

In `internal/domain/flag_schema.go`, add to `FlagSchema`:

```go
type FlagSchema struct {
	Version     string
	BackendKind BackendKind
	Flags       map[string]FlagSpec
	Rules       []CrossFieldRule
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/domain/ -v`
Expected: PASS (all domain tests).

- [ ] **Step 5: Commit**

```bash
git add internal/domain/
git commit -m "feat(domain): add CrossFieldRule and extend schema envelope"
```

---

## Phase 2 — Validator

### Task 4: Required-flag presence rule

**Files:**
- Modify: `internal/service/validator/rules.go`
- Modify: `internal/service/validator/validator.go`
- Test: `internal/service/validator/validator_test.go`

- [ ] **Step 1: Write the failing test**

```go
func TestValidate_RequiredFlagMissing(t *testing.T) {
	schema := domain.FlagSchema{
		BackendKind: domain.BackendKindLlamaServer,
		Flags: map[string]FlagSpecAlias{}, // see note
	}
	_ = schema
	sch := domain.FlagSchema{
		Flags: map[string]domain.FlagSpec{
			"port": {Long: "port", Type: domain.FlagTypeInt, Required: true},
		},
	}
	p := domain.Profile{Args: map[string]any{}} // port absent
	rep := New(nil).Validate(p, sch, domain.BackendKindLlamaServer)
	if !rep.HasBlockingErrors() {
		t.Fatalf("expected error for missing required flag")
	}
}

func TestValidate_RequiredFlagPresent(t *testing.T) {
	sch := domain.FlagSchema{
		Flags: map[string]domain.FlagSpec{
			"port": {Long: "port", Type: domain.FlagTypeInt, Required: true},
		},
	}
	p := domain.Profile{Args: map[string]any{"port": 4321}}
	rep := New(nil).Validate(p, sch, domain.BackendKindLlamaServer)
	if rep.HasBlockingErrors() {
		t.Fatalf("unexpected errors: %+v", rep.Errors)
	}
}
```

> Note: delete the stray `FlagSpecAlias` scaffold line; it is only there to remind you the first block is illustrative. Keep only the `sch`-based assertions.

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/service/validator/ -run RequiredFlag -v`
Expected: FAIL — no required-flag check exists.

- [ ] **Step 3: Implement the rule**

Add to `internal/service/validator/rules.go`:

```go
// applyRequiredRules flags any schema flag marked Required that is absent from
// both Args and ExtraArgs.
func applyRequiredRules(p domain.Profile, schema domain.FlagSchema, rep Report) Report {
	for long, spec := range schema.Flags {
		if !spec.Required {
			continue
		}
		if _, ok := p.Args[long]; ok {
			continue
		}
		if _, ok := p.Args[domain.CanonicalFlag(spec.Short)]; spec.Short != "" && ok {
			continue
		}
		if extraArgsContain(p.ExtraArgs, spec) {
			continue
		}
		rep = appendIssue(rep, FieldIssue{
			Field:    long,
			Message:  "required flag is missing",
			Severity: SeverityError,
		})
	}
	return rep
}

func extraArgsContain(extra []string, spec domain.FlagSpec) bool {
	for _, a := range extra {
		flag, _, _ := parseExtraArg(a)
		c := domain.CanonicalFlag(flag)
		if c == spec.Long || c == spec.Short {
			return true
		}
		for _, al := range spec.Aliases {
			if c == al {
				return true
			}
		}
	}
	return false
}
```

In `internal/service/validator/validator.go`, add the step in `Validate` after `applyExtraArgsRules`:

```go
	rep = applyTypeRules(p, schema, rep)
	rep = applyExtraArgsRules(p, schema, rep)
	rep = applyRequiredRules(p, schema, rep)
	rep = applyExistenceRules(p, kind, rep)
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/service/validator/ -run RequiredFlag -v`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add internal/service/validator/
git commit -m "feat(validator): enforce Required flags"
```

### Task 5: Cross-field rule evaluation

**Files:**
- Create: `internal/service/validator/crossfield.go`
- Modify: `internal/service/validator/validator.go`
- Test: `internal/service/validator/crossfield_test.go`

- [ ] **Step 1: Write the failing test**

```go
package validator

import (
	"testing"

	"github.com/quantmind-br/model-loader/internal/domain"
)

func ruleSchema(rule domain.CrossFieldRule, flags map[string]domain.FlagSpec) domain.FlagSchema {
	return domain.FlagSchema{Flags: flags, Rules: []domain.CrossFieldRule{rule}}
}

func TestCrossField_LimitViolation(t *testing.T) {
	rule := domain.CrossFieldRule{
		ID:       "r1",
		When:     domain.Cond{Flag: "flash-attn", Op: "eq", Value: "on"},
		Then:     domain.Effect{Kind: "limit", Flag: "ctx-size", Op: "le", Value: "32768"},
		Severity: "warning",
	}
	flags := map[string]domain.FlagSpec{
		"flash-attn": {Long: "flash-attn", Type: domain.FlagTypeString},
		"ctx-size":   {Long: "ctx-size", Type: domain.FlagTypeInt},
	}
	p := domain.Profile{Args: map[string]any{"flash-attn": "on", "ctx-size": 65536}}
	rep := New(nil).Validate(p, ruleSchema(rule, flags), domain.BackendKindLlamaServer)
	if len(rep.Warnings) != 1 {
		t.Fatalf("expected 1 warning, got %+v", rep)
	}
}

func TestCrossField_ConditionFalseSkips(t *testing.T) {
	rule := domain.CrossFieldRule{
		ID:       "r1",
		When:     domain.Cond{Flag: "flash-attn", Op: "eq", Value: "on"},
		Then:     domain.Effect{Kind: "message", Message: "x"},
		Severity: "error",
	}
	flags := map[string]domain.FlagSpec{"flash-attn": {Long: "flash-attn", Type: domain.FlagTypeString}}
	p := domain.Profile{Args: map[string]any{"flash-attn": "off"}}
	rep := New(nil).Validate(p, ruleSchema(rule, flags), domain.BackendKindLlamaServer)
	if rep.HasBlockingErrors() {
		t.Fatalf("rule should not fire when condition false: %+v", rep)
	}
}

func TestCrossField_RequireMissing(t *testing.T) {
	rule := domain.CrossFieldRule{
		ID:       "r1",
		When:     domain.Cond{Flag: "cache-type-k", Op: "ne", Value: "f16"},
		Then:     domain.Effect{Kind: "require", Flag: "flash-attn", Value: "on"},
		Severity: "error",
	}
	flags := map[string]domain.FlagSpec{
		"cache-type-k": {Long: "cache-type-k", Type: domain.FlagTypeString},
		"flash-attn":   {Long: "flash-attn", Type: domain.FlagTypeString},
	}
	p := domain.Profile{Args: map[string]any{"cache-type-k": "q8_0"}} // flash-attn absent
	rep := New(nil).Validate(p, ruleSchema(rule, flags), domain.BackendKindLlamaServer)
	if !rep.HasBlockingErrors() {
		t.Fatalf("expected error: require flash-attn=on")
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/service/validator/ -run CrossField -v`
Expected: FAIL — `applyCrossFieldRules` undefined.

- [ ] **Step 3: Implement evaluation**

Create `internal/service/validator/crossfield.go`:

```go
package validator

import (
	"strconv"

	"github.com/quantmind-br/model-loader/internal/domain"
)

func applyCrossFieldRules(p domain.Profile, schema domain.FlagSchema, rep Report) Report {
	for _, rule := range schema.Rules {
		if !condHolds(rule.When, p, schema) {
			continue
		}
		sev := SeverityWarning
		if rule.Severity == "error" {
			sev = SeverityError
		}
		switch rule.Then.Kind {
		case "message":
			rep = appendIssue(rep, FieldIssue{Field: rule.When.Flag, Message: rule.Then.Message, Severity: sev})
		case "limit":
			if !compare(argString(p, rule.Then.Flag, schema), rule.Then.Op, rule.Then.Value, schema, rule.Then.Flag) {
				rep = appendIssue(rep, FieldIssue{
					Field:    rule.Then.Flag,
					Message:  ruleMessage(rule),
					Severity: sev,
				})
			}
		case "require":
			got := argString(p, rule.Then.Flag, schema)
			if got == "" || (rule.Then.Value != "" && got != rule.Then.Value) {
				rep = appendIssue(rep, FieldIssue{
					Field:    rule.Then.Flag,
					Message:  ruleMessage(rule),
					Severity: sev,
				})
			}
		}
	}
	return rep
}

func ruleMessage(r domain.CrossFieldRule) string {
	if r.Then.Message != "" {
		return r.Then.Message
	}
	return "cross-field rule violated (when " + r.When.Flag + ")"
}

func condHolds(c domain.Cond, p domain.Profile, schema domain.FlagSchema) bool {
	return compare(argString(p, c.Flag, schema), c.Op, c.Value, schema, c.Flag)
}

// argString renders the profile arg for flag as a string for comparison.
// Bools render "on"/"off"; numbers via strconv; missing -> "".
func argString(p domain.Profile, flag string, schema domain.FlagSchema) string {
	v, ok := p.Args[flag]
	if !ok {
		return ""
	}
	switch x := v.(type) {
	case string:
		return x
	case bool:
		if x {
			return "on"
		}
		return "off"
	case float64:
		if x == float64(int64(x)) {
			return strconv.FormatInt(int64(x), 10)
		}
		return strconv.FormatFloat(x, 'f', -1, 64)
	case int:
		return strconv.Itoa(x)
	default:
		return ""
	}
}

// compare evaluates "got op want". For le/ge it tries numeric comparison and
// falls back to string comparison when either side is non-numeric.
func compare(got, op, want string, schema domain.FlagSchema, flag string) bool {
	switch op {
	case "eq":
		return got == want
	case "ne":
		return got != want
	case "le", "ge":
		gf, gerr := strconv.ParseFloat(got, 64)
		wf, werr := strconv.ParseFloat(want, 64)
		if gerr == nil && werr == nil {
			if op == "le" {
				return gf <= wf
			}
			return gf >= wf
		}
		if op == "le" {
			return got <= want
		}
		return got >= want
	}
	return false
}
```

In `internal/service/validator/validator.go`, add after `applyRequiredRules`:

```go
	rep = applyRequiredRules(p, schema, rep)
	rep = applyCrossFieldRules(p, schema, rep)
	rep = applyExistenceRules(p, kind, rep)
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/service/validator/ -v`
Expected: PASS (all validator tests, including pre-existing).

- [ ] **Step 5: Commit**

```bash
git add internal/service/validator/
git commit -m "feat(validator): evaluate cross-field rules"
```

---

## Phase 3 — Presentation seed + migration

### Task 6: Presentation builder + essentials seed

**Files:**
- Create: `internal/service/backendschema/presentation.go`
- Test: `internal/service/backendschema/presentation_test.go`

> The essentials seed below is copied from the current
> `internal/ui/pages/profile_editor/essentials.go` (`essentialFields`) so the
> highlighted "Essenciais" group preserves today's curated UX after that package
> is deleted in Phase 6.

- [ ] **Step 1: Write the failing test**

```go
package backendschema

import (
	"testing"

	"github.com/quantmind-br/model-loader/internal/domain"
)

func TestBuildPresentation_HighlightsEssentialsFirst(t *testing.T) {
	schema := domain.BackendValidationSchema{
		BackendKind: domain.BackendKindLlamaServer,
		Flags: map[string]domain.FlagSpec{
			"ctx-size": {Long: "ctx-size", Type: domain.FlagTypeInt, Group: "common"},
			"temp":     {Long: "temp", Type: domain.FlagTypeFloat, Group: "sampling"},
			"port":     {Long: "port", Type: domain.FlagTypeInt, Group: "common"},
		},
	}
	pres := BuildPresentation(schema)
	if len(pres.Groups) == 0 || !pres.Groups[0].Highlighted {
		t.Fatalf("first group must be highlighted essentials: %+v", pres)
	}
	// ctx-size and port are llama essentials; temp is not.
	if !containsStr(pres.Groups[0].Flags, "ctx-size") || !containsStr(pres.Groups[0].Flags, "port") {
		t.Fatalf("essentials missing: %+v", pres.Groups[0])
	}
	// temp must appear in some non-highlighted group.
	if !flagInAnyGroup(pres, "temp") {
		t.Fatalf("temp should be grouped somewhere: %+v", pres)
	}
}

func containsStr(xs []string, s string) bool {
	for _, x := range xs {
		if x == s {
			return true
		}
	}
	return false
}

func flagInAnyGroup(p domain.Presentation, flag string) bool {
	for _, g := range p.Groups {
		if containsStr(g.Flags, flag) {
			return true
		}
	}
	return false
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/service/backendschema/ -run BuildPresentation -v`
Expected: FAIL — `BuildPresentation` undefined.

- [ ] **Step 3: Implement the builder + seed**

Create `internal/service/backendschema/presentation.go`:

```go
package backendschema

import (
	"sort"

	"github.com/quantmind-br/model-loader/internal/domain"
)

// essentialSeed lists, per backend kind, the flag long-names that should land
// in the highlighted "Essenciais" group when synthesizing a default
// Presentation. Copied from the retired profile_editor essentials registry.
var essentialSeed = map[domain.BackendKind][]string{
	domain.BackendKindLlamaServer:  {"n-gpu-layers", "ctx-size", "batch-size", "ubatch-size", "port", "flash-attn", "cache-type-k", "cache-type-v"},
	domain.BackendKindBuunLlamaCpp: {"n-gpu-layers", "ctx-size", "batch-size", "ubatch-size", "port", "flash-attn", "cache-type-k", "cache-type-v"},
	domain.BackendKindVLLM:         {"tensor-parallel-size", "gpu-memory-utilization", "max-model-len", "dtype", "quantization", "port", "served-model-name"},
	domain.BackendKindSGLang:       {"tp-size", "dp-size", "mem-fraction-static", "dtype", "quantization", "context-length", "port", "served-model-name"},
	domain.BackendKindDFlash:       {"draft", "max-ctx", "budget", "verify-mode", "cache-type-k", "cache-type-v", "fa-window"},
}

// BuildPresentation synthesizes a default Presentation from a schema:
// the highlighted "Essenciais" group holds the seed flags that exist in the
// schema (in seed order); remaining flags are grouped by FlagSpec.Group
// (alphabetical group order, alphabetical flags within a group).
func BuildPresentation(schema domain.BackendValidationSchema) domain.Presentation {
	essentialSet := map[string]bool{}
	var essentials []string
	for _, long := range essentialSeed[schema.BackendKind] {
		if _, ok := schema.Flags[long]; ok {
			essentials = append(essentials, long)
			essentialSet[long] = true
		}
	}

	byGroup := map[string][]string{}
	for long, spec := range schema.Flags {
		if essentialSet[long] {
			continue
		}
		g := spec.Group
		if g == "" {
			g = "other"
		}
		byGroup[g] = append(byGroup[g], long)
	}

	groups := []domain.PresentationGroup{{
		Name:        "Essenciais",
		Highlighted: true,
		Flags:       essentials,
	}}

	groupNames := make([]string, 0, len(byGroup))
	for g := range byGroup {
		groupNames = append(groupNames, g)
	}
	sort.Strings(groupNames)
	for _, g := range groupNames {
		flags := byGroup[g]
		sort.Strings(flags)
		groups = append(groups, domain.PresentationGroup{Name: g, Flags: flags})
	}
	return domain.Presentation{Groups: groups}
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/service/backendschema/ -run BuildPresentation -v`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add internal/service/backendschema/presentation.go internal/service/backendschema/presentation_test.go
git commit -m "feat(backendschema): synthesize default Presentation from schema"
```

### Task 7: Migration — ensure every schema has a Presentation

**Files:**
- Modify: `internal/service/migration/migration.go`
- Test: `internal/service/migration/migration_test.go`

- [ ] **Step 1: Write the failing test**

```go
func TestEnsurePresentations_SeedsMissing(t *testing.T) {
	dir := t.TempDir()
	schemaStore := backendcatalog.NewFSSchemaStore(dir) // see existing constructor name
	ref := "llama.json"
	_ = schemaStore.Save(ref, domain.BackendValidationSchema{
		SchemaVersion: 1,
		Kind:          domain.ValidationSchemaCLIFlagsV1,
		BackendKind:   domain.BackendKindLlamaServer,
		BackendID:     "llama",
		Flags:         map[string]domain.FlagSpec{"ctx-size": {Long: "ctx-size", Type: domain.FlagTypeInt, Group: "common"}},
	})
	catalog := domain.BackendCatalog{
		SchemaVersion:    1,
		DefaultBackendID: "llama",
		Backends:         []domain.Backend{{ID: "llama", Kind: domain.BackendKindLlamaServer, SchemaRef: "schemas/llama.json"}},
	}
	if n, err := ensurePresentations(catalog, schemaStore); err != nil || n != 1 {
		t.Fatalf("ensurePresentations n=%d err=%v", n, err)
	}
	got, _ := schemaStore.Load(ref)
	if got.Presentation == nil || len(got.Presentation.Groups) == 0 {
		t.Fatalf("presentation not seeded: %+v", got)
	}
	// idempotent: second run seeds nothing.
	if n, _ := ensurePresentations(catalog, schemaStore); n != 0 {
		t.Fatalf("expected idempotent second run, seeded %d", n)
	}
}
```

> Confirm the real schema-store constructor name in `internal/service/backendcatalog/` (e.g. `NewFSSchemaStore`/`NewSchemaStore`) and the `Save` ref convention before running. Adjust the test call to match.

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/service/migration/ -run EnsurePresentations -v`
Expected: FAIL — `ensurePresentations` undefined.

- [ ] **Step 3: Implement and wire it**

Add to `internal/service/migration/migration.go`:

```go
// ensurePresentations seeds a default Presentation into every backend schema
// that lacks one. Idempotent: schemas that already have a Presentation are
// skipped. Returns the number of schemas updated. It does NOT mark the schema
// Source.Editable — a synthesized default is not a manual edit, so RefreshSchema
// is free to regenerate it.
func ensurePresentations(catalog domain.BackendCatalog, schemaStore backendcatalog.SchemaStore) (int, error) {
	updated := 0
	for _, b := range catalog.Backends {
		ref := backendcatalog.SchemaStoreRef(b.SchemaRef)
		schema, err := schemaStore.Load(ref)
		if err != nil {
			continue // missing schema is handled elsewhere; skip
		}
		if schema.Presentation != nil {
			continue
		}
		pres := backendschema.BuildPresentation(schema)
		schema.Presentation = &pres
		if err := schemaStore.Save(ref, schema); err != nil {
			return updated, fmt.Errorf("save presentation for %s: %w", b.ID, err)
		}
		updated++
	}
	return updated, nil
}
```

Call it at the end of `Run`, after the profile loop, before `return rep, nil`:

```go
	catalog, _ = s.catalogStore.Load()
	if n, err := ensurePresentations(catalog, s.schemaStore); err != nil {
		rep.Warnings = append(rep.Warnings, fmt.Sprintf("ensure presentations: %v", err))
	} else if n > 0 {
		rep.Warnings = append(rep.Warnings, fmt.Sprintf("seeded %d backend presentation(s)", n))
	}

	return rep, nil
```

> Because `Run` early-returns when `needsMigration` is false, also call
> `ensurePresentations` on that early-return path so existing installs get
> seeded. Replace the `if !needs { return rep, nil }` block with:
> ```go
> if !needs {
> 	catalog, err := s.catalogStore.Load()
> 	if err == nil {
> 		_, _ = ensurePresentations(catalog, s.schemaStore)
> 	}
> 	return rep, nil
> }
> ```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/service/migration/ -v`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add internal/service/migration/
git commit -m "feat(migration): seed default Presentation into backend schemas"
```

---

## Phase 4 — configweb server (backend only)

### Task 8: Web Draft ⇄ Profile mapping

**Files:**
- Create: `internal/service/configweb/draft.go`
- Test: `internal/service/configweb/draft_test.go`

- [ ] **Step 1: Write the failing test**

```go
package configweb

import (
	"testing"

	"github.com/quantmind-br/model-loader/internal/domain"
)

func TestDraft_ToProfile_TypesArgsViaSchema(t *testing.T) {
	schema := domain.FlagSchema{Flags: map[string]domain.FlagSpec{
		"ctx-size":   {Long: "ctx-size", Type: domain.FlagTypeInt},
		"flash-attn": {Long: "flash-attn", Type: domain.FlagTypeString},
	}}
	d := Draft{
		ID:        "qwen",
		Name:      "Qwen",
		Model:     "/models/qwen.gguf",
		BackendID: "llama",
		Args:      map[string]string{"ctx-size": "8192", "flash-attn": "on"},
	}
	p := d.ToProfile(schema)
	if p.Args["ctx-size"] != float64(8192) && p.Args["ctx-size"] != 8192 {
		t.Fatalf("ctx-size not coerced to int: %#v", p.Args["ctx-size"])
	}
	if p.Args["flash-attn"] != "on" {
		t.Fatalf("flash-attn wrong: %#v", p.Args["flash-attn"])
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/service/configweb/ -run Draft -v`
Expected: FAIL — package/`Draft` undefined.

- [ ] **Step 3: Implement the Draft type and mapping**

Create `internal/service/configweb/draft.go`:

```go
// Package configweb runs the on-demand web GUI used by the TUI to edit profiles.
package configweb

import (
	"strconv"
	"time"

	"github.com/quantmind-br/model-loader/internal/domain"
)

// Draft is the web form payload for a profile being created or edited.
// Args are string-typed at the boundary (HTML form values) and coerced to the
// schema's FlagType in ToProfile/ApplyTo.
type Draft struct {
	ID        string            `json:"id"`
	OrigID    string            `json:"origId"`
	IsNew     bool              `json:"isNew"`
	Name      string            `json:"name"`
	Description string           `json:"description"`
	Tags      []string          `json:"tags"`
	Model     string            `json:"model"`
	BackendID string            `json:"backendId"`
	Args      map[string]string `json:"args"`
	ExtraArgs []string          `json:"extraArgs"`
}

// ToProfile builds a fresh Profile from the draft, coercing arg values by type.
func (d Draft) ToProfile(schema domain.FlagSchema) domain.Profile {
	now := time.Now().UTC()
	return domain.Profile{
		SchemaVersion: domain.SchemaVersion,
		ID:            d.ID,
		Name:          d.Name,
		Description:   d.Description,
		Tags:          d.Tags,
		Model:         d.Model,
		Args:          coerceArgs(d.Args, schema),
		ExtraArgs:     d.ExtraArgs,
		Launch:        domain.LaunchConfig{BackendID: d.BackendID},
		Meta:          domain.ProfileMeta{CreatedAt: now, UpdatedAt: now},
	}
}

// ApplyTo overlays the draft onto an existing profile, preserving Meta/Launch
// fields the web form does not edit.
func (d Draft) ApplyTo(existing domain.Profile, schema domain.FlagSchema) domain.Profile {
	existing.Name = d.Name
	existing.Description = d.Description
	existing.Tags = d.Tags
	existing.Model = d.Model
	existing.Args = coerceArgs(d.Args, schema)
	existing.ExtraArgs = d.ExtraArgs
	existing.Launch.BackendID = d.BackendID
	existing.Meta.UpdatedAt = time.Now().UTC()
	return existing
}

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
			out[k] = v == "on" || v == "true"
		default:
			out[k] = v
		}
	}
	return out
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/service/configweb/ -run Draft -v`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add internal/service/configweb/draft.go internal/service/configweb/draft_test.go
git commit -m "feat(configweb): web Draft to Profile mapping"
```

### Task 9: View model builder

**Files:**
- Create: `internal/service/configweb/viewmodel.go`
- Test: `internal/service/configweb/viewmodel_test.go`

- [ ] **Step 1: Write the failing test**

```go
package configweb

import (
	"testing"

	"github.com/quantmind-br/model-loader/internal/domain"
)

func TestBuildViewModel_OrdersGroupsAndWidgets(t *testing.T) {
	schema := domain.BackendValidationSchema{
		BackendKind: domain.BackendKindLlamaServer,
		Flags: map[string]domain.FlagSpec{
			"ctx-size":   {Long: "ctx-size", Type: domain.FlagTypeInt, Default: 8192},
			"flash-attn": {Long: "flash-attn", Type: domain.FlagTypeEnum, EnumValues: []string{"on", "off", "auto"}},
		},
		Presentation: &domain.Presentation{Groups: []domain.PresentationGroup{
			{Name: "Essenciais", Highlighted: true, Flags: []string{"ctx-size", "flash-attn"}},
		}},
	}
	d := Draft{Args: map[string]string{"ctx-size": "4096"}}
	vm := BuildViewModel(d, schema)
	if len(vm.Groups) != 1 || vm.Groups[0].Name != "Essenciais" {
		t.Fatalf("groups wrong: %+v", vm.Groups)
	}
	f0 := vm.Groups[0].Fields[0]
	if f0.Flag != "ctx-size" || f0.Widget != "number" || f0.Value != "4096" {
		t.Fatalf("field 0 wrong: %+v", f0)
	}
	f1 := vm.Groups[0].Fields[1]
	if f1.Widget != "select" || len(f1.Options) != 3 {
		t.Fatalf("enum widget wrong: %+v", f1)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/service/configweb/ -run BuildViewModel -v`
Expected: FAIL — `BuildViewModel` undefined.

- [ ] **Step 3: Implement**

Create `internal/service/configweb/viewmodel.go`:

```go
package configweb

import (
	"strconv"

	"github.com/quantmind-br/model-loader/internal/domain"
)

// ViewModel is the template data for the editor page.
type ViewModel struct {
	Draft  Draft
	Groups []GroupVM
}

type GroupVM struct {
	Name        string
	Highlighted bool
	Fields      []FieldVM
}

type FieldVM struct {
	Flag        string
	Label       string
	Help        string
	Widget      string // number | select | toggle | text
	Value       string
	Options     []string
	Required    bool
	Min, Max    *int
}

// BuildViewModel renders schema + presentation + draft into template data.
func BuildViewModel(d Draft, schema domain.BackendValidationSchema) ViewModel {
	vm := ViewModel{Draft: d}
	pres := schema.Presentation
	if pres == nil {
		// Defensive: a schema with no presentation renders a single group.
		var flags []string
		for long := range schema.Flags {
			flags = append(flags, long)
		}
		pres = &domain.Presentation{Groups: []domain.PresentationGroup{{Name: "Flags", Highlighted: true, Flags: flags}}}
	}
	for _, g := range pres.Groups {
		gvm := GroupVM{Name: g.Name, Highlighted: g.Highlighted}
		for _, long := range g.Flags {
			spec, ok := schema.Flags[long]
			if !ok {
				continue
			}
			gvm.Fields = append(gvm.Fields, fieldVM(long, spec, d))
		}
		vm.Groups = append(vm.Groups, gvm)
	}
	return vm
}

func fieldVM(long string, spec domain.FlagSpec, d domain.Profile) FieldVM { return FieldVM{} } // overwritten below
```

> Replace the stub `fieldVM` with the real one (it takes the Draft, not a Profile):

```go
func fieldVM(long string, spec domain.FlagSpec, d Draft) FieldVM {
	f := FieldVM{
		Flag:     long,
		Label:    long,
		Help:     spec.HelpText,
		Required: spec.Required,
		Min:      spec.Min,
		Max:      spec.Max,
	}
	switch spec.Type {
	case domain.FlagTypeInt, domain.FlagTypeFloat:
		f.Widget = "number"
	case domain.FlagTypeBool:
		f.Widget = "toggle"
	case domain.FlagTypeEnum:
		f.Widget = "select"
		f.Options = spec.EnumValues
	default:
		f.Widget = "text"
	}
	if v, ok := d.Args[long]; ok {
		f.Value = v
	} else if spec.Default != nil {
		f.Value = defaultString(spec.Default)
	}
	return f
}

func defaultString(v any) string {
	switch x := v.(type) {
	case string:
		return x
	case bool:
		if x {
			return "on"
		}
		return "off"
	case int:
		return strconv.Itoa(x)
	case float64:
		if x == float64(int64(x)) {
			return strconv.FormatInt(int64(x), 10)
		}
		return strconv.FormatFloat(x, 'f', -1, 64)
	default:
		return ""
	}
}
```

> Delete the stub `fieldVM` line from the first block; keep only the real implementation.

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/service/configweb/ -run BuildViewModel -v`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add internal/service/configweb/viewmodel.go internal/service/configweb/viewmodel_test.go
git commit -m "feat(configweb): build editor view model from schema+presentation"
```

### Task 10: Session lifecycle + result channel

**Files:**
- Create: `internal/service/configweb/session.go`
- Test: `internal/service/configweb/session_test.go`

- [ ] **Step 1: Write the failing test**

```go
package configweb

import (
	"net/http"
	"testing"
	"time"
)

func TestSession_StartServesAndShutsDownOnSave(t *testing.T) {
	s := NewSession(Deps{}) // empty deps: GET / still serves a page
	url, err := s.Start()
	if err != nil {
		t.Fatalf("start: %v", err)
	}
	resp, err := http.Get(url + "/healthz")
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	resp.Body.Close()
	if resp.StatusCode != 200 {
		t.Fatalf("healthz status %d", resp.StatusCode)
	}
	// Signal completion and assert the result channel delivers + server stops.
	s.complete(Result{Saved: false})
	select {
	case res := <-s.Done():
		if res.Saved {
			t.Fatalf("expected cancel result")
		}
	case <-time.After(2 * time.Second):
		t.Fatalf("timeout waiting for Done")
	}
	if _, err := http.Get(url + "/healthz"); err == nil {
		t.Fatalf("server should be down after complete")
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/service/configweb/ -run Session -v`
Expected: FAIL — `NewSession`/`Deps`/`Result` undefined.

- [ ] **Step 3: Implement**

Create `internal/service/configweb/session.go`:

```go
package configweb

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"sync"

	"github.com/quantmind-br/model-loader/internal/service/backendcatalog"
	"github.com/quantmind-br/model-loader/internal/service/profilestore"
)

// Deps are the stores/services the session needs to read and persist data.
type Deps struct {
	Profiles     profilestore.Store
	Catalog      backendcatalog.Store
	Schemas      backendcatalog.SchemaStore
	InitialDraft Draft
}

// Result is delivered on Done() when the session finishes.
type Result struct {
	Saved     bool
	ProfileID string
	Err       error
}

// Session owns one editing browser session and its HTTP server.
type Session struct {
	deps   Deps
	srv    *http.Server
	done   chan Result
	once   sync.Once
	cancel context.CancelFunc
}

func NewSession(deps Deps) *Session {
	return &Session{deps: deps, done: make(chan Result, 1)}
}

// Done delivers the single terminal Result.
func (s *Session) Done() <-chan Result { return s.done }

// Start binds an ephemeral localhost port and serves in a goroutine.
// Returns the base URL (http://127.0.0.1:PORT).
func (s *Session) Start() (string, error) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return "", fmt.Errorf("bind: %w", err)
	}
	_, ctx := context.Background(), context.Background()
	ctx, s.cancel = context.WithCancel(ctx)

	mux := http.NewServeMux()
	s.routes(mux)
	s.srv = &http.Server{Handler: mux}

	go func() {
		_ = s.srv.Serve(ln)
	}()
	go func() {
		<-ctx.Done()
		_ = s.srv.Close()
	}()

	port := ln.Addr().(*net.TCPAddr).Port
	return fmt.Sprintf("http://127.0.0.1:%d", port), nil
}

// complete delivers the result once and triggers shutdown.
func (s *Session) complete(res Result) {
	s.once.Do(func() {
		s.done <- res
		if s.cancel != nil {
			s.cancel()
		}
	})
}
```

> `s.routes(mux)` is implemented in Task 11. For this task, add a temporary
> minimal `routes` so the package compiles and `/healthz` works:

Create `internal/service/configweb/server.go` with just:

```go
package configweb

import "net/http"

func (s *Session) routes(mux *http.ServeMux) {
	mux.HandleFunc("/healthz", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/service/configweb/ -run Session -v`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add internal/service/configweb/session.go internal/service/configweb/server.go internal/service/configweb/session_test.go
git commit -m "feat(configweb): session lifecycle with ephemeral server"
```

### Task 11: `/validate` handler

**Files:**
- Modify: `internal/service/configweb/server.go`
- Create: `internal/service/configweb/handlers.go`
- Test: `internal/service/configweb/handlers_test.go`

- [ ] **Step 1: Write the failing test**

```go
package configweb

import (
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/quantmind-br/model-loader/internal/domain"
)

func TestValidateHandler_ReportsUnknownFlag(t *testing.T) {
	schema := domain.BackendValidationSchema{
		BackendKind: domain.BackendKindLlamaServer,
		BackendID:   "llama",
		Flags:       map[string]domain.FlagSpec{"ctx-size": {Long: "ctx-size", Type: domain.FlagTypeInt}},
	}
	s := &Session{deps: Deps{Schemas: stubSchemaStore{schema: schema}, Catalog: stubCatalog{id: "llama", ref: "llama.json"}}}
	form := url.Values{"backendId": {"llama"}, "arg.bogus": {"1"}}
	req := httptest.NewRequest("POST", "/validate", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	rec := httptest.NewRecorder()
	s.handleValidate(rec, req)
	if rec.Code != 200 {
		t.Fatalf("status %d", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), "unknown flag") {
		t.Fatalf("expected unknown-flag issue, got: %s", rec.Body.String())
	}
}
```

> Provide `stubSchemaStore` and `stubCatalog` test doubles in
> `handlers_test.go` implementing `backendcatalog.SchemaStore` and `Store`
> (return the canned schema/catalog; no-op Save/Delete).

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/service/configweb/ -run ValidateHandler -v`
Expected: FAIL — `handleValidate` undefined.

- [ ] **Step 3: Implement**

Create `internal/service/configweb/handlers.go`:

```go
package configweb

import (
	"net/http"
	"strings"

	"github.com/quantmind-br/model-loader/internal/domain"
	"github.com/quantmind-br/model-loader/internal/service/backendcatalog"
	"github.com/quantmind-br/model-loader/internal/service/validator"
)

// draftFromForm parses an editor form POST into a Draft.
// Arg fields are named "arg.<flag>"; meta fields use their plain names.
func draftFromForm(r *http.Request) Draft {
	_ = r.ParseForm()
	d := Draft{
		ID:        r.FormValue("id"),
		OrigID:    r.FormValue("origId"),
		IsNew:     r.FormValue("isNew") == "true",
		Name:      r.FormValue("name"),
		Description: r.FormValue("description"),
		Model:     r.FormValue("model"),
		BackendID: r.FormValue("backendId"),
		Args:      map[string]string{},
	}
	if tags := strings.TrimSpace(r.FormValue("tags")); tags != "" {
		for _, t := range strings.Split(tags, ",") {
			if t = strings.TrimSpace(t); t != "" {
				d.Tags = append(d.Tags, t)
			}
		}
	}
	for k, vs := range r.Form {
		if strings.HasPrefix(k, "arg.") && len(vs) > 0 {
			d.Args[strings.TrimPrefix(k, "arg.")] = vs[0]
		}
	}
	return d
}

func (s *Session) loadSchema(backendID string) (domain.BackendValidationSchema, error) {
	catalog, err := s.deps.Catalog.Load()
	if err != nil {
		return domain.BackendValidationSchema{}, err
	}
	if backendID == "" {
		backendID = catalog.DefaultBackendID
	}
	for _, b := range catalog.Backends {
		if b.ID == backendID {
			return s.deps.Schemas.Load(backendcatalog.SchemaStoreRef(b.SchemaRef))
		}
	}
	return domain.BackendValidationSchema{}, backendcatalog.ErrBackendNotFound
}

func (s *Session) handleValidate(w http.ResponseWriter, r *http.Request) {
	d := draftFromForm(r)
	schema, err := s.loadSchema(d.BackendID)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	fs := schema.ToFlagSchema()
	p := d.ToProfile(fs)
	rep := validator.New(nil).Validate(p, fs, schema.BackendKind)
	renderIssues(w, rep)
}

// renderIssues writes an HTMX partial listing errors and warnings.
func renderIssues(w http.ResponseWriter, rep validator.Report) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	var b strings.Builder
	b.WriteString(`<div id="issues" hx-swap-oob="true">`)
	if len(rep.Errors) == 0 && len(rep.Warnings) == 0 {
		b.WriteString(`<span class="ok">✓ válido</span>`)
	}
	for _, e := range rep.Errors {
		b.WriteString(`<div class="issue error" data-field="` + htmlEscape(e.Field) + `">` + htmlEscape(e.Field) + `: ` + htmlEscape(e.Message) + `</div>`)
	}
	for _, wn := range rep.Warnings {
		b.WriteString(`<div class="issue warn" data-field="` + htmlEscape(wn.Field) + `">` + htmlEscape(wn.Field) + `: ` + htmlEscape(wn.Message) + `</div>`)
	}
	b.WriteString(`</div>`)
	_, _ = w.Write([]byte(b.String()))
}

func htmlEscape(s string) string {
	r := strings.NewReplacer("&", "&amp;", "<", "&lt;", ">", "&gt;", `"`, "&quot;")
	return r.Replace(s)
}
```

Update `routes` in `server.go`:

```go
func (s *Session) routes(mux *http.ServeMux) {
	mux.HandleFunc("/healthz", func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(http.StatusOK) })
	mux.HandleFunc("/validate", s.handleValidate)
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/service/configweb/ -run ValidateHandler -v`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add internal/service/configweb/
git commit -m "feat(configweb): /validate handler with HTMX issue partial"
```

### Task 12: `/save` and `/cancel` handlers

**Files:**
- Modify: `internal/service/configweb/handlers.go`, `server.go`
- Test: `internal/service/configweb/handlers_test.go`

- [ ] **Step 1: Write the failing test**

```go
func TestSaveHandler_PersistsAndCompletes(t *testing.T) {
	schema := domain.BackendValidationSchema{
		BackendKind: domain.BackendKindLlamaServer, BackendID: "llama",
		Flags: map[string]domain.FlagSpec{"ctx-size": {Long: "ctx-size", Type: domain.FlagTypeInt}},
	}
	ps := &memProfileStore{}
	s := &Session{
		deps: Deps{Profiles: ps, Schemas: stubSchemaStore{schema: schema}, Catalog: stubCatalog{id: "llama", ref: "llama.json"}},
		done: make(chan Result, 1),
	}
	form := url.Values{
		"isNew": {"true"}, "id": {"qwen"}, "name": {"Qwen"},
		"backendId": {"llama"}, "model": {"/m.gguf"}, "arg.ctx-size": {"8192"},
	}
	req := httptest.NewRequest("POST", "/save", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	rec := httptest.NewRecorder()
	s.handleSave(rec, req)
	res := <-s.Done()
	if !res.Saved || res.ProfileID != "qwen" {
		t.Fatalf("unexpected result: %+v", res)
	}
	if _, err := ps.Get("qwen"); err != nil {
		t.Fatalf("profile not persisted: %v", err)
	}
}
```

> Add a `memProfileStore` test double implementing `profilestore.Store` (map-backed Create/Save/Get; other methods minimal).

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/service/configweb/ -run SaveHandler -v`
Expected: FAIL — `handleSave` undefined.

- [ ] **Step 3: Implement**

Add to `internal/service/configweb/handlers.go`:

```go
func (s *Session) handleSave(w http.ResponseWriter, r *http.Request) {
	d := draftFromForm(r)
	if d.ID == "" {
		d.ID = domain.Slugify(d.Name)
	}
	schema, err := s.loadSchema(d.BackendID)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	fs := schema.ToFlagSchema()

	var perr error
	if d.IsNew {
		perr = s.deps.Profiles.Create(d.ToProfile(fs))
	} else {
		lookup := d.OrigID
		if lookup == "" {
			lookup = d.ID
		}
		existing, gerr := s.deps.Profiles.Get(lookup)
		if gerr != nil {
			perr = s.deps.Profiles.Create(d.ToProfile(fs))
		} else {
			perr = s.deps.Profiles.Save(d.ApplyTo(existing, fs))
		}
	}
	if perr != nil {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_, _ = w.Write([]byte(`<div class="issue error">` + htmlEscape(perr.Error()) + `</div>`))
		return
	}
	w.Header().Set("HX-Redirect", "/closed") // browser shows a "you can close this tab" page
	w.WriteHeader(http.StatusOK)
	s.complete(Result{Saved: true, ProfileID: d.ID})
}

func (s *Session) handleCancel(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("HX-Redirect", "/closed")
	w.WriteHeader(http.StatusOK)
	s.complete(Result{Saved: false})
}

func (s *Session) handleClosed(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	_, _ = w.Write([]byte(`<!doctype html><meta charset="utf-8"><title>Pronto</title><body style="font-family:sans-serif;padding:3rem;text-align:center"><h2>Pronto — pode fechar esta aba.</h2></body>`))
}
```

Update `routes`:

```go
	mux.HandleFunc("/validate", s.handleValidate)
	mux.HandleFunc("/save", s.handleSave)
	mux.HandleFunc("/cancel", s.handleCancel)
	mux.HandleFunc("/closed", s.handleClosed)
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/service/configweb/ -run SaveHandler -v`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add internal/service/configweb/
git commit -m "feat(configweb): /save and /cancel handlers complete the session"
```

### Task 13: Customize-mode persistence handlers

**Files:**
- Modify: `internal/service/configweb/handlers.go`, `server.go`
- Test: `internal/service/configweb/handlers_test.go`

- [ ] **Step 1: Write the failing test**

```go
func TestCustomize_SaveFlagConstraintMarksEditable(t *testing.T) {
	schema := domain.BackendValidationSchema{
		BackendKind: domain.BackendKindLlamaServer, BackendID: "llama",
		Flags: map[string]domain.FlagSpec{"ctx-size": {Long: "ctx-size", Type: domain.FlagTypeInt}},
	}
	store := &captureSchemaStore{schema: schema}
	s := &Session{deps: Deps{Schemas: store, Catalog: stubCatalog{id: "llama", ref: "llama.json"}}, done: make(chan Result, 1)}
	form := url.Values{
		"backendId": {"llama"}, "flag": {"ctx-size"},
		"min": {"512"}, "max": {"131072"}, "default": {"8192"}, "required": {"on"},
	}
	req := httptest.NewRequest("POST", "/customize/flag", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	s.handleCustomizeFlag(httptest.NewRecorder(), req)

	saved := store.saved
	if saved.Flags["ctx-size"].Min == nil || *saved.Flags["ctx-size"].Min != 512 {
		t.Fatalf("min not saved: %+v", saved.Flags["ctx-size"])
	}
	if !saved.Flags["ctx-size"].Required {
		t.Fatalf("required not saved")
	}
	if !saved.Source.Editable {
		t.Fatalf("schema must be marked Editable after manual edit")
	}
}
```

> `captureSchemaStore` records the last `Save`d schema in a `saved` field and returns the initial schema from `Load`.

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/service/configweb/ -run Customize -v`
Expected: FAIL — `handleCustomizeFlag` undefined.

- [ ] **Step 3: Implement**

Add to `internal/service/configweb/handlers.go`:

```go
import (
	// add to existing imports:
	"strconv"
)

// loadSchemaRef returns the schema plus its store ref for a backend.
func (s *Session) loadSchemaRef(backendID string) (domain.BackendValidationSchema, string, error) {
	catalog, err := s.deps.Catalog.Load()
	if err != nil {
		return domain.BackendValidationSchema{}, "", err
	}
	if backendID == "" {
		backendID = catalog.DefaultBackendID
	}
	for _, b := range catalog.Backends {
		if b.ID == backendID {
			ref := backendcatalog.SchemaStoreRef(b.SchemaRef)
			sch, err := s.deps.Schemas.Load(ref)
			return sch, ref, err
		}
	}
	return domain.BackendValidationSchema{}, "", backendcatalog.ErrBackendNotFound
}

func atoiPtr(s string) *int {
	if s == "" {
		return nil
	}
	if n, err := strconv.Atoi(s); err == nil {
		return &n
	}
	return nil
}

// handleCustomizeFlag updates one flag's editable constraints and marks the
// schema as user-edited so RefreshSchema preserves it.
func (s *Session) handleCustomizeFlag(w http.ResponseWriter, r *http.Request) {
	_ = r.ParseForm()
	backendID := r.FormValue("backendId")
	flag := r.FormValue("flag")
	schema, ref, err := s.loadSchemaRef(backendID)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	spec, ok := schema.Flags[flag]
	if !ok {
		http.Error(w, "unknown flag", http.StatusBadRequest)
		return
	}
	spec.Min = atoiPtr(r.FormValue("min"))
	spec.Max = atoiPtr(r.FormValue("max"))
	spec.Required = r.FormValue("required") == "on"
	if dv := r.FormValue("default"); dv != "" {
		spec.Default = dv
	}
	if ev := r.FormValue("enumValues"); ev != "" {
		spec.EnumValues = splitCSV(ev)
	}
	schema.Flags[flag] = spec
	persistEditable(&schema)
	if err := s.deps.Schemas.Save(ref, schema); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	w.WriteHeader(http.StatusOK)
}

func splitCSV(s string) []string {
	var out []string
	for _, p := range strings.Split(s, ",") {
		if p = strings.TrimSpace(p); p != "" {
			out = append(out, p)
		}
	}
	return out
}

// persistEditable marks the schema as a manual edit. RefreshSchema (see
// backendschema.Manager) preserves schemas with Source.Editable=true.
func persistEditable(schema *domain.BackendValidationSchema) {
	schema.Source.Editable = true
}
```

Add sibling handlers in the same file for the remaining customize actions
(each loads via `loadSchemaRef`, mutates, calls `persistEditable`, saves):

```go
// handleCustomizeAddFlag adds a new user-defined flag to the schema.
func (s *Session) handleCustomizeAddFlag(w http.ResponseWriter, r *http.Request) {
	_ = r.ParseForm()
	schema, ref, err := s.loadSchemaRef(r.FormValue("backendId"))
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	long := r.FormValue("flag")
	if long == "" {
		http.Error(w, "flag name required", http.StatusBadRequest)
		return
	}
	if schema.Flags == nil {
		schema.Flags = map[string]domain.FlagSpec{}
	}
	schema.Flags[long] = domain.FlagSpec{
		Long:     long,
		Type:     parseFlagType(r.FormValue("type")),
		HelpText: r.FormValue("help"),
		Group:    "custom",
	}
	persistEditable(&schema)
	if err := s.deps.Schemas.Save(ref, schema); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	w.WriteHeader(http.StatusOK)
}

// handleCustomizeRemoveFlag deletes a flag from the schema and any presentation group referencing it.
func (s *Session) handleCustomizeRemoveFlag(w http.ResponseWriter, r *http.Request) {
	_ = r.ParseForm()
	schema, ref, err := s.loadSchemaRef(r.FormValue("backendId"))
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	long := r.FormValue("flag")
	delete(schema.Flags, long)
	if schema.Presentation != nil {
		for gi := range schema.Presentation.Groups {
			schema.Presentation.Groups[gi].Flags = removeStr(schema.Presentation.Groups[gi].Flags, long)
		}
	}
	persistEditable(&schema)
	if err := s.deps.Schemas.Save(ref, schema); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	w.WriteHeader(http.StatusOK)
}

// handleCustomizePresentation overwrites the presentation (group order, flag order, highlights)
// from a JSON body posted by the Alpine reorder UI.
func (s *Session) handleCustomizePresentation(w http.ResponseWriter, r *http.Request) {
	schema, ref, err := s.loadSchemaRef(r.URL.Query().Get("backendId"))
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	var pres domain.Presentation
	if err := jsonDecode(r, &pres); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	schema.Presentation = &pres
	persistEditable(&schema)
	if err := s.deps.Schemas.Save(ref, schema); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	w.WriteHeader(http.StatusOK)
}

// handleCustomizeRules overwrites the cross-field rules from a JSON body.
func (s *Session) handleCustomizeRules(w http.ResponseWriter, r *http.Request) {
	schema, ref, err := s.loadSchemaRef(r.URL.Query().Get("backendId"))
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	var rules []domain.CrossFieldRule
	if err := jsonDecode(r, &rules); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	if msg := validateRules(rules, schema); msg != "" {
		http.Error(w, msg, http.StatusBadRequest)
		return
	}
	schema.Rules = rules
	persistEditable(&schema)
	if err := s.deps.Schemas.Save(ref, schema); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	w.WriteHeader(http.StatusOK)
}
```

Add the small helpers `parseFlagType`, `removeStr`, `jsonDecode`, and
`validateRules` (rejects rules whose `When.Flag`/`Then.Flag` are not in
`schema.Flags`, or whose `Severity` is not `warning`/`error`) at the bottom of
`handlers.go`:

```go
func parseFlagType(s string) domain.FlagType {
	switch s {
	case "int":
		return domain.FlagTypeInt
	case "float":
		return domain.FlagTypeFloat
	case "bool":
		return domain.FlagTypeBool
	case "enum":
		return domain.FlagTypeEnum
	default:
		return domain.FlagTypeString
	}
}

func removeStr(xs []string, s string) []string {
	out := xs[:0]
	for _, x := range xs {
		if x != s {
			out = append(out, x)
		}
	}
	return out
}

func jsonDecode(r *http.Request, v any) error {
	defer r.Body.Close()
	return json.NewDecoder(r.Body).Decode(v)
}

func validateRules(rules []domain.CrossFieldRule, schema domain.BackendValidationSchema) string {
	for _, rule := range rules {
		if _, ok := schema.Flags[rule.When.Flag]; !ok {
			return "rule references unknown flag: " + rule.When.Flag
		}
		if rule.Then.Flag != "" {
			if _, ok := schema.Flags[rule.Then.Flag]; !ok {
				return "rule effect references unknown flag: " + rule.Then.Flag
			}
		}
		if rule.Severity != "warning" && rule.Severity != "error" {
			return "rule severity must be warning or error"
		}
	}
	return ""
}
```

Add `"encoding/json"` to the imports. Register routes:

```go
	mux.HandleFunc("/customize/flag", s.handleCustomizeFlag)
	mux.HandleFunc("/customize/flag/add", s.handleCustomizeAddFlag)
	mux.HandleFunc("/customize/flag/remove", s.handleCustomizeRemoveFlag)
	mux.HandleFunc("/customize/presentation", s.handleCustomizePresentation)
	mux.HandleFunc("/customize/rules", s.handleCustomizeRules)
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/service/configweb/ -run Customize -v`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add internal/service/configweb/
git commit -m "feat(configweb): customize-mode persistence handlers"
```

---

## Phase 5 — Frontend assets

### Task 14: Embed assets + base template + vendored libs

**Files:**
- Create: `internal/service/configweb/assets/embed.go`
- Create: `internal/service/configweb/assets/templates/base.gohtml`
- Create: `internal/service/configweb/assets/static/app.css`
- Create: `internal/service/configweb/assets/static/htmx.min.js`
- Create: `internal/service/configweb/assets/static/alpine.min.js`
- Test: `internal/service/configweb/assets/embed_test.go`

- [ ] **Step 1: Vendor the JS libraries (pinned)**

Run:

```bash
mkdir -p internal/service/configweb/assets/static internal/service/configweb/assets/templates
curl -fsSL https://unpkg.com/htmx.org@2.0.3/dist/htmx.min.js -o internal/service/configweb/assets/static/htmx.min.js
curl -fsSL https://cdn.jsdelivr.net/npm/alpinejs@3.14.1/dist/cdn.min.js -o internal/service/configweb/assets/static/alpine.min.js
```

Expected: both files exist and are non-empty (`wc -c` > 1000).

- [ ] **Step 2: Write the failing test**

```go
package assets

import "testing"

func TestEmbed_HasTemplatesAndStatic(t *testing.T) {
	if _, err := FS.ReadFile("templates/base.gohtml"); err != nil {
		t.Fatalf("base template missing: %v", err)
	}
	if b, err := FS.ReadFile("static/htmx.min.js"); err != nil || len(b) < 1000 {
		t.Fatalf("htmx not embedded: %v len=%d", err, len(b))
	}
}
```

- [ ] **Step 3: Create embed + base template + CSS**

`internal/service/configweb/assets/embed.go`:

```go
// Package assets embeds the config web GUI templates and static files.
package assets

import "embed"

//go:embed templates/*.gohtml static/*
var FS embed.FS
```

`internal/service/configweb/assets/templates/base.gohtml` (frame; mode tabs + sidebar shell; content blocks filled by Task 15/16):

```html
{{define "base"}}<!doctype html>
<html lang="pt-br">
<head>
<meta charset="utf-8">
<meta name="viewport" content="width=device-width, initial-scale=1">
<title>{{.Draft.Name}} · model-loader</title>
<link rel="stylesheet" href="/static/app.css">
<script src="/static/htmx.min.js"></script>
<script defer src="/static/alpine.min.js"></script>
</head>
<body x-data="{mode:'configure'}">
<header class="topbar">
  <div class="title">{{.Draft.Name}} <span class="muted">· {{.Draft.BackendID}}</span></div>
  <nav class="modes">
    <button :class="{active: mode==='configure'}" @click="mode='configure'">Configurar</button>
    <button :class="{active: mode==='customize'}" @click="mode='customize'">Personalizar</button>
  </nav>
  <div class="actions">
    <button class="ghost" hx-post="/cancel">Cancelar</button>
    <button class="primary" form="profile-form" hx-post="/save" hx-target="#issues">Salvar</button>
  </div>
</header>
<main class="layout">
  <aside class="sidebar">{{block "sidebar" .}}{{end}}</aside>
  <section class="content">
    <div x-show="mode==='configure'">{{block "configure" .}}{{end}}</div>
    <div x-show="mode==='customize'">{{block "customize" .}}{{end}}</div>
  </section>
</main>
<footer class="statusbar"><div id="issues"><span class="ok">✓ válido</span></div></footer>
</body>
</html>{{end}}
```

`internal/service/configweb/assets/static/app.css` (concise, dark, readable):

```css
:root{--bg:#0f1320;--panel:#171c2e;--ink:#e7ecf5;--muted:#8b95ad;--accent:#6ea8fe;--err:#ff6b6b;--warn:#ffcf66;--ok:#5fd38a}
*{box-sizing:border-box}body{margin:0;font:14px/1.5 system-ui,sans-serif;background:var(--bg);color:var(--ink)}
.topbar{display:flex;align-items:center;gap:1rem;padding:.6rem 1rem;background:var(--panel);border-bottom:1px solid #222a44}
.topbar .title{font-weight:600}.muted{color:var(--muted)}
.modes{margin-left:auto;display:flex;gap:.25rem}.modes button{background:transparent;color:var(--muted);border:0;padding:.4rem .8rem;border-radius:.4rem;cursor:pointer}
.modes button.active{background:#22294a;color:var(--ink)}
.actions{display:flex;gap:.5rem}button.primary{background:var(--accent);color:#04122e;border:0;padding:.45rem .9rem;border-radius:.4rem;font-weight:600;cursor:pointer}
button.ghost{background:transparent;border:1px solid #333c5e;color:var(--ink);padding:.45rem .9rem;border-radius:.4rem;cursor:pointer}
.layout{display:flex;min-height:calc(100vh - 96px)}
.sidebar{width:200px;padding:1rem;background:var(--panel);border-right:1px solid #222a44}
.sidebar a{display:block;color:var(--muted);text-decoration:none;padding:.3rem .4rem;border-radius:.3rem}
.sidebar a.hl{color:var(--ink);font-weight:600}
.content{flex:1;padding:1.2rem;overflow:auto}
.group{margin-bottom:1.4rem}.group h3{margin:.2rem 0 .6rem;font-size:.85rem;text-transform:uppercase;letter-spacing:.05em;color:var(--muted)}
.field{display:flex;flex-direction:column;gap:.2rem;margin-bottom:.7rem;max-width:520px}
.field label{font-size:.8rem;color:var(--muted)}
.field input,.field select{background:#0c1020;border:1px solid #2a3354;color:var(--ink);border-radius:.4rem;padding:.45rem .6rem}
.statusbar{position:sticky;bottom:0;padding:.5rem 1rem;background:var(--panel);border-top:1px solid #222a44}
.issue{padding:.15rem 0}.issue.error{color:var(--err)}.issue.warn{color:var(--warn)}.ok{color:var(--ok)}
.flag-edit{border:1px solid #2a3354;border-radius:.5rem;padding:.7rem;margin-bottom:.6rem}
.rule{display:flex;gap:.4rem;align-items:center;flex-wrap:wrap;border:1px solid #2a3354;border-radius:.5rem;padding:.5rem;margin-bottom:.4rem}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/service/configweb/assets/ -v`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add internal/service/configweb/assets/
git commit -m "feat(configweb): embed assets, base template, vendored htmx+alpine"
```

### Task 15: Configure-mode template + render wiring

**Files:**
- Create: `internal/service/configweb/assets/templates/configure.gohtml`
- Modify: `internal/service/configweb/server.go` (add `GET /` renderer)
- Create: `internal/service/configweb/render.go`
- Test: `internal/service/configweb/render_test.go`

- [ ] **Step 1: Write the failing test**

```go
package configweb

import (
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/quantmind-br/model-loader/internal/domain"
)

func TestIndexRendersGroupedFields(t *testing.T) {
	schema := domain.BackendValidationSchema{
		BackendKind: domain.BackendKindLlamaServer, BackendID: "llama",
		Flags: map[string]domain.FlagSpec{"ctx-size": {Long: "ctx-size", Type: domain.FlagTypeInt, Default: 8192}},
		Presentation: &domain.Presentation{Groups: []domain.PresentationGroup{
			{Name: "Essenciais", Highlighted: true, Flags: []string{"ctx-size"}},
		}},
	}
	s := &Session{deps: Deps{
		Schemas: stubSchemaStore{schema: schema},
		Catalog: stubCatalog{id: "llama", ref: "llama.json"},
		InitialDraft: Draft{ID: "qwen", Name: "Qwen", BackendID: "llama", Args: map[string]string{"ctx-size": "4096"}},
	}}
	rec := httptest.NewRecorder()
	s.handleIndex(rec, httptest.NewRequest("GET", "/", nil))
	body := rec.Body.String()
	if !strings.Contains(body, `name="arg.ctx-size"`) || !strings.Contains(body, `value="4096"`) {
		t.Fatalf("ctx-size field not rendered with value: %s", body)
	}
	if !strings.Contains(body, "Essenciais") {
		t.Fatalf("group title missing")
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/service/configweb/ -run IndexRenders -v`
Expected: FAIL — `handleIndex` undefined.

- [ ] **Step 3: Implement render + template**

Create `internal/service/configweb/render.go`:

```go
package configweb

import (
	"html/template"
	"net/http"

	"github.com/quantmind-br/model-loader/internal/service/configweb/assets"
)

var tmpl = template.Must(template.ParseFS(assets.FS,
	"templates/base.gohtml",
	"templates/configure.gohtml",
	"templates/customize.gohtml",
))

func (s *Session) handleIndex(w http.ResponseWriter, r *http.Request) {
	schema, err := s.loadSchema(s.deps.InitialDraft.BackendID)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	vm := BuildViewModel(s.deps.InitialDraft, schema)
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if err := tmpl.ExecuteTemplate(w, "base", vm); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
	}
}
```

Create `internal/service/configweb/assets/templates/configure.gohtml`:

```html
{{define "sidebar"}}
{{range .Groups}}<a href="#g-{{.Name}}" class="{{if .Highlighted}}hl{{end}}">{{if .Highlighted}}★ {{end}}{{.Name}}</a>{{end}}
{{end}}

{{define "configure"}}
<form id="profile-form" hx-post="/validate" hx-trigger="input changed delay:300ms" hx-target="#issues">
  <input type="hidden" name="id" value="{{.Draft.ID}}">
  <input type="hidden" name="origId" value="{{.Draft.OrigID}}">
  <input type="hidden" name="isNew" value="{{if .Draft.IsNew}}true{{else}}false{{end}}">
  <input type="hidden" name="backendId" value="{{.Draft.BackendID}}">
  <div class="field"><label>Nome</label><input name="name" value="{{.Draft.Name}}"></div>
  <div class="field"><label>Model (caminho ou repo)</label><input name="model" value="{{.Draft.Model}}"></div>
  {{range .Groups}}
  <div class="group" id="g-{{.Name}}">
    <h3>{{if .Highlighted}}★ {{end}}{{.Name}}</h3>
    {{range .Fields}}
    <div class="field">
      <label>{{.Label}}{{if .Required}} *{{end}}</label>
      {{if eq .Widget "select"}}
        <select name="arg.{{.Flag}}">
          {{range .Options}}<option value="{{.}}" {{if eq . $.Draft.Args}}{{end}}>{{.}}</option>{{end}}
        </select>
      {{else if eq .Widget "toggle"}}
        <select name="arg.{{.Flag}}"><option value="on">on</option><option value="off">off</option></select>
      {{else if eq .Widget "number"}}
        <input type="number" name="arg.{{.Flag}}" value="{{.Value}}" {{if .Min}}min="{{.Min}}"{{end}} {{if .Max}}max="{{.Max}}"{{end}}>
      {{else}}
        <input name="arg.{{.Flag}}" value="{{.Value}}">
      {{end}}
      {{if .Help}}<small class="muted">{{.Help}}</small>{{end}}
    </div>
    {{end}}
  </div>
  {{end}}
</form>
{{end}}
```

> Note: the `select` "selected" handling for enums is refined in a follow-up
> commit if needed; the default `.Value` already pre-fills number/text fields.
> Create a minimal `customize.gohtml` stub now so `ParseFS` succeeds:
> `{{define "customize"}}<p class="muted">Personalizar — carregado na Task 16.</p>{{end}}`

Update `routes` to add `GET /` and static serving:

```go
	mux.HandleFunc("/", s.handleIndex)
	mux.Handle("/static/", http.FileServer(http.FS(assets.FS)))
```

> `http.FS(assets.FS)` serves `/static/...` because the embedded paths include
> the `static/` prefix. Verify the URL maps correctly; if not, use
> `http.StripPrefix` with a sub-FS rooted at `static`.

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/service/configweb/ -run IndexRenders -v`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add internal/service/configweb/
git commit -m "feat(configweb): configure-mode render and template"
```

### Task 16: Customize-mode template (flag editor + rule builder)

**Files:**
- Modify: `internal/service/configweb/assets/templates/customize.gohtml`
- Test: `internal/service/configweb/render_test.go`

- [ ] **Step 1: Write the failing test**

```go
func TestCustomizeModeRendersFlagEditors(t *testing.T) {
	schema := domain.BackendValidationSchema{
		BackendKind: domain.BackendKindLlamaServer, BackendID: "llama",
		Flags: map[string]domain.FlagSpec{"ctx-size": {Long: "ctx-size", Type: domain.FlagTypeInt}},
		Presentation: &domain.Presentation{Groups: []domain.PresentationGroup{{Name: "Essenciais", Highlighted: true, Flags: []string{"ctx-size"}}}},
		Rules: []domain.CrossFieldRule{{ID: "r1", When: domain.Cond{Flag: "ctx-size", Op: "ge", Value: "1"}, Then: domain.Effect{Kind: "message", Message: "hi"}, Severity: "warning"}},
	}
	s := &Session{deps: Deps{Schemas: stubSchemaStore{schema: schema}, Catalog: stubCatalog{id: "llama", ref: "llama.json"}, InitialDraft: Draft{BackendID: "llama"}}}
	rec := httptest.NewRecorder()
	s.handleIndex(rec, httptest.NewRequest("GET", "/", nil))
	body := rec.Body.String()
	if !strings.Contains(body, "/customize/flag") {
		t.Fatalf("flag editor form action missing")
	}
	if !strings.Contains(body, "Regras entre campos") {
		t.Fatalf("rules section missing")
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/service/configweb/ -run CustomizeModeRenders -v`
Expected: FAIL — stub template lacks the markup.

- [ ] **Step 3: Implement the customize template**

The view model needs schema-level data (all flags + rules) in customize mode.
Extend `ViewModel` in `viewmodel.go` with raw schema fields and populate them in
`BuildViewModel`:

```go
type ViewModel struct {
	Draft   Draft
	Groups  []GroupVM
	BackendID string
	AllFlags  []FlagEditVM
	Rules     []domain.CrossFieldRule
}

type FlagEditVM struct {
	Flag     string
	Type     string
	Min, Max string
	Default  string
	Required bool
	Enum     string
}
```

In `BuildViewModel`, set `vm.BackendID = schema.BackendID`, `vm.Rules = schema.Rules`, and build `vm.AllFlags` from `schema.Flags` (sorted by long name; render `Min`/`Max`/`Default` as strings, `Enum` as comma-joined). Add a small `flagEdit(long, spec)` helper.

Replace `customize.gohtml`:

```html
{{define "customize"}}
<div x-data="customize()">
  <h3>Flags do backend ({{.BackendID}})</h3>
  {{range .AllFlags}}
  <form class="flag-edit" hx-post="/customize/flag">
    <input type="hidden" name="backendId" value="{{$.BackendID}}">
    <input type="hidden" name="flag" value="{{.Flag}}">
    <strong>{{.Flag}}</strong>
    <select name="type">
      <option value="int" {{if eq .Type "int"}}selected{{end}}>int</option>
      <option value="float" {{if eq .Type "float"}}selected{{end}}>float</option>
      <option value="bool" {{if eq .Type "bool"}}selected{{end}}>bool</option>
      <option value="enum" {{if eq .Type "enum"}}selected{{end}}>enum</option>
      <option value="string" {{if eq .Type "string"}}selected{{end}}>string</option>
    </select>
    <input name="min" placeholder="min" value="{{.Min}}" size="6">
    <input name="max" placeholder="max" value="{{.Max}}" size="8">
    <input name="default" placeholder="default" value="{{.Default}}" size="10">
    <input name="enumValues" placeholder="enum (csv)" value="{{.Enum}}" size="14">
    <label><input type="checkbox" name="required" {{if .Required}}checked{{end}}> obrigatória</label>
    <button class="primary" type="submit">Salvar flag</button>
    <button class="ghost" type="button"
      hx-post="/customize/flag/remove" hx-vals='{"backendId":"{{$.BackendID}}","flag":"{{.Flag}}"}'>🗑</button>
  </form>
  {{end}}

  <form class="flag-edit" hx-post="/customize/flag/add">
    <input type="hidden" name="backendId" value="{{.BackendID}}">
    <input name="flag" placeholder="nova flag (long name)" required>
    <select name="type"><option>string</option><option>int</option><option>float</option><option>bool</option><option>enum</option></select>
    <input name="help" placeholder="descrição">
    <button class="primary" type="submit">＋ Adicionar flag</button>
  </form>

  <h3>Regras entre campos</h3>
  <template x-for="(rule,i) in rules" :key="i">
    <div class="rule">
      SE <input x-model="rule.when.flag" placeholder="flag" size="12">
      <select x-model="rule.when.op"><option value="eq">=</option><option value="ne">≠</option><option value="le">≤</option><option value="ge">≥</option></select>
      <input x-model="rule.when.value" placeholder="valor" size="8">
      ENTÃO
      <select x-model="rule.then.kind"><option value="limit">limitar</option><option value="require">exigir</option><option value="message">mensagem</option></select>
      <input x-model="rule.then.flag" placeholder="campo alvo" size="12" x-show="rule.then.kind!=='message'">
      <select x-model="rule.then.op" x-show="rule.then.kind==='limit'"><option value="le">≤</option><option value="ge">≥</option><option value="eq">=</option><option value="ne">≠</option></select>
      <input x-model="rule.then.value" placeholder="valor" size="8" x-show="rule.then.kind!=='message'">
      <input x-model="rule.then.message" placeholder="mensagem" size="20" x-show="rule.then.kind==='message'">
      <select x-model="rule.severity"><option value="warning">aviso</option><option value="error">erro</option></select>
      <button class="ghost" type="button" @click="rules.splice(i,1)">🗑</button>
    </div>
  </template>
  <button class="ghost" type="button" @click="addRule()">＋ Nova regra</button>
  <button class="primary" type="button" @click="saveRules()">Salvar regras</button>

  <script>
    function customize(){return{
      rules: {{ .Rules | toJSON }},
      backendId: "{{.BackendID}}",
      addRule(){this.rules.push({id:crypto.randomUUID(),when:{flag:'',op:'eq',value:''},then:{kind:'message',message:''},severity:'warning'})},
      async saveRules(){
        const r = await fetch('/customize/rules?backendId='+encodeURIComponent(this.backendId),
          {method:'POST',headers:{'Content-Type':'application/json'},body:JSON.stringify(this.rules)});
        if(!r.ok){alert(await r.text())}
      }
    }}
  </script>
</div>
{{end}}
```

Register a `toJSON` template func in `render.go` so the rules array seeds Alpine:

```go
var tmpl = template.Must(template.New("").Funcs(template.FuncMap{
	"toJSON": func(v any) (template.JS, error) {
		b, err := json.Marshal(v)
		return template.JS(b), err
	},
}).ParseFS(assets.FS,
	"templates/base.gohtml",
	"templates/configure.gohtml",
	"templates/customize.gohtml",
))
```

Add `"encoding/json"` to `render.go` imports.

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/service/configweb/ -run CustomizeModeRenders -v`
Expected: PASS

- [ ] **Step 5: Manual smoke (optional) + Commit**

```bash
git add internal/service/configweb/
git commit -m "feat(configweb): customize-mode template with flag editor and rule builder"
```

---

## Phase 6 — TUI integration

### Task 17: Launch command + modal + result handling

**Files:**
- Create: `internal/ui/pages/profiles_webedit.go`
- Test: `internal/ui/pages/profiles_webedit_test.go`

- [ ] **Step 1: Write the failing test**

```go
package pages

import (
	"testing"
)

func TestWebEditMsgs_OpenAndResult(t *testing.T) {
	// webEditStartedMsg carries the session + url; webEditDoneMsg carries the result.
	started := webEditStartedMsg{url: "http://127.0.0.1:1234"}
	if started.url == "" {
		t.Fatal("url should be set")
	}
	done := webEditDoneMsg{saved: true, profileID: "qwen"}
	if !done.saved || done.profileID != "qwen" {
		t.Fatalf("done msg wrong: %+v", done)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/ui/pages/ -run WebEditMsgs -v`
Expected: FAIL — message types undefined.

- [ ] **Step 3: Implement launch plumbing**

Create `internal/ui/pages/profiles_webedit.go`:

```go
package pages

import (
	"os/exec"
	"runtime"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/quantmind-br/model-loader/internal/service/configweb"
)

type webEditStartedMsg struct {
	session *configweb.Session
	url     string
}

type webEditDoneMsg struct {
	saved     bool
	profileID string
	err       error
}

type webEditFailedMsg struct{ err error }

// startWebEdit boots a configweb session for the given draft and returns a Cmd
// that emits webEditStartedMsg (or webEditFailedMsg). The Profiles page holds
// the stores it needs to build Deps.
func (p ProfilesPage) startWebEdit(draft configweb.Draft) tea.Cmd {
	return func() tea.Msg {
		sess := configweb.NewSession(configweb.Deps{
			Profiles:     p.store,
			Catalog:      p.catalogStore,
			Schemas:      p.schemaStore,
			InitialDraft: draft,
		})
		url, err := sess.Start()
		if err != nil {
			return webEditFailedMsg{err: err}
		}
		openBrowser(url)
		return webEditStartedMsg{session: sess, url: url}
	}
}

// waitForWebEdit blocks on the session result channel in a Cmd goroutine.
func waitForWebEdit(sess *configweb.Session) tea.Cmd {
	return func() tea.Msg {
		res := <-sess.Done()
		return webEditDoneMsg{saved: res.Saved, profileID: res.ProfileID, err: res.Err}
	}
}

func openBrowser(url string) {
	var cmd string
	var args []string
	switch runtime.GOOS {
	case "darwin":
		cmd = "open"
	case "windows":
		cmd, args = "cmd", []string{"/c", "start"}
	default:
		cmd = "xdg-open"
	}
	args = append(args, url)
	_ = exec.Command(cmd, args...).Start()
}
```

> Confirm `p.store`, `p.catalogStore`, `p.schemaStore` are the field names on
> `ProfilesPage` (see `profiles.go`). Adjust if they differ.

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/ui/pages/ -run WebEditMsgs -v`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add internal/ui/pages/profiles_webedit.go internal/ui/pages/profiles_webedit_test.go
git commit -m "feat(ui): web-edit session launch plumbing"
```

### Task 18: Wire Profiles page to web editor + input capture

**Files:**
- Modify: `internal/ui/pages/profiles.go` (add `webEditing bool`, `webURL string`, `webSession`)
- Modify: `internal/ui/pages/profiles_update.go` (handle new msgs; route `n`/`e` to `startWebEdit`)
- Modify: `internal/ui/pages/profiles_crud.go` (build `configweb.Draft` from selection/new)
- Modify: `internal/ui/pages/profiles.go` `View()` + `IsCapturingInput()`
- Test: `internal/ui/pages/profiles_test.go`, `internal/ui/root_test.go`

- [ ] **Step 1: Write the failing test**

```go
func TestProfilesPage_CapturesInputWhileWebEditing(t *testing.T) {
	p := ProfilesPage{webEditing: true}
	if !p.IsCapturingInput() {
		t.Fatal("must capture input while web editing modal is up")
	}
}

func TestProfilesPage_WebEditDoneReloadsAndClears(t *testing.T) {
	p := newTestProfilesPage(t) // existing helper in profiles_test.go
	p.webEditing = true
	m, cmd := p.Update(webEditDoneMsg{saved: true, profileID: "qwen"})
	pp := m.(ProfilesPage)
	if pp.webEditing {
		t.Fatal("webEditing should be cleared after done")
	}
	if cmd == nil {
		t.Fatal("expected a reload cmd")
	}
}
```

> Reuse whatever profiles-page constructor the existing tests use; if none, build the struct literal with the stores the page needs.

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/ui/pages/ -run 'WebEditing|WebEditDone' -v`
Expected: FAIL — `webEditing` field / message handling missing.

- [ ] **Step 3: Implement wiring**

In `profiles.go`, add fields to `ProfilesPage`:

```go
	webEditing bool
	webURL     string
	webSession *configweb.Session
```

Update `IsCapturingInput` to include the new state (keep existing conditions):

```go
func (p ProfilesPage) IsCapturingInput() bool {
	return p.editing || p.pickerActive || p.confirmDelete || p.webEditing
}
```

> If the page still has `p.editing` (huh) it is removed in Task 19; for now keep
> it and just add `|| p.webEditing`.

In `profiles_crud.go`, replace the bodies of `startNew` and `startEditSelected`
to build a `configweb.Draft` and launch the web editor instead of the huh editor:

```go
func (p ProfilesPage) startNew() (tea.Model, tea.Cmd) {
	d := configweb.Draft{
		IsNew:     true,
		Name:      "New Profile",
		ID:        domain.Slugify("New Profile"),
		Args:      map[string]string{},
	}
	if p.catalogStore != nil {
		if catalog, err := p.catalogStore.Load(); err == nil {
			d.BackendID = catalog.DefaultBackendID
		}
	}
	p.webEditing = true
	return p, p.startWebEdit(d)
}

func (p ProfilesPage) startEditSelected() (tea.Model, tea.Cmd) {
	sel, ok := p.list.SelectedItem().(item)
	if !ok {
		return p, nil
	}
	pr := sel.p
	d := configweb.Draft{
		ID:        pr.ID,
		OrigID:    pr.ID,
		Name:      pr.Name,
		Description: pr.Description,
		Tags:      pr.Tags,
		Model:     pr.Model,
		BackendID: pr.Launch.BackendID,
		Args:      argsToStrings(pr.Args),
	}
	p.webEditing = true
	return p, p.startWebEdit(d)
}

// argsToStrings renders persisted typed args back to form strings.
func argsToStrings(args map[string]any) map[string]string {
	out := map[string]string{}
	for k, v := range args {
		out[k] = formatArgValue(v) // existing helper in this file
	}
	return out
}
```

In `profiles_update.go`, handle the new messages in `Update`'s message switch:

```go
	case webEditStartedMsg:
		p.webSession = msg.session
		p.webURL = msg.url
		return p, waitForWebEdit(msg.session)
	case webEditFailedMsg:
		p.webEditing = false
		pp, fc := p.withFlashError("could not open editor: " + msg.err.Error())
		return pp, fc
	case webEditDoneMsg:
		p.webEditing = false
		p.webSession = nil
		if msg.err != nil {
			pp, fc := p.withFlashError("editor error: " + msg.err.Error())
			return pp, fc
		}
		var fc tea.Cmd
		if msg.saved {
			p, fc = p.withFlash("saved " + msg.profileID)
		}
		return p, tea.Batch(p.loadCmd(), fc)
```

In `View()`, when `p.webEditing`, render the modal text instead of the list
(follow the existing overlay/modal pattern in `overlay.go`):

```go
	if p.webEditing {
		return p.renderWebEditModal()
	}
```

Add `renderWebEditModal` to `profiles.go`:

```go
func (p ProfilesPage) renderWebEditModal() string {
	return "\n  Editando profile no navegador…\n\n  " + p.webURL +
		"\n\n  Salve ou cancele na página. (esc cancela)\n"
}
```

Handle `esc` while `webEditing` in the key path to cancel: POST `/cancel` is
driven by the browser, but `esc` should also complete the session. Add to the
`tea.KeyMsg` handling (early, before other keys):

```go
	if p.webEditing {
		if keyMsg, ok := msg.(tea.KeyMsg); ok && keyMsg.String() == "esc" && p.webSession != nil {
			p.webSession.Cancel() // see note
		}
		return p, nil // swallow all other keys while editing
	}
```

> Add an exported `Cancel()` to `configweb.Session` that calls
> `s.complete(Result{Saved:false})`. Add a one-line test in
> `session_test.go` asserting `Cancel()` delivers a non-saved result on `Done()`.

In `root_test.go`, add a test proving the Profiles page swallows a printable key
(e.g. `"n"`) while `webEditing` is true, using the existing `capturingPage`
pattern, to satisfy the TUI input-routing contract.

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/ui/... -v`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add internal/ui/ internal/service/configweb/session.go internal/service/configweb/session_test.go
git commit -m "feat(ui): launch web profile editor from Profiles tab"
```

### Task 19: Remove the huh profile_editor package

**Files:**
- Delete: `internal/ui/pages/profile_editor/` (whole directory)
- Modify: `internal/ui/pages/profiles.go`, `profiles_crud.go`, `profiles_update.go`, `profiles_test.go` — remove all `profile_editor` references, the `editor` field, `prepareEditor`, `handleEditorCommitted`, `backendOptions`, `newDraftDefaults`, and the `editing` capture flag now unused.

- [ ] **Step 1: Find every reference**

Run: `grep -rn "profile_editor" internal/ | grep -v _test.go`
Expected: a list of import sites and call sites to clean.

- [ ] **Step 2: Remove references and the directory**

Delete the import `"github.com/quantmind-br/model-loader/internal/ui/pages/profile_editor"` from `profiles_crud.go` and any others. Remove the now-dead functions listed above and the `editor` field from `ProfilesPage`. Then:

Run: `git rm -r internal/ui/pages/profile_editor`

- [ ] **Step 3: Build and test**

Run: `go build ./... && go test ./... `
Expected: PASS — no references to the removed package remain.

> If `huh` is now unused project-wide, leave `go.mod` alone unless `go mod tidy`
> removes it cleanly; run `go mod tidy` and inspect the diff. Only commit the
> tidy if the build still passes.

- [ ] **Step 4: Re-run full suite**

Run: `make tests`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add -A
git commit -m "refactor(ui): remove huh profile_editor in favor of web editor"
```

---

## Phase 7 — Docs & goldens

### Task 20: Sync schema docs, goldens, and CLAUDE.md

**Files:**
- Modify: `docs/profile-schema.json` (only if a persisted profile field changed — none did; verify and note "no change")
- Modify: backend validation-schema golden fixtures under `testdata/` (regenerate)
- Modify: `CLAUDE.md` (WHERE TO LOOK + ESSENTIALS/ANTI-PATTERNS notes)

- [ ] **Step 1: Confirm persisted-profile shape is unchanged**

Run: `git diff main -- internal/domain/profile.go`
Expected: empty (the profile envelope did not change — only the backend schema
envelope and `FlagSpec`/`FlagSchema` did). If empty, `docs/profile-schema.json`
needs no change; record that in the commit message.

- [ ] **Step 2: Regenerate goldens**

Run: `go test ./... -update`
Then: `git diff --stat testdata/`
Expected: backend-schema golden fixtures gain `presentation` (and where added in
tests, `rules`) blocks. Inspect the diff to confirm only additive changes.

- [ ] **Step 3: Update CLAUDE.md**

Add to the "WHERE TO LOOK" table:

```
| Web profile editor | internal/service/configweb/ | on-demand HTTP GUI launched by Profiles tab; replaces huh editor |
| Editor presentation/rules | internal/domain/backend_schema.go | Presentation + CrossFieldRule on the schema envelope |
```

Replace the "ESSENTIALS REGISTRY CONTRACT" section's reference to the deleted
`essentials.go` with a pointer to `backendschema/presentation.go`
(`essentialSeed` + `BuildPresentation`). Update "ANTI-PATTERNS" to drop the
`essentialFields` rule and add: "DO NOT hand-edit a backend schema's
`presentation`/`rules` blocks in JSON — edit them through the web Customize mode
so `Source.Editable` is set and `RefreshSchema` preserves them."

- [ ] **Step 4: Build + full suite**

Run: `make build && make tests`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add docs/ testdata/ CLAUDE.md
git commit -m "docs: sync schema goldens and knowledge base for web editor"
```

---

## Self-Review notes (for the implementer)

- **Spec coverage:** per-flag constraints → Task 1, 13; add/remove flags → Task 13; highlights/groups/order → Tasks 6, 13, 16; cross-field rules → Tasks 3, 5, 13, 16; per-backend scope → all customize handlers operate on the backend schema; on-demand in-process server → Tasks 10, 17, 18; schema-driven engine → Tasks 9, 15; envelope persistence → Tasks 2, 3; HTMX/Alpine frontend → Tasks 14-16; migration seeding → Task 7; retire huh editor → Task 19; docs sync → Task 20.
- **Type consistency:** `Draft.ToProfile/ApplyTo` (Task 8) used by `/save` (Task 12); `BuildViewModel`/`FieldVM` (Task 9) used by render (Task 15) and extended for customize (Task 16); `Session`/`Deps`/`Result` (Task 10) used by handlers (11-13) and TUI (17-18); `BuildPresentation` (Task 6) used by migration (Task 7).
- **Known stub cleanups flagged inline:** the stub `fieldVM` in Task 9 and the illustrative first block in Task 4 must be deleted as noted; do not leave them.

---

## Execution

After the plan is approved, this should be implemented task-by-task with review between tasks.
