package components

import (
	"strings"
	"testing"
	"time"
)

func TestInfoPanel_ZeroMtimeRendersEmDash(t *testing.T) {
	p := InfoPanel{
		Filename:   "foo.gguf",
		Path:       "/m/foo.gguf",
		SizeOnDisk: 1024,
	}
	out := p.Render(60)
	if !strings.Contains(out, "Modified: —") {
		t.Errorf("zero Mtime should render 'Modified: —'; got:\n%s", out)
	}
	if strings.Contains(out, "Modified: 0001-01-01") {
		t.Errorf("zero Mtime leaked RFC3339 zero value:\n%s", out)
	}
}

func TestInfoPanel_NonZeroMtimeRendersRFC3339(t *testing.T) {
	mtime, _ := time.Parse(time.RFC3339, "2025-04-30T10:15:00Z")
	p := InfoPanel{Filename: "foo.gguf", Mtime: mtime}
	out := p.Render(60)
	if !strings.Contains(out, "Modified: 2025-04-30T10:15:00Z") {
		t.Errorf("non-zero Mtime should render RFC3339; got:\n%s", out)
	}
}
