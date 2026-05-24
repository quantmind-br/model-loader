package configweb

import (
	"io"
	"net/http"
	"strings"
	"testing"
	"time"
)

func TestSession_StaysUpForDonePageThenShutsDown(t *testing.T) {
	s := NewSession(Deps{}) // empty deps: /healthz and /closed still serve
	url, err := s.Start()
	if err != nil {
		t.Fatalf("start: %v", err)
	}
	resp, err := http.Get(url + "/healthz")
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	resp.Body.Close()
	if resp.StatusCode != 200 {
		t.Fatalf("healthz status %d", resp.StatusCode)
	}

	// Completion must deliver the result WITHOUT tearing the server down: the
	// browser still has to fetch /closed (the done page) after the HX-Redirect.
	s.complete(Result{Saved: false})
	select {
	case res := <-s.Done():
		if res.Saved {
			t.Fatalf("expected cancel result")
		}
	case <-time.After(2 * time.Second):
		t.Fatalf("timeout waiting for Done")
	}

	// The server must still be reachable so /closed does not ERR_CONNECTION_REFUSED.
	dresp, err := http.Get(url + "/closed")
	if err != nil {
		t.Fatalf("/closed must be served after complete, got: %v", err)
	}
	body, _ := io.ReadAll(dresp.Body)
	dresp.Body.Close()
	if dresp.StatusCode != 200 || !strings.Contains(string(body), "All set") {
		t.Fatalf("/closed should serve the done page, got %d: %s", dresp.StatusCode, body)
	}

	// Serving /closed requests shutdown; the server goes down shortly after.
	down := false
	for range 100 {
		if _, err := http.Get(url + "/healthz"); err != nil {
			down = true
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	if !down {
		t.Fatalf("server should shut down after /closed is served")
	}
}

func TestSession_CancelDeliversNotSaved(t *testing.T) {
	s := NewSession(Deps{})
	if _, err := s.Start(); err != nil {
		t.Fatalf("start: %v", err)
	}
	s.Cancel()
	select {
	case res := <-s.Done():
		if res.Saved {
			t.Fatalf("Cancel should deliver Saved=false")
		}
	case <-time.After(2 * time.Second):
		t.Fatalf("timeout")
	}
}
