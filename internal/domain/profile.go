// Package domain holds shared types with zero external dependencies.
package domain

import (
	"regexp"
	"strings"
	"time"
	"unicode"
)

// SchemaVersion is the current Profile JSON schema version.
const SchemaVersion = 3

// Profile represents a llama-server load profile persisted on disk.
// Full field definitions are added in slice 1, task 1.
type Profile struct {
	SchemaVersion int               `json:"schemaVersion"`
	ID            string            `json:"id"`
	Name          string            `json:"name"`
	Description   string            `json:"description,omitempty"`
	Tags          []string          `json:"tags,omitempty"`
	Model         string            `json:"model"`
	// Note: ints round-trip as float64 via JSON; see validator.checkType.
	Args          map[string]any    `json:"args"`
	ExtraArgs     []string          `json:"extraArgs,omitempty"`
	Launch        LaunchConfig      `json:"launch"`
	Meta          ProfileMeta       `json:"meta"`
	Pinned        bool              `json:"pinned"`
}

// EnvVar is a single KEY=VALUE pair applied to the backend process at launch.
// Keys must match [A-Za-z_][A-Za-z0-9_]*. Values are used literally.
type EnvVar struct {
	Key   string `json:"key"`
	Value string `json:"value"`
}

type RestartPolicy string

const (
	RestartPolicyNone      RestartPolicy = "none"
	RestartPolicyOnFailure RestartPolicy = "on-failure"
	RestartPolicyAlways    RestartPolicy = "always"
)

// LaunchConfig holds per-profile launcher defaults.
type LaunchConfig struct {
	DefaultBackground bool   `json:"defaultBackground"`
	LogFilePath       string `json:"logFilePath,omitempty"`
	BackendID         string `json:"backendId,omitempty"`

	// Env are KEY=VALUE pairs overlaid on the inherited TUI environment
	// when launching the backend. Last value wins on duplicate keys.
	Env []EnvVar `json:"env,omitempty"`

	// Legacy field kept for read-only migration. Not written after migration.
	LlamaServerBinaryPath string `json:"llamaServerBinaryPath,omitempty"`

	// ResolvedExecutable is set at launch time by the launcher after
	// resolving the backend. It avoids double-resolution in processmgr.
	// Not persisted.
	ResolvedExecutable string `json:"-"`

	// ResolvedBackendKind is set at launch time by the launcher so
	// processmgr knows which arg builder to use. Not persisted.
	ResolvedBackendKind BackendKind `json:"-"`

	RestartPolicy  RestartPolicy `json:"restart_policy"`
	MaxRestarts    int           `json:"max_restarts"`
	BackoffSeconds int           `json:"backoff_seconds"`
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

// validSlugRE matches a filename- and model-id-safe identifier: lowercase
// alphanumeric groups joined by single "." "_" or "-" separators, with no
// leading/trailing/double separator. Dots and underscores are allowed because
// they are common in model ids (e.g. "qwen3.6") and safe in filenames.
var validSlugRE = regexp.MustCompile(`^[a-z0-9]+([._-][a-z0-9]+)*$`)

// IsValidSlug reports whether s is a valid profile ID: lowercase, made of
// a-z/0-9 separated by single "." "_" or "-". Used to validate user-entered IDs.
// Note this is broader than Slugify's output (which collapses "." to "-").
func IsValidSlug(s string) bool {
	return validSlugRE.MatchString(s)
}
