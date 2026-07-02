package httpproxy

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"

	"github.com/quantmind-br/model-loader/internal/service/profilestore"
)

// anthropicAPIError carries an Anthropic-enveloped failure through the
// translation and handler layers — the analog of SwapError for the routes
// that speak the Anthropic error shape.
type anthropicAPIError struct {
	Status  int
	Type    string // "invalid_request_error" | "not_found_error" | "request_too_large" | "api_error"
	Message string
}

func (e *anthropicAPIError) Error() string { return e.Message }

// writeAnthropicError renders the Anthropic error envelope
// {"type":"error","error":{"type":...,"message":...}}. Only the /v1/messages
// routes use it — every other endpoint keeps the OpenAI envelope.
func writeAnthropicError(w http.ResponseWriter, status int, errType, msg string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(anthropicErrorEnvelope{
		Type:  "error",
		Error: anthropicErrorBody{Type: errType, Message: msg},
	})
}

func writeAnthropicAPIError(w http.ResponseWriter, e *anthropicAPIError) {
	writeAnthropicError(w, e.Status, e.Type, e.Message)
}

// anthropicErrorFromSwap maps a *SwapError from ensureLoaded onto the
// Anthropic envelope (mirrors writeSwapError's errors.As handling).
func anthropicErrorFromSwap(err error) *anthropicAPIError {
	var se *SwapError
	if !errors.As(err, &se) {
		return &anthropicAPIError{http.StatusInternalServerError, "api_error", err.Error()}
	}
	switch {
	case se.StatusCode == http.StatusNotFound:
		return &anthropicAPIError{http.StatusNotFound, "not_found_error",
			se.Msg + "; list available models via GET /v1/models"}
	case se.StatusCode >= 400 && se.StatusCode < 500:
		return &anthropicAPIError{se.StatusCode, "invalid_request_error", se.Msg}
	default:
		return &anthropicAPIError{se.StatusCode, "api_error", se.Msg}
	}
}

// decodeAnthropicBody reads the request body capped at MaxBodyBuffer and
// unmarshals it. Unknown fields are ignored on purpose (metadata, thinking,
// cache_control, betas, ... — never reject them).
func (s *Server) decodeAnthropicBody(r *http.Request) (*anthropicMessagesRequest, *anthropicAPIError) {
	if r.Body == nil || r.Body == http.NoBody {
		return nil, &anthropicAPIError{http.StatusBadRequest, "invalid_request_error", "request body required"}
	}
	defer r.Body.Close()
	data, err := io.ReadAll(io.LimitReader(r.Body, s.cfg.MaxBodyBuffer+1))
	if err != nil {
		return nil, &anthropicAPIError{http.StatusBadRequest, "invalid_request_error", "read body: " + err.Error()}
	}
	if int64(len(data)) > s.cfg.MaxBodyBuffer {
		return nil, &anthropicAPIError{http.StatusRequestEntityTooLarge, "request_too_large",
			fmt.Sprintf("request body exceeds the proxy limit of %d bytes", s.cfg.MaxBodyBuffer)}
	}
	var req anthropicMessagesRequest
	if err := json.Unmarshal(data, &req); err != nil {
		return nil, &anthropicAPIError{http.StatusBadRequest, "invalid_request_error", "invalid JSON body: " + err.Error()}
	}
	return &req, nil
}

// validateAnthropicRequest applies the strict top-level rules: model is
// required (no fall-through to the currently-loaded backend), messages must
// be non-empty, and — for /v1/messages — max_tokens must be a positive int.
func validateAnthropicRequest(req *anthropicMessagesRequest, requireMaxTokens bool) (string, *anthropicAPIError) {
	profileID := normalizeRequestModel(strings.TrimSpace(req.Model))
	if profileID == "" {
		return "", &anthropicAPIError{http.StatusBadRequest, "invalid_request_error",
			"model: required (use a profile id; list them via GET /v1/models)"}
	}
	if !validProfileID(profileID) {
		return "", &anthropicAPIError{http.StatusBadRequest, "invalid_request_error",
			"model id must match [A-Za-z0-9._-]+ (got: " + truncate(profileID, 64) + ")"}
	}
	if len(req.Messages) == 0 {
		return "", &anthropicAPIError{http.StatusBadRequest, "invalid_request_error",
			"messages: at least one message is required"}
	}
	if requireMaxTokens && (req.MaxTokens == nil || *req.MaxTokens <= 0) {
		return "", &anthropicAPIError{http.StatusBadRequest, "invalid_request_error",
			"max_tokens: required and must be > 0"}
	}
	return profileID, nil
}

// handleAnthropicMessages implements POST /v1/messages (Anthropic Messages
// API), translating to the backend's OpenAI /v1/chat/completions.
func (s *Server) handleAnthropicMessages(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.Header().Set("Allow", http.MethodPost)
		writeAnthropicError(w, http.StatusMethodNotAllowed, "invalid_request_error",
			"method "+r.Method+" not allowed on /v1/messages")
		return
	}

	// Same in-flight accounting as handleForward — the only other
	// inflightWG increment site, so /_admin/unload drains and Stop() cover
	// translated requests too. This handler never waits on the WaitGroup,
	// so it cannot deadlock the drain.
	s.inflight.Add(1)
	s.inflightWG.Add(1)
	defer func() {
		s.inflight.Add(-1)
		s.inflightWG.Done()
	}()

	req, aerr := s.decodeAnthropicBody(r)
	if aerr != nil {
		writeAnthropicAPIError(w, aerr)
		return
	}
	// Model suffix override: "profile-id(level|budget)" forces the reasoning
	// level via the model name; stripped before profile resolution.
	base, suffix, hasSuffix := parseModelSuffix(strings.TrimSpace(req.Model))
	if hasSuffix {
		req.Model = base
	}
	profileID, aerr := validateAnthropicRequest(req, true)
	if aerr != nil {
		writeAnthropicAPIError(w, aerr)
		return
	}

	loaded, err := s.ensureLoaded(r.Context(), profileID)
	if err != nil {
		writeAnthropicAPIError(w, anthropicErrorFromSwap(err))
		return
	}

	oaiReq, aerr := translateAnthropicRequest(req, loaded.profileID)
	if aerr != nil {
		writeAnthropicAPIError(w, aerr)
		return
	}
	if hasSuffix {
		applyReasoningOverride(oaiReq, suffix)
	}

	resp, aerr := s.postUpstreamChat(r.Context(), loaded, oaiReq)
	if aerr != nil {
		writeAnthropicAPIError(w, aerr)
		return
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		writeAnthropicAPIError(w, anthropicErrorFromUpstreamStatus(resp, s.cfg.MaxBodyBuffer))
		return
	}

	if req.Stream {
		s.serveAnthropicStream(w, r, resp, req.Model)
		return
	}
	s.serveAnthropicJSON(w, resp, req.Model)
}

// postUpstreamChat issues the translated chat-completions call directly to
// the loaded backend. Deliberately NOT routed through loaded.proxy: its
// ModifyResponse (mirrorReasoningIntoEmptyContent) rewrites reasoning_content
// into content, which would destroy the reasoning→thinking mapping here.
func (s *Server) postUpstreamChat(ctx context.Context, loaded *loadedBackend, oaiReq *oaiChatRequest) (*http.Response, *anthropicAPIError) {
	payload, err := json.Marshal(oaiReq)
	if err != nil {
		return nil, &anthropicAPIError{http.StatusInternalServerError, "api_error", "encode upstream request: " + err.Error()}
	}
	url := fmt.Sprintf("http://127.0.0.1:%d/v1/chat/completions", loaded.port)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(payload))
	if err != nil {
		return nil, &anthropicAPIError{http.StatusInternalServerError, "api_error", "build upstream request: " + err.Error()}
	}
	req.Header.Set("Content-Type", "application/json")
	if oaiReq.Stream {
		req.Header.Set("Accept", "text/event-stream")
	}
	if loaded.authToken != "" {
		req.Header.Set("Authorization", "Bearer "+loaded.authToken)
	}
	resp, err := s.upstream.Do(req)
	if err != nil {
		return nil, &anthropicAPIError{http.StatusBadGateway, "api_error", "upstream: " + err.Error()}
	}
	return resp, nil
}

// anthropicErrorFromUpstreamStatus maps a non-200 upstream response: 4xx
// becomes a client-visible 400 carrying the upstream message, everything
// else a 502.
func anthropicErrorFromUpstreamStatus(resp *http.Response, maxBody int64) *anthropicAPIError {
	body, _ := io.ReadAll(io.LimitReader(resp.Body, maxBody))
	msg := extractOpenAIErrorMessage(body)
	if msg == "" {
		msg = strings.TrimSpace(string(body))
	}
	if msg == "" {
		msg = resp.Status
	}
	if resp.StatusCode >= 400 && resp.StatusCode < 500 {
		return &anthropicAPIError{http.StatusBadRequest, "invalid_request_error",
			"upstream rejected the request: " + truncate(msg, 512)}
	}
	return &anthropicAPIError{http.StatusBadGateway, "api_error",
		"upstream error: " + truncate(msg, 512)}
}

func extractOpenAIErrorMessage(body []byte) string {
	var envelope struct {
		Error struct {
			Message string `json:"message"`
		} `json:"error"`
	}
	if err := json.Unmarshal(body, &envelope); err != nil {
		return ""
	}
	return envelope.Error.Message
}

// serveAnthropicJSON translates a non-streaming upstream 200 into the
// Anthropic message shape.
func (s *Server) serveAnthropicJSON(w http.ResponseWriter, resp *http.Response, requestedModel string) {
	data, err := io.ReadAll(io.LimitReader(resp.Body, s.cfg.MaxBodyBuffer+1))
	if err != nil {
		writeAnthropicError(w, http.StatusBadGateway, "api_error", "read upstream response: "+err.Error())
		return
	}
	if int64(len(data)) > s.cfg.MaxBodyBuffer {
		writeAnthropicError(w, http.StatusBadGateway, "api_error", "upstream response exceeds the proxy body limit")
		return
	}
	var oaiResp oaiChatResponse
	if err := json.Unmarshal(data, &oaiResp); err != nil {
		writeAnthropicError(w, http.StatusBadGateway, "api_error", "decode upstream response: "+err.Error())
		return
	}
	out := buildAnthropicResponse(&oaiResp, requestedModel, s.logger)
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(out)
}

// serveAnthropicStream translates an upstream SSE stream into Anthropic SSE
// frames; when a stream=true upstream answers plain JSON 200 instead, the
// complete message is synthesized as a full event sequence.
func (s *Server) serveAnthropicStream(w http.ResponseWriter, r *http.Request, resp *http.Response, requestedModel string) {
	ct := strings.ToLower(resp.Header.Get("Content-Type"))
	if strings.Contains(ct, "text/event-stream") {
		if err := runAnthropicStream(r.Context(), w, resp.Body, requestedModel, s.logger); err != nil && r.Context().Err() == nil {
			s.logger.Warn("anthropic_stream_failed", "err", err)
		}
		return
	}
	data, err := io.ReadAll(io.LimitReader(resp.Body, s.cfg.MaxBodyBuffer+1))
	if err != nil || int64(len(data)) > s.cfg.MaxBodyBuffer {
		writeAnthropicError(w, http.StatusBadGateway, "api_error", "upstream returned an unreadable non-stream response")
		return
	}
	var oaiResp oaiChatResponse
	if err := json.Unmarshal(data, &oaiResp); err != nil {
		writeAnthropicError(w, http.StatusBadGateway, "api_error", "decode upstream response: "+err.Error())
		return
	}
	out := buildAnthropicResponse(&oaiResp, requestedModel, s.logger)
	w.Header().Set("Content-Type", "text/event-stream; charset=utf-8")
	w.Header().Set("Cache-Control", "no-cache")
	w.WriteHeader(http.StatusOK)
	if err := synthesizeAnthropicStream(newSSEWriter(w), out); err != nil {
		s.logger.Warn("anthropic_stream_synthesize_failed", "err", err)
	}
}

// handleAnthropicCountTokens implements POST /v1/messages/count_tokens with a
// deterministic local estimate. It never contacts, loads, or swaps a backend,
// and — like the admin endpoints — must not touch inflight/inflightWG (it
// never occupies a backend, and staying out preserves unload drain semantics).
func (s *Server) handleAnthropicCountTokens(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.Header().Set("Allow", http.MethodPost)
		writeAnthropicError(w, http.StatusMethodNotAllowed, "invalid_request_error",
			"method "+r.Method+" not allowed on /v1/messages/count_tokens")
		return
	}
	req, aerr := s.decodeAnthropicBody(r)
	if aerr != nil {
		writeAnthropicAPIError(w, aerr)
		return
	}
	if base, _, ok := parseModelSuffix(strings.TrimSpace(req.Model)); ok {
		req.Model = base // a reasoning suffix does not affect the token estimate
	}
	profileID, aerr := validateAnthropicRequest(req, false)
	if aerr != nil {
		writeAnthropicAPIError(w, aerr)
		return
	}
	if _, err := s.deps.ProfileStore.Get(profileID); err != nil {
		if errors.Is(err, profilestore.ErrNotFound) || errors.Is(err, profilestore.ErrInvalidID) {
			writeAnthropicError(w, http.StatusNotFound, "not_found_error",
				fmt.Sprintf("model %q not found; list available models via GET /v1/models", profileID))
			return
		}
		writeAnthropicError(w, http.StatusInternalServerError, "api_error", "profile lookup: "+err.Error())
		return
	}
	// Real token count via tiktoken over the translated OpenAI request — still
	// fully local (never contacts the backend), mirroring CLIProxyAPI. Falls
	// back to the byte heuristic if translation or the tokenizer fails.
	count := estimateAnthropicTokens(req)
	if oaiReq, terr := translateAnthropicRequest(req, profileID); terr == nil {
		if enc, err := tokenizerForModel(profileID); err == nil {
			count = countChatTokens(enc, oaiReq)
		}
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(anthropicCountTokensResponse{InputTokens: count})
}
