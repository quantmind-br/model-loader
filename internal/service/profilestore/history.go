package profilestore

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"

	"github.com/quantmind-br/model-loader/internal/domain"
	"github.com/quantmind-br/model-loader/internal/service/internal/fsx"
)

func previousPath(dataDir, id string) string {
	return filepath.Join(dataDir, ".history", id+".previous.json")
}

// SavePrevious persists the previous profile snapshot under dataDir/.history.
func SavePrevious(dataDir string, p domain.Profile) error {
	if err := fsx.WriteJSONAtomic(previousPath(dataDir, p.ID), p); err != nil {
		return fmt.Errorf("save previous profile: %w", err)
	}
	return nil
}

// LoadPrevious reads the previous profile snapshot for id.
func LoadPrevious(dataDir, id string) (domain.Profile, bool, error) {
	data, err := os.ReadFile(previousPath(dataDir, id))
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return domain.Profile{}, false, nil
		}
		return domain.Profile{}, false, fmt.Errorf("read previous profile: %w", err)
	}

	var p domain.Profile
	if err := json.Unmarshal(data, &p); err != nil {
		return domain.Profile{}, false, fmt.Errorf("parse previous profile: %w", err)
	}
	return p, true, nil
}

// DeletePrevious removes the previous profile snapshot for id.
func DeletePrevious(dataDir, id string) error {
	if err := os.Remove(previousPath(dataDir, id)); err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return nil
		}
		return fmt.Errorf("delete previous profile: %w", err)
	}
	return nil
}
