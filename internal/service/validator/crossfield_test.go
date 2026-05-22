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

func TestCrossField_LimitWithinBoundPasses(t *testing.T) {
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
	p := domain.Profile{Args: map[string]any{"flash-attn": "on", "ctx-size": 16384}}
	rep := New(nil).Validate(p, ruleSchema(rule, flags), domain.BackendKindLlamaServer)
	if len(rep.Warnings) != 0 || rep.HasBlockingErrors() {
		t.Fatalf("compliant value should produce no issues: %+v", rep)
	}
}

func TestCrossField_MessageFires(t *testing.T) {
	rule := domain.CrossFieldRule{
		ID:       "r1",
		When:     domain.Cond{Flag: "flash-attn", Op: "eq", Value: "on"},
		Then:     domain.Effect{Kind: "message", Message: "heads up"},
		Severity: "warning",
	}
	flags := map[string]domain.FlagSpec{"flash-attn": {Long: "flash-attn", Type: domain.FlagTypeString}}
	p := domain.Profile{Args: map[string]any{"flash-attn": "on"}}
	rep := New(nil).Validate(p, ruleSchema(rule, flags), domain.BackendKindLlamaServer)
	if len(rep.Warnings) != 1 || rep.Warnings[0].Message != "heads up" {
		t.Fatalf("expected one warning 'heads up', got %+v", rep)
	}
}
