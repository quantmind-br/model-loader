package profilestore

import (
	"fmt"
	"path/filepath"
	"time"

	"github.com/quantmind-br/model-loader/internal/domain"
	"github.com/quantmind-br/model-loader/internal/service/internal/fsx"
)

// ExportBundleSchemaVersion is the current schema version of an exported
// profile bundle. Bumped only on a breaking change to the JSON shape.
const ExportBundleSchemaVersion = 1

// ExportBundle is the JSON envelope written by ExportAll. It carries a
// schema version (independent of domain.SchemaVersion) plus the export
// timestamp and the list of valid profiles at the time of export.
//
// Corrupt entries from ListWithDiagnostics are intentionally excluded so
// importing the bundle elsewhere never replays broken JSON.
type ExportBundle struct {
	SchemaVersion int              `json:"schemaVersion"`
	ExportedAt    time.Time        `json:"exportedAt"`
	Profiles      []domain.Profile `json:"profiles"`
}

// ExportAll snapshots every valid profile from store and writes the bundle
// as JSON into dir. The filename is derived from the export timestamp
// (UTC) — profiles-export-YYYYMMDD-HHMMSS.json — so multiple exports never
// collide.
//
// Returns the bundle (populated even on write failure so the caller can
// inspect what would have been written) and the write error, if any.
func ExportAll(store Store, dir string) (ExportBundle, error) {
	profiles, err := store.List()
	if err != nil {
		return ExportBundle{}, fmt.Errorf("list profiles: %w", err)
	}
	if profiles == nil {
		profiles = []domain.Profile{}
	}

	now := time.Now().UTC()
	bundle := ExportBundle{
		SchemaVersion: ExportBundleSchemaVersion,
		ExportedAt:    now,
		Profiles:      profiles,
	}

	if dir == "" {
		return bundle, fmt.Errorf("export dir is empty")
	}

	filename := fmt.Sprintf("profiles-export-%s.json", now.Format("20060102-150405"))
	path := filepath.Join(dir, filename)

	if err := fsx.WriteJSONAtomic(path, bundle); err != nil {
		return bundle, fmt.Errorf("write export bundle: %w", err)
	}
	return bundle, nil
}

// ExportFilename returns the path that ExportAll would write to for a
// given dir and timestamp. Exposed for tests and for the UI flash that
// reports the destination filename.
func ExportFilename(dir string, at time.Time) string {
	return filepath.Join(dir, fmt.Sprintf("profiles-export-%s.json", at.UTC().Format("20060102-150405")))
}
