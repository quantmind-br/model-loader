package benchmark

import (
	"math/rand/v2"
	"sort"
)

// sampleIDs returns a deterministic n-element subset of ids: the input is
// sorted, shuffled with a seeded PRNG, truncated to n, and re-sorted, so the
// same (ids, n, seed) always selects the same subset regardless of input
// order. n <= 0 or n >= len(ids) returns every id, sorted.
func sampleIDs(ids []string, n int, seed int64) []string {
	out := make([]string, len(ids))
	copy(out, ids)
	sort.Strings(out)
	if n <= 0 || n >= len(out) {
		return out
	}
	rng := rand.New(rand.NewPCG(uint64(seed), 0))
	rng.Shuffle(len(out), func(i, j int) { out[i], out[j] = out[j], out[i] })
	out = out[:n]
	sort.Strings(out)
	return out
}
