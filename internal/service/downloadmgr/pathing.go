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
// Both modes nest files under a "{publisher}/{repo}" subdirectory so the
// on-disk layout mirrors the Hugging Face cache and LM Studio conventions
// (e.g. ~/.lmstudio/models/Qwen/Qwen2.5-0.5B-Instruct-GGUF/model.gguf) instead
// of piling every file flat into the search root. Individual mode keeps only
// rfilename's basename; snapshot mode preserves any nested subdirectories in
// rfilename. The function refuses to overwrite existing destinations and
// rejects repoID/rfilename values that would escape searchPath via "..".
func ResolveDest(searchPath, repoID, rfilename string, isSnapshot bool) (destDir, destFile string, err error) {
	if searchPath == "" {
		return "", "", ErrNoSearchPath
	}
	info, statErr := os.Stat(searchPath)
	if statErr != nil || !info.IsDir() {
		return "", "", ErrSearchPathMissing
	}

	cleanRoot := filepath.Clean(searchPath)

	// repoID is "{publisher}/{repo}"; map it to a real nested path. A repoID
	// containing ".." (or an absolute segment) is rejected by the withinRoot
	// guard below.
	destDir = filepath.Join(searchPath, filepath.FromSlash(repoID))
	if !withinRoot(destDir, cleanRoot) {
		return "", "", ErrPathTraversal
	}

	if isSnapshot {
		if _, err := os.Stat(destDir); err == nil {
			return "", "", ErrAlreadyExists
		}
		destFile = filepath.Join(destDir, rfilename)
		if !withinRoot(destFile, cleanRoot) {
			return "", "", ErrPathTraversal
		}
		return destDir, destFile, nil
	}

	destFile = filepath.Join(destDir, filepath.Base(rfilename))
	if !withinRoot(destFile, cleanRoot) {
		return "", "", ErrPathTraversal
	}
	if _, err := os.Stat(destFile); err == nil {
		return "", "", ErrAlreadyExists
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
