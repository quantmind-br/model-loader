package httpproxy

import (
	"encoding/json"
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
