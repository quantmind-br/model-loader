package cli

import (
	"sync"
	"sync/atomic"
	"testing"
)

func TestWithProfileLock_Serializes(t *testing.T) {
	// T6: flock serializes the critical sections but establishes no Go
	// happens-before, so a plain shared int would be a genuine data race under
	// -race. Assert mutual exclusion via atomic overlap detection instead: no
	// two goroutines may be inside the locked section at once, and fn must run
	// exactly 50 times.
	dir := t.TempDir()
	const id = "p1"
	var active int32
	var count int64
	var wg sync.WaitGroup
	for range 50 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_ = withProfileLock(dir, id, func() error {
				if !atomic.CompareAndSwapInt32(&active, 0, 1) {
					t.Error("concurrent entry into locked section")
				}
				atomic.AddInt64(&count, 1)
				atomic.StoreInt32(&active, 0)
				return nil
			})
		}()
	}
	wg.Wait()
	if count != 50 {
		t.Fatalf("lock ran fn %d times, want 50", count)
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
