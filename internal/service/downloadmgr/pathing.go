package downloadmgr

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
)

var (
	// ErrNoSearchPath indicates no download root was configured.
	ErrNoSearchPath = errors.New("no download path configured")
	// ErrSearchPathMissing indicates the configured root is missing or not a directory.
	ErrSearchPathMissing = errors.New("download path does not exist")
	// ErrAlreadyExists indicates the resolved destination already exists; the caller skips.
	ErrAlreadyExists = errors.New("already exists")
	// ErrPathTraversal indicates rfilename resolved outside searchPath.
	ErrPathTraversal = errors.New("path traversal detected")
)

// ResolveDest computes the destination directory and file for a download.
// Individual mode places rfilename's basename directly under searchPath.
// Snapshot mode places files inside a sanitized "{org}__{repo}" subdir,
// preserving any nested subdirectories in rfilename. The function refuses
// to overwrite existing destinations and rejects rfilename values that
// would escape searchPath via "..".
func ResolveDest(searchPath, repoID, rfilename string, isSnapshot bool) (destDir, destFile string, err error) {
	if searchPath == "" {
		return "", "", ErrNoSearchPath
	}
	info, statErr := os.Stat(searchPath)
	if statErr != nil || !info.IsDir() {
		return "", "", ErrSearchPathMissing
	}

	cleanRoot := filepath.Clean(searchPath)

	if isSnapshot {
		sanitized := strings.ReplaceAll(repoID, "/", "__")
		destDir = filepath.Join(searchPath, sanitized)
		if _, err := os.Stat(destDir); err == nil {
			return "", "", ErrAlreadyExists
		}
		destFile = filepath.Join(destDir, rfilename)
		if !withinRoot(destFile, cleanRoot) {
			return "", "", ErrPathTraversal
		}
		return destDir, destFile, nil
	}

	destDir = searchPath
	destFile = filepath.Join(searchPath, filepath.Base(rfilename))
	if _, err := os.Stat(destFile); err == nil {
		return "", "", ErrAlreadyExists
	}
	if !withinRoot(destFile, cleanRoot) {
		return "", "", ErrPathTraversal
	}
	return destDir, destFile, nil
}

func withinRoot(path, cleanRoot string) bool {
	cleaned := filepath.Clean(path)
	if cleaned == cleanRoot {
		return true
	}
	prefix := cleanRoot + string(filepath.Separator)
	return strings.HasPrefix(cleaned, prefix)
}
