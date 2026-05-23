package benchmark

import "testing"

func TestLoadArxivDocs(t *testing.T) {
	docs, err := loadArxivDocs()
	if err != nil {
		t.Fatalf("loadArxivDocs: %v", err)
	}
	if len(docs) < 20 {
		t.Fatalf("want >= 20 abstracts, got %d", len(docs))
	}
	for i, d := range docs {
		if d.Abstract == "" || d.Title == "" {
			t.Errorf("doc %d (%s) has empty title/abstract", i, d.ID)
		}
	}
}
