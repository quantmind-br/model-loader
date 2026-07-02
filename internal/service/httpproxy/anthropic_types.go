package httpproxy

import (
	"bytes"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
)

// This file holds the hand-rolled wire types for the Anthropic-standard
// endpoints (POST /v1/messages, POST /v1/messages/count_tokens) and for the
// OpenAI chat-completions payloads the proxy sends to / receives from the
// loaded backend when translating those requests. No SDK dependencies.

// anthropicMessagesRequest is the POST /v1/messages (and count_tokens) body.
// Unknown fields (metadata, thinking, cache_control, betas, ...) are ignored
// by plain json.Unmarshal on purpose — never reject them.
type anthropicMessagesRequest struct {
	Model         string               `json:"model"`
	MaxTokens     *int                 `json:"max_tokens"` // pointer: required-field validation
	Messages      []anthropicMessage   `json:"messages"`
	System        json.RawMessage      `json:"system,omitempty"` // string | [{type:"text",text}]
	StopSequences []string             `json:"stop_sequences,omitempty"`
	Stream        bool                 `json:"stream,omitempty"`
	Temperature   *float64             `json:"temperature,omitempty"`
	TopP          *float64             `json:"top_p,omitempty"`
	TopK          *int                 `json:"top_k,omitempty"`
	Tools         []anthropicTool      `json:"tools,omitempty"`
	ToolChoice    *anthropicToolChoice `json:"tool_choice,omitempty"`
	// Reasoning controls. Thinking is {type:"enabled"|"disabled"|"adaptive"|
	// "auto", budget_tokens?:int}; OutputConfig is Claude 4.6's {effort:level}.
	// Both feed the canonical thinking pivot (anthropic_thinking.go).
	Thinking     json.RawMessage `json:"thinking,omitempty"`
	OutputConfig json.RawMessage `json:"output_config,omitempty"`
	Metadata     json.RawMessage `json:"metadata,omitempty"` // {user_id?:string}
}

type anthropicMessage struct {
	Role    string                  `json:"role"` // "user" | "assistant"
	Content anthropicMessageContent `json:"content"`
}

// anthropicMessageContent accepts the Anthropic dual form: a plain string or
// a list of typed content blocks.
type anthropicMessageContent struct {
	IsString bool
	Text     string
	Blocks   []anthropicContentBlock
}

func (c *anthropicMessageContent) UnmarshalJSON(b []byte) error {
	trimmed := bytes.TrimSpace(b)
	if len(trimmed) > 0 && trimmed[0] == '"' {
		c.IsString = true
		return json.Unmarshal(b, &c.Text)
	}
	c.IsString = false
	return json.Unmarshal(b, &c.Blocks)
}

// anthropicContentBlock is the tagged union for input blocks, discriminated
// by Type: text | image | tool_use | tool_result | thinking |
// redacted_thinking | document. Unknown types are skipped by the translator.
type anthropicContentBlock struct {
	Type      string           `json:"type"`
	Text      string           `json:"text,omitempty"`
	Source    *anthropicSource `json:"source,omitempty"`      // image, document
	ID        string           `json:"id,omitempty"`          // tool_use
	Name      string           `json:"name,omitempty"`        // tool_use
	Input     json.RawMessage  `json:"input,omitempty"`       // tool_use (object)
	ToolUseID string           `json:"tool_use_id,omitempty"` // tool_result
	Content   json.RawMessage  `json:"content,omitempty"`     // tool_result: string | [block]
	IsError   bool             `json:"is_error,omitempty"`    // tool_result
	Thinking  string           `json:"thinking,omitempty"`    // thinking
}

type anthropicSource struct {
	Type      string `json:"type"` // "base64" | "url" | "text"
	MediaType string `json:"media_type,omitempty"`
	Data      string `json:"data,omitempty"`
	URL       string `json:"url,omitempty"`
}

type anthropicTool struct {
	Name        string          `json:"name"`
	Description string          `json:"description,omitempty"`
	InputSchema json.RawMessage `json:"input_schema"`
}

type anthropicToolChoice struct {
	Type                   string `json:"type"` // auto | any | tool | none
	Name                   string `json:"name,omitempty"`
	DisableParallelToolUse *bool  `json:"disable_parallel_tool_use,omitempty"`
}

// --- Anthropic response side -------------------------------------------------

// anthropicMessageResponse is the POST /v1/messages success body and the
// skeleton embedded in the message_start stream event. StopReason stays nil
// in the skeleton, hence the pointer.
type anthropicMessageResponse struct {
	ID           string         `json:"id"`
	Type         string         `json:"type"` // "message"
	Role         string         `json:"role"` // "assistant"
	Model        string         `json:"model"`
	Content      []any          `json:"content"` // text/thinking/tool_use blocks; [] not null
	StopReason   *string        `json:"stop_reason"`
	StopSequence *string        `json:"stop_sequence"`
	Usage        anthropicUsage `json:"usage"`
}

// Output blocks intentionally carry NO omitempty on the discriminated fields:
// content_block_start must emit {"type":"text","text":""} for SDK accumulators.
type anthropicTextBlock struct {
	Type string `json:"type"`
	Text string `json:"text"`
}

type anthropicThinkingBlock struct {
	Type     string `json:"type"`
	Thinking string `json:"thinking"`
}

type anthropicToolUseBlock struct {
	Type  string         `json:"type"`
	ID    string         `json:"id"`
	Name  string         `json:"name"`
	Input map[string]any `json:"input"` // {} on unmarshal failure, never null
}

type anthropicUsage struct {
	InputTokens  int `json:"input_tokens"`
	OutputTokens int `json:"output_tokens"`
}

type anthropicCountTokensResponse struct {
	InputTokens int `json:"input_tokens"`
}

// newAnthropicMessageID returns "msg_" + 24 hex chars from crypto/rand.
func newAnthropicMessageID() string {
	var b [12]byte
	_, _ = rand.Read(b[:]) // crypto/rand.Read cannot realistically fail
	return "msg_" + hex.EncodeToString(b[:])
}

// --- Anthropic SSE frame payloads ---------------------------------------------

type ssePing struct {
	Type string `json:"type"`
}

type sseMessageStart struct {
	Type    string                   `json:"type"`
	Message anthropicMessageResponse `json:"message"`
}

type sseContentBlockStart struct {
	Type         string `json:"type"`
	Index        int    `json:"index"`
	ContentBlock any    `json:"content_block"`
}

type sseContentBlockDelta struct {
	Type  string `json:"type"`
	Index int    `json:"index"`
	Delta any    `json:"delta"`
}

type sseContentBlockStop struct {
	Type  string `json:"type"`
	Index int    `json:"index"`
}

type sseTextDelta struct {
	Type string `json:"type"`
	Text string `json:"text"`
}

type sseThinkingDelta struct {
	Type     string `json:"type"`
	Thinking string `json:"thinking"`
}

type sseInputJSONDelta struct {
	Type        string `json:"type"`
	PartialJSON string `json:"partial_json"`
}

type sseMessageDelta struct {
	Type  string              `json:"type"`
	Delta sseMessageDeltaBody `json:"delta"`
	Usage anthropicUsage      `json:"usage"`
}

type sseMessageDeltaBody struct {
	StopReason   string  `json:"stop_reason"`
	StopSequence *string `json:"stop_sequence"`
}

type sseMessageStop struct {
	Type string `json:"type"`
}

// anthropicErrorEnvelope is the Anthropic error shape, used both as the
// plain HTTP error body of the new routes and as the mid-stream failure
// frame (event: error).
type anthropicErrorEnvelope struct {
	Type  string             `json:"type"` // "error"
	Error anthropicErrorBody `json:"error"`
}

type anthropicErrorBody struct {
	Type    string `json:"type"`
	Message string `json:"message"`
}

// --- OpenAI upstream side -----------------------------------------------------

type oaiChatRequest struct {
	Model             string            `json:"model"`
	Messages          []oaiChatMessage  `json:"messages"`
	MaxTokens         int               `json:"max_tokens,omitempty"`
	Stop              []string          `json:"stop,omitempty"`
	Stream            bool              `json:"stream,omitempty"`
	StreamOptions     *oaiStreamOptions `json:"stream_options,omitempty"`
	Temperature       *float64          `json:"temperature,omitempty"`
	TopP              *float64          `json:"top_p,omitempty"`
	TopK              *int              `json:"top_k,omitempty"` // llama.cpp/vLLM extension
	Tools             []oaiTool         `json:"tools,omitempty"`
	ToolChoice        any               `json:"tool_choice,omitempty"` // "auto"|"required"|"none" | oaiToolChoiceFunc
	ParallelToolCalls *bool             `json:"parallel_tool_calls,omitempty"`
	// Reasoning controls emitted from the thinking pivot. ReasoningEffort is the
	// OpenAI/gpt-oss discrete level; ChatTemplateKwargs carries enable_thinking
	// for the Qwen3/llama.cpp family. Backends ignore whichever they don't use.
	ReasoningEffort    string         `json:"reasoning_effort,omitempty"`
	ChatTemplateKwargs map[string]any `json:"chat_template_kwargs,omitempty"`
	User               string         `json:"user,omitempty"` // from metadata.user_id
}

type oaiStreamOptions struct {
	IncludeUsage bool `json:"include_usage"`
}

type oaiChatMessage struct {
	Role       string        `json:"role"`
	Content    any           `json:"content"` // string | []oaiContentPart | nil
	ToolCalls  []oaiToolCall `json:"tool_calls,omitempty"`
	ToolCallID string        `json:"tool_call_id,omitempty"`
}

type oaiContentPart struct {
	Type     string       `json:"type"`
	Text     string       `json:"text,omitempty"`
	ImageURL *oaiImageURL `json:"image_url,omitempty"`
}

type oaiImageURL struct {
	URL string `json:"url"`
}

type oaiToolCall struct {
	ID       string          `json:"id"`
	Type     string          `json:"type"`
	Function oaiFunctionCall `json:"function"`
}

type oaiFunctionCall struct {
	Name      string `json:"name"`
	Arguments string `json:"arguments"`
}

type oaiTool struct {
	Type     string          `json:"type"`
	Function oaiToolFunction `json:"function"`
}

type oaiToolFunction struct {
	Name        string          `json:"name"`
	Description string          `json:"description,omitempty"`
	Parameters  json.RawMessage `json:"parameters,omitempty"`
}

// oaiToolChoiceFunc is the forced-tool form of OpenAI tool_choice.
type oaiToolChoiceFunc struct {
	Type     string `json:"type"` // "function"
	Function struct {
		Name string `json:"name"`
	} `json:"function"`
}

type oaiChatResponse struct {
	ID      string      `json:"id"`
	Choices []oaiChoice `json:"choices"`
	Usage   *oaiUsage   `json:"usage"`
}

type oaiChoice struct {
	FinishReason string         `json:"finish_reason"`
	Message      oaiRespMessage `json:"message"`
}

type oaiRespMessage struct {
	Content          string        `json:"content"` // JSON null decodes to ""
	ReasoningContent string        `json:"reasoning_content"`
	ToolCalls        []oaiToolCall `json:"tool_calls"`
}

type oaiUsage struct {
	PromptTokens     int `json:"prompt_tokens"`
	CompletionTokens int `json:"completion_tokens"`
}

type oaiStreamChunk struct {
	Choices []oaiStreamChoice `json:"choices"`
	Usage   *oaiUsage         `json:"usage"` // final include_usage chunk (empty choices)
}

type oaiStreamChoice struct {
	Delta        oaiStreamDelta `json:"delta"`
	FinishReason *string        `json:"finish_reason"`
}

type oaiStreamDelta struct {
	Content          string             `json:"content"`
	ReasoningContent string             `json:"reasoning_content"`
	ToolCalls        []oaiToolCallDelta `json:"tool_calls"`
}

type oaiToolCallDelta struct {
	Index    *int   `json:"index"` // pointer: absent vs 0
	ID       string `json:"id"`
	Function struct {
		Name      string `json:"name"`
		Arguments string `json:"arguments"`
	} `json:"function"`
}
