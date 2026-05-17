package hfhub

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"strings"
	"time"
)

const DefaultBaseURL = "https://huggingface.co"

// retryAfterCap bounds how long a 429 Retry-After is honored before giving up.
const retryAfterCap = 30 * time.Second

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

// BaseURL returns the client's runtime base URL (respects HF_BASE_URL).
func (c *Client) BaseURL() string { return c.baseURL }

// DownloadURL builds a direct download URL for a file in a repo.
func (c *Client) DownloadURL(repoID, filename string) string {
	return c.baseURL + "/" + repoID + "/resolve/main/" + filename
}

// searchResultDTO is the wire shape returned by /api/models?search=…&full=true.
type searchResultDTO struct {
	ID           string   `json:"id"`
	Author       string   `json:"author"`
	ModelID      string   `json:"modelId"`
	Tags         []string `json:"tags"`
	Downloads    int      `json:"downloads"`
	Likes        int      `json:"likes"`
	LastModified string   `json:"lastModified"`
	LibraryName  string   `json:"library_name"`
	PipelineTag  string   `json:"pipeline_tag"`
}

func (d searchResultDTO) toSearchResult() SearchResult {
	var t time.Time
	if d.LastModified != "" {
		if parsed, err := time.Parse(time.RFC3339Nano, d.LastModified); err == nil {
			t = parsed
		} else if parsed, err := time.Parse(time.RFC3339, d.LastModified); err == nil {
			t = parsed
		}
	}
	return SearchResult{
		ID:           d.ID,
		Author:       d.Author,
		ModelID:      d.ModelID,
		Tags:         d.Tags,
		Downloads:    d.Downloads,
		Likes:        d.Likes,
		LastModified: t,
		LibraryName:  d.LibraryName,
		PipelineTag:  d.PipelineTag,
	}
}

// repoInfoDTO is the wire shape returned by /api/models/{id}.
type repoInfoDTO struct {
	ID       string       `json:"id"`
	Tags     []string     `json:"tags"`
	Siblings []siblingDTO `json:"siblings"`
}

type siblingDTO struct {
	RFilename string `json:"rfilename"`
	Size      int64  `json:"size"`
}

func (d repoInfoDTO) toRepoInfo() *RepoInfo {
	out := &RepoInfo{
		ID:   d.ID,
		Tags: d.Tags,
	}
	if len(d.Siblings) > 0 {
		out.Siblings = make([]Sibling, len(d.Siblings))
		for i, s := range d.Siblings {
			out.Siblings[i] = Sibling{RFilename: s.RFilename, Size: s.Size}
		}
	}
	return out
}

// Search queries the Hub for models/datasets matching query.
func (c *Client) Search(ctx context.Context, query string, limit int) ([]SearchResult, error) {
	u, err := url.Parse(c.baseURL)
	if err != nil {
		return nil, fmt.Errorf("hfhub: parse baseURL: %w", err)
	}
	u.Path = "/api/models"
	q := url.Values{}
	q.Set("search", query)
	if limit > 0 {
		q.Set("limit", strconv.Itoa(limit))
	}
	q.Set("full", "true")
	u.RawQuery = q.Encode()

	resp, err := c.doJSON(ctx, u.String())
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	var dtos []searchResultDTO
	if err := json.NewDecoder(resp.Body).Decode(&dtos); err != nil {
		return nil, fmt.Errorf("hfhub: decode search response: %w", err)
	}

	out := make([]SearchResult, len(dtos))
	for i, d := range dtos {
		out[i] = d.toSearchResult()
	}
	return out, nil
}

// RepoInfo fetches metadata for a repository. repoID is the literal
// "{org}/{name}" string and is NOT url-escaped.
func (c *Client) RepoInfo(ctx context.Context, repoID string) (*RepoInfo, error) {
	u, err := url.Parse(c.baseURL)
	if err != nil {
		return nil, fmt.Errorf("hfhub: parse baseURL: %w", err)
	}
	u.Path = "/api/models/" + repoID

	resp, err := c.doJSON(ctx, u.String())
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	var dto repoInfoDTO
	if err := json.NewDecoder(resp.Body).Decode(&dto); err != nil {
		return nil, fmt.Errorf("hfhub: decode repo info: %w", err)
	}
	return dto.toRepoInfo(), nil
}

// OpenDownload streams a single file from a repository. The caller MUST close
// the returned ReadCloser. ContentLength is -1 when the server omits it.
func (c *Client) OpenDownload(ctx context.Context, repoID, filename string) (io.ReadCloser, int64, error) {
	u, err := url.Parse(c.baseURL)
	if err != nil {
		return nil, 0, fmt.Errorf("hfhub: parse baseURL: %w", err)
	}
	u.Path = "/" + repoID + "/resolve/main/" + filename

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u.String(), nil)
	if err != nil {
		return nil, 0, fmt.Errorf("hfhub: build download request: %w", err)
	}
	if c.userAgent != "" {
		req.Header.Set("User-Agent", c.userAgent)
	}

	resp, err := c.http.Do(req)
	if err != nil {
		return nil, 0, fmt.Errorf("hfhub: download request: %w", err)
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		_ = resp.Body.Close()
		return nil, 0, &ErrHTTP{Status: resp.StatusCode, URL: u.String()}
	}
	return resp.Body, resp.ContentLength, nil
}

// doJSON performs a GET request expecting a JSON body. It honors a single
// 429 Retry-After up to retryAfterCap and turns any non-2xx response into
// a *ErrHTTP. On success the caller owns resp.Body.
func (c *Client) doJSON(ctx context.Context, fullURL string) (*http.Response, error) {
	resp, err := c.sendJSON(ctx, fullURL)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode == http.StatusTooManyRequests {
		wait := parseRetryAfter(resp.Header.Get("Retry-After"))
		_ = resp.Body.Close()
		if wait > retryAfterCap {
			wait = retryAfterCap
		}
		if wait > 0 {
			select {
			case <-ctx.Done():
				return nil, ctx.Err()
			case <-time.After(wait):
			}
		}
		resp, err = c.sendJSON(ctx, fullURL)
		if err != nil {
			return nil, err
		}
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		_ = resp.Body.Close()
		return nil, &ErrHTTP{Status: resp.StatusCode, URL: fullURL}
	}
	return resp, nil
}

func (c *Client) sendJSON(ctx context.Context, fullURL string) (*http.Response, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, fullURL, nil)
	if err != nil {
		return nil, fmt.Errorf("hfhub: build request: %w", err)
	}
	if c.userAgent != "" {
		req.Header.Set("User-Agent", c.userAgent)
	}
	req.Header.Set("Accept", "application/json")
	resp, err := c.http.Do(req)
	if err != nil {
		return nil, fmt.Errorf("hfhub: do request: %w", err)
	}
	return resp, nil
}

// parseRetryAfter accepts either a delta-seconds value or an HTTP-date.
// It returns 0 when the header is missing or unparseable.
func parseRetryAfter(h string) time.Duration {
	h = strings.TrimSpace(h)
	if h == "" {
		return 0
	}
	if secs, err := strconv.Atoi(h); err == nil {
		if secs < 0 {
			return 0
		}
		return time.Duration(secs) * time.Second
	}
	if t, err := http.ParseTime(h); err == nil {
		d := time.Until(t)
		if d < 0 {
			return 0
		}
		return d
	}
	return 0
}
