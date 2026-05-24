package configweb

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"sync"
	"time"

	"github.com/quantmind-br/model-loader/internal/service/backendcatalog"
	"github.com/quantmind-br/model-loader/internal/service/backendschema"
	"github.com/quantmind-br/model-loader/internal/service/profilestore"
)

// Deps are the stores/services the session needs to read and persist data.
type Deps struct {
	Profiles             profilestore.Store
	Catalog              backendcatalog.Store
	Schemas              backendcatalog.SchemaStore
	Manager              *backendschema.Manager
	InitialDraft         Draft
	InitialBackendDraft  BackendDraft
}

// Result is delivered on Done() when the session finishes.
type Result struct {
	Saved     bool
	ProfileID string
	BackendID string
	Err       error
}

// Session owns one editing browser session and its HTTP server.
type Session struct {
	deps         Deps
	srv          *http.Server
	done         chan Result
	once         sync.Once
	shutdownOnce sync.Once
	shutdownReq  chan struct{}
	cancel       context.CancelFunc
}

// NewSession constructs a session bound to the given deps.
func NewSession(deps Deps) *Session {
	return &Session{deps: deps, done: make(chan Result, 1), shutdownReq: make(chan struct{})}
}

// Done delivers the single terminal Result.
func (s *Session) Done() <-chan Result { return s.done }

// Start binds an ephemeral localhost port and serves in a goroutine.
// Returns the base URL (http://127.0.0.1:PORT).
func (s *Session) Start() (string, error) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return "", fmt.Errorf("bind: %w", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	s.cancel = cancel

	mux := http.NewServeMux()
	s.routes(mux)
	s.srv = &http.Server{Handler: mux}

	go func() { _ = s.srv.Serve(ln) }()
	go func() {
		<-ctx.Done()
		// Graceful shutdown waits for the in-flight /closed request (the done
		// page) to finish before tearing the listener down, so the browser is
		// never cut off mid-response. Fall back to a hard close on timeout.
		shutCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := s.srv.Shutdown(shutCtx); err != nil {
			_ = s.srv.Close()
		}
	}()

	port := ln.Addr().(*net.TCPAddr).Port
	return fmt.Sprintf("http://127.0.0.1:%d", port), nil
}

// Cancel completes the session as not-saved (used by the TUI esc handler).
func (s *Session) Cancel() { s.complete(Result{Saved: false}) }

// complete delivers the result once. The HTTP server is NOT torn down here:
// save/cancel handlers respond with HX-Redirect to /closed (the "All set"
// page), and closing immediately would refuse that follow-up GET
// (ERR_CONNECTION_REFUSED). The server lingers until /closed is served
// (requestShutdown), or a short grace period elapses for an abandoned tab.
func (s *Session) complete(res Result) {
	s.once.Do(func() {
		s.done <- res
		if s.cancel == nil {
			return // no server bound (unit tests)
		}
		go func() {
			select {
			case <-s.shutdownReq:
			case <-time.After(3 * time.Second):
			}
			s.cancel()
		}()
	})
}

// requestShutdown tears the HTTP server down now. Called once the done page has
// been served so the session does not linger for the full grace period.
// Safe to call multiple times.
func (s *Session) requestShutdown() {
	s.shutdownOnce.Do(func() {
		if s.shutdownReq != nil {
			close(s.shutdownReq)
		}
	})
}
