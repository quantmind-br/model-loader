package httpproxy

import "encoding/json"

// Wire types for the Gemini API (/v1beta/models/{model}:generateContent,
// :streamGenerateContent, :countTokens), translated in-proxy to the backend's
// OpenAI chat completions (reusing the oai* types). No SDK dependencies.

// --- Gemini request ----------------------------------------------------------

type geminiRequest struct {
	Contents          []geminiContent         `json:"contents"`
	SystemInstruction *geminiContent          `json:"systemInstruction"`
	Tools             []geminiTool            `json:"tools"`
	ToolConfig        *geminiToolConfig       `json:"toolConfig"`
	GenerationConfig  *geminiGenerationConfig `json:"generationConfig"`
}

// UnmarshalJSON accepts the snake_case aliases the official SDKs emit
// (system_instruction, generation_config, tool_config).
func (r *geminiRequest) UnmarshalJSON(b []byte) error {
	type alias geminiRequest
	var v struct {
		alias
		SystemInstructionSnake *geminiContent          `json:"system_instruction"`
		GenerationConfigSnake  *geminiGenerationConfig `json:"generation_config"`
		ToolConfigSnake        *geminiToolConfig       `json:"tool_config"`
	}
	if err := json.Unmarshal(b, &v); err != nil {
		return err
	}
	*r = geminiRequest(v.alias)
	if r.SystemInstruction == nil {
		r.SystemInstruction = v.SystemInstructionSnake
	}
	if r.GenerationConfig == nil {
		r.GenerationConfig = v.GenerationConfigSnake
	}
	if r.ToolConfig == nil {
		r.ToolConfig = v.ToolConfigSnake
	}
	return nil
}

type geminiContent struct {
	Role  string       `json:"role,omitempty"`
	Parts []geminiPart `json:"parts,omitempty"`
}

type geminiPart struct {
	Text             string                  `json:"text,omitempty"`
	InlineData       *geminiInlineData       `json:"inlineData,omitempty"`
	FunctionCall     *geminiFunctionCall     `json:"functionCall,omitempty"`
	FunctionResponse *geminiFunctionResponse `json:"functionResponse,omitempty"`
	Thought          bool                    `json:"thought,omitempty"`
}

type geminiInlineData struct {
	MimeType string `json:"mimeType,omitempty"`
	Data     string `json:"data,omitempty"`
}

type geminiFunctionCall struct {
	Name string          `json:"name"`
	Args json.RawMessage `json:"args,omitempty"`
}

type geminiFunctionResponse struct {
	Name     string          `json:"name"`
	Response json.RawMessage `json:"response,omitempty"`
}

type geminiTool struct {
	FunctionDeclarations []geminiFunctionDeclaration `json:"functionDeclarations"`
}

type geminiFunctionDeclaration struct {
	Name                 string          `json:"name"`
	Description          string          `json:"description,omitempty"`
	Parameters           json.RawMessage `json:"parameters,omitempty"`
	ParametersJSONSchema json.RawMessage `json:"parametersJsonSchema,omitempty"`
}

type geminiToolConfig struct {
	FunctionCallingConfig *struct {
		Mode string `json:"mode"`
	} `json:"functionCallingConfig"`
}

type geminiGenerationConfig struct {
	Temperature     *float64              `json:"temperature"`
	TopP            *float64              `json:"topP"`
	TopK            *int                  `json:"topK"`
	MaxOutputTokens *int                  `json:"maxOutputTokens"`
	StopSequences   []string              `json:"stopSequences"`
	CandidateCount  *int                  `json:"candidateCount"`
	ThinkingConfig  *geminiThinkingConfig `json:"thinkingConfig"`
}

type geminiThinkingConfig struct {
	ThinkingBudget  *int   `json:"thinkingBudget"`
	ThinkingLevel   string `json:"thinkingLevel"`
	IncludeThoughts bool   `json:"includeThoughts"`
}

// --- Gemini response ---------------------------------------------------------

type geminiResponse struct {
	Candidates    []geminiCandidate    `json:"candidates"`
	UsageMetadata *geminiUsageMetadata `json:"usageMetadata,omitempty"`
	ModelVersion  string               `json:"modelVersion,omitempty"`
}

type geminiCandidate struct {
	Content      geminiOutContent `json:"content"`
	FinishReason string           `json:"finishReason,omitempty"`
	Index        int              `json:"index"`
}

// geminiOutContent/geminiOutPart mirror geminiContent/geminiPart but carry
// output-shaped function calls (args as an object) and always emit role.
type geminiOutContent struct {
	Role  string          `json:"role"`
	Parts []geminiOutPart `json:"parts"`
}

type geminiOutPart struct {
	Text         string             `json:"text,omitempty"`
	Thought      bool               `json:"thought,omitempty"`
	FunctionCall *geminiOutFuncCall `json:"functionCall,omitempty"`
}

type geminiOutFuncCall struct {
	Name string          `json:"name"`
	Args json.RawMessage `json:"args"`
}

type geminiUsageMetadata struct {
	PromptTokenCount     int `json:"promptTokenCount"`
	CandidatesTokenCount int `json:"candidatesTokenCount"`
	TotalTokenCount      int `json:"totalTokenCount"`
	ThoughtsTokenCount   int `json:"thoughtsTokenCount,omitempty"`
}

// --- Gemini countTokens / models ---------------------------------------------

type geminiCountTokensResponse struct {
	TotalTokens         int                        `json:"totalTokens"`
	PromptTokensDetails []geminiModalityTokenCount `json:"promptTokensDetails"`
}

type geminiModalityTokenCount struct {
	Modality   string `json:"modality"`
	TokenCount int    `json:"tokenCount"`
}

type geminiModel struct {
	Name                       string   `json:"name"`
	Version                    string   `json:"version,omitempty"`
	DisplayName                string   `json:"displayName,omitempty"`
	Description                string   `json:"description,omitempty"`
	InputTokenLimit            int      `json:"inputTokenLimit,omitempty"`
	OutputTokenLimit           int      `json:"outputTokenLimit,omitempty"`
	SupportedGenerationMethods []string `json:"supportedGenerationMethods,omitempty"`
	SupportedInputModalities   []string `json:"supportedInputModalities,omitempty"`
	SupportedOutputModalities  []string `json:"supportedOutputModalities,omitempty"`
}

type geminiModelsList struct {
	Models []geminiModel `json:"models"`
}
