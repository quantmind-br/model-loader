package domain

import "testing"

func TestBuildFlagSchema(t *testing.T) {
	rows := []FlagSpecRow{
		{
			Long:       "model",
			Short:      "m",
			Aliases:    []string{"model-path"},
			Type:       FlagTypeString,
			EnumValues: []string{"a", "b"},
			Default:    "default-model",
			HelpText:   "model path",
			Group:      "test",
		},
	}
	schema := BuildFlagSchema("v1", rows)
	if schema.Version != "v1" {
		t.Errorf("Version = %q, want %q", schema.Version, "v1")
	}
	spec, ok := schema.Flags["model"]
	if !ok {
		t.Fatal("expected flag 'model' in Flags map")
	}
	if spec.Long != "model" {
		t.Errorf("Long = %q, want %q", spec.Long, "model")
	}
	if spec.Short != "m" {
		t.Errorf("Short = %q, want %q", spec.Short, "m")
	}
	if len(spec.Aliases) != 1 || spec.Aliases[0] != "model-path" {
		t.Errorf("Aliases = %v, want [model-path]", spec.Aliases)
	}
	if spec.Type != FlagTypeString {
		t.Errorf("Type = %v, want FlagTypeString", spec.Type)
	}
	if len(spec.EnumValues) != 2 || spec.EnumValues[0] != "a" {
		t.Errorf("EnumValues = %v, want [a b]", spec.EnumValues)
	}
	if spec.Default != "default-model" {
		t.Errorf("Default = %v, want default-model", spec.Default)
	}
	if spec.HelpText != "model path" {
		t.Errorf("HelpText = %q, want %q", spec.HelpText, "model path")
	}
	if spec.Group != "test" {
		t.Errorf("Group = %q, want %q", spec.Group, "test")
	}
}
