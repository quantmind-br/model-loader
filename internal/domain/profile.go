// Package domain holds shared types with zero external dependencies.
package domain

import (
	"strings"
	"time"
	"unicode"
)

// SchemaVersion is the current Profile JSON schema version.
const SchemaVersion = 1

// Profile represents a llama-server load profile persisted on disk.
// Full field definitions are added in slice 1, task 1.
type Profile struct {
	SchemaVersion int               `json:"schemaVersion"`
	ID            string            `json:"id"`
	Name          string            `json:"name"`
	Description   string            `json:"description,omitempty"`
	Tags          []string          `json:"tags,omitempty"`
	Model         string            `json:"model"`
	Args          map[string]any    `json:"args"`
	ExtraArgs     []string          `json:"extraArgs,omitempty"`
	Launch        LaunchConfig      `json:"launch"`
	Meta          ProfileMeta       `json:"meta"`
}

// LaunchConfig holds per-profile launcher defaults.
type LaunchConfig struct {
	DefaultBackground bool   `json:"defaultBackground"`
	LogFilePath       string `json:"logFilePath,omitempty"`
	BackendID         string `json:"backendId,omitempty"`

	// Legacy field kept for read-only migration. Not written after migration.
	LlamaServerBinaryPath string `json:"llamaServerBinaryPath,omitempty"`

	// ResolvedExecutable is set at launch time by the launcher after
	// resolving the backend. It avoids double-resolution in processmgr.
	// Not persisted.
	ResolvedExecutable string `json:"-"`

	// ResolvedBackendKind is set at launch time by the launcher so
	// processmgr knows which arg builder to use. Not persisted.
	ResolvedBackendKind BackendKind `json:"-"`
}

// ProfileMeta holds timestamps and bookkeeping.
type ProfileMeta struct {
	CreatedAt  time.Time  `json:"createdAt"`
	UpdatedAt  time.Time  `json:"updatedAt"`
	LastUsedAt *time.Time `json:"lastUsedAt,omitempty"`
}

// Slugify produces an ASCII kebab-case ID safe for filenames.
func Slugify(s string) string {
	var b strings.Builder
	prevDash := true
	for _, r := range strings.ToLower(s) {
		switch {
		case unicode.IsLetter(r) && r < 128, unicode.IsDigit(r) && r < 128:
			b.WriteRune(r)
			prevDash = false
		default:
			if !prevDash {
				b.WriteByte('-')
				prevDash = true
			}
		}
	}
	return strings.Trim(b.String(), "-")
}
