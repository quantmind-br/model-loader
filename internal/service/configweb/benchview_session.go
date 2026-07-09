package configweb

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"sync"
	"time"

	"github.com/quantmind-br/model-loader/internal/service/benchmark"
	"github.com/quantmind-br/model-loader/internal/service/benchmarkstore"
	"github.com/quantmind-br/model-loader/internal/service/profilestore"
)

// BenchViewerDeps are the read-only stores plus the live-feed accessor the
// benchmark viewer needs. Live is nil-safe: a nil field OR a nil return renders
// "no run in progress" (runs are started and cancelled only from the TUI/CLI).
type BenchViewerDeps struct {
	Runs     benchmarkstore.Store
	Profiles profilestore.Store
	Live     func() *benchmark.RunFeed
	// ImmediateShutdown tears the server down as soon as Cancel is called,
	// skipping the /closed linger. The TUI leaves this false so a same-origin
	// navigation to /closed is still served; the CLI (no browser done page on
	// Ctrl-C) sets it true so SIGINT exits promptly. (UIUX-027)
	ImmediateShutdown bool
}

// BenchViewer owns one read-only benchmark browser session and its HTTP server.
// It mirrors Session's ephemeral 127.0.0.1:0 bind and lingering-shutdown
// lifecycle, but serves saved runs plus a live monitor — never mutates state.
type BenchViewer struct {
	deps         BenchViewerDeps
	srv          *http.Server
	done         chan struct{}
	completeOnce sync.Once
	shutdownOnce sync.Once
	shutdownReq  chan struct{}
	cancel       context.CancelFunc
}

// NewBenchViewer constructs a viewer bound to the given deps.
func NewBenchViewer(deps BenchViewerDeps) *BenchViewer {
	return &BenchViewer{deps: deps, done: make(chan struct{}), shutdownReq: make(chan struct{})}
}

// Done is closed once the session's HTTP server has been torn down.
func (v *BenchViewer) Done() <-chan struct{} { return v.done }

// Start binds an ephemeral localhost port and serves in a goroutine, returning
// the base URL (http://127.0.0.1:PORT).
func (v *BenchViewer) Start() (string, error) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return "", fmt.Errorf("bind: %w", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	v.cancel = cancel

	mux := http.NewServeMux()
	v.routes(mux)
	v.srv = &http.Server{Handler: mux}

	go func() { _ = v.srv.Serve(ln) }()
	go func() {
		<-ctx.Done()
		// Graceful shutdown drains any in-flight request (e.g. a /closed done
		// page) before tearing the listener down. Fall back to a hard close on
		// timeout. Done() is closed only after the server is really down.
		shutCtx, c := context.WithTimeout(context.Background(), 5*time.Second)
		defer c()
		if err := v.srv.Shutdown(shutCtx); err != nil {
			_ = v.srv.Close()
		}
		close(v.done)
	}()

	port := ln.Addr().(*net.TCPAddr).Port
	return fmt.Sprintf("http://127.0.0.1:%d", port), nil
}

// Cancel requests shutdown (idempotent). Used by the TUI esc handler and the
// CLI SIGINT path.
func (v *BenchViewer) Cancel() { v.complete() }

// complete begins teardown once. The server is NOT torn down immediately: a
// browser may still be fetching /closed after a same-origin navigation, and
// closing now would refuse that GET. The server lingers until /closed is served
// (requestShutdown) or a short grace period elapses for an abandoned tab.
func (v *BenchViewer) complete() {
	v.completeOnce.Do(func() {
		if v.cancel == nil {
			close(v.done) // no server bound (unit tests)
			return
		}
		if v.deps.ImmediateShutdown {
			// UIUX-027: no /closed round-trip in the CLI path — cancel now so
			// SIGINT exits without the 3s linger.
			v.cancel()
			return
		}
		go func() {
			select {
			case <-v.shutdownReq:
			case <-time.After(3 * time.Second):
			}
			v.cancel()
		}()
	})
}

// requestShutdown ends the linger and tears the server down now. Called once
// the done page has been served. Safe to call multiple times.
func (v *BenchViewer) requestShutdown() {
	v.shutdownOnce.Do(func() {
		if v.shutdownReq != nil {
			close(v.shutdownReq)
		}
	})
}
