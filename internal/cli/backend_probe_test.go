package cli

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/quantmind-br/model-loader/internal/service/backendcatalog"
)

func probeEvents() *fakeProber {
	return &fakeProber{events: []backendcatalog.ProbeEvent{
		{BackendID: "llama-server", Status: backendcatalog.ProbeStatusOK, Latency: 12 * time.Millisecond, Detail: "v7376"},
		{BackendID: "vllm-main", Status: backendcatalog.ProbeStatusErr, Latency: 0, Detail: "not found", Err: errBoom},
		{Done: true},
	}}
}

var errBoom = errBoomT{}

type errBoomT struct{}

func (errBoomT) Error() string { return "boom" }

func TestProbeBackends_TableAllSkipsDone(t *testing.T) {
	var out bytes.Buffer
	if err := probeBackends(&out, probeEvents(), "", false); err != nil {
		t.Fatalf("probeBackends: %v", err)
	}
	s := out.String()
	if !strings.Contains(s, "llama-server") || !strings.Contains(s, "vllm-main") {
		t.Fatalf("missing rows: %q", s)
	}
	if !strings.Contains(s, "OK") || !strings.Contains(s, "ERR") {
		t.Fatalf("missing statuses: %q", s)
	}
}

func TestProbeBackends_FilterByID(t *testing.T) {
	var out bytes.Buffer
	if err := probeBackends(&out, probeEvents(), "vllm-main", false); err != nil {
		t.Fatalf("probeBackends: %v", err)
	}
	s := out.String()
	if strings.Contains(s, "llama-server") {
		t.Fatalf("should not include llama-server when filtered: %q", s)
	}
	if !strings.Contains(s, "vllm-main") {
		t.Fatalf("filtered backend missing: %q", s)
	}
}

func TestProbeBackends_JSON(t *testing.T) {
	var out bytes.Buffer
	if err := probeBackends(&out, probeEvents(), "", true); err != nil {
		t.Fatalf("probeBackends: %v", err)
	}
	var items []probeItem
	if err := json.Unmarshal(out.Bytes(), &items); err != nil {
		t.Fatalf("unmarshal: %v\n%s", err, out.String())
	}
	if len(items) != 2 {
		t.Fatalf("want 2 probe items (Done skipped), got %d: %+v", len(items), items)
	}
	if items[1].Error == "" {
		t.Fatalf("error backend should carry error string: %+v", items[1])
	}
}
