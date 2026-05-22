package benchmark

import "testing"

func TestRegistry_LookupUnknown(t *testing.T) {
	if _, ok := handlerFor(Mode("does-not-exist")); ok {
		t.Fatal("handlerFor(unknown) returned ok=true")
	}
}
