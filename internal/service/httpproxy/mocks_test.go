package httpproxy

import (
	"errors"
	"io"
	"os"
	"sync"
	"time"

	"github.com/quantmind-br/model-loader/internal/domain"
	"github.com/quantmind-br/model-loader/internal/service/processmgr"
	"github.com/quantmind-br/model-loader/internal/service/profilestore"
)

// stubStore is a minimal in-memory profilestore.Store for tests.
type stubStore struct {
	mu       sync.Mutex
	profiles map[string]domain.Profile
}

func newStubStore(profiles ...domain.Profile) *stubStore {
	s := &stubStore{profiles: map[string]domain.Profile{}}
	for _, p := range profiles {
		s.profiles[p.ID] = p
	}
	return s
}

func (s *stubStore) List() ([]domain.Profile, error) {
	ps, _, err := s.ListWithDiagnostics()
	return ps, err
}

func (s *stubStore) ListWithDiagnostics() ([]domain.Profile, []profilestore.ListDiagnostic, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]domain.Profile, 0, len(s.profiles))
	for _, p := range s.profiles {
		out = append(out, p)
	}
	return out, nil, nil
}

func (s *stubStore) Get(id string) (domain.Profile, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if id == "" {
		return domain.Profile{}, profilestore.ErrInvalidID
	}
	p, ok := s.profiles[id]
	if !ok {
		return domain.Profile{}, profilestore.ErrNotFound
	}
	return p, nil
}

func (s *stubStore) Create(p domain.Profile) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.profiles[p.ID]; ok {
		return profilestore.ErrDuplicateID
	}
	s.profiles[p.ID] = p
	return nil
}

func (s *stubStore) Save(p domain.Profile) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.profiles[p.ID] = p
	return nil
}

func (s *stubStore) Delete(id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.profiles, id)
	return nil
}

func (s *stubStore) Duplicate(srcID, newID string) (domain.Profile, error) {
	return domain.Profile{}, errors.New("not implemented in stub")
}

func (s *stubStore) Rename(oldID string, p domain.Profile) error {
	return errors.New("not implemented in stub")
}

// stubManager is a minimal processmgr.Manager that hands out deterministic
// PIDs and records every call. Backends never actually run; tests configure
// healthFn to choose whether WaitHealthy returns nil or an error.
type stubManager struct {
	mu       sync.Mutex
	nextPID  int
	tracked  map[int]domain.RunningInstance
	launches []domain.Profile
	kills    []int
	healthFn func(pid, port int) error
	killFn   func(pid int) error // optional: when set, Kill records the attempt then returns this error without untracking (mirrors the real manager keeping the entry on a failed kill)

	swapDelay  time.Duration // optional: delay inside Launch to widen swap race
	readyToken string        // token WaitReady returns when healthFn passes
}

func newStubManager() *stubManager {
	return &stubManager{
		nextPID:  1000,
		tracked:  map[int]domain.RunningInstance{},
		healthFn: func(int, int) error { return nil },
	}
}

func (m *stubManager) Launch(p domain.Profile, mode processmgr.LaunchMode, attemptID string) (domain.RunningInstance, error) {
	if m.swapDelay > 0 {
		time.Sleep(m.swapDelay)
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	m.nextPID++
	port, _ := portFromArgs(p.Args)
	inst := domain.RunningInstance{
		ProfileID: p.ID,
		// Use the always-live test process PID so the proxy's liveness guard
		// (procutil.SameProcess, audit A6) treats a stubbed backend as alive.
		// StartTicks stays 0 → SameProcess degrades to Alive(pid). Tests that
		// need a "crashed" backend install s.current with a dead pid directly.
		PID:        os.Getpid(),
		Port:       port,
		LogPath:    "/tmp/x.log",
		StartedAt:  time.Now(),
		Background: true,
	}
	m.tracked[inst.PID] = inst
	m.launches = append(m.launches, p)
	return inst, nil
}

func (m *stubManager) Kill(pid int) error {
	if m.killFn != nil {
		m.mu.Lock()
		m.kills = append(m.kills, pid)
		m.mu.Unlock()
		return m.killFn(pid)
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, ok := m.tracked[pid]; !ok {
		return processmgr.ErrUnknownPID
	}
	delete(m.tracked, pid)
	m.kills = append(m.kills, pid)
	return nil
}

func (m *stubManager) List() []domain.RunningInstance {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := make([]domain.RunningInstance, 0, len(m.tracked))
	for _, v := range m.tracked {
		out = append(out, v)
	}
	return out
}

func (m *stubManager) WaitHealthy(pid, port int, timeout time.Duration, attemptID string) error {
	return m.healthFn(pid, port)
}

func (m *stubManager) WaitReady(inst domain.RunningInstance, timeout time.Duration, attemptID string) (string, error) {
	if err := m.healthFn(inst.PID, inst.Port); err != nil {
		return "", err
	}
	return m.readyToken, nil
}

func (m *stubManager) TailLogs(pid int) (io.ReadCloser, error) {
	return nil, processmgr.ErrUnknownPID
}

func (m *stubManager) Close() error { return nil }

func (m *stubManager) GetExitInfo(pid int) (processmgr.ExitInfo, bool) {
	return processmgr.ExitInfo{}, false
}

func (m *stubManager) History() []domain.ExitedInstance {
	return nil
}

func (m *stubManager) RefreshFromDisk() error { return nil }

func (m *stubManager) launchCount() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return len(m.launches)
}

func (m *stubManager) killCount() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return len(m.kills)
}

// portFromArgs extracts the "port" value from a profile's Args map.
// The stub echoes this value back in the returned RunningInstance so that
// port assertions in tests remain stable — a real manager allocates ephemeral
// ports and ignores the args port entirely.
func portFromArgs(args map[string]any) (int, bool) {
	v, ok := args["port"]
	if !ok {
		return 0, false
	}
	switch t := v.(type) {
	case float64:
		return int(t), true
	case int:
		return t, true
	}
	return 0, false
}

// makeProfile is a tiny constructor used by handler/swap tests.
func makeProfile(id string, port int) domain.Profile {
	return domain.Profile{
		ID:    id,
		Name:  id,
		Model: "/tmp/fake.gguf",
		Args:  map[string]any{"port": float64(port)},
	}
}
