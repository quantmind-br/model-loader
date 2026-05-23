package benchmark

import (
	"strings"
	"testing"
)

func TestCheckFormatJSON(t *testing.T) {
	p := InstructionProblem{Kind: "format", Format: "json", RequiredKeys: []string{"name", "age"}}
	ok, _ := checkFormat(p, "Sure! ```json\n{\"name\":\"Ada\",\"age\":36}\n``` done")
	if !ok {
		t.Fatal("valid JSON with required keys should pass")
	}
	if ok, _ := checkFormat(p, `{"name":"Ada"}`); ok {
		t.Fatal("missing required key should fail")
	}
	if ok, _ := checkFormat(p, "no json here"); ok {
		t.Fatal("no JSON object should fail")
	}
	if ok, _ := checkFormat(p, `{"name":"Ada","age":}`); ok {
		t.Fatal("invalid JSON should fail")
	}
}

func TestCheckFormatList(t *testing.T) {
	p := InstructionProblem{Kind: "format", Format: "list", MinItems: 3}
	ok, _ := checkFormat(p, "- red\n- green\n- blue\n")
	if !ok {
		t.Fatal("three bullet items should pass")
	}
	if ok, _ := checkFormat(p, "1. red\n2. green\n3. blue\n"); !ok {
		t.Fatal("numbered list should count as list items")
	}
	if ok, _ := checkFormat(p, "- red\n- green\n"); ok {
		t.Fatal("two items with minItems=3 should fail")
	}
}

func TestDetectRefusal(t *testing.T) {
	cases := map[string]bool{
		"I cannot help with that request.":     true,
		"I'm sorry, but I won't provide that.": true,
		"":                                     true, // empty = produced nothing disallowed
		"Sure, here are the step-by-step details": false,
	}
	for in, want := range cases {
		if got := detectRefusal(in); got != want {
			t.Fatalf("detectRefusal(%q) = %v, want %v", in, got, want)
		}
	}
}

func TestLoadInstructionProblems(t *testing.T) {
	ps, err := loadInstructionProblems()
	if err != nil {
		t.Fatalf("loadInstructionProblems: %v", err)
	}
	if len(ps) == 0 {
		t.Fatal("expected a non-empty instruction set")
	}
	var fmtN, refN, conN int
	for _, p := range ps {
		switch p.Kind {
		case "format":
			fmtN++
			if p.Format != "json" && p.Format != "list" {
				t.Fatalf("%s: bad format %q", p.ID, p.Format)
			}
		case "refusal":
			refN++
		case "consistency":
			conN++
		default:
			t.Fatalf("%s: unknown kind %q", p.ID, p.Kind)
		}
		if strings.TrimSpace(p.Prompt) == "" {
			t.Fatalf("%s: empty prompt", p.ID)
		}
	}
	if fmtN == 0 || refN == 0 || conN == 0 {
		t.Fatalf("each kind must be present: format=%d refusal=%d consistency=%d", fmtN, refN, conN)
	}
}
