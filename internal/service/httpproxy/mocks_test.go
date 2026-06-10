package httpproxy

import (
	"errors"
	"io"
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

	swapDelay time.Duration // optional: delay inside Launch to widen swap race
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
		ProfileID:  p.ID,
		PID:        m.nextPID,
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
// Ports are manager-allocated at runtime; this helper fabricates the
// instance port from the args so tests can stand up a real backend server.
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
