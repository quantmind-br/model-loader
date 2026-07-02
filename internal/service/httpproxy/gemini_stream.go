package httpproxy

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strings"
)

// geminiStreamer converts an upstream OpenAI SSE stream into the Gemini stream
// shape: a sequence of "data: {candidates:[…]}" frames with no [DONE]
// terminator. Text/reasoning stream incrementally; tool calls are accumulated
// and flushed with the finishReason frame.
type geminiStreamer struct {
	sw     *sseWriter
	logger *slog.Logger
	model  string

	tools        map[int]*toolAccum
	toolOrder    []int
	lastToolIx   int
	usage        *oaiUsage
	finishReason string
	emittedBytes int
	sawFinal     bool
}

func (st *geminiStreamer) emitParts(parts []geminiOutPart, finishReason string) error {
	cand := geminiCandidate{Index: 0, Content: geminiOutContent{Role: "model", Parts: parts}, FinishReason: finishReason}
	return st.sw.writeData(geminiResponse{Candidates: []geminiCandidate{cand}, ModelVersion: st.model})
}

func (st *geminiStreamer) onChunk(c *oaiStreamChunk) error {
	if c.Usage != nil {
		st.usage = c.Usage
	}
	if len(c.Choices) == 0 {
		return nil
	}
	ch := c.Choices[0]
	if rc := ch.Delta.ReasoningContent; rc != "" {
		st.emittedBytes += len(rc)
		if err := st.emitParts([]geminiOutPart{{Thought: true, Text: rc}}, ""); err != nil {
			return err
		}
	}
	if tx := ch.Delta.Content; tx != "" {
		st.emittedBytes += len(tx)
		if err := st.emitParts([]geminiOutPart{{Text: tx}}, ""); err != nil {
			return err
		}
	}
	for _, tc := range ch.Delta.ToolCalls {
		ix := st.lastToolIx
		if tc.Index != nil {
			ix = *tc.Index
		}
		acc := st.tools[ix]
		if acc == nil {
			if st.tools == nil {
				st.tools = map[int]*toolAccum{}
			}
			acc = &toolAccum{}
			st.tools[ix] = acc
			st.toolOrder = append(st.toolOrder, ix)
		}
		st.lastToolIx = ix
		if tc.ID != "" {
			acc.id = tc.ID
		}
		if tc.Function.Name != "" {
			acc.name = tc.Function.Name
		}
		if frag := tc.Function.Arguments; frag != "" {
			acc.args.WriteString(frag)
			st.emittedBytes += len(frag)
		}
	}
	if ch.FinishReason != nil && *ch.FinishReason != "" {
		st.finishReason = *ch.FinishReason
		return st.emitFinal()
	}
	return nil
}

// emitFinal flushes accumulated tool calls plus the finishReason frame (once).
// Also called at stream end so tool calls survive backends that close without
// a finish_reason (CLIProxyAPI drops them there).
func (st *geminiStreamer) emitFinal() error {
	if st.sawFinal {
		return nil
	}
	st.sawFinal = true
	var parts []geminiOutPart
	for _, ix := range st.toolOrder {
		acc := st.tools[ix]
		parts = append(parts, geminiOutPart{FunctionCall: &geminiOutFuncCall{Name: acc.name, Args: parseArgsRaw(acc.args.String())}})
	}
	if st.finishReason == "" && len(st.toolOrder) > 0 {
		st.finishReason = "tool_calls"
	}
	return st.emitParts(parts, mapFinishReasonGemini(st.finishReason))
}

func (st *geminiStreamer) finish() error {
	if err := st.emitFinal(); err != nil {
		return err
	}
	var um *geminiUsageMetadata
	if st.usage != nil {
		um = &geminiUsageMetadata{
			PromptTokenCount:     st.usage.PromptTokens,
			CandidatesTokenCount: st.usage.CompletionTokens,
			TotalTokenCount:      st.usage.PromptTokens + st.usage.CompletionTokens,
		}
	} else {
		est := max((st.emittedBytes+3)/4, 1)
		um = &geminiUsageMetadata{CandidatesTokenCount: est, TotalTokenCount: est}
	}
	return st.sw.writeData(geminiResponse{Candidates: []geminiCandidate{}, UsageMetadata: um, ModelVersion: st.model})
}

// runGeminiStream converts one upstream OpenAI SSE body into Gemini SSE frames.
func runGeminiStream(ctx context.Context, w http.ResponseWriter, upstream io.Reader, model string, logger *slog.Logger) error {
	w.Header().Set("Content-Type", "text/event-stream; charset=utf-8")
	w.Header().Set("Cache-Control", "no-cache")
	w.WriteHeader(http.StatusOK)

	st := &geminiStreamer{sw: newSSEWriter(w), logger: logger, model: model}
	scanner := bufio.NewScanner(upstream)
	scanner.Buffer(make([]byte, 0, 64*1024), 4*1024*1024)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if !strings.HasPrefix(line, "data:") {
			continue
		}
		payload := strings.TrimSpace(strings.TrimPrefix(line, "data:"))
		if payload == "[DONE]" {
			return st.finish()
		}
		var chunk oaiStreamChunk
		if err := json.Unmarshal([]byte(payload), &chunk); err != nil {
			if ctx.Err() != nil {
				return ctx.Err()
			}
			return fmt.Errorf("malformed upstream chunk: %w", err)
		}
		if err := st.onChunk(&chunk); err != nil {
			return err
		}
	}
	if err := scanner.Err(); err != nil {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		return fmt.Errorf("upstream stream aborted: %w", err)
	}
	return st.finish()
}

// synthesizeGeminiStream emits Gemini frames from a complete OpenAI response
// (used when a stream=true upstream answered plain JSON 200).
func synthesizeGeminiStream(w http.ResponseWriter, oai *oaiChatResponse, model string) error {
	st := &geminiStreamer{sw: newSSEWriter(w), model: model}
	if len(oai.Choices) > 0 {
		ch := oai.Choices[0]
		fake := &oaiStreamChunk{Choices: []oaiStreamChoice{{Delta: oaiStreamDelta{
			ReasoningContent: ch.Message.ReasoningContent,
			Content:          ch.Message.Content,
		}}}}
		for i := range ch.Message.ToolCalls {
			tc := ch.Message.ToolCalls[i]
			ix := i
			var d oaiToolCallDelta
			d.Index = &ix
			d.ID = tc.ID
			d.Function.Name = tc.Function.Name
			d.Function.Arguments = tc.Function.Arguments
			fake.Choices[0].Delta.ToolCalls = append(fake.Choices[0].Delta.ToolCalls, d)
		}
		if err := st.onChunk(fake); err != nil {
			return err
		}
	}
	st.usage = oai.Usage
	return st.finish()
}
