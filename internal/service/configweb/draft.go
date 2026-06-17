// Package configweb runs the on-demand web GUI used by the TUI to edit profiles.
package configweb

import (
	"maps"
	"strconv"
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
	return domain.Profile{
		SchemaVersion: domain.SchemaVersion,
		ID:            d.ID,
		Name:          d.Name,
		Description:   d.Description,
		Tags:          d.Tags,
		Model:         d.Model,
		Args:          coerceArgs(d.Args, schema),
		ExtraArgs:     d.ExtraArgs,
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
	existing.Args = mergeArgs(existing.Args, d.Args, schema, surfaced)
	existing.ExtraArgs = d.ExtraArgs
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
func mergeArgs(existing map[string]any, form map[string]string, schema domain.FlagSchema, surfaced map[string]bool) map[string]any {
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
	maps.Copy(out, coerceArgs(form, schema))
	return out
}

func coerceArgs(in map[string]string, schema domain.FlagSchema) map[string]any {
	out := map[string]any{}
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
