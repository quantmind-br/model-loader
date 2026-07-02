package httpproxy

import (
	"strings"
	"testing"
)

func TestTokenizerForModel(t *testing.T) {
	// Local model ids (and empty) must resolve to a codec without error.
	for _, m := range []string{"qwen3.6-27b-mtp", "gemma-4-12b-it", "gpt-4o", ""} {
		if _, err := tokenizerForModel(m); err != nil {
			t.Errorf("tokenizerForModel(%q): %v", m, err)
		}
	}
}

func TestCountChatTokens(t *testing.T) {
	enc, err := tokenizerForModel("qwen3.6-27b")
	if err != nil {
		t.Fatalf("tokenizer: %v", err)
	}

	if n := countChatTokens(enc, &oaiChatRequest{}); n != 0 {
		t.Errorf("empty request = %d, want 0", n)
	}

	short := &oaiChatRequest{Messages: []oaiChatMessage{{Role: "user", Content: "hello world"}}}
	n1 := countChatTokens(enc, short)
	if n1 <= 0 {
		t.Errorf("non-empty = %d, want > 0 (real tokenizer)", n1)
	}
	if n2 := countChatTokens(enc, short); n2 != n1 {
		t.Errorf("non-deterministic: %d vs %d", n1, n2)
	}

	long := &oaiChatRequest{Messages: []oaiChatMessage{{Role: "user",
		Content: "hello world this is a considerably longer message carrying many more tokens than the short one"}}}
	if countChatTokens(enc, long) <= n1 {
		t.Errorf("longer content should count more tokens")
	}

	// Tools contribute (name/description/params).
	withTool := &oaiChatRequest{
		Messages: []oaiChatMessage{{Role: "user", Content: "hi"}},
		Tools:    []oaiTool{{Type: "function", Function: oaiToolFunction{Name: "get_weather", Description: "Get the weather for a city", Parameters: []byte(`{"type":"object","properties":{"city":{"type":"string"}}}`)}}},
	}
	small := &oaiChatRequest{Messages: []oaiChatMessage{{Role: "user", Content: "hi"}}}
	if countChatTokens(enc, withTool) <= countChatTokens(enc, small) {
		t.Errorf("tools should add tokens")
	}

	// A base64 image must add a FLAT cost, not be tokenized as a huge string.
	img := &oaiChatRequest{Messages: []oaiChatMessage{{Role: "user", Content: []oaiContentPart{
		{Type: "text", Text: "look"},
		{Type: "image_url", ImageURL: &oaiImageURL{URL: "data:image/png;base64," + strings.Repeat("A", 100000)}},
	}}}}
	nImg := countChatTokens(enc, img)
	if nImg > 5000 {
		t.Errorf("base64 image tokenized as text? got %d, want ~flat cost", nImg)
	}
	if nImg < imageTokenCost {
		t.Errorf("image must add the flat cost, got %d (want >= %d)", nImg, imageTokenCost)
	}
}
