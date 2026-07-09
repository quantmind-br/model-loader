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
	"github.com/quantmind-br/model-loader/internal/service/internal/procutil"
	"github.com/quantmind-br/model-loader/internal/service/processmgr"
	"github.com/quantmind-br/model-loader/internal/service/profilestore"
)

// registerRoutes installs the catch-all reverse proxy plus the exact-path
// exceptions. Exact-match semantics in net/http.ServeMux keep
// `/v1/models/anything`, `/v1/messages/` (trailing slash) and every other
// path routed to the forwarder — only the exact paths below are intercepted.
func (s *Server) registerRoutes(mux *http.ServeMux) {
	mux.HandleFunc("/v1/models", s.handleModelsList)
	mux.HandleFunc("/v1/messages", s.handleAnthropicMessages)
	mux.HandleFunc("/v1/messages/count_tokens", s.handleAnthropicCountTokens)
	mux.HandleFunc("/v1/responses", s.handleResponses)
	mux.HandleFunc("/v1beta/models", s.handleGeminiModels)
	mux.HandleFunc("/v1beta/models/", s.handleGeminiAction)
	mux.HandleFunc("/_status", s.handleStatus)
	mux.HandleFunc("/_admin/load", s.handleAdminLoad)
	mux.HandleFunc("/_admin/unload", s.handleAdminUnload)
	mux.HandleFunc("/", s.handleForward)
}

// modelsResponse is the list envelope returned by /v1/models. The envelope
// keeps OpenAI's `object: "list"`; each entry mirrors the OpenRouter model
// object (see models_openrouter.go). The Anthropic pagination keys ride
// alongside additively: has_more is always false (no pagination) and
// first_id/last_id are the first/last item ids (null on an empty list).
type modelsResponse struct {
	Object  string    `json:"object"`
	Data    []orModel `json:"data"`
	HasMore bool      `json:"has_more"`
	FirstID *string   `json:"first_id"`
	LastID  *string   `json:"last_id"`
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
		Data:   make([]orModel, 0, len(profiles)),
	}
	for _, p := range profiles {
		out.Data = append(out.Data, buildORModel(p))
	}
	if len(out.Data) > 0 {
		out.FirstID = &out.Data[0].ID
		out.LastID = &out.Data[len(out.Data)-1].ID
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
	defer s.inflight.Add(-1)

	requested := extractProfileID(r, s.cfg.MaxBodyBuffer)

	if requested == "" {
		// Fall through to currently-loaded backend, if any.
		s.stateMu.RLock()
		cur := s.current
		s.stateMu.RUnlock()
		if cur != nil && !procutil.SameProcess(cur.pid, cur.startTicks) {
			// Loaded backend died out-of-band — don't proxy to a corpse (A6).
			s.handleBackendCrash(cur)
			cur = nil
		}
		if cur == nil {
			writeOpenAIError(w, http.StatusServiceUnavailable, "model_not_loaded",
				"no_model_loaded",
				"no model loaded; specify one via the JSON \"model\" field or ?model= query param")
			return
		}
		s.serving.Add(1)
		cur.proxy.ServeHTTP(w, r)
		s.serving.Add(-1)
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

	s.serving.Add(1)
	loaded.proxy.ServeHTTP(w, r)
	s.serving.Add(-1)
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
		if procutil.SameProcess(cur.pid, cur.startTicks) {
			return cur, nil
		}
		// Loaded backend for our target died out-of-band; relaunch (audit A6).
		s.handleBackendCrash(cur)
		cur = nil
	}

	// Serialize swaps.
	s.swapMu.Lock()
	defer s.swapMu.Unlock()

	// Double-check: another swap may have already loaded our target.
	s.stateMu.RLock()
	cur = s.current
	s.stateMu.RUnlock()
	if cur != nil && cur.profileID == profileID {
		if procutil.SameProcess(cur.pid, cur.startTicks) {
			return cur, nil
		}
		s.handleBackendCrash(cur)
		cur = nil
	}

	// Respect upstream cancellation before doing real work.
	if err := ctx.Err(); err != nil {
		// Client closed the request before the swap — this is a client-side
		// cancellation, not a backend failure (audit B5). 499 = de-facto
		// client-closed-request; skip recordError (kept from prior behavior).
		return nil, &SwapError{499, "invalid_request_error", "request_canceled",
			fmt.Sprintf("request canceled before swap: %v", err)}
	}

	profile, err := s.resolveProfile(profileID)
	if err != nil {
		return nil, err
	}

	swapStart := time.Now()
	attemptID := fmt.Sprintf("proxy-%d", swapStart.UnixNano())

	if err := s.killOldBackend(cur, attemptID); err != nil {
		return nil, err
	}

	loaded, err := s.launchNewBackend(profile, profileID, attemptID)
	if err != nil {
		return nil, err
	}

	s.recordSwapMetrics(loaded, swapStart, attemptID)
	return loaded, nil
}

// handleBackendCrash records that the loaded backend died out-of-band and
// clears s.current (only when unchanged) so the swap path relaunches instead
// of proxying to a dead PID (audit A6). Never routed through killOldBackend —
// the dead PID's registry entry is handled by its own reaper/liveness.
func (s *Server) handleBackendCrash(cur *loadedBackend) {
	s.recordError(fmt.Sprintf("backend_crashed: profile %s pid %d exited", cur.profileID, cur.pid))
	s.logger.Warn("proxy_backend_crash_detected",
		"profile_id", cur.profileID, "pid", cur.pid, "port", cur.port)
	s.stateMu.Lock()
	if s.current == cur {
		s.current = nil
	}
	s.stateMu.Unlock()
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
// s.current so a failed launch below can never leave a stale pointer. A kill
// that fails to confirm death (ErrStillAlive → the backend still holds VRAM)
// aborts the swap with a retriable 503 and leaves s.current pointing at the
// still-live backend, so a launch never contends for VRAM (P4/DF11).
func (s *Server) killOldBackend(cur *loadedBackend, attemptID string) error {
	if cur == nil {
		return nil
	}
	s.logger.Info("proxy_swap_killing",
		"from_profile", cur.profileID, "pid", cur.pid, "attempt_id", attemptID)
	if killErr := s.deps.ProcessMgr.Kill(cur.pid); killErr != nil && !errors.Is(killErr, processmgr.ErrUnknownPID) {
		// The previous backend did not die (still holds VRAM). Do NOT clear
		// s.current or launch a new backend into contended VRAM — that is the
		// P4/DF11 OOM cascade. Surface a retriable 503; s.current stays pointed
		// at the still-live backend so a same-target request still hot-paths.
		s.logger.Error("proxy_swap_kill_failed", "err", killErr, "pid", cur.pid, "attempt_id", attemptID)
		s.recordError(fmt.Sprintf("kill %s: %v", cur.profileID, killErr))
		return &SwapError{http.StatusServiceUnavailable, "backend_error", "backend_busy",
			fmt.Sprintf("could not free previous backend (pid %d): %v; GPU/VRAM may still be in use — retry shortly", cur.pid, killErr)}
	}
	s.stateMu.Lock()
	s.current = nil
	s.stateMu.Unlock()
	return nil
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

	token, rErr := s.deps.ProcessMgr.WaitReady(inst, s.cfg.HealthCheckTimeout, attemptID)
	if rErr != nil {
		s.logger.Error("proxy_swap_unhealthy",
			"profile_id", profileID, "pid", inst.PID, "port", inst.Port,
			"attempt_id", attemptID, "err", rErr)
		_ = s.deps.ProcessMgr.Kill(inst.PID)
		s.recordError(fmt.Sprintf("readiness %s: %v", profileID, rErr))
		return nil, &SwapError{http.StatusGatewayTimeout, "backend_error", "backend_unhealthy",
			fmt.Sprintf("backend %s not ready: %v", profileID, rErr)}
	}

	loaded := &loadedBackend{
		profileID:  profileID,
		pid:        inst.PID,
		port:       inst.Port,
		logPath:    inst.LogPath,
		authToken:  token,
		startTicks: inst.StartTicks,
		proxy:      newReverseProxy(inst.Port, token, s.cfg.MaxBodyBuffer),
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
	profileID := normalizeRequestModel(strings.TrimSpace(req.ProfileID))
	if profileID == "" {
		profileID = normalizeRequestModel(strings.TrimSpace(req.Model))
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

	// Still under swapMu — now safe, because drainServing counts only the
	// backend-use phase, so requests parked on swapMu (about to swap) aren't
	// waited on (audit C6).
	if !force && drainTimeout > 0 {
		if !s.drainServing(r.Context(), drainTimeout) {
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
