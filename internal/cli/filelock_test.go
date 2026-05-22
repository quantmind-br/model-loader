package cli

import (
	"sync"
	"testing"
)

func TestWithProfileLock_Serializes(t *testing.T) {
	dir := t.TempDir()
	const id = "p1"
	var counter int
	var wg sync.WaitGroup
	for i := 0; i < 50; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_ = withProfileLock(dir, id, func() error {
				c := counter
				c++
				counter = c
				return nil
			})
		}()
	}
	wg.Wait()
	if counter != 50 {
		t.Fatalf("lock failed to serialize: counter=%d want 50", counter)
	}
}

func TestWithProfileLock_PropagatesError(t *testing.T) {
	dir := t.TempDir()
	sentinel := errSentinel{}
	err := withProfileLock(dir, "p1", func() error { return sentinel })
	if err != sentinel {
		t.Fatalf("expected fn error to propagate, got %v", err)
	}
}

type errSentinel struct{}

func (errSentinel) Error() string { return "sentinel" }
