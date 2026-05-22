package cli

import (
	"strings"
	"testing"

	"github.com/quantmind-br/model-loader/internal/domain"
	"github.com/quantmind-br/model-loader/internal/service/validator"
)

func testSchema() domain.FlagSchema {
	return domain.FlagSchema{
		Flags: map[string]domain.FlagSpec{
			"port":       {Long: "port", Type: domain.FlagTypeInt},
			"temp":       {Long: "temp", Type: domain.FlagTypeFloat},
			"flash-attn": {Long: "flash-attn", Type: domain.FlagTypeBool},
			"alias":      {Long: "alias", Type: domain.FlagTypeString},
		},
	}
}

func TestCoerceArgs_TypesBySchema(t *testing.T) {
	got := coerceArgs(map[string]string{
		"port":       "8080",
		"temp":       "0.7",
		"flash-attn": "true",
		"alias":      "my-model",
		"unknown":    "raw",
	}, testSchema())

	if got["port"] != 8080 {
		t.Errorf("port = %v (%T), want int 8080", got["port"], got["port"])
	}
	if got["temp"] != 0.7 {
		t.Errorf("temp = %v, want 0.7", got["temp"])
	}
	if got["flash-attn"] != true {
		t.Errorf("flash-attn = %v, want true", got["flash-attn"])
	}
	if got["alias"] != "my-model" {
		t.Errorf("alias = %v", got["alias"])
	}
	if got["unknown"] != "raw" {
		t.Errorf("unknown flag should stay string: %v", got["unknown"])
	}
}

func TestAssembleProfile_FlagsOverrideBase(t *testing.T) {
	base := domain.Profile{
		ID:    "p1",
		Name:  "Old",
		Model: "old.gguf",
		Args:  map[string]any{"port": 1, "ctx-size": 2048},
		Launch: domain.LaunchConfig{BackendID: "llama"},
	}
	in := profileInput{
		name:      "New",
		model:     "new.gguf",
		backend:   "vllm",
		args:      map[string]string{"port": "8080"},
		extraArgs: []string{"--verbose"},
		env:       []domain.EnvVar{{Key: "CUDA_VISIBLE_DEVICES", Value: "0"}},
		setName:   true, setModel: true, setBackend: true,
	}
	out := assembleProfile(base, in, testSchema())

	if out.ID != "p1" {
		t.Errorf("id must be preserved: %s", out.ID)
	}
	if out.Name != "New" || out.Model != "new.gguf" || out.Launch.BackendID != "vllm" {
		t.Errorf("scalar overrides failed: %+v", out)
	}
	if out.Args["port"] != 8080 {
		t.Errorf("arg override/coerce failed: %v", out.Args["port"])
	}
	if out.Args["ctx-size"] != 2048 {
		t.Errorf("untouched base arg lost: %v", out.Args["ctx-size"])
	}
	if len(out.ExtraArgs) != 1 || out.ExtraArgs[0] != "--verbose" {
		t.Errorf("extra args: %+v", out.ExtraArgs)
	}
	if len(out.Launch.Env) != 1 || out.Launch.Env[0].Key != "CUDA_VISIBLE_DEVICES" {
		t.Errorf("env: %+v", out.Launch.Env)
	}
}

func TestParseKVPairs(t *testing.T) {
	m, err := parseKVPairs([]string{"a=1", "b=two=2"})
	if err != nil {
		t.Fatalf("parseKVPairs: %v", err)
	}
	if m["a"] != "1" || m["b"] != "two=2" {
		t.Fatalf("bad parse: %+v", m)
	}
	if _, err := parseKVPairs([]string{"nokey"}); err == nil {
		t.Fatalf("expected error for missing '='")
	}
}

func TestRunProfileWrite_CreateAndEdit(t *testing.T) {
	s := newTempStore(t)
	dir := s.Dir()
	var out, errw strings.Builder

	// create (nil validator — skipped by nil guard)
	code := runProfileWrite(&out, &errw, profileWriteDeps{store: s, dir: dir}, false, "",
		"", profileInput{id: "p1", name: "P1", model: "m.gguf", setName: true, setModel: true})
	if code != 0 {
		t.Fatalf("create code=%d err=%q", code, errw.String())
	}
	got, err := s.Get("p1")
	if err != nil || got.Name != "P1" {
		t.Fatalf("created profile missing/wrong: %+v %v", got, err)
	}

	// edit (nil resolver → schema empty, noopValidator)
	out.Reset()
	errw.Reset()
	code = runProfileWrite(&out, &errw, profileWriteDeps{store: s, val: noopValidator{}, dir: dir}, true, "p1",
		"", profileInput{name: "P1-renamed", setName: true})
	if code != 0 {
		t.Fatalf("edit code=%d err=%q", code, errw.String())
	}
	got, _ = s.Get("p1")
	if got.Name != "P1-renamed" {
		t.Fatalf("edit did not apply: %+v", got)
	}
}

type noopValidator struct{}

func (noopValidator) Validate(domain.Profile, domain.FlagSchema, domain.BackendKind) validator.Report {
	return validator.Report{}
}
