package sdnotify

import (
	"fmt"
	"net"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

func withEnv(t *testing.T, k, v string) {
	t.Helper()
	old, had := os.LookupEnv(k)
	t.Cleanup(func() {
		if had {
			_ = os.Setenv(k, old)
		} else {
			_ = os.Unsetenv(k)
		}
	})
	_ = os.Setenv(k, v)
}

// listenDatagram binds a unixgram socket at path (filesystem) and collects
// received payloads.
func listenDatagram(t *testing.T, path string) (received *[]string, mu *sync.Mutex) {
	t.Helper()
	var got []string
	var m sync.Mutex
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
			m.Lock()
			got = append(got, string(buf[:n]))
			m.Unlock()
		}
	}()
	return &got, &m
}

func waitFor(t *testing.T, got *[]string, mu *sync.Mutex, want string) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		mu.Lock()
		for _, s := range *got {
			if s == want {
				mu.Unlock()
				return
			}
		}
		mu.Unlock()
		time.Sleep(10 * time.Millisecond)
	}
	mu.Lock()
	defer mu.Unlock()
	t.Fatalf("never received %q; got %v", want, *got)
}

func TestNotifyState_SendsPayload(t *testing.T) {
	sock := filepath.Join(t.TempDir(), "notify.sock")
	got, mu := listenDatagram(t, sock)
	withEnv(t, "NOTIFY_SOCKET", sock)

	if err := NotifyState("READY=1"); err != nil {
		t.Fatalf("NotifyState: %v", err)
	}
	waitFor(t, got, mu, "READY=1")
}

func TestNotifyState_MissingSocketIsNoop(t *testing.T) {
	withEnv(t, "NOTIFY_SOCKET", "")
	if err := NotifyState("READY=1"); err != nil {
		t.Fatalf("unset NOTIFY_SOCKET must be no-op, got: %v", err)
	}
	if err := Ready(); err != nil {
		t.Fatalf("Ready: %v", err)
	}
	if err := Watchdog(); err != nil {
		t.Fatalf("Watchdog: %v", err)
	}
	if err := Stopping(); err != nil {
		t.Fatalf("Stopping: %v", err)
	}
	if err := Status("listening"); err != nil {
		t.Fatalf("Status: %v", err)
	}
	if Enabled() {
		t.Fatal("Enabled must be false without NOTIFY_SOCKET")
	}
}

func TestNotifyState_UnreachableSocketErrors(t *testing.T) {
	withEnv(t, "NOTIFY_SOCKET", filepath.Join(t.TempDir(), "nope.sock"))
	if err := NotifyState("READY=1"); err == nil {
		t.Fatal("expected error for unreachable socket")
	}
}

func TestEnabled(t *testing.T) {
	withEnv(t, "NOTIFY_SOCKET", "/run/whatever.sock")
	if !Enabled() {
		t.Fatal("Enabled must be true with NOTIFY_SOCKET set")
	}
}

func TestWatchdogInterval(t *testing.T) {
	pid := os.Getpid()
	other := pid + 1000000

	cases := []struct {
		name   string
		usec   string
		pid    string
		want   time.Duration
		wantOK bool
	}{
		{"unset", "", "", 0, false},
		{"zero", "0", "", 0, false},
		{"garbage", "abc", "", 0, false},
		{"plain", "15000000", "", 15 * time.Second, true},
		{"spaces", "  15000000  ", "", 15 * time.Second, true},
		{"matching_pid", "15000000", fmt.Sprint(pid), 15 * time.Second, true},
		{"other_pid", "15000000", fmt.Sprint(other), 0, false},
		{"bad_pid", "15000000", "xyz", 0, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			withEnv(t, "WATCHDOG_USEC", tc.usec)
			withEnv(t, "WATCHDOG_PID", tc.pid)
			got, ok := WatchdogInterval()
			if ok != tc.wantOK || got != tc.want {
				t.Fatalf("WatchdogInterval() = (%v, %v), want (%v, %v)", got, ok, tc.want, tc.wantOK)
			}
		})
	}
}

func TestHalfIntervalAndProbeTimeout(t *testing.T) {
	if got := HalfInterval(15 * time.Second); got != 7500*time.Millisecond {
		t.Fatalf("HalfInterval(15s) = %v", got)
	}
	if got := HalfInterval(500 * time.Millisecond); got != time.Second {
		t.Fatalf("tiny interval must floor at 1s, got %v", got)
	}
	if got := ProbeTimeout(15 * time.Second); got != 3750*time.Millisecond {
		t.Fatalf("ProbeTimeout(15s) = %v", got)
	}
	if got := ProbeTimeout(time.Second); got != time.Second {
		t.Fatalf("ProbeTimeout floor = %v", got)
	}
	if got := ProbeTimeout(time.Hour); got != 5*time.Second {
		t.Fatalf("ProbeTimeout ceiling = %v", got)
	}
}

func TestStatusPayload(t *testing.T) {
	sock := filepath.Join(t.TempDir(), "notify.sock")
	got, mu := listenDatagram(t, sock)
	withEnv(t, "NOTIFY_SOCKET", sock)

	if err := Status("serving on 127.0.0.1:4321"); err != nil {
		t.Fatal(err)
	}
	waitFor(t, got, mu, "STATUS=serving on 127.0.0.1:4321")
	mu.Lock()
	defer mu.Unlock()
	if !strings.HasPrefix((*got)[0], "STATUS=") {
		t.Fatalf("payload = %q, want STATUS= prefix", (*got)[0])
	}
}
