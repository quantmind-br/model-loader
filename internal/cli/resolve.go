package cli

import (
	"errors"
	"strings"
)

// errResolveNotFound is the sentinel resolveByPrefix returns when ref matches no
// item by exact key or unique prefix. Callers wrap it with a domain-specific
// "not found" message.
var errResolveNotFound = errors.New("not found")

// ambiguousMatchError reports that ref matched more than one item — by exact key
// or by prefix. It carries the matched items so each caller can format its own
// candidate list and hint (PIDs, ids, etc.).
type ambiguousMatchError[T any] struct {
	Matches []T
}

func (e *ambiguousMatchError[T]) Error() string { return "ambiguous reference" }

// resolveByPrefix matches ref against items by the keys each item exposes.
// A case-insensitive exact match on any key wins; with no exact match, items
// whose key has ref as a case-insensitive prefix are considered. Exactly one
// match (exact, else prefix) is returned. Zero matches yields errResolveNotFound;
// more than one yields *ambiguousMatchError[T] carrying the candidates.
func resolveByPrefix[T any](items []T, ref string, keys func(T) []string) (T, error) {
	var zero T
	lref := strings.ToLower(ref)
	var exact, prefix []T
	for _, it := range items {
		isExact, isPrefix := false, false
		for _, k := range keys(it) {
			lk := strings.ToLower(k)
			if lk == lref {
				isExact = true
				break
			}
			if strings.HasPrefix(lk, lref) {
				isPrefix = true
			}
		}
		switch {
		case isExact:
			exact = append(exact, it)
		case isPrefix:
			prefix = append(prefix, it)
		}
	}
	candidates := exact
	if len(candidates) == 0 {
		candidates = prefix
	}
	switch len(candidates) {
	case 1:
		return candidates[0], nil
	case 0:
		return zero, errResolveNotFound
	default:
		return zero, &ambiguousMatchError[T]{Matches: candidates}
	}
}
