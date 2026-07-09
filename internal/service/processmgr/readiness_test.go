package processmgr

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"testing"
	"time"
)

var testKeyRe = regexp.MustCompile(`sk-unsloth-[0-9a-f]{32}`)

func TestWaitForLogToken_FindsKey(t *testing.T) {
	p := filepath.Join(t.TempDir(), "x.log")
	if err := os.WriteFile(p, []byte("starting...\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	go func() {
		time.Sleep(150 * time.Millisecond)
		f, _ := os.OpenFile(p, os.O_APPEND|os.O_WRONLY, 0o644)
		_, _ = f.WriteString("API Key:      sk-unsloth-0123456789abcdef0123456789abcdef\n")
		_ = f.Close()
	}()
	got, err := waitForLogToken(p, testKeyRe, 3*time.Second, os.Getpid())
	if err != nil {
		t.Fatalf("waitForLogToken: %v", err)
	}
	if got != "sk-unsloth-0123456789abcdef0123456789abcdef" {
		t.Fatalf("token = %q", got)
	}
}

func TestWaitForLogToken_Timeout(t *testing.T) {
	p := filepath.Join(t.TempDir(), "x.log")
	if err := os.WriteFile(p, []byte("no key here\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	_, err := waitForLogToken(p, testKeyRe, 300*time.Millisecond, os.Getpid())
	if err == nil {
		t.Fatal("expected timeout error")
	}
}

func TestWaitForLogToken_MissingFileTimesOut(t *testing.T) {
	missing := filepath.Join(t.TempDir(), "does-not-exist.log")
	_, err := waitForLogToken(missing, testKeyRe, 250*time.Millisecond, os.Getpid())
	if err == nil {
		t.Fatal("expected timeout error for missing file")
	}
	if !errors.Is(err, ErrReadyTimeout) {
		t.Fatalf("expected ErrReadyTimeout, got %v", err)
	}
}

func TestWaitForLogToken_DeadProcessFailsFast(t *testing.T) {
	p := filepath.Join(t.TempDir(), "x.log")
	if err := os.WriteFile(p, []byte("no key here\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command("sh", "-c", "exit 0")
	if err := cmd.Start(); err != nil {
		t.Fatalf("start doomed process: %v", err)
	}
	_ = cmd.Wait()
	deadPID := cmd.Process.Pid

	start := time.Now()
	_, err := waitForLogToken(p, testKeyRe, 5*time.Second, deadPID)
	elapsed := time.Since(start)

	if !errors.Is(err, ErrProcessExited) {
		t.Fatalf("err = %v, want ErrProcessExited", err)
	}
	if elapsed > time.Second {
		t.Fatalf("elapsed = %v, want fast failure (<1s)", elapsed)
	}
}

// TestWaitForLogToken_SplitAcrossAppends — audit C7: the token is written in
// two appends so the poll boundary can split it; the carry-tail must still
// match it.
func TestWaitForLogToken_SplitAcrossAppends(t *testing.T) {
	p := filepath.Join(t.TempDir(), "x.log")
	if err := os.WriteFile(p, []byte("boot\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	go func() {
		f, _ := os.OpenFile(p, os.O_APPEND|os.O_WRONLY, 0o644)
		_, _ = f.WriteString("API Key:      sk-unsloth-0123456789ab")
		_ = f.Sync()
		time.Sleep(250 * time.Millisecond)
		_, _ = f.WriteString("cdef0123456789abcdef\n")
		_ = f.Close()
	}()
	got, err := waitForLogToken(p, testKeyRe, 3*time.Second, os.Getpid())
	if err != nil {
		t.Fatalf("waitForLogToken: %v", err)
	}
	if got != "sk-unsloth-0123456789abcdef0123456789abcdef" {
		t.Fatalf("token = %q", got)
	}
}
