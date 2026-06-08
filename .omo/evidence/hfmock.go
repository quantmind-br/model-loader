// Mock Hugging Face Hub server for QA testing.
//
// Endpoints:
//   GET /api/models?search=*     -> JSON array of fake SearchResult
//   GET /api/models/{repoID}     -> JSON RepoInfo with siblings
//   GET /{repoID}/resolve/main/* -> small test file content
//
// Run:  go run hfmock.go (defaults to :9999)
package main

import (
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"strings"
	"time"
)

const addr = "localhost:9999"

type sibling struct {
	RFilename string `json:"rfilename"`
	Size      int64  `json:"size"`
}

type repoInfo struct {
	ID       string    `json:"id"`
	Tags     []string  `json:"tags"`
	Siblings []sibling `json:"siblings"`
}

type searchResult struct {
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

func nowRFC3339() string { return time.Now().UTC().Format(time.RFC3339) }

func main() {
	mux := http.NewServeMux()

	handler := func(w http.ResponseWriter, r *http.Request) {
		// Single-model lookup: /api/models/{org}/{repo}
		path := strings.TrimPrefix(r.URL.Path, "/api/models")
		if path != "" && path != "/" {
			repoID := strings.TrimPrefix(path, "/")
			info := repoInfo{
				ID:   repoID,
				Tags: []string{"gguf", "text-generation"},
				Siblings: []sibling{
					{RFilename: "README.md", Size: 1024},
					{RFilename: "config.json", Size: 512},
					{RFilename: "model-Q4_K_M.gguf", Size: 4096},
					{RFilename: "model-Q8_0.gguf", Size: 8192},
				},
			}
			log.Printf("repo_info repo=%s", repoID)
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(info)
			return
		}

		// Search: /api/models?search=...
		query := r.URL.Query().Get("search")
		log.Printf("search query=%q", query)
		results := []searchResult{
			{
				ID:           "fake-org/test-gguf-model",
				Author:       "fake-org",
				ModelID:      "fake-org/test-gguf-model",
				Tags:         []string{"gguf", "text-generation", "llama"},
				Downloads:    1234,
				Likes:        56,
				LastModified: nowRFC3339(),
				LibraryName:  "gguf",
				PipelineTag:  "text-generation",
			},
			{
				ID:           "fake-org/test-safetensors-model",
				Author:       "fake-org",
				ModelID:      "fake-org/test-safetensors-model",
				Tags:         []string{"safetensors", "text-generation"},
				Downloads:    789,
				Likes:        12,
				LastModified: nowRFC3339(),
				LibraryName:  "transformers",
				PipelineTag:  "text-generation",
			},
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(results)
	}
	mux.HandleFunc("/api/models", handler)
	mux.HandleFunc("/api/models/", handler)

	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		if !strings.Contains(r.URL.Path, "/resolve/main/") {
			http.NotFound(w, r)
			return
		}
		log.Printf("download path=%s user-agent=%q", r.URL.Path, r.Header.Get("User-Agent"))
		body := fmt.Sprintf("mock file content for %s\n%s\n",
			r.URL.Path,
			strings.Repeat("x", 128))
		w.Header().Set("Content-Type", "application/octet-stream")
		w.Header().Set("Content-Length", fmt.Sprintf("%d", len(body)))
		_, _ = w.Write([]byte(body))
	})

	log.Printf("hfmock listening on %s", addr)
	if err := http.ListenAndServe(addr, mux); err != nil {
		log.Fatal(err)
	}
}
