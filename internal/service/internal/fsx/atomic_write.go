package fsx

import (
	"encoding/json"
	"os"
	"path/filepath"
)

// WriteJSONAtomic marshals v as indented JSON and writes it atomically to path.
// It creates the parent directory if missing, writes to a temporary file, then
// renames it into place. If the rename fails, the temporary file is removed.
func WriteJSONAtomic(path string, v any) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	data, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return err
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o644); err != nil {
		return err
	}
	if err := os.Rename(tmp, path); err != nil {
		_ = os.Remove(tmp)
		return err
	}
	return nil
}
