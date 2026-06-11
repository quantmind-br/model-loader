package cli

import (
	"bytes"
	"encoding/json"
	"io"
	"strings"
	"testing"
	"time"

	"github.com/quantmind-br/model-loader/internal/domain"
	"github.com/quantmind-br/model-loader/internal/service/processmgr"
)

// fakeManager implements processmgr.Manager for CLI tests.
type fakeManager struct {
	running   []domain.RunningInstance
	exited    []domain.ExitedInstance
	killed    []int
	launched  []domain.Profile
	launchErr error
	killErr   error
	tail      string
}

func (f *fakeManager) Launch(p domain.Profile, mode processmgr.LaunchMode, attemptID string) (domain.RunningInstance, error) {
	if f.launchErr != nil {
		return domain.RunningInstance{}, f.launchErr
	}
	f.launched = append(f.launched, p)
	ri := domain.RunningInstance{ProfileID: p.ID, PID: 4242, Port: 8080}
	f.running = append(f.running, ri)
	return ri, nil
}
func (f *fakeManager) Kill(pid int) error {
	if f.killErr != nil {
		return f.killErr
	}
	f.killed = append(f.killed, pid)
	return nil
}
func (f *fakeManager) List() []domain.RunningInstance { return f.running }
func (f *fakeManager) WaitHealthy(pid, port int, timeout time.Duration, attemptID string) error {
	return nil
}
func (f *fakeManager) TailLogs(pid int) (io.ReadCloser, error) {
	return io.NopCloser(strings.NewReader(f.tail)), nil
}
func (f *fakeManager) Close() error { return nil }
func (f *fakeManager) GetExitInfo(pid int) (processmgr.ExitInfo, bool) {
	return processmgr.ExitInfo{}, false
}
func (f *fakeManager) History() []domain.ExitedInstance { return f.exited }
func (f *fakeManager) RefreshFromDisk() error           { return nil }

func TestListInstances_TableAndJSON(t *testing.T) {
	m := &fakeManager{running: []domain.RunningInstance{
		{ProfileID: "alpha", PID: 100, Port: 8080, StartedAt: time.Now()},
	}}
	var buf bytes.Buffer
	if err := listInstances(&buf, m, false); err != nil {
		t.Fatalf("list: %v", err)
	}
	if !strings.Contains(buf.String(), "alpha") || !strings.Contains(buf.String(), "100") {
		t.Fatalf("table missing data: %q", buf.String())
	}

	buf.Reset()
	if err := listInstances(&buf, m, true); err != nil {
		t.Fatalf("list json: %v", err)
	}
	var got []domain.RunningInstance
	if err := json.Unmarshal(buf.Bytes(), &got); err != nil {
		t.Fatalf("not JSON: %v", err)
	}
	if len(got) != 1 || got[0].PID != 100 {
		t.Fatalf("json wrong: %+v", got)
	}
}

func TestResolveInstance_ByPIDAndProfile(t *testing.T) {
	m := &fakeManager{running: []domain.RunningInstance{
		{ProfileID: "alpha", PID: 100},
		{ProfileID: "beta", PID: 200},
	}}
	if ri, err := resolveInstance(m, "200"); err != nil || ri.ProfileID != "beta" {
		t.Fatalf("by pid: %+v %v", ri, err)
	}
	if ri, err := resolveInstance(m, "alpha"); err != nil || ri.PID != 100 {
		t.Fatalf("by profile id: %+v %v", ri, err)
	}
	_, err := resolveInstance(m, "nope")
	if err == nil {
		t.Fatalf("expected not found")
	}
	if !strings.Contains(err.Error(), "use a pid or profile id") {
		t.Fatalf("not-found error should hint about pid or profile id, got: %v", err)
	}
}

func TestResolveInstance_AmbiguousPrefix(t *testing.T) {
	m := &fakeManager{running: []domain.RunningInstance{
		{ProfileID: "alpha-one", PID: 101},
		{ProfileID: "alpha-two", PID: 102},
	}}
	_, err := resolveInstance(m, "alpha")
	if err == nil {
		t.Fatalf("expected ambiguous error")
	}
	if !strings.Contains(err.Error(), "101") || !strings.Contains(err.Error(), "102") {
		t.Fatalf("ambiguous error should list candidate PIDs, got: %v", err)
	}
	if !strings.Contains(err.Error(), "use the pid") {
		t.Fatalf("ambiguous error should hint about pid, got: %v", err)
	}
}

func TestHistory_JSON(t *testing.T) {
	m := &fakeManager{exited: []domain.ExitedInstance{{ProfileID: "alpha", PID: 100, DurationSeconds: 42}}}
	var buf bytes.Buffer
	if err := listHistory(&buf, m, true); err != nil {
		t.Fatalf("history: %v", err)
	}
	if !strings.Contains(buf.String(), "alpha") {
		t.Fatalf("history json missing: %q", buf.String())
	}
}
