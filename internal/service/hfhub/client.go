package hfhub

import (
	"context"
	"errors"
	"io"
	"net/http"
	"os"
)

const DefaultBaseURL = "https://huggingface.co"

// Client is a minimal HTTP client for the Hugging Face Hub API.
type Client struct {
	http      *http.Client
	baseURL   string
	userAgent string
}

// NewClient creates a Client. If httpClient is nil, http.DefaultClient is used.
// baseURL defaults to DefaultBaseURL unless the environment variable HF_BASE_URL is set.
func NewClient(httpClient *http.Client, userAgent string) *Client {
	if httpClient == nil {
		httpClient = http.DefaultClient
	}

	baseURL := DefaultBaseURL
	if envURL := os.Getenv("HF_BASE_URL"); envURL != "" {
		baseURL = envURL
	}

	return &Client{
		http:      httpClient,
		baseURL:   baseURL,
		userAgent: userAgent,
	}
}

// Search queries the Hub for models/datasets matching query.
func (c *Client) Search(ctx context.Context, query string, limit int) ([]SearchResult, error) {
	return nil, errors.New("not implemented")
}

// RepoInfo fetches metadata for a repository.
func (c *Client) RepoInfo(ctx context.Context, repoID string) (*RepoInfo, error) {
	return nil, errors.New("not implemented")
}

// OpenDownload returns a ReadCloser for a file in a repository and its size in bytes.
func (c *Client) OpenDownload(ctx context.Context, repoID, filename string) (io.ReadCloser, int64, error) {
	return nil, 0, errors.New("not implemented")
}
