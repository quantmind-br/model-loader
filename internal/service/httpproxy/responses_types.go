package httpproxy

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
)

// Wire types for the OpenAI Responses API (POST /v1/responses), translated
// in-proxy to the backend's OpenAI chat completions (reusing oaiChatRequest/
// Response/StreamChunk). No SDK dependencies.

// responsesRequest is the POST /v1/responses body.
type responsesRequest struct {
	Model             string              `json:"model"`
	Input             json.RawMessage     `json:"input"`        // string | []responsesInputItem
	Instructions      string              `json:"instructions"` // → system message
	Tools             []responsesTool     `json:"tools,omitempty"`
	ToolChoice        json.RawMessage     `json:"tool_choice,omitempty"`
	MaxOutputTokens   *int                `json:"max_output_tokens,omitempty"`
	Temperature       *float64            `json:"temperature,omitempty"`
	TopP              *float64            `json:"top_p,omitempty"`
	Stream            bool                `json:"stream,omitempty"`
	ParallelToolCalls *bool               `json:"parallel_tool_calls,omitempty"`
	Reasoning         *responsesReasoning `json:"reasoning,omitempty"`
	Metadata          json.RawMessage     `json:"metadata,omitempty"`
}

type responsesReasoning struct {
	Effort string `json:"effort,omitempty"`
}

// responsesTool is the flat Responses tool shape ({type:"function",name,...}),
// unlike chat completions' nested {type,function:{...}}.
type responsesTool struct {
	Type        string          `json:"type"`
	Name        string          `json:"name"`
	Description string          `json:"description,omitempty"`
	Parameters  json.RawMessage `json:"parameters,omitempty"`
}

// responsesInputItem is one element of an input array.
type responsesInputItem struct {
	Type      string          `json:"type"`
	Role      string          `json:"role,omitempty"`
	Content   json.RawMessage `json:"content,omitempty"` // string | []responsesContentPart
	CallID    string          `json:"call_id,omitempty"`
	Name      string          `json:"name,omitempty"`
	Arguments string          `json:"arguments,omitempty"`
	Output    json.RawMessage `json:"output,omitempty"`  // function_call_output: string | object
	Summary   json.RawMessage `json:"summary,omitempty"` // reasoning summary
}

type responsesContentPart struct {
	Type     string `json:"type"` // input_text | output_text | input_image
	Text     string `json:"text,omitempty"`
	ImageURL string `json:"image_url,omitempty"`
}

// --- Responses output side ---------------------------------------------------

type responsesResponse struct {
	ID        string          `json:"id"`
	Object    string          `json:"object"` // "response"
	CreatedAt int64           `json:"created_at"`
	Status    string          `json:"status"` // "completed" | "in_progress"
	Model     string          `json:"model"`
	Output    []any           `json:"output"`
	Usage     *responsesUsage `json:"usage,omitempty"`
}

type responsesUsage struct {
	InputTokens  int `json:"input_tokens"`
	OutputTokens int `json:"output_tokens"`
	TotalTokens  int `json:"total_tokens"`
}

// Output items (discriminated by Type; no omitempty on discriminators so
// content_part accumulators in SDKs see explicit empty strings).
type responsesMessageItem struct {
	Type    string `json:"type"` // "message"
	ID      string `json:"id"`
	Status  string `json:"status"`
	Role    string `json:"role"` // "assistant"
	Content []any  `json:"content"`
}

type responsesOutputText struct {
	Type        string `json:"type"` // "output_text"
	Text        string `json:"text"`
	Annotations []any  `json:"annotations"`
}

type responsesFunctionCallItem struct {
	Type      string `json:"type"` // "function_call"
	ID        string `json:"id"`
	CallID    string `json:"call_id"`
	Name      string `json:"name"`
	Arguments string `json:"arguments"`
	Status    string `json:"status"`
}

type responsesReasoningItem struct {
	Type    string `json:"type"` // "reasoning"
	ID      string `json:"id"`
	Summary []any  `json:"summary"`
}

type responsesSummaryText struct {
	Type string `json:"type"` // "summary_text"
	Text string `json:"text"`
}

// newResponsesID returns a prefixed random id ("resp_"/"msg_"/"fc_"/"rs_").
func newResponsesID(prefix string) string {
	var b [16]byte
	_, _ = rand.Read(b[:])
	return prefix + hex.EncodeToString(b[:])
}
