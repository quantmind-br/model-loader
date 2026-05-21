// Package profile_editor encapsulates the profile editor sub-model
// (form, draft, sub-tab, advanced table, discard-confirm) extracted from
// ProfilesPage. The page composes an Editor by value and forwards messages
// to it while it is Active.
package profile_editor

import (
	"fmt"
	"sort"
	"strconv"
	"strings"

	"github.com/charmbracelet/bubbles/table"
	"github.com/charmbracelet/huh"

	"github.com/quantmind-br/model-loader/internal/domain"
	"github.com/quantmind-br/model-loader/internal/ui/internal/filter"
)

// subTab selects between the Essentials huh form, the Advanced
// flag-reference table, and the Environment variable list inside the editor.
type subTab int

const (
	subTabEssentials subTab = iota
	subTabAdvanced
	subTabEnvironment
	subTabSizing
)

func (s subTab) String() string {
	switch s {
	case subTabEssentials:
		return "Essentials"
	case subTabAdvanced:
		return "Advanced"
	case subTabEnvironment:
		return "Environment"
	case subTabSizing:
		return "Sizing"
	default:
		return "?"
	}
}

type Draft struct {
	ID string
	// OrigID is the profile's id at edit-open time. Page-only metadata, never
	// bound to a form field; empty for new profiles. Used to detect a rename
	// (ID changed) so the commit handler can move the underlying file.
	OrigID      string
	Name        string
	Description string
	Tags        string
	Model       string
	BackendID   string
	IsNew       bool
	// Essentials holds schema-driven essential flag values keyed by canonical
	// long flag name (e.g. "n-gpu-layers", "ctx-size"). Populated by
	// hydrateEssentials and kept in sync with the huh form via syncEssentials.
	// Value map (not pointer) so Draft remains copyable for snapshot/dirty-check.
	Essentials map[string]string
	// Args holds generic schema-backed flag values edited in the Advanced
	// tab. Keys are flag long names; values are typed per domain.FlagSpec.
	// Essentials fields (ngl, ctx-size, etc.) take precedence and overwrite
	// matching keys here during ApplyToWithSchema.
	Args map[string]any
	// Env mirrors Profile.Launch.Env in editor-friendly form. Slice preserves
	// insertion order as shown in the Environment sub-tab.
	Env            []domain.EnvVar
	RestartPolicy  string
	MaxRestarts    string
	BackoffSeconds string
}

// ParseTags converts a comma-separated editor string to the trimmed
// []string form stored on domain.Profile.Tags. Empty and whitespace-only
// entries are dropped; an empty input yields a nil slice so it round-trips
// cleanly through `json:"tags,omitempty"`.
func ParseTags(raw string) []string {
	if strings.TrimSpace(raw) == "" {
		return nil
	}
	parts := strings.Split(raw, ",")
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		if t := strings.TrimSpace(p); t != "" {
			out = append(out, t)
		}
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

// FormatTags renders a []string from domain.Profile.Tags as the
// comma-separated form shown in the editor's Tags input.
func FormatTags(tags []string) string {
	return strings.Join(tags, ", ")
}

// ToProfile maps the editor draft to a domain.Profile. Always sets
// ngl/ctx-size/port (zero on parse fail) so save and live preview produce
// the same shape. Caller owns ID generation and Meta preservation.
func (d Draft) ToProfile() domain.Profile {
	return d.ApplyTo(domain.Profile{})
}

// ToProfileWithSchema maps the draft to a domain.Profile using schema-aware
// filtering so only flags known by the backend are included.
func (d Draft) ToProfileWithSchema(schema domain.FlagSchema) domain.Profile {
	return d.ApplyToWithSchema(domain.Profile{}, schema)
}

// ApplyTo maps the editor draft onto base, preserving caller-owned fields
// that the editor does not track while overwriting editor-tracked fields.
func (d Draft) ApplyTo(base domain.Profile) domain.Profile {
	return d.ApplyToWithSchema(base, domain.FlagSchema{})
}

// ApplyToWithSchema maps the editor draft onto base, filtering args to only
// include flags known by the provided schema. When schema is empty (no flags),
// it falls back to pass-through behaviour for any keys present.
//
// Precedence (lowest → highest):
//  1. base.Args (filtered by schema; entries whose canonical long collides
//     with an Essentials key are skipped to dedupe legacy short keys such as
//     "ngl" vs "n-gpu-layers").
//  2. Draft.Args (Advanced tab edits, overlay).
//  3. Draft.Essentials (highest; keyed by canonical long name; empty values
//     are omitted so the user can clear a field back to backend default;
//     each value is parsed via parseFlagValue and silently dropped on parse
//     error since the form validator should have caught it).
func (d Draft) ApplyToWithSchema(base domain.Profile, schema domain.FlagSchema) domain.Profile {
	args := map[string]any{}
	hasSchema := len(schema.Flags) > 0

	// Set of canonical longs in Essentials, used to dedupe legacy short keys
	// (e.g. base.Args["ngl"] when Essentials carries "n-gpu-layers").
	essLong := map[string]bool{}
	for k := range d.Essentials {
		essLong[k] = true
	}

	include := func(key string, val any) {
		if !hasSchema {
			args[key] = val
			return
		}
		if _, ok := schema.Lookup(key); ok {
			args[key] = val
		}
	}

	// 1. base.Args valid in schema, except those colliding with an Essentials
	//    key under their canonical long name.
	for k, v := range base.Args {
		if hasSchema {
			if spec, ok := schema.Lookup(k); ok && essLong[spec.Long] {
				continue // will be (re)written by Essentials under the long name
			}
		}
		include(k, v)
	}

	// 2. Draft.Args (Advanced tab).
	for k, v := range d.Args {
		include(k, v)
	}

	// 3. Essentials, under canonical long name, parsed per schema type.
	for long, raw := range d.Essentials {
		if strings.TrimSpace(raw) == "" {
			continue // empty = use backend default / unset
		}
		if !hasSchema {
			args[long] = raw
			continue
		}
		spec, ok := schema.Lookup(long)
		if !ok {
			continue
		}
		val, err := parseFlagValue(raw, schema, long)
		if err != nil {
			continue // validator already gated; defensive
		}
		args[spec.Long] = val
	}

	out := base
	out.ID = d.ID
	out.Name = d.Name
	out.Description = d.Description
	out.Tags = ParseTags(d.Tags)
	out.Model = d.Model
	out.Args = args
	out.Launch.DefaultBackground = true
	out.Launch.BackendID = d.BackendID
	if len(d.Env) > 0 {
		out.Launch.Env = append([]domain.EnvVar(nil), d.Env...)
	} else {
		out.Launch.Env = nil
	}
	out.Launch.RestartPolicy = domain.RestartPolicy(d.RestartPolicy)
	if v, err := strconv.Atoi(d.MaxRestarts); err == nil {
		out.Launch.MaxRestarts = v
	}
	if v, err := strconv.Atoi(d.BackoffSeconds); err == nil {
		out.Launch.BackoffSeconds = v
	}
	return out
}

// ArgString converts a stored args-map value to its editor-string form.
// Exported because callers (ProfilesPage.startEditSelected) need it to
// hydrate a Draft from an existing domain.Profile.
func ArgString(v any) string {
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

// FlashAttnToString converts a stored flash-attn value to the editor's
// string form. Older profiles may have boolean values; map true → "on",
// false → "off". Strings pass through; anything else falls back to "auto".
func FlashAttnToString(v any) string {
	switch t := v.(type) {
	case string:
		return t
	case bool:
		if t {
			return "on"
		}
		return "off"
	default:
		return "auto"
	}
}

func buildForm(d *Draft, schema domain.FlagSchema, backendOpts []huh.Option[string], kind domain.BackendKind, idTaken func(string) bool) (*huh.Form, map[string]*string, map[string]huh.Field) {
	ptrs := map[string]*string{}
	fieldMap := map[string]huh.Field{}

	g1Fields := []huh.Field{
		setFieldKey(huh.NewInput().Title("ID (slug)").Description(`Filename + the "model" value clients send; lowercase a-z 0-9 . _ -`).Value(&d.ID).Validate(slugValidator(d, idTaken)), "id"),
		setFieldKey(huh.NewInput().Title("Name").Description("Display name / label").Value(&d.Name), "name"),
		setFieldKey(huh.NewInput().Title("Description").Value(&d.Description), "description"),
		setFieldKey(huh.NewInput().Title("Tags").Description("comma-separated").Value(&d.Tags), "tags"),
		setFieldKey(huh.NewInput().Title(modelLabel(kind)).Description(modelDesc(kind)).Value(&d.Model), "model"),
	}
	if len(backendOpts) > 0 {
		g1Fields = append(g1Fields, setFieldKey(huh.NewSelect[string]().
			Title("Backend").
			Description("Which backend engine to use").
			Options(backendOpts...).
			Value(&d.BackendID), "backend"))
	}
	groups := []*huh.Group{huh.NewGroup(g1Fields...)}

	if ess := essentialsFor(kind, schema); len(ess) > 0 {
		fields := make([]huh.Field, 0, len(ess))
		for _, f := range ess {
			spec, _ := schema.Lookup(f.Flag)
			val := d.Essentials[spec.Long]
			p := &val
			ptrs[spec.Long] = p
			fld := setFieldKey(essentialField(f, spec, p), spec.Long)
			fields = append(fields, fld)
			fieldMap[spec.Long] = fld
		}
		groups = append(groups, huh.NewGroup(fields...))
	}

	restartOpts := []huh.Option[string]{
		huh.NewOption("none", string(domain.RestartPolicyNone)),
		huh.NewOption("on-failure", string(domain.RestartPolicyOnFailure)),
		huh.NewOption("always", string(domain.RestartPolicyAlways)),
	}
	groups = append(groups, huh.NewGroup(
		setFieldKey(huh.NewSelect[string]().Title("Restart policy").Description("Auto-restart behaviour on crash").Options(restartOpts...).Value(&d.RestartPolicy), "restart_policy"),
		setFieldKey(huh.NewInput().Title("Max restarts").Description("Maximum consecutive restarts (0 = unlimited)").Value(&d.MaxRestarts).Validate(intRange(0, 100, false)), "max_restarts"),
		setFieldKey(huh.NewInput().Title("Backoff seconds").Description("Base delay before first restart attempt").Value(&d.BackoffSeconds).Validate(intRange(1, 3600, false)), "backoff_seconds"),
	))
	return huh.NewForm(groups...).WithShowHelp(true), ptrs, fieldMap
}

func setFieldKey(f huh.Field, key string) huh.Field {
	switch v := f.(type) {
	case *huh.Input:
		return v.Key(key)
	case *huh.Select[string]:
		return v.Key(key)
	}
	return f
}

func essentialField(f EssentialField, spec domain.FlagSpec, p *string) huh.Field {
	title := f.Label
	if title == "" {
		title = spec.Long
	}
	title = decorateLabel(title, spec)
	desc := f.Description
	if desc == "" {
		desc = spec.HelpText
	}
	switch spec.Type {
	case domain.FlagTypeEnum:
		return huh.NewSelect[string]().Title(title).Description(desc).Options(toOptions(spec.EnumValues)...).Value(p)
	case domain.FlagTypeBool:
		return huh.NewSelect[string]().Title(title).Description(desc).Options(toOptions([]string{"true", "false"})...).Value(p)
	case domain.FlagTypeFloat:
		return huh.NewInput().Title(title).Description(desc).Value(p).Validate(floatValidator(f.AllowEmpty))
	case domain.FlagTypeInt:
		return huh.NewInput().Title(title).Description(desc).Value(p).Validate(intValidatorFor(f))
	default:
		return huh.NewInput().Title(title).Description(desc).Value(p)
	}
}

func intValidatorFor(f EssentialField) func(string) error {
	if f.IsPort {
		return portValidator()
	}
	min, max := -1<<31, 1<<31-1
	if f.Min != nil {
		min = *f.Min
	}
	if f.Max != nil {
		max = *f.Max
	}
	return intRange(min, max, f.AllowEmpty)
}

// slugValidator gates the editable profile ID: must be canonical kebab-case
// and not collide with another existing profile. The current id (d.OrigID) is
// always allowed so an unchanged id passes during edit.
func slugValidator(d *Draft, idTaken func(string) bool) func(string) error {
	return func(s string) error {
		if !domain.IsValidSlug(s) {
			return fmt.Errorf("must be lowercase a-z, 0-9 separated by . _ or -")
		}
		if s != d.OrigID && idTaken != nil && idTaken(s) {
			return fmt.Errorf("id already exists")
		}
		return nil
	}
}

func floatValidator(allowEmpty bool) func(string) error {
	return func(s string) error {
		if s == "" {
			if allowEmpty {
				return nil
			}
			return fmt.Errorf("required")
		}
		if _, err := strconv.ParseFloat(s, 64); err != nil {
			return fmt.Errorf("must be a number")
		}
		return nil
	}
}

func modelLabel(kind domain.BackendKind) string {
	switch kind {
	case domain.BackendKindVLLM, domain.BackendKindSGLang:
		return "Model (HF repo id or local path)"
	default:
		return "Model path (.gguf)"
	}
}

func modelDesc(kind domain.BackendKind) string {
	switch kind {
	case domain.BackendKindVLLM, domain.BackendKindSGLang:
		return "HuggingFace repo id (e.g., meta-llama/Llama-3-8B) or local path (Ctrl+P to browse)"
	default:
		return "Absolute path to a .gguf file (Ctrl+P to browse)"
	}
}

// intRange returns a huh validator for integer fields in [min,max].
// allowEmpty=true treats "" as valid (used for optional fields like
// batch-size that fall back to llama-server defaults when blank).
func intRange(min, max int, allowEmpty bool) func(string) error {
	return func(s string) error {
		if s == "" {
			if allowEmpty {
				return nil
			}
			return fmt.Errorf("required")
		}
		v, err := strconv.Atoi(s)
		if err != nil {
			return fmt.Errorf("must be an integer")
		}
		if v < min || v > max {
			return fmt.Errorf("must be in [%d, %d]", min, max)
		}
		return nil
	}
}

// portValidator restricts to valid TCP port range (0 reserved → require 1+).
func portValidator() func(string) error {
	return func(s string) error {
		if s == "" {
			return fmt.Errorf("required")
		}
		v, err := strconv.Atoi(s)
		if err != nil {
			return fmt.Errorf("must be an integer")
		}
		if v < 1 || v > 65535 {
			return fmt.Errorf("must be in [1, 65535]")
		}
		return nil
	}
}

func decorateLabel(title string, spec domain.FlagSpec) string {
	if spec.HelpText != "" {
		if spec.Default != nil {
			return fmt.Sprintf("%s — %s (default %v)", title, spec.HelpText, spec.Default)
		}
		return fmt.Sprintf("%s — %s", title, spec.HelpText)
	}
	return title
}

func toOptions(values []string) []huh.Option[string] {
	out := make([]huh.Option[string], 0, len(values))
	for _, v := range values {
		out = append(out, huh.NewOption(v, v))
	}
	return out
}

func newEnvTable(envs []domain.EnvVar) table.Model {
	cols := []table.Column{
		{Title: "KEY", Width: 32},
		{Title: "VALUE", Width: 60},
	}
	rows := make([]table.Row, 0, len(envs))
	for _, ev := range envs {
		rows = append(rows, table.Row{ev.Key, ev.Value})
	}
	return table.New(
		table.WithColumns(cols),
		table.WithRows(rows),
		table.WithFocused(true),
		table.WithHeight(12),
	)
}

func newAdvancedTable(schema domain.FlagSchema, args map[string]any, kind domain.BackendKind, width, height int) table.Model {
	flagWidth := 22
	typeWidth := 8
	valWidth := 14
	helpWidth := width - flagWidth - typeWidth - valWidth - 16
	if helpWidth < 16 {
		helpWidth = 16
	}
	cols := []table.Column{
		{Title: "Flag", Width: flagWidth},
		{Title: "Type", Width: typeWidth},
		{Title: "Value", Width: valWidth},
		{Title: "Help", Width: helpWidth},
	}
	rows := schemaRows(schema, args, kind)
	t := table.New(table.WithColumns(cols), table.WithRows(rows), table.WithFocused(true), table.WithHeight(height))
	return t
}

func schemaRows(schema domain.FlagSchema, args map[string]any, kind domain.BackendKind) []table.Row {
	essential := make(map[string]bool)
	for _, f := range essentialsFor(kind, schema) {
		if spec, ok := schema.Lookup(f.Flag); ok {
			essential[spec.Long] = true
		}
	}

	names := make([]string, 0, len(schema.Flags))
	for k := range schema.Flags {
		names = append(names, k)
	}
	sort.Strings(names)
	rows := make([]table.Row, 0, len(names))
	for _, name := range names {
		// model-path is edited via the Essentials "Model path" field,
		// so skip it from the Advanced table to avoid duplication.
		if name == "model-path" {
			continue
		}
		spec := schema.Flags[name]
		if essential[spec.Long] {
			continue
		}
		val := ""
		if args != nil {
			val = ArgString(args[name])
		}
		rows = append(rows, table.Row{
			spec.Long,
			typeLabel(spec.Type),
			val,
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
	return filter.ContainsFold(all, q, func(r table.Row) string { return r[0] })
}

// parseFlagValue converts a raw string entered in the Advanced tab into the
// correct Go type for the given flag according to the schema.
func parseFlagValue(raw string, schema domain.FlagSchema, flag string) (any, error) {
	spec, ok := schema.Lookup(flag)
	if !ok {
		return raw, nil
	}
	switch spec.Type {
	case domain.FlagTypeBool:
		if raw == "true" || raw == "1" || raw == "on" {
			return true, nil
		}
		if raw == "false" || raw == "0" || raw == "off" {
			return false, nil
		}
		return nil, fmt.Errorf("expected bool (true/false/on/off/1/0), got %q", raw)
	case domain.FlagTypeInt:
		v, err := strconv.Atoi(raw)
		if err != nil {
			return nil, fmt.Errorf("expected int, got %q", raw)
		}
		return float64(v), nil
	case domain.FlagTypeFloat:
		v, err := strconv.ParseFloat(raw, 64)
		if err != nil {
			return nil, fmt.Errorf("expected float, got %q", raw)
		}
		return v, nil
	case domain.FlagTypeEnum:
		for _, ev := range spec.EnumValues {
			if ev == raw {
				return raw, nil
			}
		}
		return nil, fmt.Errorf("expected one of %v, got %q", spec.EnumValues, raw)
	default:
		return raw, nil
	}
}
