package benchmark

import (
	"reflect"
	"sort"
	"testing"
)

func TestSampleIDs(t *testing.T) {
	base := []string{"d", "b", "a", "c", "e"}

	tests := []struct {
		name string
		ids  []string
		n    int
		seed int64
	}{
		{name: "subset", ids: base, n: 2, seed: 0},
		{name: "subset other seed", ids: base, n: 3, seed: 7},
		{name: "n equals len", ids: base, n: len(base), seed: 0},
		{name: "n over len", ids: base, n: 99, seed: 0},
		{name: "n zero", ids: base, n: 0, seed: 0},
		{name: "n negative", ids: base, n: -1, seed: 0},
	}

	inSet := func(id string, ids []string) bool {
		for _, x := range ids {
			if x == id {
				return true
			}
		}
		return false
	}
	isSorted := func(ids []string) bool {
		return sort.SliceIsSorted(ids, func(i, j int) bool { return ids[i] < ids[j] })
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := sampleIDs(tt.ids, tt.n, tt.seed)

			// Determinism: same (ids, n, seed) twice → identical result.
			again := sampleIDs(tt.ids, tt.n, tt.seed)
			if !reflect.DeepEqual(got, again) {
				t.Fatalf("non-deterministic: %v != %v", got, again)
			}

			// Result is always sorted.
			if !isSorted(got) {
				t.Fatalf("result not sorted: %v", got)
			}

			// Every element is drawn from the input.
			for _, id := range got {
				if !inSet(id, tt.ids) {
					t.Fatalf("element %q not in input %v", id, tt.ids)
				}
			}

			// Length: capped at len for n<=0 or n>=len, else exactly n.
			wantLen := tt.n
			if tt.n <= 0 || tt.n >= len(tt.ids) {
				wantLen = len(tt.ids)
			}
			if len(got) != wantLen {
				t.Fatalf("len = %d, want %d", len(got), wantLen)
			}

			// Order-independence: shuffled input yields the same subset.
			shuffled := []string{tt.ids[len(tt.ids)-1]}
			shuffled = append(shuffled, tt.ids[:len(tt.ids)-1]...)
			fromShuffled := sampleIDs(shuffled, tt.n, tt.seed)
			if !reflect.DeepEqual(got, fromShuffled) {
				t.Fatalf("order-dependent: %v != %v (from %v)", got, fromShuffled, shuffled)
			}
		})
	}
}
