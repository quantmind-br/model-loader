package httpproxy

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/quantmind-br/model-loader/internal/domain"
	"github.com/quantmind-br/model-loader/internal/service/processmgr"
	"github.com/quantmind-br/model-loader/internal/service/profilestore"
)

// registerRoutes installs the catch-all reverse proxy plus the
// `GET /v1/models` exception. Exact-match semantics in net/http.ServeMux
// keep `/v1/models/anything` and every other path routed to the forwarder.
func (s *Server) registerRoutes(mux *http.ServeMux) {
	mux.HandleFunc("/v1/models", s.handleModelsList)
	mux.HandleFunc("/_status", s.handleStatus)
	mux.HandleFunc("/_admin/load", s.handleAdminLoad)
	mux.HandleFunc("/_admin/unload", s.handleAdminUnload)
	mux.HandleFunc("/", s.handleForward)
}

// modelEntry is the OpenAI-shaped object emitted by /v1/models.
type modelEntry struct {
	ID      string `json:"id"`
	Object  string `json:"object"`
	Created int64  `json:"created"`
	OwnedBy string `json:"owned_by"`
}

type modelsResponse struct {
	Object string       `json:"object"`
	Data   []modelEntry `json:"data"`
}

func (s *Server) handleModelsList(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		w.Header().Set("Allow", http.MethodGet)
		writeOpenAIError(w, http.StatusMethodNotAllowed, "invalid_request_error",
			"method_not_allowed", "method "+r.Method+" not allowed on /v1/models")
		return
	}
	profiles, diags, err := s.deps.ProfileStore.ListWithDiagnostics()
	if err != nil {
		s.logger.Error("models_list_failed", "err", err)
		writeOpenAIError(w, http.StatusInternalServerError, "server_error",
			"profile_store_error", err.Error())
		return
	}
	for _, d := range diags {
		s.logger.Warn("models_list_skipped_corrupt", "profile_id", d.ID, "err", d.Err)
	}
	out := modelsResponse{
		Object: "list",
		Data:   make([]modelEntry, 0, len(profiles)),
	}
	for _, p := range profiles {
		out.Data = append(out.Data, modelEntry{
			ID:      p.ID,
			Object:  "model",
			Created: p.Meta.CreatedAt.Unix(),
			OwnedBy: "model-loader",
		})
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(out)
}

func (s *Server) handleStatus(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		w.Header().Set("Allow", http.MethodGet)
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	st := s.Status()
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(st)
}

func (s *Server) handleForward(w http.ResponseWriter, r *http.Request) {
	s.inflight.Add(1)
	s.inflightWG.Add(1)
	defer func() {
		s.inflight.Add(-1)
		s.inflightWG.Done()
	}()

	requested := extractProfileID(r, s.cfg.MaxBodyBuffer)

	if requested == "" {
		// Fall through to currently-loaded backend, if any.
		s.stateMu.RLock()
		cur := s.current
		s.stateMu.RUnlock()
		if cur == nil {
			writeOpenAIError(w, http.StatusServiceUnavailable, "model_not_loaded",
				"no_model_loaded",
				"no model loaded; specify one via the JSON \"model\" field or ?model= query param")
			return
		}
		cur.proxy.ServeHTTP(w, r)
		return
	}

	if !validProfileID(requested) {
		writeOpenAIError(w, http.StatusBadRequest, "invalid_request_error",
			"invalid_model_id",
			"model id must match [A-Za-z0-9._-]+ (got: "+truncate(requested, 64)+")")
		return
	}

	loaded, err := s.ensureLoaded(r.Context(), requested)
	if err != nil {
		writeSwapError(w, err)
		return
	}

	loaded.proxy.ServeHTTP(w, r)
}

// ensureLoaded returns the snapshot of the loaded backend for profileID,
// performing a swap if needed. On failure it returns a *SwapError carrying the
// HTTP status/body the caller should surface (via writeSwapError). The body is
// a sequence of phase helpers: resolve → kill-old → launch-new → record.
func (s *Server) ensureLoaded(ctx context.Context, profileID string) (*loadedBackend, error) {
	// Fast path: snapshot under read lock.
	s.stateMu.RLock()
	cur := s.current
	s.stateMu.RUnlock()
	if cur != nil && cur.profileID == profileID {
		return cur, nil
	}

	// Serialize swaps.
	s.swapMu.Lock()
	defer s.swapMu.Unlock()

	// Double-check: another swap may have already loaded our target.
	s.stateMu.RLock()
	cur = s.current
	s.stateMu.RUnlock()
	if cur != nil && cur.profileID == profileID {
		return cur, nil
	}

	// Respect upstream cancellation before doing real work.
	if err := ctx.Err(); err != nil {
		return nil, &SwapError{http.StatusGatewayTimeout, "backend_error", "backend_unhealthy",
			fmt.Sprintf("request canceled before swap: %v", err)}
	}

	profile, err := s.resolveProfile(profileID)
	if err != nil {
		return nil, err
	}

	swapStart := time.Now()
	attemptID := fmt.Sprintf("proxy-%d", swapStart.UnixNano())

	s.killOldBackend(cur, attemptID)

	loaded, err := s.launchNewBackend(profile, profileID, attemptID)
	if err != nil {
		return nil, err
	}

	s.recordSwapMetrics(loaded, swapStart, attemptID)
	return loaded, nil
}

// resolveProfile loads the requested profile, translating store errors into the
// matching *SwapError: a missing/invalid id is a 404 model_not_found, any other
// store error a recorded 500.
func (s *Server) resolveProfile(profileID string) (domain.Profile, error) {
	profile, err := s.deps.ProfileStore.Get(profileID)
	if err != nil {
		if errors.Is(err, profilestore.ErrNotFound) || errors.Is(err, profilestore.ErrInvalidID) {
			return domain.Profile{}, &SwapError{http.StatusNotFound, "invalid_request_error",
				"model_not_found", fmt.Sprintf("profile %q not found", profileID)}
		}
		s.recordError(fmt.Sprintf("profile_lookup_failed: %v", err))
		return domain.Profile{}, &SwapError{http.StatusInternalServerError, "server_error",
			"swap_failed", fmt.Sprintf("profile lookup: %v", err)}
	}
	return profile, nil
}

// killOldBackend terminates the previously-loaded backend (if any) and clears
// s.current so a failed launch below can never leave a stale pointer. Kill
// failures are non-fatal (logged), preserving the prior inline behavior.
func (s *Server) killOldBackend(cur *loadedBackend, attemptID string) {
	if cur == nil {
		return
	}
	s.logger.Info("proxy_swap_killing",
		"from_profile", cur.profileID, "pid", cur.pid, "attempt_id", attemptID)
	if killErr := s.deps.ProcessMgr.Kill(cur.pid); killErr != nil && !errors.Is(killErr, processmgr.ErrUnknownPID) {
		s.logger.Warn("proxy_swap_kill_warning", "err", killErr, "attempt_id", attemptID)
	}
	s.stateMu.Lock()
	s.current = nil
	s.stateMu.Unlock()
}

// launchNewBackend launches profile, waits for it to become healthy, and on
// success installs it as s.current. A launch failure maps to 502; an unhealthy
// backend is killed and maps to 504.
func (s *Server) launchNewBackend(profile domain.Profile, profileID, attemptID string) (*loadedBackend, error) {
	inst, launchErr := s.launchProfile(profile, attemptID)
	if launchErr != nil {
		s.recordError(fmt.Sprintf("launch %s: %v", profileID, launchErr))
		return nil, &SwapError{http.StatusBadGateway, "backend_error", "backend_launch_failed",
			fmt.Sprintf("launch %s: %v", profileID, launchErr)}
	}

	if hErr := s.deps.ProcessMgr.WaitHealthy(inst.PID, inst.Port, s.cfg.HealthCheckTimeout, attemptID); hErr != nil {
		s.logger.Error("proxy_swap_unhealthy",
			"profile_id", profileID, "pid", inst.PID, "port", inst.Port,
			"attempt_id", attemptID, "err", hErr)
		_ = s.deps.ProcessMgr.Kill(inst.PID)
		s.recordError(fmt.Sprintf("healthcheck %s: %v", profileID, hErr))
		return nil, &SwapError{http.StatusGatewayTimeout, "backend_error", "backend_unhealthy",
			fmt.Sprintf("backend %s unhealthy: %v", profileID, hErr)}
	}

	loaded := &loadedBackend{
		profileID: profileID,
		pid:       inst.PID,
		port:      inst.Port,
		logPath:   inst.LogPath,
		proxy:     newReverseProxy(inst.Port),
	}
	s.stateMu.Lock()
	s.current = loaded
	s.stateMu.Unlock()
	return loaded, nil
}

// recordSwapMetrics records the swap duration, clears the last error, and logs
// completion.
func (s *Server) recordSwapMetrics(loaded *loadedBackend, swapStart time.Time, attemptID string) {
	s.recordSwap(time.Since(swapStart))
	s.clearError()
	s.logger.Info("proxy_swap_complete",
		"profile_id", loaded.profileID, "pid", loaded.pid, "port", loaded.port,
		"attempt_id", attemptID, "duration_ms", time.Since(swapStart).Milliseconds())
}

// launchProfile is a thin wrapper to keep the swap path readable.
func (s *Server) launchProfile(p domain.Profile, attemptID string) (domain.RunningInstance, error) {
	return s.deps.ProcessMgr.Launch(p, processmgr.LaunchBackground, attemptID)
}

// adminLoadRequest is the JSON body accepted by POST /_admin/load. Either
// `profile_id` (preferred) or `model` (OpenAI-style alias) names the target.
type adminLoadRequest struct {
	ProfileID string `json:"profile_id"`
	Model     string `json:"model"`
}

// handleAdminLoad explicitly loads a profile without piggybacking on a chat
// completion. Reuses the same ensureLoaded swap path as the catch-all proxy,
// so concurrency, kill-old/launch-new ordering, and health checks are
// identical. Returns 200 + Status JSON on success.
func (s *Server) handleAdminLoad(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.Header().Set("Allow", http.MethodPost)
		writeOpenAIError(w, http.StatusMethodNotAllowed, "invalid_request_error",
			"method_not_allowed", "method "+r.Method+" not allowed on /_admin/load")
		return
	}
	var req adminLoadRequest
	if r.Body != nil && r.Body != http.NoBody {
		defer r.Body.Close()
		limited := io.LimitReader(r.Body, s.cfg.MaxBodyBuffer)
		dec := json.NewDecoder(limited)
		if err := dec.Decode(&req); err != nil && !errors.Is(err, io.EOF) {
			writeOpenAIError(w, http.StatusBadRequest, "invalid_request_error",
				"invalid_body", "invalid JSON body: "+err.Error())
			return
		}
	}
	profileID := strings.TrimSpace(req.ProfileID)
	if profileID == "" {
		profileID = strings.TrimSpace(req.Model)
	}
	if profileID == "" {
		writeOpenAIError(w, http.StatusBadRequest, "invalid_request_error",
			"missing_profile_id", `request body must include "profile_id" or "model"`)
		return
	}
	if !validProfileID(profileID) {
		writeOpenAIError(w, http.StatusBadRequest, "invalid_request_error",
			"invalid_model_id",
			"profile id must match [A-Za-z0-9._-]+ (got: "+truncate(profileID, 64)+")")
		return
	}

	if _, err := s.ensureLoaded(r.Context(), profileID); err != nil {
		writeSwapError(w, err)
		return
	}
	s.logger.Info("proxy_admin_load_ok", "profile_id", profileID)
	writeStatusJSON(w, s.Status())
}

// handleAdminUnload kills the currently-loaded backend, freeing its VRAM.
// Optional query params:
//   - force=true       skip draining in-flight requests
//   - drain_timeout=10s upper bound on the drain wait (default: ShutdownGracePeriod)
//
// Idempotent: returns 200 with the same Status JSON shape when nothing is loaded.
func (s *Server) handleAdminUnload(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.Header().Set("Allow", http.MethodPost)
		writeOpenAIError(w, http.StatusMethodNotAllowed, "invalid_request_error",
			"method_not_allowed", "method "+r.Method+" not allowed on /_admin/unload")
		return
	}
	force := r.URL.Query().Get("force") == "true"
	drainTimeout := s.cfg.ShutdownGracePeriod
	if raw := r.URL.Query().Get("drain_timeout"); raw != "" {
		if d, err := time.ParseDuration(raw); err == nil && d >= 0 {
			drainTimeout = d
		}
	}

	// Serialize against ensureLoaded so a swap-in-progress cannot collide
	// with the kill below.
	s.swapMu.Lock()
	defer s.swapMu.Unlock()

	s.stateMu.RLock()
	empty := s.current == nil
	s.stateMu.RUnlock()
	if empty {
		s.logger.Info("proxy_admin_unload_noop")
		writeStatusJSON(w, s.Status())
		return
	}

	if !force && drainTimeout > 0 {
		drained := make(chan struct{})
		go func() {
			s.inflightWG.Wait()
			close(drained)
		}()
		select {
		case <-drained:
		case <-time.After(drainTimeout):
			s.logger.Warn("proxy_admin_unload_drain_timeout",
				"timeout_ms", drainTimeout.Milliseconds())
		}
	}

	if err := s.killCurrentBackend(); err != nil {
		s.recordError("admin_unload_kill: " + err.Error())
		s.logger.Error("proxy_admin_unload_kill_failed", "err", err)
		writeSwapError(w, &SwapError{http.StatusInternalServerError, "server_error",
			"unload_failed", err.Error()})
		return
	}
	s.logger.Info("proxy_admin_unload_ok")
	writeStatusJSON(w, s.Status())
}

// writeStatusJSON is the shared 200 response for admin endpoints — same shape
// as GET /_status so a client can treat the bodies interchangeably.
func writeStatusJSON(w http.ResponseWriter, st Status) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(st)
}

// truncate returns s clipped to n runes, appending "..." when clipped.
// Avoids dumping unbounded attacker-controlled IDs into error messages.
func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "..."
}
