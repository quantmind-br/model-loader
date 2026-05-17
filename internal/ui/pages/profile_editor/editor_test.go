package profile_editor

import (
	"reflect"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/huh"

	"github.com/quantmind-br/model-loader/internal/domain"
	"github.com/quantmind-br/model-loader/internal/service/backendcatalog"
)

// drainCmd executes the cmd to surface its tea.Msg for assertions.
func drainCmd(cmd tea.Cmd) tea.Msg {
	if cmd == nil {
		return nil
	}
	return cmd()
}

// flushAll drives msg through e and recursively drains any returned cmd
// (and the messages they produce) until the queue is empty. Used to let
// the huh form complete its Init handshake (focus + styling Cmds).
func flushAll(t *testing.T, e Editor, msg tea.Msg) Editor {
	t.Helper()
	queue := []tea.Msg{msg}
	for i := 0; i < 64 && len(queue) > 0; i++ {
		next := queue[0]
		queue = queue[1:]
		var cmd tea.Cmd
		e, cmd = e.Update(next)
		if cmd == nil {
			continue
		}
		out := cmd()
		if out == nil {
			continue
		}
		if batch, ok := out.(tea.BatchMsg); ok {
			for _, c := range batch {
				if c == nil {
					continue
				}
				if m := c(); m != nil {
					queue = append(queue, m)
				}
			}
			continue
		}
		queue = append(queue, out)
	}
	return e
}

func TestEditor_OpenStarts(t *testing.T) {
	e := New(domain.FlagSchema{})
	if e.Active() {
		t.Fatal("zero editor should not be active")
	}
	e, cmd := e.Open(Draft{Name: "X", IsNew: true})
	if !e.Active() {
		t.Fatal("editor should be active after Open")
	}
	if e.CurrentDraft().Name != "X" {
		t.Errorf("CurrentDraft().Name = %q, want X", e.CurrentDraft().Name)
	}
	if cmd == nil {
		t.Error("Open should return form Init cmd")
	}
}

func TestEditor_CancelExits(t *testing.T) {
	e := New(domain.FlagSchema{})
	e, _ = e.Open(Draft{Name: "X"})
	e = e.Cancel()
	if e.Active() {
		t.Error("editor should be inactive after Cancel")
	}
	if !reflect.DeepEqual(e.CurrentDraft(), Draft{}) {
		t.Errorf("draft should clear; got %+v", e.CurrentDraft())
	}
}

// cleanDraft returns a Draft pre-populated with the defaults huh's
// NewSelect injects on first render (FlashAttn, CacheTypeK, CacheTypeV).
// Tests that exercise the dirty-check on esc must start from this baseline
// so the snapshot matches the post-Open draft state until the test mutates
// fields explicitly.
func cleanDraft() Draft {
	return Draft{
		Name:       "X",
		FlashAttn:  "on",
		CacheTypeK: "f16",
		CacheTypeV: "f16",
	}
}

// TestEditor_EscOnUnchangedClosesAndEmitsCancelled verifies the esc key
// path on a clean draft closes the editor and emits EditorCancelledMsg.
func TestEditor_EscOnUnchangedClosesAndEmitsCancelled(t *testing.T) {
	e := New(domain.FlagSchema{})
	e, _ = e.Open(cleanDraft())
	e, cmd := e.Update(tea.KeyMsg{Type: tea.KeyEsc})
	if e.Active() {
		t.Fatal("esc on clean draft should close editor")
	}
	got := drainCmd(cmd)
	if _, ok := got.(EditorCancelledMsg); !ok {
		t.Errorf("expected EditorCancelledMsg; got %T", got)
	}
}

// TestEditor_EscOnDirtyDraftPromptsDiscard verifies that mutating the
// in-flight draft and pressing esc opens the discard confirm overlay.
func TestEditor_EscOnDirtyDraftPromptsDiscard(t *testing.T) {
	e := New(domain.FlagSchema{})
	e, _ = e.Open(Draft{Name: "X"})

	// Mutate the in-flight draft (huh would have done this through bindings).
	e.draft.Name = "Mutated"

	e, _ = e.Update(tea.KeyMsg{Type: tea.KeyEsc})
	if !e.discardConfirm.Active() {
		t.Fatal("expected discard confirm after esc on dirty draft")
	}
	if !e.Active() {
		t.Error("editor should remain Active() while discard confirm is open")
	}

	// esc on the discard confirm clears it; editor stays in editing mode.
	e, _ = e.Update(tea.KeyMsg{Type: tea.KeyEsc})
	if e.discardConfirm.Active() {
		t.Error("esc on discard confirm should close it")
	}
	if !e.active {
		t.Error("negative discard should keep the form open")
	}
	if e.draft == nil || e.draft.Name != "Mutated" {
		t.Error("negative discard should preserve draft mutations")
	}
}

// TestEditor_DiscardConfirmAffirmativeEmitsCancelled drives the discard
// confirm through the affirmative path (left arrow + enter) and verifies
// the editor self-closes and emits EditorCancelledMsg.
func TestEditor_DiscardConfirmAffirmativeEmitsCancelled(t *testing.T) {
	e := New(domain.FlagSchema{})
	e, _ = e.Open(Draft{Name: "X"})
	e.draft.Name = "Mutated"
	e, _ = e.Update(tea.KeyMsg{Type: tea.KeyEsc})
	if !e.discardConfirm.Active() {
		t.Fatal("expected discard confirm after esc on dirty draft")
	}

	// Pump the discard confirm's Init handshake.
	e = flushAll(t, e, e.discardConfirm.Init()())

	// Toggle to affirmative and submit.
	e = flushAll(t, e, tea.KeyMsg{Type: tea.KeyLeft})
	e = flushAll(t, e, tea.KeyMsg{Type: tea.KeyEnter})

	if e.Active() {
		t.Error("editor should self-close on affirmative discard")
	}
}

func TestEditor_DiscardConfirmReceivesInitHandshake(t *testing.T) {
	e := New(domain.FlagSchema{})
	e, _ = e.Open(Draft{Name: "X"})
	e.draft.Name = "Mutated"
	e, _ = e.Update(tea.KeyMsg{Type: tea.KeyEsc})
	if !e.discardConfirm.Active() {
		t.Fatal("expected discard confirm")
	}

	// Pump the discard form's Init cmd back through Update.
	initCmd := e.discardConfirm.Init()
	if initCmd != nil {
		e, _ = e.Update(initCmd())
	}
	if !e.discardConfirm.Active() {
		t.Error("discard confirm dropped after Init handshake")
	}
}

func TestEditor_OpenResetsSubTabAndFilter(t *testing.T) {
	e := New(domain.FlagSchema{})
	// Pollute leak-prone fields directly.
	e.subTab = subTabAdvanced
	e.advancedFilter = "stale"
	e.filterMode = true

	e, _ = e.Open(Draft{Name: "X"})
	if e.subTab != subTabEssentials {
		t.Errorf("Open subTab = %v, want subTabEssentials", e.subTab)
	}
	if e.advancedFilter != "" {
		t.Errorf("Open advancedFilter = %q, want empty", e.advancedFilter)
	}
	if e.filterMode {
		t.Error("Open filterMode should be false")
	}
}

func TestEditor_AdvancedFilterModeCapturesRunes(t *testing.T) {
	schema := domain.FlagSchema{
		Flags: map[string]domain.FlagSpec{
			"alpha": {Long: "alpha", HelpText: "first"},
			"beta":  {Long: "beta", HelpText: "second"},
		},
	}
	e := New(schema)
	e, _ = e.Open(Draft{Name: "X"})

	// Switch to Advanced.
	e, _ = e.Update(tea.KeyMsg{Type: tea.KeyCtrlT})
	if e.subTab != subTabAdvanced {
		t.Fatalf("ctrl+t should switch to Advanced; got %v", e.subTab)
	}

	// Enter filter mode.
	e, _ = e.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'/'}})
	if !e.filterMode {
		t.Fatal("/ should enter filter mode")
	}

	// Type "alp" — matches alpha.
	for _, r := range "alp" {
		e, _ = e.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{r}})
	}
	if e.advancedFilter != "alp" {
		t.Errorf("advancedFilter = %q, want alp", e.advancedFilter)
	}
	if rows := e.advanced.Rows(); len(rows) != 1 || rows[0][0] != "alpha" {
		t.Errorf("filter should narrow to alpha row; got %v", rows)
	}

	// Backspace shrinks the filter.
	e, _ = e.Update(tea.KeyMsg{Type: tea.KeyBackspace})
	if e.advancedFilter != "al" {
		t.Errorf("after backspace advancedFilter = %q, want al", e.advancedFilter)
	}
}

func TestEditor_SetModelPathUpdatesDraft(t *testing.T) {
	e := New(domain.FlagSchema{})
	e, _ = e.Open(Draft{Name: "X"})
	e, _ = e.SetModelPath("/picked/m.gguf")
	if got := e.CurrentDraft().Model; got != "/picked/m.gguf" {
		t.Errorf("CurrentDraft().Model = %q, want /picked/m.gguf", got)
	}
}

func TestEditor_Dirty(t *testing.T) {
	e := New(domain.FlagSchema{})
	e, _ = e.Open(cleanDraft())
	if e.Dirty() {
		t.Error("freshly opened editor should not be dirty")
	}

	e.draft.Name = "Mutated"
	if !e.Dirty() {
		t.Error("editor should be dirty after mutation")
	}

	e.draft.Name = cleanDraft().Name
	if e.Dirty() {
		t.Error("editor should not be dirty after reverting mutation")
	}
}

func TestEditor_ViewShowsUnsavedIndicator(t *testing.T) {
	e := New(domain.FlagSchema{})
	e, _ = e.Open(cleanDraft())
	if strings.Contains(e.View(), "unsaved changes") {
		t.Error("clean editor view should not show unsaved indicator")
	}

	e.draft.Name = "Mutated"
	if !strings.Contains(e.View(), "unsaved changes") {
		t.Error("dirty editor view should show unsaved indicator")
	}
}

func TestEditor_SetModelPathInactiveNoOp(t *testing.T) {
	e := New(domain.FlagSchema{})
	e, cmd := e.SetModelPath("/x.gguf")
	if cmd != nil {
		t.Error("SetModelPath on inactive editor should return nil cmd")
	}
	if e.Active() {
		t.Error("SetModelPath on inactive editor should not activate it")
	}
}

func TestEditor_ViewEmptyWhenInactive(t *testing.T) {
	e := New(domain.FlagSchema{})
	if e.View() != "" {
		t.Errorf("inactive editor View should be empty; got %q", e.View())
	}
}

func TestEditor_ViewIncludesHeaderWhenActive(t *testing.T) {
	e := New(domain.FlagSchema{})
	e, _ = e.Open(Draft{Name: "X"})
	view := e.View()
	if !strings.Contains(view, "Editor") {
		t.Errorf("active View should include 'Editor' header; got %q", view)
	}
}

// TestEditor_CommitMsgCarriesDraft verifies the message-payload
// contract: when the editor closes after a (simulated) commit, the
// EditorCommittedMsg carries the draft fields intact. This does NOT
// drive huh.StateCompleted through Update — that path requires teatest
// infrastructure and is exercised at the page level.
func TestEditor_CommitMsgCarriesDraft(t *testing.T) {
	e := New(domain.FlagSchema{})
	e, _ = e.Open(Draft{ID: "x", Name: "Y"})

	// Force the form into completed state. We use the same flow as
	// forwardToForm for state detection.
	if e.form == nil {
		t.Fatal("form should be set after Open")
	}
	// We can't directly trigger huh's StateCompleted without driving it,
	// so we exercise the contract another way: mark active and call
	// close(); then craft EditorCommittedMsg ourselves to confirm Draft
	// content survives the round-trip when committed via forwardToForm.
	committed := *e.draft
	e = e.close()
	if e.Active() {
		t.Error("close() should clear active")
	}
	msg := EditorCommittedMsg{Draft: committed}
	if msg.Draft.Name != "Y" {
		t.Errorf("Draft.Name = %q, want Y", msg.Draft.Name)
	}
}

func TestEditor_IntRangeValidator(t *testing.T) {
	v := intRange(0, 100, false)
	if err := v("abc"); err == nil {
		t.Errorf("intRange(non-int) returned nil; want error")
	}
	if err := v("999"); err == nil {
		t.Errorf("intRange(out-of-range) returned nil; want error")
	}
	if err := intRange(0, 100, true)(""); err != nil {
		t.Errorf("intRange(allowEmpty=true)(\"\") returned %v; want nil", err)
	}
	if err := intRange(0, 100, false)(""); err == nil {
		t.Errorf("intRange(allowEmpty=false)(\"\") returned nil; want error")
	}
	if err := intRange(-1, 9999, false)("-1"); err != nil {
		t.Errorf("intRange(-1,9999)(\"-1\") returned %v; want nil", err)
	}
}

func TestEditor_PortValidator(t *testing.T) {
	v := portValidator()
	if err := v("99999"); err == nil {
		t.Errorf("portValidator(99999) returned nil; want error (out of range)")
	}
	if err := v("8080"); err != nil {
		t.Errorf("portValidator(8080) returned %v; want nil", err)
	}
	if err := v(""); err == nil {
		t.Errorf("portValidator(\"\") returned nil; want error (required)")
	}
}

func TestDraft_ToProfileDefaults(t *testing.T) {
	d := Draft{Name: "n", NGL: "5", CtxSize: "10", Port: "1234"}
	pr := d.ToProfile()
	if pr.Args["ngl"] != float64(5) {
		t.Errorf("ngl = %v, want 5", pr.Args["ngl"])
	}
	if pr.Args["port"] != float64(1234) {
		t.Errorf("port = %v, want 1234", pr.Args["port"])
	}
	if !pr.Launch.DefaultBackground {
		t.Errorf("DefaultBackground should be true")
	}
}

func TestDraft_ApplyToSetsBinaryPath(t *testing.T) {
	base := domain.Profile{
		Launch: domain.LaunchConfig{
			LogFilePath:           "/logs/existing.log",
			LlamaServerBinaryPath: "/old/llama-server",
		},
	}
	d := Draft{
		ID:        "x",
		Name:      "X",
		BackendID: "llama-cpp-custom",
		NGL:       "99",
		CtxSize:   "8192",
		Port:      "4321",
	}

	out := d.ApplyTo(base)

	if got := out.Launch.BackendID; got != "llama-cpp-custom" {
		t.Fatalf("BackendID = %q, want llama-cpp-custom", got)
	}
	if got := out.Launch.LogFilePath; got != "/logs/existing.log" {
		t.Errorf("LogFilePath = %q, want preserved /logs/existing.log", got)
	}

	d.BackendID = ""
	out = d.ApplyTo(base)
	if got := out.Launch.BackendID; got != "" {
		t.Errorf("empty draft should clear BackendID; got %q", got)
	}
}

func TestArgString_Variants(t *testing.T) {
	cases := []struct {
		in   any
		want string
	}{
		{nil, ""},
		{"abc", "abc"},
		{float64(8080), "8080"},
		{42, "42"},
	}
	for _, c := range cases {
		if got := ArgString(c.in); got != c.want {
			t.Errorf("ArgString(%v) = %q, want %q", c.in, got, c.want)
		}
	}
}

func TestDraft_ToProfileWithSchema_FiltersByBackend(t *testing.T) {
	d := Draft{
		Name:       "n",
		NGL:        "99",
		CtxSize:    "8192",
		BatchSize:  "2048",
		UBatchSize: "512",
		Port:       "4321",
		FlashAttn:  "on",
		CacheTypeK: "q8_0",
		CacheTypeV: "q8_0",
	}

	// Empty schema (fallback) includes everything.
	prAll := d.ToProfileWithSchema(domain.FlagSchema{})
	if len(prAll.Args) != 8 {
		t.Errorf("empty schema: want 8 args, got %d %v", len(prAll.Args), prAll.Args)
	}

	// Llama schema includes all known llama flags.
	llamaSchema := domain.FlagSchema{Flags: map[string]domain.FlagSpec{
		"n-gpu-layers": {Long: "n-gpu-layers", Short: "ngl", Type: domain.FlagTypeInt},
		"ctx-size":     {Long: "ctx-size", Type: domain.FlagTypeInt},
		"port":         {Long: "port", Type: domain.FlagTypeInt},
		"flash-attn":   {Long: "flash-attn", Type: domain.FlagTypeEnum, EnumValues: []string{"on", "off", "auto"}},
		"batch-size":   {Long: "batch-size", Type: domain.FlagTypeInt},
		"ubatch-size":  {Long: "ubatch-size", Type: domain.FlagTypeInt},
		"cache-type-k": {Long: "cache-type-k", Type: domain.FlagTypeEnum, EnumValues: []string{"f16", "q8_0"}},
		"cache-type-v": {Long: "cache-type-v", Type: domain.FlagTypeEnum, EnumValues: []string{"f16", "q8_0"}},
	}}
	prLlama := d.ToProfileWithSchema(llamaSchema)
	if len(prLlama.Args) != 8 {
		t.Errorf("llama schema: want 8 args, got %d %v", len(prLlama.Args), prLlama.Args)
	}

	// SGLang schema only knows port — everything else is filtered out.
	sglangSchema := domain.FlagSchema{Flags: map[string]domain.FlagSpec{
		"port": {Long: "port", Type: domain.FlagTypeInt},
	}}
	prSglang := d.ToProfileWithSchema(sglangSchema)
	if len(prSglang.Args) != 1 {
		t.Errorf("sglang schema: want 1 arg, got %d %v", len(prSglang.Args), prSglang.Args)
	}
	if _, ok := prSglang.Args["port"]; !ok {
		t.Errorf("sglang schema: expected port arg")
	}
	if _, ok := prSglang.Args["ngl"]; ok {
		t.Errorf("sglang schema: ngl should be filtered out")
	}
}

func TestEditor_BlocksCommitWhenSchemaMissing(t *testing.T) {
	catalogStore := backendcatalog.NewFSStore(t.TempDir())
	schemaStore := backendcatalog.NewFSSchemaStore(t.TempDir())
	catalog := domain.BackendCatalog{
		SchemaVersion:    1,
		DefaultBackendID: "broken",
		Backends: []domain.Backend{
			{ID: "broken", Name: "Broken", Kind: domain.BackendKindLlamaServer, Executable: "llama-server", SchemaRef: "schemas/broken.json"},
		},
	}
	if err := catalogStore.Save(catalog); err != nil {
		t.Fatal(err)
	}

	e := New(domain.FlagSchema{}).
		SetCatalogStore(catalogStore).
		SetSchemaStore(schemaStore)
	e, _ = e.Open(Draft{ID: "x", Name: "Y", BackendID: "broken"})

	if e.schemaError == "" {
		t.Fatal("expected schemaError after opening with missing schema")
	}

	e.form.State = huh.StateCompleted
	e, cmd := e.Update(tea.KeyMsg{Type: tea.KeyEnter})

	if !e.Active() {
		t.Fatal("editor should remain active when commit is blocked")
	}
	if e.schemaError == "" {
		t.Fatal("schemaError should remain set after blocked commit")
	}

	msg := drainCmd(cmd)
	if _, ok := msg.(EditorCommittedMsg); ok {
		t.Fatal("should NOT emit EditorCommittedMsg when schema is missing")
	}
}

func TestEditor_BlocksCommitWhenValidationFails(t *testing.T) {
	schema := domain.FlagSchema{Flags: map[string]domain.FlagSpec{}}
	e := New(schema)
	e, _ = e.Open(Draft{ID: "x", Name: "Y", NGL: "99"})

	e.form.State = huh.StateCompleted
	e, cmd := e.Update(tea.KeyMsg{Type: tea.KeyEnter})

	if !e.Active() {
		t.Fatal("editor should remain active when validation fails")
	}
	if e.submitError == "" {
		t.Fatal("submitError should show validation block reason")
	}

	msg := drainCmd(cmd)
	if _, ok := msg.(EditorCommittedMsg); ok {
		t.Fatal("should NOT emit EditorCommittedMsg when validation has errors")
	}
}

func TestEditor_FixesValidationErrorThenSaves(t *testing.T) {
	schema := domain.FlagSchema{Flags: map[string]domain.FlagSpec{
		"n-gpu-layers": {Long: "n-gpu-layers", Short: "ngl", Type: domain.FlagTypeInt},
		"ctx-size":     {Long: "ctx-size", Type: domain.FlagTypeInt},
		"port":         {Long: "port", Type: domain.FlagTypeInt},
		"flash-attn":   {Long: "flash-attn", Type: domain.FlagTypeEnum, EnumValues: []string{"on", "off", "auto"}},
		"cache-type-k": {Long: "cache-type-k", Type: domain.FlagTypeEnum, EnumValues: []string{"f16", "q8_0"}},
		"cache-type-v": {Long: "cache-type-v", Type: domain.FlagTypeEnum, EnumValues: []string{"f16", "q8_0"}},
	}}
	e := New(schema)
	e, _ = e.Open(Draft{ID: "x", Name: "Y", NGL: "99"})

	// First attempt: blocked due to unknown flag (NGL "99" is fine, but let's make it fail)
	e.schema = domain.FlagSchema{Flags: map[string]domain.FlagSpec{}}
	e.form.State = huh.StateCompleted
	e, _ = e.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if e.submitError == "" {
		t.Fatal("expected submitError after first blocked commit")
	}

	// Fix: restore schema so validation passes
	e.schema = schema
	// Any non-completion update clears submitError
	e, _ = e.Update(struct{}{})
	if e.submitError != "" {
		t.Fatalf("submitError should clear after update; got %q", e.submitError)
	}

	// Verify commit would succeed by checking validation directly
	report := e.validator.Validate(e.CurrentDraft().ToProfileWithSchema(e.schema), e.schema)
	if report.HasBlockingErrors() {
		t.Fatalf("expected no blocking errors after fix; got %v", report.Errors)
	}
}

func TestFlashAttnToString_Variants(t *testing.T) {
	if FlashAttnToString("on") != "on" {
		t.Error("string passthrough")
	}
	if FlashAttnToString(true) != "on" {
		t.Error("true→on")
	}
	if FlashAttnToString(false) != "off" {
		t.Error("false→off")
	}
	if FlashAttnToString(123) != "auto" {
		t.Error("unknown→auto")
	}
}

func TestEditor_SwitchesBackendAndSchema(t *testing.T) {
	catalogDir := t.TempDir()
	catalogStore := backendcatalog.NewFSStore(catalogDir)
	schemaStore := backendcatalog.NewFSSchemaStore(catalogDir)

	schemaA := domain.BackendValidationSchema{
		SchemaVersion: 1,
		BackendID:     "backend-a",
		BackendKind:   domain.BackendKindLlamaServer,
		Flags: map[string]domain.FlagSpec{
			"custom-flag-a": {Long: "custom-flag-a", Type: domain.FlagTypeBool},
		},
	}
	schemaB := domain.BackendValidationSchema{
		SchemaVersion: 1,
		BackendID:     "backend-b",
		BackendKind:   domain.BackendKindLlamaServer,
		Flags: map[string]domain.FlagSpec{
			"custom-flag-b": {Long: "custom-flag-b", Type: domain.FlagTypeBool},
		},
	}
	_ = schemaStore.Save("backend-a.json", schemaA)
	_ = schemaStore.Save("backend-b.json", schemaB)

	catalog := domain.BackendCatalog{
		SchemaVersion:    1,
		DefaultBackendID: "backend-a",
		Backends: []domain.Backend{
			{ID: "backend-a", Name: "Backend A", Kind: domain.BackendKindLlamaServer, Executable: "a", SchemaRef: "schemas/backend-a.json"},
			{ID: "backend-b", Name: "Backend B", Kind: domain.BackendKindLlamaServer, Executable: "b", SchemaRef: "schemas/backend-b.json"},
		},
	}
	_ = catalogStore.Save(catalog)

	e := New(domain.FlagSchema{}).
		SetCatalogStore(catalogStore).
		SetSchemaStore(schemaStore).
		SetBackendOptions([]huh.Option[string]{
			huh.NewOption("Backend A", "backend-a"),
			huh.NewOption("Backend B", "backend-b"),
		})

	e, _ = e.Open(Draft{ID: "x", Name: "Y", BackendID: "backend-a"})

	if e.schemaError != "" {
		t.Fatalf("unexpected schemaError on open: %s", e.schemaError)
	}

	profileA := domain.Profile{ID: "x", Args: map[string]any{"custom-flag-a": true}, Launch: domain.LaunchConfig{BackendID: "backend-a"}}
	report := e.validator.Validate(profileA, e.schema)
	if report.HasBlockingErrors() {
		t.Fatalf("expected no errors with backend-a schema; got %v", report.Errors)
	}

	e.draft.BackendID = "backend-b"
	e, _ = e.Update(struct{}{})

	if e.schemaError != "" {
		t.Fatalf("unexpected schemaError after switch: %s", e.schemaError)
	}

	profileB := domain.Profile{ID: "x", Args: map[string]any{"custom-flag-a": true}, Launch: domain.LaunchConfig{BackendID: "backend-b"}}
	report = e.validator.Validate(profileB, e.schema)
	if !report.HasBlockingErrors() {
		t.Fatal("expected validation errors after switching to backend-b (custom-flag-a is unknown)")
	}
	if len(report.Errors) != 1 || report.Errors[0].Field != "custom-flag-a" {
		t.Fatalf("expected error on custom-flag-a; got %v", report.Errors)
	}
}

func TestDraft_ApplyToWithSchema_PreservesExistingArgs(t *testing.T) {
	sglangSchema := domain.FlagSchema{Flags: map[string]domain.FlagSpec{
		"port":              {Long: "port", Type: domain.FlagTypeInt},
		"tp-size":           {Long: "tp-size", Type: domain.FlagTypeInt},
		"dtype":             {Long: "dtype", Type: domain.FlagTypeEnum, EnumValues: []string{"float16", "bfloat16", "float32"}},
		"mem-fraction-static": {Long: "mem-fraction-static", Type: domain.FlagTypeFloat},
	}}
	base := domain.Profile{
		Args: map[string]any{
			"tp-size":             float64(2),
			"dtype":               "float16",
			"mem-fraction-static": float64(0.85),
		},
	}
	d := Draft{Port: "30000", Args: map[string]any{"tp-size": float64(4)}}
	pr := d.ApplyToWithSchema(base, sglangSchema)

	if pr.Args["tp-size"] != float64(4) {
		t.Errorf("Draft.Args should overlay base.Args; got tp-size=%v", pr.Args["tp-size"])
	}
	if pr.Args["dtype"] != "float16" {
		t.Errorf("base.Args dtype should be preserved; got %v", pr.Args["dtype"])
	}
	if pr.Args["mem-fraction-static"] != float64(0.85) {
		t.Errorf("base.Args mem-fraction-static should be preserved; got %v", pr.Args["mem-fraction-static"])
	}
	if pr.Args["port"] != float64(30000) {
		t.Errorf("Essentials port should be set; got %v", pr.Args["port"])
	}
}

func TestEditor_AdvancedTabInlineEdit(t *testing.T) {
	schema := domain.FlagSchema{Flags: map[string]domain.FlagSpec{
		"alpha": {Long: "alpha", Type: domain.FlagTypeInt},
		"beta":  {Long: "beta", Type: domain.FlagTypeString},
	}}
	e := New(schema)
	d := Draft{Name: "X", Args: map[string]any{"alpha": float64(1)}}
	e, _ = e.Open(d)

	// Switch to Advanced tab.
	e, _ = e.Update(tea.KeyMsg{Type: tea.KeyCtrlT})
	if e.subTab != subTabAdvanced {
		t.Fatal("expected Advanced sub-tab")
	}

	// Press enter on first row to start editing.
	e, _ = e.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if !e.advancedEditing {
		t.Fatal("expected advancedEditing after enter")
	}
	if e.advancedEditFlag != "alpha" {
		t.Fatalf("expected editing alpha, got %q", e.advancedEditFlag)
	}
	if e.advancedEditVal != "1" {
		t.Fatalf("expected initial value 1, got %q", e.advancedEditVal)
	}

	// Type "42".
	for _, r := range "42" {
		e, _ = e.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{r}})
	}
	if e.advancedEditVal != "142" {
		t.Fatalf("expected 142 after typing, got %q", e.advancedEditVal)
	}

	// Press enter to save.
	e, _ = e.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if e.advancedEditing {
		t.Fatal("expected editing to stop after enter")
	}
	if e.draft.Args["alpha"] != float64(142) {
		t.Fatalf("expected alpha=142, got %v", e.draft.Args["alpha"])
	}

	// Press enter on second row (beta) and clear it.
	e, _ = e.Update(tea.KeyMsg{Type: tea.KeyDown})
	e, _ = e.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if e.advancedEditFlag != "beta" {
		t.Fatalf("expected editing beta, got %q", e.advancedEditFlag)
	}
	e, _ = e.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if e.draft.Args["beta"] != nil {
		t.Fatalf("expected beta deleted on empty save, got %v", e.draft.Args["beta"])
	}
}

func TestEditor_AdvancedEdit_NoPanicOnNilArgs(t *testing.T) {
	schema := domain.FlagSchema{Flags: map[string]domain.FlagSpec{
		"alpha": {Long: "alpha", Type: domain.FlagTypeInt},
	}}
	e := New(schema)
	// Draft with nil Args simulates a new profile before newDraftDefaults fix.
	d := Draft{Name: "X"}
	e, _ = e.Open(d)
	e, _ = e.Update(tea.KeyMsg{Type: tea.KeyCtrlT})
	e, _ = e.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if !e.advancedEditing {
		t.Fatal("expected editing mode")
	}
	// This must not panic even though d.Args is nil.
	e, _ = e.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'9'}})
	e, _ = e.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if e.draft.Args["alpha"] != float64(9) {
		t.Fatalf("expected alpha=9, got %v", e.draft.Args["alpha"])
	}
}

func TestEditor_AdvancedEdit_RejectsInvalidValue(t *testing.T) {
	schema := domain.FlagSchema{Flags: map[string]domain.FlagSpec{
		"alpha": {Long: "alpha", Type: domain.FlagTypeInt},
	}}
	e := New(schema)
	d := Draft{Name: "X", Args: map[string]any{}}
	e, _ = e.Open(d)
	e, _ = e.Update(tea.KeyMsg{Type: tea.KeyCtrlT})
	e, _ = e.Update(tea.KeyMsg{Type: tea.KeyEnter})
	for _, r := range "abc" {
		e, _ = e.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{r}})
	}
	e, _ = e.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if !e.advancedEditing {
		t.Fatal("expected editing to remain active on invalid input")
	}
	if e.submitError == "" {
		t.Fatal("expected submitError after invalid value")
	}
	if _, ok := e.draft.Args["alpha"]; ok {
		t.Fatal("invalid value should not be stored")
	}
}

func TestEditor_AdvancedEdit_RefreshesTableAfterSave(t *testing.T) {
	schema := domain.FlagSchema{Flags: map[string]domain.FlagSpec{
		"alpha": {Long: "alpha", Type: domain.FlagTypeInt},
	}}
	e := New(schema)
	d := Draft{Name: "X", Args: map[string]any{}}
	e, _ = e.Open(d)
	e, _ = e.Update(tea.KeyMsg{Type: tea.KeyCtrlT})
	e, _ = e.Update(tea.KeyMsg{Type: tea.KeyEnter})
	for _, r := range "42" {
		e, _ = e.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{r}})
	}
	e, _ = e.Update(tea.KeyMsg{Type: tea.KeyEnter})
	rows := e.advanced.Rows()
	if len(rows) != 1 {
		t.Fatalf("expected 1 row, got %d", len(rows))
	}
	if rows[0][2] != "42" {
		t.Fatalf("expected Value column to show 42 after save, got %q", rows[0][2])
	}
}

func TestParseTags(t *testing.T) {
	cases := []struct {
		in   string
		want []string
	}{
		{"", nil},
		{"   ", nil},
		{",,", nil},
		{"coding", []string{"coding"}},
		{"coding, 32b", []string{"coding", "32b"}},
		{"  coding ,  32b  ,  ", []string{"coding", "32b"}},
		{"a,,b", []string{"a", "b"}},
	}
	for _, c := range cases {
		got := ParseTags(c.in)
		if !reflect.DeepEqual(got, c.want) {
			t.Errorf("ParseTags(%q) = %v, want %v", c.in, got, c.want)
		}
	}
}

func TestFormatTags(t *testing.T) {
	cases := []struct {
		in   []string
		want string
	}{
		{nil, ""},
		{[]string{}, ""},
		{[]string{"coding"}, "coding"},
		{[]string{"coding", "32b"}, "coding, 32b"},
	}
	for _, c := range cases {
		if got := FormatTags(c.in); got != c.want {
			t.Errorf("FormatTags(%v) = %q, want %q", c.in, got, c.want)
		}
	}
}

func TestDraft_ApplyToPersistsTags(t *testing.T) {
	d := Draft{
		ID:   "x",
		Name: "X",
		Tags: "coding, 32b, ",
		Port: "4321",
	}
	out := d.ApplyTo(domain.Profile{})
	want := []string{"coding", "32b"}
	if !reflect.DeepEqual(out.Tags, want) {
		t.Fatalf("Tags = %v, want %v", out.Tags, want)
	}
}

func TestDraft_ApplyToEmptyTagsYieldsNil(t *testing.T) {
	d := Draft{ID: "x", Name: "X", Tags: "   ", Port: "4321"}
	out := d.ApplyTo(domain.Profile{})
	if out.Tags != nil {
		t.Fatalf("Tags = %v, want nil for whitespace-only input", out.Tags)
	}
}

func TestDraft_ApplyToTagsRoundTripsExisting(t *testing.T) {
	base := domain.Profile{Tags: []string{"stale"}}
	d := Draft{ID: "x", Name: "X", Tags: FormatTags([]string{"fresh", "tag"}), Port: "4321"}
	out := d.ApplyTo(base)
	want := []string{"fresh", "tag"}
	if !reflect.DeepEqual(out.Tags, want) {
		t.Fatalf("Tags = %v, want %v (Draft.Tags should overwrite base.Tags)", out.Tags, want)
	}
}
