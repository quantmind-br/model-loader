// Package httpproxy exposes an HTTP reverse proxy that routes OpenAI-shaped
// requests to a single currently-loaded backend (managed via processmgr),
// swapping the backend on demand based on the requested model id.
package httpproxy

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"net/http/httputil"
	"sync"
	"sync/atomic"
	"time"

	mllog "github.com/quantmind-br/model-loader/internal/log"
	"github.com/quantmind-br/model-loader/internal/service/processmgr"
	"github.com/quantmind-br/model-loader/internal/service/profilestore"
)

const (
	defaultHealthCheckTimeout  = 120 * time.Second
	defaultMaxBodyBuffer       = int64(8 << 20) // 8 MiB
	defaultShutdownGracePeriod = 10 * time.Second
)

// Config holds runtime tuning for the proxy server.
type Config struct {
	Host                string
	Port                int
	HealthCheckTimeout  time.Duration
	MaxBodyBuffer       int64
	ShutdownGracePeriod time.Duration
}

// Deps wires the proxy to the rest of the app. ProfileStore is consulted
// for /v1/models and per-request profile lookup; ProcessMgr drives backend
// launch/kill; Logger receives lifecycle and error events.
type Deps struct {
	ProfileStore profilestore.Store
	ProcessMgr   processmgr.Manager
	Logger       *slog.Logger
}

// Status is the snapshot consumed by the TUI Server page and by Stop()
// idempotency checks.
type Status struct {
	Running          bool
	Addr             string
	LoadedProfileID  string
	LoadedPID        int
	LoadedPort       int
	LastSwapAt       time.Time
	LastSwapDur      time.Duration
	LastError        string
	LastErrorAt      time.Time
	InflightRequests int
}

// loadedBackend captures the proxy's view of the currently-running backend.
// It owns a pre-built ReverseProxy to avoid re-allocating one per request.
type loadedBackend struct {
	profileID string
	pid       int
	port      int
	proxy     *httputil.ReverseProxy
}

// Server is the HTTP proxy. Construct with New; drive via Start/Stop;
// observe via Status.
type Server struct {
	cfg    Config
	deps   Deps
	logger *slog.Logger

	startMu sync.Mutex      // serializes Start/Stop
	httpSrv *http.Server    // non-nil while Running
	listenerAddr string     // captured at Start time
	serveErrCh   chan error // closed/errored when http.Serve returns

	stateMu sync.RWMutex
	current *loadedBackend

	swapMu sync.Mutex // serializes load/unload across concurrent requests

	inflight atomic.Int64
	inflightWG sync.WaitGroup

	statusMu     sync.Mutex
	lastSwapAt   time.Time
	lastSwapDur  time.Duration
	lastError    string
	lastErrorAt  time.Time
}

// New constructs a Server with sane defaults. Logger nil → no-op.
func New(cfg Config, deps Deps) *Server {
	if cfg.HealthCheckTimeout <= 0 {
		cfg.HealthCheckTimeout = defaultHealthCheckTimeout
	}
	if cfg.MaxBodyBuffer <= 0 {
		cfg.MaxBodyBuffer = defaultMaxBodyBuffer
	}
	if cfg.ShutdownGracePeriod <= 0 {
		cfg.ShutdownGracePeriod = defaultShutdownGracePeriod
	}
	if cfg.Host == "" {
		cfg.Host = "127.0.0.1"
	}
	logger := deps.Logger
	if logger == nil {
		logger = mllog.Nop()
	}
	return &Server{
		cfg:    cfg,
		deps:   deps,
		logger: logger,
	}
}

// Start binds the listener and serves in a background goroutine. Returns
// nil once the socket is bound and the server is ready. Idempotent: calling
// Start while already running is a no-op returning nil.
func (s *Server) Start(_ context.Context) error {
	s.startMu.Lock()
	defer s.startMu.Unlock()

	if s.httpSrv != nil {
		return nil
	}

	addr := fmt.Sprintf("%s:%d", s.cfg.Host, s.cfg.Port)
	ln, err := net.Listen("tcp", addr)
	if err != nil {
		s.recordError(fmt.Sprintf("bind %s: %v", addr, err))
		return fmt.Errorf("bind %s: %w", addr, err)
	}

	mux := http.NewServeMux()
	s.registerRoutes(mux)
	s.httpSrv = &http.Server{
		Handler:           mux,
		ReadHeaderTimeout: 30 * time.Second,
	}
	s.listenerAddr = ln.Addr().String()
	s.serveErrCh = make(chan error, 1)
	s.clearError()

	go func() {
		err := s.httpSrv.Serve(ln)
		if err != nil && !errors.Is(err, http.ErrServerClosed) {
			s.recordError(fmt.Sprintf("serve: %v", err))
			s.logger.Error("proxy_serve_failed", "err", err)
			s.serveErrCh <- err
		}
		close(s.serveErrCh)
	}()

	s.logger.Info("proxy_listening",
		"addr", s.listenerAddr,
		"health_timeout", s.cfg.HealthCheckTimeout)
	return nil
}

// Stop shuts down the HTTP server (waiting up to ctx-deadline for in-flight
// requests), then kills any backend the proxy launched. Idempotent.
func (s *Server) Stop(ctx context.Context) error {
	s.startMu.Lock()
	defer s.startMu.Unlock()

	if s.httpSrv == nil {
		return nil
	}

	srv := s.httpSrv
	s.httpSrv = nil

	shutdownErr := srv.Shutdown(ctx)
	if shutdownErr != nil {
		s.logger.Warn("proxy_shutdown_warning", "err", shutdownErr)
	}

	// Best-effort drain. Shutdown already waits for active connections,
	// but in-flight ServeHTTP handlers that haven't returned yet still
	// hold the inflightWG counter. Bounded by ctx.
	drained := make(chan struct{})
	go func() {
		s.inflightWG.Wait()
		close(drained)
	}()
	select {
	case <-drained:
	case <-ctx.Done():
		s.logger.Warn("proxy_shutdown_drain_deadline")
	}

	killErr := s.killCurrentBackend()
	s.listenerAddr = ""
	s.serveErrCh = nil

	if shutdownErr != nil {
		return fmt.Errorf("shutdown: %w", shutdownErr)
	}
	if killErr != nil {
		return fmt.Errorf("kill backend: %w", killErr)
	}
	s.logger.Info("proxy_stopped")
	return nil
}

// killCurrentBackend stops the backend the proxy launched (if any) and
// clears the current snapshot. Safe to call when nothing is loaded.
func (s *Server) killCurrentBackend() error {
	s.stateMu.Lock()
	cur := s.current
	s.current = nil
	s.stateMu.Unlock()
	if cur == nil {
		return nil
	}
	if err := s.deps.ProcessMgr.Kill(cur.pid); err != nil && !errors.Is(err, processmgr.ErrUnknownPID) {
		return err
	}
	s.logger.Info("proxy_backend_killed", "pid", cur.pid, "profile_id", cur.profileID)
	return nil
}

// Status returns a snapshot. Safe for concurrent callers.
func (s *Server) Status() Status {
	s.startMu.Lock()
	running := s.httpSrv != nil
	addr := s.listenerAddr
	s.startMu.Unlock()

	s.stateMu.RLock()
	cur := s.current
	s.stateMu.RUnlock()

	s.statusMu.Lock()
	st := Status{
		Running:          running,
		Addr:             addr,
		LastSwapAt:       s.lastSwapAt,
		LastSwapDur:      s.lastSwapDur,
		LastError:        s.lastError,
		LastErrorAt:      s.lastErrorAt,
		InflightRequests: int(s.inflight.Load()),
	}
	s.statusMu.Unlock()

	if cur != nil {
		st.LoadedProfileID = cur.profileID
		st.LoadedPID = cur.pid
		st.LoadedPort = cur.port
	}
	return st
}

func (s *Server) recordError(msg string) {
	s.statusMu.Lock()
	s.lastError = msg
	s.lastErrorAt = time.Now()
	s.statusMu.Unlock()
}

func (s *Server) clearError() {
	s.statusMu.Lock()
	s.lastError = ""
	s.lastErrorAt = time.Time{}
	s.statusMu.Unlock()
}

func (s *Server) recordSwap(d time.Duration) {
	s.statusMu.Lock()
	s.lastSwapAt = time.Now()
	s.lastSwapDur = d
	s.statusMu.Unlock()
}
