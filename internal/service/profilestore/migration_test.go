package profilestore

import (
	"testing"

	"github.com/quantmind-br/model-loader/internal/domain"
)

func TestMigrateProfile_v1ToCurrent(t *testing.T) {
	p := domain.Profile{SchemaVersion: 1}
	MigrateProfile(&p)
	if p.SchemaVersion != domain.SchemaVersion {
		t.Fatalf("expected schemaVersion %d, got %d", domain.SchemaVersion, p.SchemaVersion)
	}
	if p.Pinned {
		t.Fatal("expected Pinned to default to false")
	}
	if p.Launch.RestartPolicy != domain.RestartPolicyNone {
		t.Fatalf("expected RestartPolicy %q, got %q", domain.RestartPolicyNone, p.Launch.RestartPolicy)
	}
	if p.Launch.MaxRestarts != 3 {
		t.Fatalf("expected MaxRestarts 3, got %d", p.Launch.MaxRestarts)
	}
	if p.Launch.BackoffSeconds != 5 {
		t.Fatalf("expected BackoffSeconds 5, got %d", p.Launch.BackoffSeconds)
	}
}

func TestMigrateProfile_v2ToCurrent(t *testing.T) {
	p := domain.Profile{SchemaVersion: 2, Pinned: true}
	MigrateProfile(&p)
	if p.SchemaVersion != domain.SchemaVersion {
		t.Fatalf("expected schemaVersion %d, got %d", domain.SchemaVersion, p.SchemaVersion)
	}
	if !p.Pinned {
		t.Fatal("expected existing Pinned value to be preserved")
	}
	if p.Launch.RestartPolicy != domain.RestartPolicyNone {
		t.Fatalf("expected RestartPolicy %q, got %q", domain.RestartPolicyNone, p.Launch.RestartPolicy)
	}
	if p.Launch.MaxRestarts != 3 {
		t.Fatalf("expected MaxRestarts 3, got %d", p.Launch.MaxRestarts)
	}
	if p.Launch.BackoffSeconds != 5 {
		t.Fatalf("expected BackoffSeconds 5, got %d", p.Launch.BackoffSeconds)
	}
}

func TestMigrateProfile_v3Unchanged(t *testing.T) {
	p := domain.Profile{
		SchemaVersion: 3,
		Pinned:        true,
		Launch: domain.LaunchConfig{
			RestartPolicy:  domain.RestartPolicyAlways,
			MaxRestarts:    10,
			BackoffSeconds: 30,
		},
	}
	MigrateProfile(&p)
	if p.SchemaVersion != 3 {
		t.Fatalf("expected schemaVersion 3, got %d", p.SchemaVersion)
	}
	if !p.Pinned {
		t.Fatal("expected existing Pinned value to be preserved")
	}
	if p.Launch.RestartPolicy != domain.RestartPolicyAlways {
		t.Fatalf("expected RestartPolicy %q, got %q", domain.RestartPolicyAlways, p.Launch.RestartPolicy)
	}
	if p.Launch.MaxRestarts != 10 {
		t.Fatalf("expected MaxRestarts 10, got %d", p.Launch.MaxRestarts)
	}
	if p.Launch.BackoffSeconds != 30 {
		t.Fatalf("expected BackoffSeconds 30, got %d", p.Launch.BackoffSeconds)
	}
}
