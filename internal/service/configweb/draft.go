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
