package modelscanner

import (
	"context"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"github.com/quantmind-br/model-loader/internal/domain"
)

// New returns a filesystem-backed Scanner.
func New() Scanner {
	return &fsScanner{}
}

type fsScanner struct{}

const eventBuffer = 64

func (s *fsScanner) Scan(ctx context.Context, paths []string) (<-chan domain.ScanEvent, error) {
	ch := make(chan domain.ScanEvent, eventBuffer)
	go func() {
		defer close(ch)
		for _, root := range paths {
			s.scanRoot(ctx, root, ch)
			if ctx.Err() != nil {
				return
			}
		}
		select {
		case ch <- domain.ScanEvent{Type: domain.ScanEventDone}:
		case <-ctx.Done():
		}
	}()
	return ch, nil
}

func (s *fsScanner) scanRoot(ctx context.Context, root string, ch chan<- domain.ScanEvent) {
	count := 0
	walkErr := filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		if err != nil {
			return err
		}
		if d.IsDir() {
			return nil
		}
		if !strings.EqualFold(filepath.Ext(p), ".gguf") {
			return nil
		}
		mf := buildModelFile(p, d)
		count++
		select {
		case ch <- domain.ScanEvent{Type: domain.ScanEventFile, Root: root, File: &mf}:
		case <-ctx.Done():
			return ctx.Err()
		}
		return nil
	})
	if walkErr != nil && ctx.Err() == nil {
		ch <- domain.ScanEvent{Type: domain.ScanEventError, Root: root, Error: walkErr}
		return
	}
	ch <- domain.ScanEvent{Type: domain.ScanEventProgress, Root: root, Count: count}
}

func buildModelFile(path string, d fs.DirEntry) domain.ModelFile {
	name := filepath.Base(path)
	mf := domain.ModelFile{
		Path:  path,
		Name:  name,
		Quant: parseQuant(name),
	}
	if info, err := d.Info(); err == nil {
		mf.SizeBytes = info.Size()
	}
	if params := readParamsFromFile(path); params != "" {
		mf.Params = params
	} else {
		mf.Params = parseParams(name)
	}
	return mf
}

func readParamsFromFile(path string) string {
	f, err := os.Open(path)
	if err != nil {
		return ""
	}
	defer f.Close()
	p, err := readGGUFParams(f)
	if err != nil {
		return ""
	}
	return p
}
