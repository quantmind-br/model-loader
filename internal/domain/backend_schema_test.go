package domain

import (
	"encoding/json"
	"testing"
)

func TestPresentation_RoundTrip(t *testing.T) {
	in := Presentation{Groups: []PresentationGroup{
		{Name: "Essenciais", Highlighted: true, Flags: []string{"ctx-size", "port"}},
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
	if len(out.Groups) != 2 || out.Groups[0].Name != "Essenciais" || !out.Groups[0].Highlighted {
		t.Fatalf("groups not preserved: %+v", out)
	}
	if len(out.Groups[0].Flags) != 2 || out.Groups[0].Flags[1] != "port" {
		t.Fatalf("flag order not preserved: %+v", out.Groups[0])
	}
}
