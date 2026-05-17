package backendcatalog

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/quantmind-br/model-loader/internal/domain"
)

func TestProber_Probe(t *testing.T) {
	dir := t.TempDir()
	store := NewFSStore(dir)

	path := filepath.Join(dir, "test-exe")
	if err := os.WriteFile(path, []byte("#!/bin/sh\necho version 1.0.0\n"), 0o755); err != nil {
		t.Fatal(err)
	}

	catalog := domain.BackendCatalog{
		SchemaVersion: catalogSchemaVersion,
		Backends: []domain.Backend{
			{ID: "test", Executable: path, Kind: domain.BackendKindLlamaServer},
		},
	}
	if err := store.Save(catalog); err != nil {
		t.Fatalf("Save: %v", err)
	}

	prober := NewProber(store, ProbeConfig{Timeout: 5 * time.Second})
	ch, err := prober.Probe(t.Context())
	if err != nil {
		t.Fatalf("Probe: %v", err)
	}

	var events []ProbeEvent
	for ev := range ch {
		events = append(events, ev)
	}

	if len(events) == 0 {
		t.Fatal("expected at least one event")
	}
	last := events[len(events)-1]
	if !last.Done {
		t.Fatal("expected final done event")
	}

	for _, ev := range events[:len(events)-1] {
		if ev.BackendID == "" {
			t.Fatal("expected BackendID in event")
		}
		if ev.Status != ProbeStatusOK {
			t.Fatalf("expected OK for test-exe, got %s (%s)", ev.Status, ev.Detail)
		}
	}
}

func TestProber_ProbeMissingExecutable(t *testing.T) {
	dir := t.TempDir()
	store := NewFSStore(dir)

	catalog := domain.BackendCatalog{
		SchemaVersion: catalogSchemaVersion,
		Backends: []domain.Backend{
			{ID: "missing", Executable: "this-binary-does-not-exist-12345", Kind: domain.BackendKindLlamaServer},
		},
	}
	if err := store.Save(catalog); err != nil {
		t.Fatalf("Save: %v", err)
	}

	prober := NewProber(store, ProbeConfig{Timeout: 5 * time.Second})
	ch, err := prober.Probe(t.Context())
	if err != nil {
		t.Fatalf("Probe: %v", err)
	}

	var found bool
	for ev := range ch {
		if ev.Done {
			continue
		}
		found = true
		if ev.Status != ProbeStatusErr {
			t.Fatalf("expected ERR for missing binary, got %s", ev.Status)
		}
		if !strings.Contains(ev.Detail, "not found") {
			t.Fatalf("expected 'not found' in detail, got %q", ev.Detail)
		}
	}
	if !found {
		t.Fatal("expected at least one non-done event")
	}
}

func TestProber_ProbeTimeout(t *testing.T) {
	dir := t.TempDir()
	store := NewFSStore(dir)

	path := filepath.Join(dir, "slow-exe")
	if err := os.WriteFile(path, []byte("#!/bin/sh\nsleep 10\n"), 0o755); err != nil {
		t.Fatal(err)
	}

	catalog := domain.BackendCatalog{
		SchemaVersion: catalogSchemaVersion,
		Backends: []domain.Backend{
			{ID: "slow", Executable: path, Kind: domain.BackendKindLlamaServer},
		},
	}
	if err := store.Save(catalog); err != nil {
		t.Fatalf("Save: %v", err)
	}

	prober := NewProber(store, ProbeConfig{Timeout: 1 * time.Millisecond})
	ch, err := prober.Probe(t.Context())
	if err != nil {
		t.Fatalf("Probe: %v", err)
	}

	var found bool
	for ev := range ch {
		if ev.Done {
			continue
		}
		if ev.BackendID == "" {
			continue
		}
		found = true
		if ev.Status != ProbeStatusErr {
			t.Fatalf("expected ERR for timeout, got %s", ev.Status)
		}
		if !strings.Contains(ev.Detail, "timeout") {
			t.Fatalf("expected 'timeout' in detail, got %q", ev.Detail)
		}
	}
	if !found {
		t.Fatal("expected at least one non-done event")
	}
}

func TestProber_ProbeLoadError(t *testing.T) {
	store := &brokenStore{}
	prober := NewProber(store, ProbeConfig{Timeout: 5 * time.Second})
	_, err := prober.Probe(t.Context())
	if err == nil {
		t.Fatal("expected error for broken store")
	}
}

type brokenStore struct{}

func (b *brokenStore) Load() (domain.BackendCatalog, error) {
	return domain.BackendCatalog{}, exec.ErrNotFound
}
func (b *brokenStore) Save(domain.BackendCatalog) error   { return nil }
func (b *brokenStore) Delete(string) error                { return nil }
func (b *brokenStore) SetDefault(string) error              { return nil }
func (b *brokenStore) DefaultBackendID() (string, error)  { return "", nil }
func (b *brokenStore) Backends() ([]domain.Backend, error) { return nil, nil }

