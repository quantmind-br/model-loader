package configweb

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"sync"

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
	deps   Deps
	srv    *http.Server
	done   chan Result
	once   sync.Once
	cancel context.CancelFunc
}

// NewSession constructs a session bound to the given deps.
func NewSession(deps Deps) *Session {
	return &Session{deps: deps, done: make(chan Result, 1)}
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
		_ = s.srv.Close()
	}()

	port := ln.Addr().(*net.TCPAddr).Port
	return fmt.Sprintf("http://127.0.0.1:%d", port), nil
}

// Cancel completes the session as not-saved (used by the TUI esc handler).
func (s *Session) Cancel() { s.complete(Result{Saved: false}) }

// complete delivers the result once and triggers shutdown.
func (s *Session) complete(res Result) {
	s.once.Do(func() {
		s.done <- res
		if s.cancel != nil {
			s.cancel()
		}
	})
}
