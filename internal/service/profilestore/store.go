// Package profilestore persists Profile JSON files on disk.
package profilestore

import (
	"errors"

	"github.com/quantmind-br/model-loader/internal/domain"
)

// ListDiagnostic descreve uma entry de profile que falhou ao carregar.
// Usado pela UI para marcar profiles corruptos com ⚠ e excluí-los das
// operações de launch.
type ListDiagnostic struct {
	ID  string
	Err error
}

// Store is the interface for profile persistence.
type Store interface {
	List() ([]domain.Profile, error)
	ListWithDiagnostics() ([]domain.Profile, []ListDiagnostic, error)
	Get(id string) (domain.Profile, error)
	Save(p domain.Profile) error
	Delete(id string) error
	Duplicate(srcID, newID string) (domain.Profile, error)
}

// Sentinel errors returned by Store implementations.
var (
	ErrNotFound    = errors.New("profile not found")
	ErrInvalidJSON = errors.New("profile json is invalid")
	ErrDuplicateID = errors.New("profile id already exists")
	ErrInvalidID   = errors.New("profile id is invalid")
)
