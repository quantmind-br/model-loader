package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/quantmind-br/model-loader/internal/service/hfhub"
)

// fakeHub implements hubClient with canned results.
type fakeHub struct {
	results   []hfhub.SearchResult
	repo      *hfhub.RepoInfo
	searchErr error
	repoErr   error
}

func (f *fakeHub) Search(ctx context.Context, query string, limit int) ([]hfhub.SearchResult, error) {
	return f.results, f.searchErr
}
func (f *fakeHub) RepoInfo(ctx context.Context, repoID string) (*hfhub.RepoInfo, error) {
	return f.repo, f.repoErr
}
func (f *fakeHub) DownloadURL(repoID, filename string) string {
	return "https://hf/" + repoID + "/" + filename
}

func TestSearchHub_Table(t *testing.T) {
	h := &fakeHub{results: []hfhub.SearchResult{
		{ID: "org/model-a", Downloads: 100, Likes: 5, Tags: []string{"gguf"}, PipelineTag: "text-generation", LastModified: time.Now()},
		{ID: "org/model-b", Downloads: 3, Likes: 0, Tags: []string{"safetensors"}},
	}}
	var out bytes.Buffer
	if err := searchHub(&out, h, "qwen", 20, false); err != nil {
		t.Fatalf("searchHub: %v", err)
	}
	s := out.String()
	if !strings.Contains(s, "org/model-a") || !strings.Contains(s, "yes") {
		t.Fatalf("table missing data / gguf flag: %q", s)
	}
}

func TestSearchHub_JSON(t *testing.T) {
	h := &fakeHub{results: []hfhub.SearchResult{{ID: "org/x", Tags: []string{"gguf"}}}}
	var out bytes.Buffer
	if err := searchHub(&out, h, "x", 20, true); err != nil {
		t.Fatalf("searchHub: %v", err)
	}
	var items []searchItem
	if err := json.Unmarshal(out.Bytes(), &items); err != nil {
		t.Fatalf("unmarshal: %v\n%s", err, out.String())
	}
	if len(items) != 1 || !items[0].GGUF || items[0].ID != "org/x" {
		t.Fatalf("unexpected items: %+v", items)
	}
}

func TestSearchHub_EmptyQuery(t *testing.T) {
	var out bytes.Buffer
	if err := searchHub(&out, &fakeHub{}, "  ", 20, false); err == nil {
		t.Fatal("expected error for blank query")
	}
}

func TestShowRepoInfo_Table(t *testing.T) {
	h := &fakeHub{repo: &hfhub.RepoInfo{
		ID:   "org/model",
		Tags: []string{"gguf", "text-generation"},
		Siblings: []hfhub.Sibling{
			{RFilename: "model.Q4_K_M.gguf", Size: 4096},
			{RFilename: "README.md", Size: 100},
		},
	}}
	var out bytes.Buffer
	if err := showRepoInfo(&out, h, "org/model", false); err != nil {
		t.Fatalf("showRepoInfo: %v", err)
	}
	s := out.String()
	if !strings.Contains(s, "org/model") || !strings.Contains(s, "model.Q4_K_M.gguf") {
		t.Fatalf("missing repo/file: %q", s)
	}
	if !strings.Contains(s, "4.0KB") {
		t.Fatalf("missing humanBytes size column: %q", s)
	}
}

func TestShowRepoInfo_NotFound(t *testing.T) {
	var out bytes.Buffer
	err := showRepoInfo(&out, &fakeHub{repo: nil}, "org/missing", false)
	if err == nil || !strings.Contains(err.Error(), "not found") {
		t.Fatalf("expected 'repo not found' error, got: %v", err)
	}
}

func TestSearchHub_Error(t *testing.T) {
	var out bytes.Buffer
	err := searchHub(&out, &fakeHub{searchErr: errors.New("network down")}, "x", 20, false)
	if err == nil || !strings.Contains(err.Error(), "search:") {
		t.Fatalf("expected wrapped search error, got: %v", err)
	}
}

func TestShowRepoInfo_Error(t *testing.T) {
	var out bytes.Buffer
	err := showRepoInfo(&out, &fakeHub{repoErr: errors.New("network down")}, "org/m", false)
	if err == nil || !strings.Contains(err.Error(), "repo info:") {
		t.Fatalf("expected wrapped repo info error, got: %v", err)
	}
}

func TestShowRepoInfo_JSON(t *testing.T) {
	h := &fakeHub{repo: &hfhub.RepoInfo{ID: "org/model", Siblings: []hfhub.Sibling{{RFilename: "a.gguf", Size: 7}}}}
	var out bytes.Buffer
	if err := showRepoInfo(&out, h, "org/model", true); err != nil {
		t.Fatalf("showRepoInfo: %v", err)
	}
	var v repoInfoView
	if err := json.Unmarshal(out.Bytes(), &v); err != nil {
		t.Fatalf("unmarshal: %v\n%s", err, out.String())
	}
	if v.ID != "org/model" || len(v.Files) != 1 || v.Files[0].Filename != "a.gguf" {
		t.Fatalf("unexpected view: %+v", v)
	}
}
