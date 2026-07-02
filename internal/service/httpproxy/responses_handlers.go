package httpproxy

import (
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"time"
)

// handleResponses implements POST /v1/responses (OpenAI Responses API),
// translated to the backend's OpenAI chat completions. Same inflight + swap
// semantics as the catch-all and /v1/messages.
func (s *Server) handleResponses(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.Header().Set("Allow", http.MethodPost)
		writeOpenAIError(w, http.StatusMethodNotAllowed, "invalid_request_error", "method_not_allowed",
			"method "+r.Method+" not allowed on /v1/responses")
		return
	}
	s.inflight.Add(1)
	s.inflightWG.Add(1)
	defer func() {
		s.inflight.Add(-1)
		s.inflightWG.Done()
	}()

	if r.Body == nil || r.Body == http.NoBody {
		writeOpenAIError(w, http.StatusBadRequest, "invalid_request_error", "missing_body", "request body required")
		return
	}
	defer r.Body.Close()
	data, err := io.ReadAll(io.LimitReader(r.Body, s.cfg.MaxBodyBuffer+1))
	if err != nil {
		writeOpenAIError(w, http.StatusBadRequest, "invalid_request_error", "read_error", "read body: "+err.Error())
		return
	}
	if int64(len(data)) > s.cfg.MaxBodyBuffer {
		writeOpenAIError(w, http.StatusRequestEntityTooLarge, "invalid_request_error", "request_too_large",
			"request body exceeds the proxy limit")
		return
	}
	var req responsesRequest
	if err := json.Unmarshal(data, &req); err != nil {
		writeOpenAIError(w, http.StatusBadRequest, "invalid_request_error", "invalid_json", "invalid JSON body: "+err.Error())
		return
	}

	base, suffix, hasSuffix := parseModelSuffix(strings.TrimSpace(req.Model))
	if hasSuffix {
		req.Model = base
	}
	profileID := normalizeRequestModel(strings.TrimSpace(req.Model))
	if profileID == "" {
		writeOpenAIError(w, http.StatusBadRequest, "invalid_request_error", "model_required",
			"model: required (use a profile id; list them via GET /v1/models)")
		return
	}
	if !validProfileID(profileID) {
		writeOpenAIError(w, http.StatusBadRequest, "invalid_request_error", "invalid_model",
			"model id must match [A-Za-z0-9._-]+")
		return
	}

	loaded, err := s.ensureLoaded(r.Context(), profileID)
	if err != nil {
		writeSwapError(w, err)
		return
	}
	oaiReq, terr := translateResponsesRequest(&req, loaded.profileID)
	if terr != nil {
		writeOpenAIError(w, http.StatusBadRequest, "invalid_request_error", "translate_error", terr.Error())
		return
	}
	if hasSuffix {
		applyReasoningOverride(oaiReq, suffix)
	}

	resp, aerr := s.postUpstreamChat(r.Context(), loaded, oaiReq)
	if aerr != nil {
		writeOpenAIError(w, aerr.Status, aerr.Type, "upstream", aerr.Message)
		return
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		ue := anthropicErrorFromUpstreamStatus(resp, s.cfg.MaxBodyBuffer)
		writeOpenAIError(w, ue.Status, ue.Type, "upstream", ue.Message)
		return
	}

	createdAt := time.Now().Unix()
	if req.Stream {
		s.serveResponsesStream(w, r, resp, req.Model, createdAt)
		return
	}
	s.serveResponsesJSON(w, resp, req.Model, createdAt)
}

func (s *Server) serveResponsesJSON(w http.ResponseWriter, resp *http.Response, model string, createdAt int64) {
	data, err := io.ReadAll(io.LimitReader(resp.Body, s.cfg.MaxBodyBuffer+1))
	if err != nil {
		writeOpenAIError(w, http.StatusBadGateway, "server_error", "upstream_read", "read upstream response: "+err.Error())
		return
	}
	var oaiResp oaiChatResponse
	if err := json.Unmarshal(data, &oaiResp); err != nil {
		writeOpenAIError(w, http.StatusBadGateway, "server_error", "upstream_decode", "decode upstream response: "+err.Error())
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(buildResponsesResponse(&oaiResp, model, createdAt))
}

func (s *Server) serveResponsesStream(w http.ResponseWriter, r *http.Request, resp *http.Response, model string, createdAt int64) {
	ct := strings.ToLower(resp.Header.Get("Content-Type"))
	if strings.Contains(ct, "text/event-stream") {
		if err := runResponsesStream(r.Context(), w, resp.Body, model, createdAt, s.logger); err != nil && r.Context().Err() == nil {
			s.logger.Warn("responses_stream_failed", "err", err)
		}
		return
	}
	data, err := io.ReadAll(io.LimitReader(resp.Body, s.cfg.MaxBodyBuffer+1))
	if err != nil {
		writeOpenAIError(w, http.StatusBadGateway, "server_error", "upstream_read", "unreadable non-stream upstream response")
		return
	}
	var oaiResp oaiChatResponse
	if err := json.Unmarshal(data, &oaiResp); err != nil {
		writeOpenAIError(w, http.StatusBadGateway, "server_error", "upstream_decode", "decode upstream response: "+err.Error())
		return
	}
	w.Header().Set("Content-Type", "text/event-stream; charset=utf-8")
	w.Header().Set("Cache-Control", "no-cache")
	w.WriteHeader(http.StatusOK)
	if err := synthesizeResponsesStream(w, &oaiResp, model, createdAt); err != nil {
		s.logger.Warn("responses_stream_synthesize_failed", "err", err)
	}
}
