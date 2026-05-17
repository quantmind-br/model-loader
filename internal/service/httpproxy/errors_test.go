package httpproxy

import (
	"encoding/json"
	"net/http/httptest"
	"testing"
)

func TestWriteOpenAIError_Shape(t *testing.T) {
	rr := httptest.NewRecorder()
	writeOpenAIError(rr, 503, "model_not_loaded", "no_model_loaded", "specify model")

	if got := rr.Code; got != 503 {
		t.Fatalf("status = %d, want 503", got)
	}
	if ct := rr.Header().Get("Content-Type"); ct != "application/json" {
		t.Fatalf("Content-Type = %q, want application/json", ct)
	}

	var body struct {
		Error struct {
			Message string `json:"message"`
			Type    string `json:"type"`
			Code    string `json:"code"`
		} `json:"error"`
	}
	if err := json.NewDecoder(rr.Body).Decode(&body); err != nil {
		t.Fatalf("decode body: %v", err)
	}
	if body.Error.Type != "model_not_loaded" {
		t.Errorf("type = %q, want model_not_loaded", body.Error.Type)
	}
	if body.Error.Code != "no_model_loaded" {
		t.Errorf("code = %q, want no_model_loaded", body.Error.Code)
	}
	if body.Error.Message != "specify model" {
		t.Errorf("message = %q, want 'specify model'", body.Error.Message)
	}
}
