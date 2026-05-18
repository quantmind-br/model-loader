package profilestore

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"
	"time"

	"github.com/quantmind-br/model-loader/internal/domain"
)

func historyProfile() domain.Profile {
	now := time.Date(2026, 5, 17, 12, 0, 0, 0, time.UTC)
	lastUsed := now.Add(time.Hour)
	return domain.Profile{
		SchemaVersion: domain.SchemaVersion,
		ID:            "qwen",
		Name:          "Qwen",
		Description:   "previous snapshot",
		Tags:          []string{"chat", "cuda"},
		Model:         "/models/qwen.gguf",
		Args: map[string]any{
			"ctx-size": float64(8192),
			"ngl":      float64(99),
		},
		ExtraArgs: []string{"--verbose"},
		Launch: domain.LaunchConfig{
			DefaultBackground: true,
			LogFilePath:       "/tmp/qwen.log",
			BackendID:         "llama.cpp",
			Env: []domain.EnvVar{
				{Key: "CUDA_VISIBLE_DEVICES", Value: "0"},
			},
			RestartPolicy:  domain.RestartPolicyOnFailure,
			MaxRestarts:    3,
			BackoffSeconds: 5,
		},
		Meta: domain.ProfileMeta{
			CreatedAt:  now,
			UpdatedAt:  now,
			LastUsedAt: &lastUsed,
		},
		Pinned: true,
	}
}

func TestSavePrevious(t *testing.T) {
	dataDir := t.TempDir()
	p := historyProfile()

	if err := SavePrevious(dataDir, p); err != nil {
		t.Fatalf("SavePrevious: %v", err)
	}

	path := filepath.Join(dataDir, ".history", p.ID+".previous.json")
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("stat previous file: %v", err)
	}
}

func TestLoadPrevious(t *testing.T) {
	dataDir := t.TempDir()
	p := historyProfile()
	if err := SavePrevious(dataDir, p); err != nil {
		t.Fatalf("SavePrevious: %v", err)
	}

	got, ok, err := LoadPrevious(dataDir, p.ID)
	if err != nil {
		t.Fatalf("LoadPrevious: %v", err)
	}
	if !ok {
		t.Fatal("LoadPrevious ok = false, want true")
	}
	if !reflect.DeepEqual(got, p) {
		t.Fatalf("LoadPrevious profile = %#v, want %#v", got, p)
	}
}

func TestLoadPreviousMissing(t *testing.T) {
	got, ok, err := LoadPrevious(t.TempDir(), "missing")
	if err != nil {
		t.Fatalf("LoadPrevious: %v", err)
	}
	if ok {
		t.Fatal("LoadPrevious ok = true, want false")
	}
	if !reflect.DeepEqual(got, domain.Profile{}) {
		t.Fatalf("LoadPrevious profile = %#v, want zero", got)
	}
}

func TestDeletePrevious(t *testing.T) {
	dataDir := t.TempDir()
	p := historyProfile()
	if err := SavePrevious(dataDir, p); err != nil {
		t.Fatalf("SavePrevious: %v", err)
	}

	if err := DeletePrevious(dataDir, p.ID); err != nil {
		t.Fatalf("DeletePrevious: %v", err)
	}

	if _, ok, err := LoadPrevious(dataDir, p.ID); err != nil {
		t.Fatalf("LoadPrevious after delete: %v", err)
	} else if ok {
		t.Fatal("LoadPrevious after delete ok = true, want false")
	}
}

func TestDeletePreviousMissing(t *testing.T) {
	if err := DeletePrevious(t.TempDir(), "missing"); err != nil {
		t.Fatalf("DeletePrevious missing: %v", err)
	}
}
