package profilestore

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
)

// ConflictMode controls how ImportBundle handles profiles whose ID already
// exists in the target store.
type ConflictMode string

const (
	ConflictModeMerge     ConflictMode = "merge"
	ConflictModeOverwrite ConflictMode = "overwrite"
	ConflictModeRename    ConflictMode = "rename"
)

// ImportResult summarizes how many profiles ImportBundle added, skipped,
// renamed, or replaced.
type ImportResult struct {
	Added    int
	Skipped  int
	Renamed  int
	Replaced int
}

// ImportBundle reads an exported profile bundle and saves its profiles into
// store according to mode.
func ImportBundle(store Store, path string, mode ConflictMode) (ImportResult, error) {
	if store == nil {
		return ImportResult{}, fmt.Errorf("import bundle: store is nil")
	}
	if path == "" {
		return ImportResult{}, fmt.Errorf("import bundle: path is empty")
	}
	if !validConflictMode(mode) {
		return ImportResult{}, fmt.Errorf("import bundle: unsupported conflict mode %q", mode)
	}

	f, err := os.Open(path)
	if err != nil {
		return ImportResult{}, fmt.Errorf("open import bundle: %w", err)
	}
	defer f.Close()

	var bundle ExportBundle
	dec := json.NewDecoder(f)
	dec.DisallowUnknownFields()
	if err := dec.Decode(&bundle); err != nil {
		return ImportResult{}, fmt.Errorf("decode import bundle: %w", err)
	}
	if err := dec.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		return ImportResult{}, fmt.Errorf("decode import bundle: trailing JSON values")
	}
	if bundle.SchemaVersion > ExportBundleSchemaVersion {
		return ImportResult{}, fmt.Errorf("import bundle schema version %d is newer than supported version %d", bundle.SchemaVersion, ExportBundleSchemaVersion)
	}

	var result ImportResult
	for i, profile := range bundle.Profiles {
		if profile.ID == "" {
			return result, fmt.Errorf("import bundle profile %d: id is empty", i)
		}

		exists, err := profileExists(store, profile.ID)
		if err != nil {
			return result, fmt.Errorf("check existing profile %q: %w", profile.ID, err)
		}

		switch mode {
		case ConflictModeMerge:
			if exists {
				result.Skipped++
				continue
			}
			if err := store.Save(profile); err != nil {
				return result, fmt.Errorf("save imported profile %q: %w", profile.ID, err)
			}
			result.Added++
		case ConflictModeOverwrite:
			if err := store.Save(profile); err != nil {
				return result, fmt.Errorf("save imported profile %q: %w", profile.ID, err)
			}
			if exists {
				result.Replaced++
			} else {
				result.Added++
			}
		case ConflictModeRename:
			if exists {
				renamed, err := nextImportedID(store, profile.ID)
				if err != nil {
					return result, fmt.Errorf("rename imported profile %q: %w", profile.ID, err)
				}
				profile.ID = renamed
				if err := store.Save(profile); err != nil {
					return result, fmt.Errorf("save renamed imported profile %q: %w", profile.ID, err)
				}
				result.Renamed++
				continue
			}
			if err := store.Save(profile); err != nil {
				return result, fmt.Errorf("save imported profile %q: %w", profile.ID, err)
			}
			result.Added++
		}
	}

	return result, nil
}

func validConflictMode(mode ConflictMode) bool {
	switch mode {
	case ConflictModeMerge, ConflictModeOverwrite, ConflictModeRename:
		return true
	default:
		return false
	}
}

func profileExists(store Store, id string) (bool, error) {
	_, err := store.Get(id)
	if err == nil {
		return true, nil
	}
	if errors.Is(err, ErrNotFound) {
		return false, nil
	}
	return false, err
}

func nextImportedID(store Store, id string) (string, error) {
	for n := 1; ; n++ {
		candidate := fmt.Sprintf("%s-imported-%d", id, n)
		exists, err := profileExists(store, candidate)
		if err != nil {
			return "", err
		}
		if !exists {
			return candidate, nil
		}
	}
}
