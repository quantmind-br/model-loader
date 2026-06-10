package proxysupervisor

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"

	"github.com/quantmind-br/model-loader/internal/service/httpproxy"
)

// BaseURL returns the proxy's HTTP root, e.g. "http://127.0.0.1:4321". It is
// derived from configuration, so it is valid even before the proxy starts.
func (s *Supervisor) BaseURL() string {
	return "http://" + net.JoinHostPort(s.host, fmt.Sprintf("%d", s.port))
}

// EnsureRunning starts the proxy when it is not already alive. Unlike Start,
// an already-running proxy is a success, not an error.
//
// The TOCTOU window between Status and Start is closed by treating
// ErrAlreadyRunning from Start as success: a concurrent caller that raced
// through Start first wins, and the proxy is running as intended.
func (s *Supervisor) EnsureRunning(ctx context.Context) error {
	if err := s.Start(ctx); err != nil && !errors.Is(err, ErrAlreadyRunning) {
		return err
	}
	return nil
}

// Load asks the proxy to swap in profileID via POST /_admin/load. It blocks
// until the backend is healthy — the proxy only answers after its health
// check — so callers should pass a ctx with a generous deadline (model load
// can take minutes).
func (s *Supervisor) Load(ctx context.Context, profileID string) (httpproxy.Status, error) {
	body, _ := json.Marshal(map[string]string{"profile_id": profileID})
	return s.adminPost(ctx, s.BaseURL()+"/_admin/load", bytes.NewReader(body))
}

// Unload asks the proxy to kill the loaded backend via POST /_admin/unload.
// force=true skips draining in-flight requests. Idempotent.
func (s *Supervisor) Unload(ctx context.Context, force bool) (httpproxy.Status, error) {
	url := s.BaseURL() + "/_admin/unload"
	if force {
		url += "?force=true"
	}
	return s.adminPost(ctx, url, nil)
}

func (s *Supervisor) adminPost(ctx context.Context, url string, body io.Reader) (httpproxy.Status, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, body)
	if err != nil {
		return httpproxy.Status{}, err
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	// No client timeout: /_admin/load legitimately blocks for the whole model
	// load. Cancellation is the caller's ctx.
	resp, err := http.DefaultTransport.RoundTrip(req)
	if err != nil {
		return httpproxy.Status{}, fmt.Errorf("proxy admin request: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		var e struct {
			Error struct {
				Message string `json:"message"`
			} `json:"error"`
		}
		_ = json.NewDecoder(resp.Body).Decode(&e)
		if e.Error.Message == "" {
			e.Error.Message = resp.Status
		}
		return httpproxy.Status{}, fmt.Errorf("proxy: %s", e.Error.Message)
	}
	var st httpproxy.Status
	if err := json.NewDecoder(resp.Body).Decode(&st); err != nil {
		return httpproxy.Status{}, fmt.Errorf("decode proxy status: %w", err)
	}
	return st, nil
}
