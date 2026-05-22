package domain

import (
	"encoding/json"
	"testing"
)

func TestBackendValidationSchema_EnvelopeRoundTrip(t *testing.T) {
	in := BackendValidationSchema{
		SchemaVersion: 1,
		Kind:          ValidationSchemaCLIFlagsV1,
		BackendKind:   BackendKindLlamaServer,
		BackendID:     "llama",
		Flags:         map[string]FlagSpec{"ctx-size": {Long: "ctx-size", Type: FlagTypeInt}},
		Presentation:  &Presentation{Groups: []PresentationGroup{{Name: "Essentials", Highlighted: true, Flags: []string{"ctx-size"}}}},
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

func TestPresentation_RoundTrip(t *testing.T) {
	in := Presentation{Groups: []PresentationGroup{
		{Name: "Essentials", Highlighted: true, Flags: []string{"ctx-size", "port"}},
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
	if len(out.Groups) != 2 || out.Groups[0].Name != "Essentials" || !out.Groups[0].Highlighted {
		t.Fatalf("groups not preserved: %+v", out)
	}
	if len(out.Groups[0].Flags) != 2 || out.Groups[0].Flags[1] != "port" {
		t.Fatalf("flag order not preserved: %+v", out.Groups[0])
	}
}
