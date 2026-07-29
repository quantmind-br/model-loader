// Package configweb runs the on-demand web GUI used by the TUI to edit profiles.
package configweb

import (
	"maps"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/quantmind-br/model-loader/internal/domain"
)

// Draft is the web form payload for a profile being created or edited.
// Args are string-typed at the boundary (HTML form values) and coerced to the
// schema's FlagType in ToProfile/ApplyTo.
type Draft struct {
	ID          string            `json:"id"`
	OrigID      string            `json:"origId"`
	IsNew       bool              `json:"isNew"`
	Name        string            `json:"name"`
	Description string            `json:"description"`
	Tags        []string          `json:"tags"`
	Model       string            `json:"model"`
	BackendID   string            `json:"backendId"`
	Args        map[string]string `json:"args"`
	ExtraArgs   []string          `json:"extraArgs"`
	Env         []domain.EnvVar   `json:"env,omitempty"`
}

// ToProfile builds a fresh Profile from the draft, coercing arg values by type.
func (d Draft) ToProfile(schema domain.FlagSchema) domain.Profile {
	now := time.Now().UTC()
	args, routed := coerceArgs(d.Args, schema)
	return domain.Profile{
		SchemaVersion: domain.SchemaVersion,
		ID:            d.ID,
		Name:          d.Name,
		Description:   d.Description,
		Tags:          d.Tags,
		Model:         d.Model,
		Args:          args,
		ExtraArgs:     mergeExtraArgs(d.ExtraArgs, routed),
		Launch:        domain.LaunchConfig{BackendID: d.BackendID, Env: d.Env},
		Meta:          domain.ProfileMeta{CreatedAt: now, UpdatedAt: now},
	}
}

// ApplyTo overlays the draft onto an existing profile, preserving Meta/Launch
// fields the web form does not edit. Args are merged, not replaced: flags the
// editor surfaced as form fields (`surfaced`) are form-driven (submitted = set,
// absent = cleared), while configured args the editor never rendered are
// preserved verbatim — otherwise saving would silently drop any flag the
// schema's presentation omits (e.g. tool-calling flags).
func (d Draft) ApplyTo(existing domain.Profile, schema domain.FlagSchema, surfaced map[string]bool) domain.Profile {
	existing.ID = d.ID
	existing.Name = d.Name
	existing.Description = d.Description
	existing.Tags = d.Tags
	existing.Model = d.Model
	args, routed := mergeArgs(existing.Args, d.Args, schema, surfaced)
	existing.Args = args
	existing.ExtraArgs = mergeExtraArgs(d.ExtraArgs, routed)
	existing.Launch.BackendID = d.BackendID
	existing.Launch.Env = d.Env
	existing.Meta.UpdatedAt = time.Now().UTC()
	return existing
}

// mergeArgs overlays the form-submitted args onto the existing profile's args.
// Flags the editor surfaced as form fields are form-driven (the submission is
// authoritative: present = set, absent = the user cleared it). Configured args
// the editor never surfaced are preserved verbatim with their original type, so
// a save can never silently drop a flag the schema's presentation omits.
// Reserved (manager-owned) flags are always excluded.
func mergeArgs(existing map[string]any, form map[string]string, schema domain.FlagSchema, surfaced map[string]bool) (map[string]any, map[string][]string) {
	out := map[string]any{}
	for k, v := range existing {
		if reservedFlags[k] {
			continue
		}
		// Surfaced flags come from the form below (so an unticked/cleared one is
		// correctly removed); keys the form never rendered are preserved as-is.
		if surfaced[domain.CanonicalFlag(k)] {
			continue
		}
		out[k] = v
	}
	formArgs, routed := coerceArgs(form, schema)
	maps.Copy(out, formArgs)
	return out, routed
}

// coerceArgs converts form strings into typed Args. Flags whose value spans
// several argv tokens are returned separately, as ready-to-emit argv tokens for
// ExtraArgs: Args holds one token per key, so that is the only representation
// the backend parses correctly (BUGS.md S15).
func coerceArgs(in map[string]string, schema domain.FlagSchema) (map[string]any, map[string][]string) {
	out := map[string]any{}
	routed := map[string][]string{}
	for k, v := range in {
		if reservedFlags[k] {
			// Manager-owned launch parameter (e.g. port): never persisted to a
			// profile, even if a forged POST submits it.
			continue
		}
		spec, ok := schema.Lookup(domain.CanonicalFlag(k))
		if !ok {
			out[k] = v
			continue
		}
		if spec.Arity > 1 {
			// Route to ExtraArgs only when the field really holds that many
			// values; a wrong count stays in Args so the validator reports it
			// rather than the launch mis-parsing silently.
			if fields := strings.Fields(v); len(fields) == spec.Arity {
				routed[spec.Long] = fields
				continue
			}
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
	return out, routed
}

// mergeExtraArgs appends routed multi-token flags to the operator's raw extra
// args, dropping any earlier occurrence of the same flag so repeated saves
// cannot stack duplicates. Only as many bare values as the flag consumes are
// dropped with it, and never a dash-prefixed token: `-ngl 30` sitting after a
// re-routed `--lora-scaled f.gguf 0.5` belongs to another flag and must survive.
func mergeExtraArgs(raw []string, routed map[string][]string) []string {
	if len(routed) == 0 {
		return raw
	}
	out := make([]string, 0, len(raw)+len(routed)*3)
	for i := 0; i < len(raw); i++ {
		tok := raw[i]
		name, _, hasEq := strings.Cut(strings.TrimPrefix(tok, "--"), "=")
		vals, stale := routed[name]
		if !stale || !strings.HasPrefix(tok, "--") {
			out = append(out, tok)
			continue
		}
		if hasEq {
			continue
		}
		for range vals {
			if i+1 >= len(raw) || strings.HasPrefix(raw[i+1], "-") {
				break
			}
			i++
		}
	}
	// Sorted so a re-save of an unchanged form produces identical JSON.
	for _, long := range slices.Sorted(maps.Keys(routed)) {
		out = append(out, "--"+long)
		out = append(out, routed[long]...)
	}
	return out
}
