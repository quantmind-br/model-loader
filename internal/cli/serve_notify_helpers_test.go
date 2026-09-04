package cli

import (
	"log/slog"
	"net"
	"strconv"
	"sync"
	"testing"
	"time"

	"github.com/quantmind-br/model-loader/internal/log"
)

func testServeLogger() *slog.Logger {
	return log.Nop()
}

func splitHostPort(t *testing.T, addr string) (string, int) {
	t.Helper()
	host, portStr, err := net.SplitHostPort(addr)
	if err != nil {
		t.Fatal(err)
	}
	port, err := strconv.Atoi(portStr)
	if err != nil {
		t.Fatal(err)
	}
	return host, port
}

// datagramCollector gathers unixgram payloads under a mutex shared with the
// waiter, so no sleep-masked race exists between the reader goroutine and
// the polling loop.
type datagramCollector struct {
	mu  sync.Mutex
	got []string
}

func (c *datagramCollector) add(s string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.got = append(c.got, s)
}

func (c *datagramCollector) has(want string) bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	for _, s := range c.got {
		if s == want {
			return true
		}
	}
	return false
}

func (c *datagramCollector) snapshot() []string {
	c.mu.Lock()
	defer c.mu.Unlock()
	return append([]string(nil), c.got...)
}

// collectDatagrams binds a unixgram socket at path and collects payloads.
func collectDatagrams(t *testing.T, path string) *datagramCollector {
	t.Helper()
	c := &datagramCollector{}
	pc, err := net.ListenPacket("unixgram", path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = pc.Close() })
	go func() {
		buf := make([]byte, 4096)
		for {
			n, _, err := pc.ReadFrom(buf)
			if err != nil {
				return
			}
			c.add(string(buf[:n]))
		}
	}()
	return c
}

func waitForPayload(t *testing.T, c *datagramCollector, want string) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if c.has(want) {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatalf("never received %q; got %v", want, c.snapshot())
}
