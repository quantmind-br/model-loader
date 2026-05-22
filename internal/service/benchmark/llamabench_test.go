package benchmark

import "testing"

func TestParsePresets_DefaultWhenEmpty(t *testing.T) {
	got, err := parsePresets(nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(got) != len(defaultPresets) {
		t.Fatalf("want %d default presets, got %d", len(defaultPresets), len(got))
	}
	if got[0] != (tpPreset{PromptTokens: 128, GenTokens: 512}) {
		t.Errorf("first default preset = %+v", got[0])
	}
}

func TestParsePresets_Valid(t *testing.T) {
	got, err := parsePresets([]string{"512/128", " 4096 / 256 "})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	want := []tpPreset{{512, 128}, {4096, 256}}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("preset %d = %+v, want %+v", i, got[i], want[i])
		}
	}
}

func TestParsePresets_Invalid(t *testing.T) {
	for _, bad := range []string{"512", "x/128", "512/0", "0/128", "512/y", "512abc/128", "512/128/2", "512/128abc"} {
		if _, err := parsePresets([]string{bad}); err == nil {
			t.Errorf("expected error for %q, got nil", bad)
		}
	}
}

func TestDefaultPresets_Expanded(t *testing.T) {
	got, err := parsePresets(nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	want := []tpPreset{{128, 512}, {512, 128}, {2048, 256}, {4096, 256}, {8192, 128}, {16384, 64}}
	if len(got) != len(want) {
		t.Fatalf("got %d default presets, want %d", len(got), len(want))
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("preset %d = %+v, want %+v", i, got[i], want[i])
		}
	}
}

func TestCountForMode_LlamaBench(t *testing.T) {
	r := &Runner{presets: []tpPreset{{512, 128}, {4096, 256}}, problems: make([]Problem, 5)}
	if got := r.CountForMode(ModeLlamaBench); got != 2 {
		t.Errorf("CountForMode(llama-bench) = %d, want 2", got)
	}
	if got := r.CountForMode(ModeLongContext); got != 1 {
		t.Errorf("CountForMode(longctx) = %d, want 1", got)
	}
}
