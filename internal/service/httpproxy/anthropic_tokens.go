package httpproxy

import (
	"strings"

	"github.com/tiktoken-go/tokenizer"
)

// imageTokenCost is the flat per-image token estimate. Images are not text and
// must never be tokenized as their base64/URL string (that would count tens of
// thousands of nonsense tokens for a single inline image).
const imageTokenCost = 1200

// tokenizerForModel returns a tiktoken codec for a model id (mirrors
// CLIProxyAPI's TokenizerForModel). Local models (Qwen/Gemma/…) do not match a
// GPT prefix and fall through to o200k_base, the most recent general-purpose
// encoding — an approximation, since each local model has its own tokenizer.
func tokenizerForModel(model string) (tokenizer.Codec, error) {
	s := strings.ToLower(strings.TrimSpace(model))
	switch {
	case strings.HasPrefix(s, "gpt-5"):
		return tokenizer.ForModel(tokenizer.GPT5)
	case strings.HasPrefix(s, "gpt-4.1"):
		return tokenizer.ForModel(tokenizer.GPT41)
	case strings.HasPrefix(s, "gpt-4o"):
		return tokenizer.ForModel(tokenizer.GPT4o)
	case strings.HasPrefix(s, "gpt-4"):
		return tokenizer.ForModel(tokenizer.GPT4)
	case strings.HasPrefix(s, "gpt-3"):
		return tokenizer.ForModel(tokenizer.GPT35Turbo)
	case strings.HasPrefix(s, "o1"):
		return tokenizer.ForModel(tokenizer.O1)
	case strings.HasPrefix(s, "o3"):
		return tokenizer.ForModel(tokenizer.O3)
	default:
		return tokenizer.Get(tokenizer.O200kBase)
	}
}

// countChatTokens counts prompt tokens for a translated OpenAI chat request
// using a real tokenizer (following CLIProxyAPI's CountOpenAIChatTokens): all
// text segments are joined and encoded, plus a flat cost per image (image data
// URIs/URLs are counted as imageTokenCost, never tokenized as text).
func countChatTokens(enc tokenizer.Codec, req *oaiChatRequest) int {
	var segs []string
	images := 0
	add := func(s string) {
		if strings.TrimSpace(s) != "" {
			segs = append(segs, s)
		}
	}
	for _, m := range req.Messages {
		add(m.Role)
		switch c := m.Content.(type) {
		case string:
			add(c)
		case []oaiContentPart:
			for _, p := range c {
				if p.Type == "image_url" || p.ImageURL != nil {
					images++
					continue
				}
				add(p.Text)
			}
		}
		for _, tc := range m.ToolCalls {
			add(tc.Function.Name)
			add(tc.Function.Arguments)
		}
		add(m.ToolCallID)
	}
	for _, t := range req.Tools {
		add(t.Function.Name)
		add(t.Function.Description)
		add(string(t.Function.Parameters))
	}

	total := images * imageTokenCost
	if joined := strings.TrimSpace(strings.Join(segs, "\n")); joined != "" {
		if n, err := enc.Count(joined); err == nil {
			total += n
		}
	}
	return total
}
