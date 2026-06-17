package httpproxy

import (
	"encoding/json"
	"errors"
	"net/http"
)

// openAIError mirrors the OpenAI API error envelope so SDKs treat our
// failures as native upstream errors.
type openAIError struct {
	Message string `json:"message"`
	Type    string `json:"type"`
	Code    string `json:"code,omitempty"`
	Param   string `json:"param,omitempty"`
}

// writeOpenAIError serializes an OpenAI-shaped error envelope and writes it
// with the given HTTP status. Safe to call multiple times only if the caller
// hasn't already written headers.
func writeOpenAIError(w http.ResponseWriter, status int, errType, code, msg string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(map[string]any{
		"error": openAIError{Message: msg, Type: errType, Code: code},
	})
}

// SwapError is a typed failure from the backend-swap path. It carries the HTTP
// status plus the OpenAI-error envelope fields (type/code/message) so the swap
// path decides the client mapping once and a single writeSwapError renders it —
// no per-handler status switch.
type SwapError struct {
	StatusCode int
	ErrType    string
	Code       string
	Msg        string
}

func (e *SwapError) Error() string { return e.Msg }

// writeSwapError renders err as an OpenAI-shaped error response. SwapError
// values carry their own status/type/code; any other error falls back to a
// generic 500 (the swap path only ever returns *SwapError, so this is defense
// in depth).
func writeSwapError(w http.ResponseWriter, err error) {
	var se *SwapError
	if errors.As(err, &se) {
		writeOpenAIError(w, se.StatusCode, se.ErrType, se.Code, se.Msg)
		return
	}
	writeOpenAIError(w, http.StatusInternalServerError, "server_error", "swap_failed", err.Error())
}
