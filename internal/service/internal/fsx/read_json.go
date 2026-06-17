package fsx

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
)

// ErrInvalidJSON wraps an unmarshal failure from ReadJSON so callers can
// distinguish a corrupt file (errors.Is == ErrInvalidJSON) from a missing or
// unreadable one (errors.Is == fs.ErrNotExist / a raw I/O error).
var ErrInvalidJSON = errors.New("invalid json")

// ReadJSON reads path and unmarshals it into T. A missing/unreadable file
// surfaces the raw os error (errors.Is(err, fs.ErrNotExist) for a missing file);
// a parse failure is wrapped with ErrInvalidJSON. On any error the zero T is
// returned. This is the read counterpart to WriteJSONAtomic.
func ReadJSON[T any](path string) (T, error) {
	var v T
	data, err := os.ReadFile(path)
	if err != nil {
		return v, err
	}
	if err := json.Unmarshal(data, &v); err != nil {
		return v, fmt.Errorf("%w: %v", ErrInvalidJSON, err)
	}
	return v, nil
}

// ListJSONFiles reads dir and unmarshals every non-directory entry whose name
// satisfies match into a T, returning them in directory order. A missing dir
// yields (nil, nil). Entries that fail to read or parse are skipped silently so
// one bad file never hides the rest — callers that need per-file diagnostics
// should enumerate and ReadJSON themselves. Results are unsorted; callers sort.
func ListJSONFiles[T any](dir string, match func(name string) bool) ([]T, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, nil
		}
		return nil, err
	}
	out := make([]T, 0, len(entries))
	for _, e := range entries {
		if e.IsDir() || !match(e.Name()) {
			continue
		}
		v, err := ReadJSON[T](filepath.Join(dir, e.Name()))
		if err != nil {
			continue // skip unreadable/corrupt files
		}
		out = append(out, v)
	}
	return out, nil
}
