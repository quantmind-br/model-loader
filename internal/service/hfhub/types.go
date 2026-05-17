package hfhub

import (
	"fmt"
	"slices"
	"time"
)

// SearchResult represents a single model or dataset entry from the Hugging Face Hub search API.
type SearchResult struct {
	ID            string
	Author        string
	ModelID       string
	Tags          []string
	Downloads     int
	Likes         int
	LastModified  time.Time
	LibraryName   string
	PipelineTag   string
}

// HasGGUFTag reports whether the result is tagged with "gguf".
func (r SearchResult) HasGGUFTag() bool {
	return slices.Contains(r.Tags, "gguf")
}

// RepoInfo holds metadata for a specific repository on the Hub.
type RepoInfo struct {
	ID       string
	Siblings []Sibling
	Tags     []string
}

// Sibling describes a single file inside a repository.
type Sibling struct {
	RFilename string
	Size      int64
}

// ErrHTTP is returned when the Hub responds with a non-2xx status code.
type ErrHTTP struct {
	Status int
	URL    string
}

func (e ErrHTTP) Error() string {
	return fmt.Sprintf("huggingface hub HTTP %d: %s", e.Status, e.URL)
}
