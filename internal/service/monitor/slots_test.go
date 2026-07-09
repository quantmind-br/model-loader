package monitor

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestSlotsPoller_EmitsSnapshots(t *testing.T) {
	// Fake llama-server.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/slots":
			body := []map[string]any{
				{"id": 0, "state": "idle", "n_ctx": 0, "n_ctx_total": 4096, "n_decoded": 0, "n_prompt": 0, "id_task": ""},
				{"id": 1, "state": "processing", "n_ctx": 128, "n_ctx_total": 4096, "n_decoded": 64, "n_prompt": 32, "id_task": "abc"},
			}
			_ = json.NewEncoder(w).Encode(body)
		case "/health":
			_, _ = w.Write([]byte(`{"status":"ok"}`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	out := make(chan MonitorEvent, 16)
	p := newSlotsPoller(srv.URL, http.DefaultClient, 50*time.Millisecond, out)
	go p.run(ctx)

	gotSlots, gotHealth := false, false
	deadline := time.After(2 * time.Second)
	for !gotSlots || !gotHealth {
		select {
		case ev := <-out:
			switch ev.Source {
			case SourceSlots:
				snap, ok := ev.Data.(SlotSnapshot)
				if !ok {
					t.Fatalf("Slots Data type = %T, want SlotSnapshot", ev.Data)
				}
				if len(snap.Slots) != 2 {
					t.Fatalf("slot count = %d, want 2", len(snap.Slots))
				}
				if snap.Slots[1].State != "processing" {
					t.Fatalf("slot[1].State = %q", snap.Slots[1].State)
				}
				gotSlots = true
			case SourceHealth:
				h, ok := ev.Data.(HealthStatus)
				if !ok {
					t.Fatalf("Health Data type = %T, want HealthStatus", ev.Data)
				}
				if !h.OK {
					t.Fatalf("Health.OK = false")
				}
				gotHealth = true
			}
		case <-deadline:
			t.Fatalf("timeout — slots=%v health=%v", gotSlots, gotHealth)
		}
	}
}

func TestSlotsPoller_ServerErrorEmitsHealthDown(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "boom", http.StatusInternalServerError)
	}))
	defer srv.Close()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	out := make(chan MonitorEvent, 8)
	p := newSlotsPoller(srv.URL, http.DefaultClient, 50*time.Millisecond, out)
	go p.run(ctx)

	deadline := time.After(2 * time.Second)
	for {
		select {
		case ev := <-out:
			if ev.Source == SourceHealth {
				h := ev.Data.(HealthStatus)
				if h.OK {
					t.Fatalf("expected Health.OK=false on 500 response")
				}
				return
			}
		case <-deadline:
			t.Fatal("timeout waiting for unhealthy event")
		}
	}
}

// TestSlotsPoller_DefaultClientTimesOut guards audit N-P3: the default poller
// client must carry a timeout so a backend that accepts the connection but
// never answers cannot wedge the poller for the subscription's lifetime.
func TestSlotsPoller_DefaultClientTimesOut(t *testing.T) {
	block := make(chan struct{})
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		<-block // never responds while the test runs
	}))
	defer srv.Close()
	defer close(block)

	out := make(chan MonitorEvent, 8)
	p := newSlotsPoller(srv.URL, nil, time.Second, out) // nil client → 5s timeout

	start := time.Now()
	done := make(chan struct{})
	go func() {
		p.fetchHealth(context.Background())
		close(done)
	}()

	select {
	case <-done:
	case <-time.After(10 * time.Second):
		t.Fatal("fetchHealth did not return; default client has no timeout")
	}
	if elapsed := time.Since(start); elapsed > 8*time.Second {
		t.Fatalf("fetchHealth took %v; expected ~5s client timeout", elapsed)
	}

	select {
	case ev := <-out:
		if ev.Source != SourceHealth {
			t.Fatalf("event source = %v, want SourceHealth", ev.Source)
		}
		h := ev.Data.(HealthStatus)
		if h.OK || h.Status != "unreachable" {
			t.Fatalf("health = %+v, want unreachable", h)
		}
	default:
		t.Fatal("no health event emitted after timeout")
	}
}