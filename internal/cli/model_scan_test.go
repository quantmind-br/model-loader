package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/quantmind-br/model-loader/internal/domain"
)

// fakeScanner emits a fixed event sequence on Scan.
type fakeScanner struct {
	events  []domain.ScanEvent
	scanErr error
}

func (f *fakeScanner) Scan(ctx context.Context, paths []string) (<-chan domain.ScanEvent, error) {
	if f.scanErr != nil {
		return nil, f.scanErr
	}
	ch := make(chan domain.ScanEvent, len(f.events))
	for _, e := range f.events {
		ch <- e
	}
	close(ch)
	return ch, nil
}

func twoModelScanner() *fakeScanner {
	return &fakeScanner{events: []domain.ScanEvent{
		{Type: domain.ScanEventFile, File: &domain.ModelFile{Path: "/m/b.gguf", Name: "b.gguf", SizeBytes: 2048, Quant: "Q4_K_M", Params: "7B", Architecture: "llama"}},
		{Type: domain.ScanEventFile, File: &domain.ModelFile{Path: "/m/a.gguf", Name: "a.gguf", SizeBytes: 1024, Quant: "Q8_0", Params: "3B", Architecture: "qwen"}},
		{Type: domain.ScanEventDone},
	}}
}

func TestListLocalModels_Table(t *testing.T) {
	var out, errw bytes.Buffer
	if err := listLocalModels(&out, &errw, twoModelScanner(), []string{"/m"}, false); err != nil {
		t.Fatalf("listLocalModels: %v", err)
	}
	s := out.String()
	if !strings.Contains(s, "a.gguf") || !strings.Contains(s, "b.gguf") {
		t.Fatalf("table missing models: %q", s)
	}
	if strings.Index(s, "a.gguf") > strings.Index(s, "b.gguf") {
		t.Fatalf("models not sorted by path: %q", s)
	}
}

func TestListLocalModels_JSON(t *testing.T) {
	var out, errw bytes.Buffer
	if err := listLocalModels(&out, &errw, twoModelScanner(), []string{"/m"}, true); err != nil {
		t.Fatalf("listLocalModels: %v", err)
	}
	var items []modelListItem
	if err := json.Unmarshal(out.Bytes(), &items); err != nil {
		t.Fatalf("unmarshal: %v\n%s", err, out.String())
	}
	if len(items) != 2 || items[0].Name != "a.gguf" {
		t.Fatalf("unexpected items: %+v", items)
	}
	if items[0].SizeBytes != 1024 || items[1].SizeBytes != 2048 {
		t.Fatalf("items sorted but SizeBytes wrong: %+v", items)
	}
	if items[1].Name != "b.gguf" {
		t.Fatalf("second item should be b.gguf: %+v", items)
	}
}

func TestListLocalModels_NoPaths(t *testing.T) {
	var out, errw bytes.Buffer
	err := listLocalModels(&out, &errw, twoModelScanner(), nil, false)
	if err == nil {
		t.Fatal("expected error when no search paths configured")
	}
	if !strings.Contains(err.Error(), "search_paths") {
		t.Fatalf("error should mention search_paths config key, got: %v", err)
	}
}

func TestListLocalModels_ScanErrorEvent(t *testing.T) {
	sc := &fakeScanner{events: []domain.ScanEvent{
		{Type: domain.ScanEventError, Root: "/bad", Error: errTestScan},
		{Type: domain.ScanEventDone},
	}}
	var out, errw bytes.Buffer
	if err := listLocalModels(&out, &errw, sc, []string{"/bad"}, false); err != nil {
		t.Fatalf("scan error events must not fail the command: %v", err)
	}
	if !strings.Contains(errw.String(), "/bad") {
		t.Fatalf("expected per-root warning on stderr: %q", errw.String())
	}
}

func TestListLocalModels_ScanError(t *testing.T) {
	sc := &fakeScanner{scanErr: errors.New("backend failure")}
	var out, errw bytes.Buffer
	err := listLocalModels(&out, &errw, sc, []string{"/x"}, false)
	if err == nil || !strings.Contains(err.Error(), "scan:") {
		t.Fatalf("expected wrapped scan error, got: %v", err)
	}
}

var errTestScan = errors.New("permission denied")
