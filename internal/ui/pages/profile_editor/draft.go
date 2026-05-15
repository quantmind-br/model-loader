// Package profile_editor encapsulates the profile editor sub-model
// (form, draft, sub-tab, advanced table, discard-confirm) extracted from
// ProfilesPage. The page composes an Editor by value and forwards messages
// to it while it is Active.
package profile_editor

import (
	"fmt"
	"sort"
	"strconv"

	"github.com/charmbracelet/bubbles/table"
	"github.com/charmbracelet/huh"

	"github.com/quantmind-br/model-loader/internal/domain"
	"github.com/quantmind-br/model-loader/internal/ui/internal/filter"
)

// subTab selects between the Essentials huh form and the Advanced
// flag-reference table inside the editor.
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

type Draft struct {
	ID          string
	Name        string
	Description string
	Model       string
	BackendID   string
	NGL         string
	CtxSize     string
	BatchSize   string
	UBatchSize  string
	Port        string
	FlashAttn   string
	CacheTypeK  string
	CacheTypeV  string
	IsNew       bool
	// Args holds generic schema-backed flag values edited in the Advanced
	// tab. Keys are flag long names; values are typed per domain.FlagSpec.
	// Essentials fields (ngl, ctx-size, etc.) take precedence and overwrite
	// matching keys here during ApplyToWithSchema.
	Args map[string]any
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
// it falls back to the legacy behaviour of including all draft fields.
// Existing base.Args that are valid in the schema are preserved, then
// Draft.Args overlays them, and finally Essentials fields take precedence.
func (d Draft) ApplyToWithSchema(base domain.Profile, schema domain.FlagSchema) domain.Profile {
	ngl, _ := strconv.Atoi(d.NGL)
	ctx, _ := strconv.Atoi(d.CtxSize)
	port, _ := strconv.Atoi(d.Port)
	args := map[string]any{}

	// include adds a flag only when the schema knows it or when no schema
	// is available (fallback for backward compatibility).
	include := func(key string, val any) {
		if len(schema.Flags) == 0 {
			args[key] = val
			return
		}
		if _, ok := schema.Lookup(key); ok {
			args[key] = val
		}
	}

	// 1. Preserve existing base args that are valid in the schema.
	for k, v := range base.Args {
		include(k, v)
	}

	// 2. Overlay generic draft args (edited in Advanced tab).
	for k, v := range d.Args {
		include(k, v)
	}

	// 3. Overlay hardcoded Essentials fields (they take precedence).
	include("port", float64(port))
	include("ngl", float64(ngl))
	include("ctx-size", float64(ctx))
	if d.FlashAttn != "" {
		include("flash-attn", d.FlashAttn)
	}
	if v, err := strconv.Atoi(d.BatchSize); err == nil {
		include("batch-size", float64(v))
	}
	if v, err := strconv.Atoi(d.UBatchSize); err == nil {
		include("ubatch-size", float64(v))
	}
	if d.CacheTypeK != "" {
		include("cache-type-k", d.CacheTypeK)
	}
	if d.CacheTypeV != "" {
		include("cache-type-v", d.CacheTypeV)
	}

	out := base
	out.ID = d.ID
	out.Name = d.Name
	out.Description = d.Description
	out.Model = d.Model
	out.Args = args
	out.Launch.DefaultBackground = true
	out.Launch.BackendID = d.BackendID
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

func buildForm(d *Draft, schema domain.FlagSchema, backendOpts []huh.Option[string], kind domain.BackendKind) *huh.Form {
	cacheOpts := selectOptions(schema, "cache-type-k", []string{"f16", "q8_0", "q4_0"})
	groups := []*huh.Group{
		huh.NewGroup(
			huh.NewInput().Title("Name").Value(&d.Name),
			huh.NewInput().Title("Description").Value(&d.Description),
			huh.NewInput().Title("Model path (.gguf)").Value(&d.Model),
		),
	}
	if len(backendOpts) > 0 {
		groups[0] = huh.NewGroup(
			huh.NewInput().Title("Name").Value(&d.Name),
			huh.NewInput().Title("Description").Value(&d.Description),
			huh.NewInput().Title("Model path (.gguf)").Value(&d.Model),
			huh.NewSelect[string]().
				Title("Backend").
				Description("Select the LLM server backend for this profile").
				Options(backendOpts...).
				Value(&d.BackendID),
		)
	}
	if kind == domain.BackendKindLlamaServer || kind == "" {
		groups = append(groups, huh.NewGroup(
			huh.NewInput().Title(labelWithHelp(schema, "n-gpu-layers", "ngl (gpu layers)")).Value(&d.NGL).Validate(intRange(-1, 9999, false)),
			huh.NewInput().Title(labelWithHelp(schema, "ctx-size", "ctx-size")).Value(&d.CtxSize).Validate(intRange(0, 1024*1024, false)),
			huh.NewInput().Title(labelWithHelp(schema, "batch-size", "batch-size")).Value(&d.BatchSize).Validate(intRange(0, 1024*1024, true)),
			huh.NewInput().Title(labelWithHelp(schema, "ubatch-size", "ubatch-size")).Value(&d.UBatchSize).Validate(intRange(0, 1024*1024, true)),
			huh.NewInput().Title(labelWithHelp(schema, "port", "port")).Value(&d.Port).Validate(portValidator()),
			huh.NewSelect[string]().Title(labelWithHelp(schema, "flash-attn", "flash-attn")).Options(toOptions(selectOptions(schema, "flash-attn", []string{"on", "off", "auto"}))...).Value(&d.FlashAttn),
			huh.NewSelect[string]().Title("cache-type-k").Options(toOptions(cacheOpts)...).Value(&d.CacheTypeK),
			huh.NewSelect[string]().Title("cache-type-v").Options(toOptions(cacheOpts)...).Value(&d.CacheTypeV),
		))
	} else {
		groups = append(groups, huh.NewGroup(
			huh.NewInput().Title(labelWithHelp(schema, "port", "port")).Value(&d.Port).Validate(portValidator()),
		))
	}
	return huh.NewForm(groups...).WithShowHelp(true)
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

func newAdvancedTable(schema domain.FlagSchema, args map[string]any, width, height int) table.Model {
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
	rows := schemaRows(schema, args)
	t := table.New(table.WithColumns(cols), table.WithRows(rows), table.WithFocused(true), table.WithHeight(height))
	return t
}

func schemaRows(schema domain.FlagSchema, args map[string]any) []table.Row {
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
