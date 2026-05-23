package benchmark

import (
	"bytes"
	"context"
	"encoding/json"
	"math"
	"net/http"
	"regexp"
	"strings"
)

// similarityGrader scores how similar two free-form texts are, returning a
// cosine score in [0,1]. It first tries the server's OpenAI-compatible
// /v1/embeddings endpoint; on any failure (no base, transport error, non-200,
// malformed body, fewer than two vectors) it falls back to a lexical
// token-frequency cosine, which always works on plain strings. Used by the
// instruction-consistency check.
type similarityGrader struct {
	doer   httpDoer
	base   string // embeddings endpoint base; empty disables the embeddings path
	apiKey string
	model  string // embeddings model name (often the model under test; servers may ignore it)
}

type embeddingsRequest struct {
	Model string   `json:"model"`
	Input []string `json:"input"`
}

type embeddingsResponse struct {
	Data []struct {
		Embedding []float64 `json:"embedding"`
	} `json:"data"`
}

// Similarity returns cosine similarity in [0,1] and the method used
// ("embeddings" or "lexical"). It never errors: embeddings problems degrade to
// the lexical fallback.
func (g similarityGrader) Similarity(ctx context.Context, a, b string) (float64, string) {
	if v, ok := g.embedSimilarity(ctx, a, b); ok {
		return v, "embeddings"
	}
	return lexicalCosine(a, b), "lexical"
}

func (g similarityGrader) embedSimilarity(ctx context.Context, a, b string) (float64, bool) {
	if g.base == "" {
		return 0, false
	}
	body, err := json.Marshal(embeddingsRequest{Model: g.model, Input: []string{a, b}})
	if err != nil {
		return 0, false
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, embeddingsURL(g.base), bytes.NewReader(body))
	if err != nil {
		return 0, false
	}
	req.Header.Set("Content-Type", "application/json")
	if g.apiKey != "" {
		req.Header.Set("Authorization", "Bearer "+g.apiKey)
	}
	doer := g.doer
	if doer == nil {
		doer = http.DefaultClient
	}
	resp, err := doer.Do(req)
	if err != nil {
		return 0, false
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return 0, false
	}
	var er embeddingsResponse
	if err := json.NewDecoder(resp.Body).Decode(&er); err != nil {
		return 0, false
	}
	if len(er.Data) < 2 {
		return 0, false
	}
	return cosine(er.Data[0].Embedding, er.Data[1].Embedding)
}

// embeddingsURL builds the embeddings endpoint, tolerating a base that already
// carries the OpenAI "/v1" suffix (mirrors the logic in Complete).
func embeddingsURL(base string) string {
	base = strings.TrimRight(base, "/")
	if strings.HasSuffix(base, "/v1") {
		return base + "/embeddings"
	}
	return base + "/v1/embeddings"
}

// cosine returns the cosine similarity of two equal-length, non-zero vectors,
// clamped to [0,1]. The bool is false when the vectors are unusable.
func cosine(a, b []float64) (float64, bool) {
	if len(a) == 0 || len(a) != len(b) {
		return 0, false
	}
	var dot, na, nb float64
	for i := range a {
		dot += a[i] * b[i]
		na += a[i] * a[i]
		nb += b[i] * b[i]
	}
	if na == 0 || nb == 0 {
		return 0, false
	}
	c := dot / (math.Sqrt(na) * math.Sqrt(nb))
	return clamp01(c), true
}

var wordRe = regexp.MustCompile(`[a-z0-9]+`)

// termFreq builds a lowercase word-frequency vector.
func termFreq(s string) map[string]float64 {
	m := map[string]float64{}
	for _, w := range wordRe.FindAllString(strings.ToLower(s), -1) {
		m[w]++
	}
	return m
}

// lexicalCosine is the embeddings-free fallback: cosine over word-frequency
// vectors. Returns 0 when either side has no tokens.
func lexicalCosine(a, b string) float64 {
	fa, fb := termFreq(a), termFreq(b)
	if len(fa) == 0 || len(fb) == 0 {
		return 0
	}
	var dot, na, nb float64
	for t, va := range fa {
		na += va * va
		if vb, ok := fb[t]; ok {
			dot += va * vb
		}
	}
	for _, vb := range fb {
		nb += vb * vb
	}
	if na == 0 || nb == 0 {
		return 0
	}
	return clamp01(dot / (math.Sqrt(na) * math.Sqrt(nb)))
}

func clamp01(v float64) float64 {
	if v < 0 {
		return 0
	}
	if v > 1 {
		return 1
	}
	return v
}
