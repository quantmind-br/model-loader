package benchmark

import (
	_ "embed"
	"encoding/json"
	"fmt"
)

//go:embed data/arxiv_docs.json
var arxivDataset []byte

// ArxivDoc is one real arXiv abstract used as realistic filler ("quality
// haystack") for the long-context needle probe.
type ArxivDoc struct {
	ID       string `json:"id"`
	Title    string `json:"title"`
	Abstract string `json:"abstract"`
	Category string `json:"category"`
}

// loadArxivDocs decodes the embedded curated arXiv abstract pool.
func loadArxivDocs() ([]ArxivDoc, error) {
	var ds []ArxivDoc
	if err := json.Unmarshal(arxivDataset, &ds); err != nil {
		return nil, fmt.Errorf("decode arxiv docs: %w", err)
	}
	if len(ds) == 0 {
		return nil, fmt.Errorf("arxiv docs dataset is empty")
	}
	return ds, nil
}
