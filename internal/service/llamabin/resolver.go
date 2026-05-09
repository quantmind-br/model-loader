// Package llamabin resolves and validates llama-server binary paths.
package llamabin

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
)

const DefaultName = "llama-server"

var (
	ErrBinaryNotFound     = errors.New("binary not found")
	ErrBinaryNotExecutable = errors.New("binary is not executable")
	ErrBinaryIsDirectory  = errors.New("path is a directory")
)

// Effective returns the raw binary string to use, applying fallback chain:
//   profileOverride > globalDefault > DefaultName
func Effective(profileOverride, globalDefault string) string {
	if profileOverride != "" {
		return profileOverride
	}
	if globalDefault != "" {
		return globalDefault
	}
	return DefaultName
}

// Resolve validates the raw binary path/name and returns the resolved path.
// Rules:
//   - empty → returns DefaultName (caller should validate this resolves via PATH)
//   - bare name (no slash) → exec.LookPath; validates executable
//   - absolute/relative path → os.Stat; validates exists, is file, is executable
func Resolve(raw string) (string, error) {
	if raw == "" {
		return DefaultName, nil
	}
	if !containsSlash(raw) {
		return resolveInPATH(raw)
	}
	return resolvePath(raw)
}

// Validate is a convenience that calls Resolve and only returns an error.
func Validate(raw string) error {
	_, err := Resolve(raw)
	return err
}

func containsSlash(s string) bool {
	return filepath.IsAbs(s) || filepath.Dir(s) != "."
}

func resolveInPATH(name string) (string, error) {
	path, err := exec.LookPath(name)
	if err != nil {
		return "", fmt.Errorf("%w: %s", ErrBinaryNotFound, name)
	}
	if err := checkExecutable(path); err != nil {
		return "", fmt.Errorf("%w: %s", err, path)
	}
	return path, nil
}

func resolvePath(p string) (string, error) {
	info, err := os.Stat(p)
	if err != nil {
		return "", fmt.Errorf("%w: %s", ErrBinaryNotFound, p)
	}
	if info.IsDir() {
		return "", fmt.Errorf("%w: %s", ErrBinaryIsDirectory, p)
	}
	if err := checkExecutable(p); err != nil {
		return "", fmt.Errorf("%w: %s", err, p)
	}
	return p, nil
}

func checkExecutable(path string) error {
	info, err := os.Stat(path)
	if err != nil {
		return err
	}
	mode := info.Mode()
	if mode.IsDir() {
		return ErrBinaryIsDirectory
	}
	if runtime.GOOS == "windows" {
		// Windows: check for known executable extensions
		ext := filepath.Ext(path)
		if ext == ".exe" || ext == ".bat" || ext == ".cmd" {
			return nil
		}
		return ErrBinaryNotExecutable
	}
	// Unix: check owner/group/other execute bits
	if mode.Perm()&0o111 == 0 {
		return ErrBinaryNotExecutable
	}
	return nil
}
