package cli

import (
	"strings"
	"testing"

	"github.com/quantmind-br/model-loader/internal/domain"
)

func TestStartInstance_LaunchesResolvedProfile(t *testing.T) {
	store := newTempStore(t)
	seed(t, store, "alpha", "Alpha")
	m := &fakeManager{}
	var out strings.Builder
	if err := startInstance(&out, m, store, "alpha", false); err != nil {
		t.Fatalf("start: %v", err)
	}
	if len(m.launched) != 1 || m.launched[0].ID != "alpha" {
		t.Fatalf("did not launch alpha: %+v", m.launched)
	}
	if !strings.Contains(out.String(), "4242") {
		t.Fatalf("expected pid in output: %q", out.String())
	}
}

func TestStopInstance_KillsResolvedPID(t *testing.T) {
	m := &fakeManager{running: []domain.RunningInstance{{ProfileID: "alpha", PID: 100}}}
	var out strings.Builder
	if err := stopInstance(&out, m, "100"); err != nil {
		t.Fatalf("stop: %v", err)
	}
	if len(m.killed) != 1 || m.killed[0] != 100 {
		t.Fatalf("did not kill 100: %+v", m.killed)
	}
}

func TestRestartInstance_KillsThenLaunches(t *testing.T) {
	store := newTempStore(t)
	seed(t, store, "alpha", "Alpha")
	m := &fakeManager{running: []domain.RunningInstance{{ProfileID: "alpha", PID: 100, Background: true}}}
	var out strings.Builder
	if err := restartInstance(&out, m, store, "100"); err != nil {
		t.Fatalf("restart: %v", err)
	}
	if len(m.killed) != 1 || m.killed[0] != 100 {
		t.Fatalf("did not kill old pid: %+v", m.killed)
	}
	if len(m.launched) != 1 || m.launched[0].ID != "alpha" {
		t.Fatalf("did not relaunch: %+v", m.launched)
	}
}
