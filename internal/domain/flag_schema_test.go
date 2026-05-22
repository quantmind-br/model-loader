package domain

import (
	"encoding/json"
	"testing"
)

func TestFlagSpec_RequiredRoundTrip(t *testing.T) {
	in := FlagSpec{Long: "model", Type: FlagTypeString, Required: true}
	b, err := json.Marshal(in)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var out FlagSpec
	if err := json.Unmarshal(b, &out); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if !out.Required {
		t.Fatalf("Required not preserved: %+v", out)
	}
}

func TestFlagSpec_RequiredOmittedWhenFalse(t *testing.T) {
	b, _ := json.Marshal(FlagSpec{Long: "x", Type: FlagTypeString})
	if string(b) == "" || flagSpecContains(string(b), "required") {
		t.Fatalf("required should be omitted when false, got %s", b)
	}
}

func flagSpecContains(s, sub string) bool {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}
