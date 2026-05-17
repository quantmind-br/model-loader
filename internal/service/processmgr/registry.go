package processmgr

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"

	"github.com/quantmind-br/model-loader/internal/domain"
	"github.com/quantmind-br/model-loader/internal/service/internal/fsx"
)

// registryFile is the on-disk shape of instances.json. Wrapping the slice in
// an object lets us add fields later without breaking parse compatibility.
type registryFile struct {
	Instances []domain.RunningInstance `json:"instances"`
}

// loadRegistry reads path and returns its instances. A missing file yields
// (nil, nil). A malformed file yields (nil, error).
func loadRegistry(path string) ([]domain.RunningInstance, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return nil, nil
		}
		return nil, fmt.Errorf("read registry: %w", err)
	}
	var rf registryFile
	if err := json.Unmarshal(data, &rf); err != nil {
		return nil, fmt.Errorf("parse registry: %w", err)
	}
	return rf.Instances, nil
}

// saveRegistry writes the slice atomically to path. Parent dir is created
// if absent.
func saveRegistry(path string, insts []domain.RunningInstance) error {
	if err := fsx.WriteJSONAtomic(path, registryFile{Instances: insts}); err != nil {
		return fmt.Errorf("save registry: %w", err)
	}
	return nil
}
