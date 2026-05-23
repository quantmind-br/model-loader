package benchmark

import (
	"context"
	"io"
	"net/http"
	"strings"
	"testing"
)

// fakeDoer is a canned-response httpDoer for tests.
type fakeDoer struct {
	resp *http.Response
	err  error
}

func (f fakeDoer) Do(*http.Request) (*http.Response, error) { return f.resp, f.err }

func jsonResp(status int, body string) *http.Response {
	return &http.Response{
		StatusCode: status,
		Body:       io.NopCloser(strings.NewReader(body)),
		Header:     make(http.Header),
	}
}

func TestLexicalCosine(t *testing.T) {
	if v := lexicalCosine("the quick brown fox", "the quick brown fox"); v < 0.999 {
		t.Fatalf("identical strings cosine = %v, want ~1", v)
	}
	if v := lexicalCosine("alpha beta", "gamma delta"); v != 0 {
		t.Fatalf("disjoint strings cosine = %v, want 0", v)
	}
	mid := lexicalCosine("the quick brown fox", "the quick red fox")
	if mid <= 0 || mid >= 1 {
		t.Fatalf("partial-overlap cosine = %v, want strictly between 0 and 1", mid)
	}
}

func TestSimilarityFallsBackToLexical(t *testing.T) {
	// No doer + empty base → cannot reach embeddings → lexical fallback.
	g := similarityGrader{base: ""}
	score, method := g.Similarity(context.Background(), "hello world", "hello world")
	if method != "lexical" {
		t.Fatalf("method = %q, want lexical", method)
	}
	if score < 0.999 {
		t.Fatalf("score = %v, want ~1", score)
	}
}

func TestSimilarityUsesEmbeddings(t *testing.T) {
	// Two identical unit vectors → cosine 1, method "embeddings".
	body := `{"data":[{"embedding":[0.6,0.8]},{"embedding":[0.6,0.8]}]}`
	g := similarityGrader{doer: fakeDoer{resp: jsonResp(200, body)}, base: "http://x", model: "m"}
	score, method := g.Similarity(context.Background(), "a", "b")
	if method != "embeddings" {
		t.Fatalf("method = %q, want embeddings", method)
	}
	if score < 0.999 {
		t.Fatalf("score = %v, want ~1", score)
	}
}

func TestSimilarityEmbeddingsErrorFallsBack(t *testing.T) {
	g := similarityGrader{doer: fakeDoer{resp: jsonResp(404, "not found")}, base: "http://x", model: "m"}
	_, method := g.Similarity(context.Background(), "alpha beta", "alpha beta")
	if method != "lexical" {
		t.Fatalf("method = %q, want lexical on non-200", method)
	}
}

func TestEmbeddingsURL(t *testing.T) {
	if got := embeddingsURL("http://h:1/"); got != "http://h:1/v1/embeddings" {
		t.Fatalf("bare host: got %q", got)
	}
	if got := embeddingsURL("http://h:1/v1"); got != "http://h:1/v1/embeddings" {
		t.Fatalf("v1 suffix: got %q", got)
	}
}
