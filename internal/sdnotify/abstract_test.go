package sdnotify

import (
	"net"
	"os"
	"testing"
	"time"
)

func TestAbstractSocket(t *testing.T) {
	pc, err := net.ListenPacket("unixgram", "@model-loader-test-abs")
	if err != nil {
		t.Skipf("abstract sockets unavailable: %v", err)
	}
	defer pc.Close()
	got := make(chan string, 1)
	go func() {
		buf := make([]byte, 4096)
		_ = pc.SetReadDeadline(time.Now().Add(2 * time.Second))
		n, _, _ := pc.ReadFrom(buf)
		got <- string(buf[:n])
	}()
	_ = os.Setenv("NOTIFY_SOCKET", "@model-loader-test-abs")
	defer os.Unsetenv("NOTIFY_SOCKET")
	if err := NotifyState("READY=1"); err != nil {
		t.Fatalf("NotifyState: %v", err)
	}
	if s := <-got; s != "READY=1" {
		t.Fatalf("got %q", s)
	}
}
