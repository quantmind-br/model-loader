// Package llamabin resolves and validates llama-server binary paths.
package llamabin

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
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
//   - command string with spaces (e.g. "python -m sglang.launch_server") →
//     resolves the first word via PATH and returns the original string so the
//     caller can split into command + prefix args.
func Resolve(raw string) (string, error) {
	if raw == "" {
		return DefaultName, nil
	}
	first, rest := splitCommand(raw)
	if first == "" {
		first = raw
	}
	if !containsSlash(first) {
		resolved, err := resolveInPATH(first)
		if err != nil {
			return "", err
		}
		// If the input was a command string with args, return the raw form
		// so the caller can reconstruct argv. Otherwise return the resolved path.
		if rest != "" {
			return raw, nil
		}
		return resolved, nil
	}
	if _, err := resolvePath(first); err != nil {
		return "", err
	}
	return raw, nil
}

// splitCommand separates the first executable token from the rest of a
// command string. Used for backends whose executable is not a single binary
// (e.g. "python -m sglang.launch_server").
func splitCommand(raw string) (string, string) {
	tokens, err := splitCommandLine(raw)
	if err != nil || len(tokens) == 0 {
		return raw, ""
	}
	if len(tokens) == 1 {
		return tokens[0], ""
	}
	return tokens[0], strings.Join(tokens[1:], " ")
}

// splitCommandLine performs basic shell-like tokenization.
// Supports double quotes, single quotes, and backslash escapes.
func splitCommandLine(raw string) ([]string, error) {
	var tokens []string
	var current strings.Builder
	inDouble := false
	inSingle := false
	escaped := false

	flush := func() {
		if current.Len() > 0 {
			tokens = append(tokens, current.String())
			current.Reset()
		}
	}

	for _, r := range raw {
		if escaped {
			current.WriteRune(r)
			escaped = false
			continue
		}
		if r == '\\' {
			if inSingle {
				current.WriteRune(r)
			} else {
				escaped = true
			}
			continue
		}
		if r == '"' && !inSingle {
			inDouble = !inDouble
			continue
		}
		if r == '\'' && !inDouble {
			inSingle = !inSingle
			continue
		}
		if (r == ' ' || r == '\t' || r == '\n' || r == '\r') && !inDouble && !inSingle {
			flush()
			continue
		}
		current.WriteRune(r)
	}

	if inDouble || inSingle {
		return nil, fmt.Errorf("unbalanced quote in command: %q", raw)
	}
	if escaped {
		return nil, fmt.Errorf("trailing backslash in command: %q", raw)
	}
	flush()
	return tokens, nil
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
