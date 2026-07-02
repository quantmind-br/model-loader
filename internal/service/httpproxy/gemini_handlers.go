package httpproxy

import (
	"encoding/json"
	"io"
	"net/http"
	"strings"

	"github.com/quantmind-br/model-loader/internal/domain"
)

// handleGeminiModels implements GET /v1beta/models — the profile list in the
// Gemini model shape.
func (s *Server) handleGeminiModels(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		w.Header().Set("Allow", http.MethodGet)
		writeGeminiError(w, http.StatusMethodNotAllowed, "method "+r.Method+" not allowed on /v1beta/models")
		return
	}
	profiles, diags, err := s.deps.ProfileStore.ListWithDiagnostics()
	if err != nil {
		writeGeminiError(w, http.StatusInternalServerError, err.Error())
		return
	}
	for _, d := range diags {
		s.logger.Warn("gemini_models_skipped_corrupt", "profile_id", d.ID, "err", d.Err)
	}
	out := geminiModelsList{Models: make([]geminiModel, 0, len(profiles))}
	for _, p := range profiles {
		out.Models = append(out.Models, buildGeminiModel(p))
	}
	writeJSONOK(w, out)
}

// handleGeminiAction implements /v1beta/models/{model}:{method} (and
// GET /v1beta/models/{model}). The {model} may carry a reasoning suffix.
func (s *Server) handleGeminiAction(w http.ResponseWriter, r *http.Request) {
	rest := strings.TrimPrefix(r.URL.Path, "/v1beta/models/")
	if rest == "" {
		writeGeminiError(w, http.StatusNotFound, "missing model")
		return
	}
	modelSpec, method, hasMethod := strings.Cut(rest, ":")
	if !hasMethod {
		if r.Method == http.MethodGet {
			s.serveGeminiModel(w, modelSpec)
			return
		}
		writeGeminiError(w, http.StatusNotFound, "missing method (expected {model}:generateContent|streamGenerateContent|countTokens)")
		return
	}
	switch method {
	case "generateContent":
		s.handleGeminiGenerate(w, r, modelSpec, false)
	case "streamGenerateContent":
		s.handleGeminiGenerate(w, r, modelSpec, true)
	case "countTokens":
		s.handleGeminiCountTokens(w, r, modelSpec)
	default:
		writeGeminiError(w, http.StatusNotFound, "unknown method "+method)
	}
}

func (s *Server) serveGeminiModel(w http.ResponseWriter, modelSpec string) {
	profileID := normalizeRequestModel(strings.TrimSpace(modelSpec))
	p, err := s.deps.ProfileStore.Get(profileID)
	if err != nil {
		writeGeminiError(w, http.StatusNotFound, "model "+profileID+" not found")
		return
	}
	writeJSONOK(w, buildGeminiModel(p))
}

func (s *Server) handleGeminiGenerate(w http.ResponseWriter, r *http.Request, modelSpec string, stream bool) {
	if r.Method != http.MethodPost {
		w.Header().Set("Allow", http.MethodPost)
		writeGeminiError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	s.inflight.Add(1)
	s.inflightWG.Add(1)
	defer func() {
		s.inflight.Add(-1)
		s.inflightWG.Done()
	}()

	req, profileID, suffix, hasSuffix, gerr := s.decodeGeminiRequest(r, modelSpec)
	if gerr != nil {
		writeGeminiError(w, gerr.status, gerr.message)
		return
	}
	loaded, err := s.ensureLoaded(r.Context(), profileID)
	if err != nil {
		gerr := anthropicErrorFromSwap(err)
		writeGeminiError(w, gerr.Status, gerr.Message)
		return
	}
	oaiReq, terr := translateGeminiRequest(req, loaded.profileID, stream)
	if terr != nil {
		writeGeminiError(w, http.StatusBadRequest, terr.Error())
		return
	}
	if hasSuffix {
		applyReasoningOverride(oaiReq, suffix)
	}

	resp, aerr := s.postUpstreamChat(r.Context(), loaded, oaiReq)
	if aerr != nil {
		writeGeminiError(w, aerr.Status, aerr.Message)
		return
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		ue := anthropicErrorFromUpstreamStatus(resp, s.cfg.MaxBodyBuffer)
		writeGeminiError(w, ue.Status, ue.Message)
		return
	}

	if stream {
		s.serveGeminiStream(w, r, resp, modelSpec)
		return
	}
	s.serveGeminiJSON(w, resp, modelSpec)
}

func (s *Server) handleGeminiCountTokens(w http.ResponseWriter, r *http.Request, modelSpec string) {
	if r.Method != http.MethodPost {
		w.Header().Set("Allow", http.MethodPost)
		writeGeminiError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	req, profileID, _, _, gerr := s.decodeGeminiRequest(r, modelSpec)
	if gerr != nil {
		writeGeminiError(w, gerr.status, gerr.message)
		return
	}
	if _, err := s.deps.ProfileStore.Get(profileID); err != nil {
		writeGeminiError(w, http.StatusNotFound, "model "+profileID+" not found; list models via GET /v1beta/models")
		return
	}
	// Local tiktoken count over the translated OpenAI request (never contacts
	// the backend).
	total := 0
	if oaiReq, terr := translateGeminiRequest(req, profileID, false); terr == nil {
		if enc, err := tokenizerForModel(profileID); err == nil {
			total = countChatTokens(enc, oaiReq)
		}
	}
	writeJSONOK(w, geminiCountTokensResponse{
		TotalTokens:         total,
		PromptTokensDetails: []geminiModalityTokenCount{{Modality: "TEXT", TokenCount: total}},
	})
}

type geminiError struct {
	status  int
	message string
}

// decodeGeminiRequest reads/validates the body and resolves the URL model
// (stripping a reasoning suffix).
func (s *Server) decodeGeminiRequest(r *http.Request, modelSpec string) (*geminiRequest, string, string, bool, *geminiError) {
	if r.Body == nil || r.Body == http.NoBody {
		return nil, "", "", false, &geminiError{http.StatusBadRequest, "request body required"}
	}
	defer r.Body.Close()
	data, err := io.ReadAll(io.LimitReader(r.Body, s.cfg.MaxBodyBuffer+1))
	if err != nil {
		return nil, "", "", false, &geminiError{http.StatusBadRequest, "read body: " + err.Error()}
	}
	if int64(len(data)) > s.cfg.MaxBodyBuffer {
		return nil, "", "", false, &geminiError{http.StatusRequestEntityTooLarge, "request body exceeds the proxy limit"}
	}
	var req geminiRequest
	if err := json.Unmarshal(data, &req); err != nil {
		return nil, "", "", false, &geminiError{http.StatusBadRequest, "invalid JSON body: " + err.Error()}
	}
	base, suffix, hasSuffix := parseModelSuffix(strings.TrimSpace(modelSpec))
	profileID := normalizeRequestModel(base)
	if profileID == "" || !validProfileID(profileID) {
		return nil, "", "", false, &geminiError{http.StatusBadRequest, "invalid model id in URL: " + modelSpec}
	}
	return &req, profileID, suffix, hasSuffix, nil
}

func (s *Server) serveGeminiJSON(w http.ResponseWriter, resp *http.Response, model string) {
	data, err := io.ReadAll(io.LimitReader(resp.Body, s.cfg.MaxBodyBuffer+1))
	if err != nil {
		writeGeminiError(w, http.StatusBadGateway, "read upstream response: "+err.Error())
		return
	}
	var oaiResp oaiChatResponse
	if err := json.Unmarshal(data, &oaiResp); err != nil {
		writeGeminiError(w, http.StatusBadGateway, "decode upstream response: "+err.Error())
		return
	}
	writeJSONOK(w, buildGeminiResponse(&oaiResp, model))
}

func (s *Server) serveGeminiStream(w http.ResponseWriter, r *http.Request, resp *http.Response, model string) {
	ct := strings.ToLower(resp.Header.Get("Content-Type"))
	if strings.Contains(ct, "text/event-stream") {
		if err := runGeminiStream(r.Context(), w, resp.Body, model, s.logger); err != nil && r.Context().Err() == nil {
			s.logger.Warn("gemini_stream_failed", "err", err)
		}
		return
	}
	data, err := io.ReadAll(io.LimitReader(resp.Body, s.cfg.MaxBodyBuffer+1))
	if err != nil {
		writeGeminiError(w, http.StatusBadGateway, "unreadable non-stream upstream response")
		return
	}
	var oaiResp oaiChatResponse
	if err := json.Unmarshal(data, &oaiResp); err != nil {
		writeGeminiError(w, http.StatusBadGateway, "decode upstream response: "+err.Error())
		return
	}
	w.Header().Set("Content-Type", "text/event-stream; charset=utf-8")
	w.Header().Set("Cache-Control", "no-cache")
	w.WriteHeader(http.StatusOK)
	if err := synthesizeGeminiStream(w, &oaiResp, model); err != nil {
		s.logger.Warn("gemini_stream_synthesize_failed", "err", err)
	}
}

func buildGeminiModel(p domain.Profile) geminiModel {
	m := geminiModel{
		Name:                       "models/" + p.ID,
		DisplayName:                p.Name,
		SupportedGenerationMethods: []string{"generateContent", "streamGenerateContent", "countTokens"},
	}
	if ctx := deriveContextLength(p.Args); ctx != nil {
		m.InputTokenLimit = *ctx
	}
	return m
}

func writeGeminiError(w http.ResponseWriter, status int, message string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(map[string]any{
		"error": map[string]any{"code": status, "message": message, "status": geminiStatusName(status)},
	})
}

func geminiStatusName(status int) string {
	switch status {
	case http.StatusBadRequest:
		return "INVALID_ARGUMENT"
	case http.StatusNotFound:
		return "NOT_FOUND"
	case http.StatusMethodNotAllowed:
		return "INVALID_ARGUMENT"
	case http.StatusRequestEntityTooLarge:
		return "PAYLOAD_TOO_LARGE"
	case http.StatusTooManyRequests:
		return "RESOURCE_EXHAUSTED"
	case http.StatusBadGateway, http.StatusServiceUnavailable:
		return "UNAVAILABLE"
	default:
		return "INTERNAL"
	}
}

func writeJSONOK(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(v)
}
