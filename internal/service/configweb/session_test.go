package configweb

import (
	"net/http"
	"testing"
	"time"
)

func TestSession_StartServesAndShutsDownOnSave(t *testing.T) {
	s := NewSession(Deps{}) // empty deps: /healthz still serves
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
	// Signal completion and assert the result channel delivers + server stops.
	s.complete(Result{Saved: false})
	select {
	case res := <-s.Done():
		if res.Saved {
			t.Fatalf("expected cancel result")
		}
	case <-time.After(2 * time.Second):
		t.Fatalf("timeout waiting for Done")
	}
	// Server shuts down asynchronously after complete(); poll briefly until it's down.
	down := false
	for i := 0; i < 50; i++ {
		if _, err := http.Get(url + "/healthz"); err != nil {
			down = true
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	if !down {
		t.Fatalf("server should be down after complete")
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
