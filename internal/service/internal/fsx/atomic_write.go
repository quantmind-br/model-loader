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
	// Unique temp name: concurrent writers to the same path exist (the download
	// worker process and the Manager both rewrite dl-<id>.json). A fixed temp
	// name would let one writer rename the other's half-written temp (audit
	// P-C9). defer os.Remove after a successful rename fails harmlessly (ENOENT).
	tmp, err := os.CreateTemp(filepath.Dir(path), "."+filepath.Base(path)+".tmp-*")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	defer os.Remove(tmpName)
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	if err := os.Chmod(tmpName, 0o644); err != nil {
		return err
	}
	return os.Rename(tmpName, path)
}

// WriteJSONExclusive marshals v as indented JSON and writes it to path only if
// path does not already exist. It writes to a unique temp file, then hard-links
// it into place: os.Link fails atomically (os.IsExist) when path already exists,
// closing the check-then-write race a plain Stat+Write would leave open. The
// content is fully written to the temp file before the link, so a crash never
// leaves a partial profile under path.
func WriteJSONExclusive(path string, v any) error {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	data, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return err
	}
	tmp, err := os.CreateTemp(dir, ".create-*.tmp")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	defer os.Remove(tmpName)
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	if err := os.Chmod(tmpName, 0o644); err != nil {
		return err
	}
	// Hard link is atomic and fails with os.IsExist when path is already taken.
	return os.Link(tmpName, path)
}
