package processmgr

import (
	"os"
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
	got, err := waitForLogToken(p, testKeyRe, 3*time.Second)
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
	_, err := waitForLogToken(p, testKeyRe, 300*time.Millisecond)
	if err == nil {
		t.Fatal("expected timeout error")
	}
}
