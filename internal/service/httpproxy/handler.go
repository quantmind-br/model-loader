package httpproxy

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
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

	loaded, status, err := s.ensureLoaded(r.Context(), requested)
	if err != nil {
		switch status {
		case http.StatusNotFound:
			writeOpenAIError(w, status, "invalid_request_error", "model_not_found", err.Error())
		case http.StatusGatewayTimeout:
			writeOpenAIError(w, status, "backend_error", "backend_unhealthy", err.Error())
		case http.StatusBadGateway:
			writeOpenAIError(w, status, "backend_error", "backend_launch_failed", err.Error())
		default:
			writeOpenAIError(w, status, "server_error", "swap_failed", err.Error())
		}
		return
	}

	loaded.proxy.ServeHTTP(w, r)
}

// ensureLoaded returns the snapshot of the loaded backend for profileID,
// performing a swap if needed. Returns (snapshot, status, err) where status
// is the HTTP status the caller should surface to the client when err != nil.
func (s *Server) ensureLoaded(ctx context.Context, profileID string) (*loadedBackend, int, error) {
	// Fast path: snapshot under read lock.
	s.stateMu.RLock()
	cur := s.current
	s.stateMu.RUnlock()
	if cur != nil && cur.profileID == profileID {
		return cur, 0, nil
	}

	// Serialize swaps.
	s.swapMu.Lock()
	defer s.swapMu.Unlock()

	// Double-check: another swap may have already loaded our target.
	s.stateMu.RLock()
	cur = s.current
	s.stateMu.RUnlock()
	if cur != nil && cur.profileID == profileID {
		return cur, 0, nil
	}

	// Respect upstream cancellation before doing real work.
	if err := ctx.Err(); err != nil {
		return nil, http.StatusGatewayTimeout, fmt.Errorf("request canceled before swap: %w", err)
	}

	// Resolve the requested profile.
	profile, err := s.deps.ProfileStore.Get(profileID)
	if err != nil {
		if errors.Is(err, profilestore.ErrNotFound) || errors.Is(err, profilestore.ErrInvalidID) {
			return nil, http.StatusNotFound, fmt.Errorf("profile %q not found", profileID)
		}
		s.recordError(fmt.Sprintf("profile_lookup_failed: %v", err))
		return nil, http.StatusInternalServerError, fmt.Errorf("profile lookup: %w", err)
	}

	swapStart := time.Now()
	attemptID := fmt.Sprintf("proxy-%d", swapStart.UnixNano())

	// Kill old backend (if any) before launching the new one.
	if cur != nil {
		s.logger.Info("proxy_swap_killing",
			"from_profile", cur.profileID, "pid", cur.pid, "attempt_id", attemptID)
		if killErr := s.deps.ProcessMgr.Kill(cur.pid); killErr != nil && !errors.Is(killErr, processmgr.ErrUnknownPID) {
			s.logger.Warn("proxy_swap_kill_warning", "err", killErr, "attempt_id", attemptID)
		}
		s.stateMu.Lock()
		s.current = nil
		s.stateMu.Unlock()
	}

	inst, launchErr := s.launchProfile(profile, attemptID)
	if launchErr != nil {
		s.recordError(fmt.Sprintf("launch %s: %v", profileID, launchErr))
		return nil, http.StatusBadGateway, fmt.Errorf("launch %s: %w", profileID, launchErr)
	}

	if hErr := s.deps.ProcessMgr.WaitHealthy(inst.PID, inst.Port, s.cfg.HealthCheckTimeout, attemptID); hErr != nil {
		s.logger.Error("proxy_swap_unhealthy",
			"profile_id", profileID, "pid", inst.PID, "port", inst.Port,
			"attempt_id", attemptID, "err", hErr)
		_ = s.deps.ProcessMgr.Kill(inst.PID)
		s.recordError(fmt.Sprintf("healthcheck %s: %v", profileID, hErr))
		return nil, http.StatusGatewayTimeout, fmt.Errorf("backend %s unhealthy: %w", profileID, hErr)
	}

	loaded := &loadedBackend{
		profileID: profileID,
		pid:       inst.PID,
		port:      inst.Port,
		proxy:     newReverseProxy(inst.Port),
	}
	s.stateMu.Lock()
	s.current = loaded
	s.stateMu.Unlock()

	s.recordSwap(time.Since(swapStart))
	s.clearError()
	s.logger.Info("proxy_swap_complete",
		"profile_id", profileID, "pid", inst.PID, "port", inst.Port,
		"attempt_id", attemptID, "duration_ms", time.Since(swapStart).Milliseconds())
	return loaded, 0, nil
}

// launchProfile is a thin wrapper to keep the swap path readable.
func (s *Server) launchProfile(p domain.Profile, attemptID string) (domain.RunningInstance, error) {
	return s.deps.ProcessMgr.Launch(p, processmgr.LaunchBackground, attemptID)
}

// truncate returns s clipped to n runes, appending "..." when clipped.
// Avoids dumping unbounded attacker-controlled IDs into error messages.
func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "..."
}
