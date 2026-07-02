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

// responsesStreamer converts an upstream OpenAI SSE stream into the OpenAI
// Responses event sequence. Reasoning and text are streamed as sequential
// output items (reasoning first); tool calls are accumulated and emitted whole
// at finish (like anthropicStreamer).
type responsesStreamer struct {
	sw      *sseWriter
	logger  *slog.Logger
	model   string
	respID  string
	created int64

	seq      int
	outIndex int

	msgOpen bool
	msgID   string
	textBuf strings.Builder

	rsnOpen bool
	rsnID   string
	rsnBuf  strings.Builder

	tools      map[int]*toolAccum
	toolOrder  []int
	lastToolIx int

	usage        *oaiUsage
	finishReason string
	emittedBytes int
}

func (st *responsesStreamer) emit(eventType string, payload map[string]any) error {
	payload["type"] = eventType
	payload["sequence_number"] = st.seq
	st.seq++
	return st.sw.writeEvent(eventType, payload)
}

func (st *responsesStreamer) skeleton(status string, output []any, usage *responsesUsage) map[string]any {
	m := map[string]any{
		"id": st.respID, "object": "response", "created_at": st.created,
		"status": status, "model": st.model, "output": output,
	}
	if usage != nil {
		m["usage"] = usage
	}
	return m
}

func (st *responsesStreamer) start() error {
	if err := st.emit("response.created", map[string]any{"response": st.skeleton("in_progress", []any{}, nil)}); err != nil {
		return err
	}
	return st.emit("response.in_progress", map[string]any{"response": st.skeleton("in_progress", []any{}, nil)})
}

// --- reasoning item ---------------------------------------------------------

func (st *responsesStreamer) ensureReasoning() error {
	if st.rsnOpen {
		return nil
	}
	st.rsnID = newResponsesID("rs_")
	item := map[string]any{"type": "reasoning", "id": st.rsnID, "summary": []any{}}
	if err := st.emit("response.output_item.added", map[string]any{"output_index": st.outIndex, "item": item}); err != nil {
		return err
	}
	part := map[string]any{"type": "summary_text", "text": ""}
	if err := st.emit("response.reasoning_summary_part.added", map[string]any{"item_id": st.rsnID, "output_index": st.outIndex, "summary_index": 0, "part": part}); err != nil {
		return err
	}
	st.rsnOpen = true
	return nil
}

func (st *responsesStreamer) closeReasoning() error {
	if !st.rsnOpen {
		return nil
	}
	text := st.rsnBuf.String()
	if err := st.emit("response.reasoning_summary_text.done", map[string]any{"item_id": st.rsnID, "output_index": st.outIndex, "summary_index": 0, "text": text}); err != nil {
		return err
	}
	if err := st.emit("response.reasoning_summary_part.done", map[string]any{"item_id": st.rsnID, "output_index": st.outIndex, "summary_index": 0, "part": map[string]any{"type": "summary_text", "text": text}}); err != nil {
		return err
	}
	item := map[string]any{"type": "reasoning", "id": st.rsnID, "summary": []any{map[string]any{"type": "summary_text", "text": text}}}
	if err := st.emit("response.output_item.done", map[string]any{"output_index": st.outIndex, "item": item}); err != nil {
		return err
	}
	st.rsnOpen = false
	st.outIndex++
	return nil
}

// --- message item -----------------------------------------------------------

func (st *responsesStreamer) ensureMessage() error {
	if st.msgOpen {
		return nil
	}
	if err := st.closeReasoning(); err != nil {
		return err
	}
	st.msgID = newResponsesID("msg_")
	item := map[string]any{"type": "message", "id": st.msgID, "status": "in_progress", "role": "assistant", "content": []any{}}
	if err := st.emit("response.output_item.added", map[string]any{"output_index": st.outIndex, "item": item}); err != nil {
		return err
	}
	part := map[string]any{"type": "output_text", "text": "", "annotations": []any{}}
	if err := st.emit("response.content_part.added", map[string]any{"item_id": st.msgID, "output_index": st.outIndex, "content_index": 0, "part": part}); err != nil {
		return err
	}
	st.msgOpen = true
	return nil
}

func (st *responsesStreamer) closeMessage() error {
	if !st.msgOpen {
		return nil
	}
	text := st.textBuf.String()
	if err := st.emit("response.output_text.done", map[string]any{"item_id": st.msgID, "output_index": st.outIndex, "content_index": 0, "text": text}); err != nil {
		return err
	}
	part := map[string]any{"type": "output_text", "text": text, "annotations": []any{}}
	if err := st.emit("response.content_part.done", map[string]any{"item_id": st.msgID, "output_index": st.outIndex, "content_index": 0, "part": part}); err != nil {
		return err
	}
	item := map[string]any{"type": "message", "id": st.msgID, "status": "completed", "role": "assistant", "content": []any{part}}
	if err := st.emit("response.output_item.done", map[string]any{"output_index": st.outIndex, "item": item}); err != nil {
		return err
	}
	st.msgOpen = false
	st.outIndex++
	return nil
}

func (st *responsesStreamer) onChunk(c *oaiStreamChunk) error {
	if c.Usage != nil {
		st.usage = c.Usage
	}
	if len(c.Choices) == 0 {
		return nil
	}
	ch := c.Choices[0]
	if rc := ch.Delta.ReasoningContent; rc != "" {
		if err := st.ensureReasoning(); err != nil {
			return err
		}
		st.rsnBuf.WriteString(rc)
		st.emittedBytes += len(rc)
		if err := st.emit("response.reasoning_summary_text.delta", map[string]any{"item_id": st.rsnID, "output_index": st.outIndex, "summary_index": 0, "delta": rc}); err != nil {
			return err
		}
	}
	if tx := ch.Delta.Content; tx != "" {
		if err := st.ensureMessage(); err != nil {
			return err
		}
		st.textBuf.WriteString(tx)
		st.emittedBytes += len(tx)
		if err := st.emit("response.output_text.delta", map[string]any{"item_id": st.msgID, "output_index": st.outIndex, "content_index": 0, "delta": tx}); err != nil {
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
	}
	return nil
}

// emitToolItems emits one function_call output item per accumulated tool call.
func (st *responsesStreamer) emitToolItems() ([]any, error) {
	var items []any
	for _, ix := range st.toolOrder {
		acc := st.tools[ix]
		callID := sanitizeToolID(acc.id)
		fcID := newResponsesID("fc_")
		args := strings.TrimSpace(acc.args.String())
		if args == "" {
			args = "{}"
		} else {
			args = fixJSON(args)
		}
		added := map[string]any{"type": "function_call", "id": fcID, "call_id": callID, "name": acc.name, "arguments": "", "status": "in_progress"}
		if err := st.emit("response.output_item.added", map[string]any{"output_index": st.outIndex, "item": added}); err != nil {
			return nil, err
		}
		if err := st.emit("response.function_call_arguments.delta", map[string]any{"item_id": fcID, "output_index": st.outIndex, "delta": args}); err != nil {
			return nil, err
		}
		if err := st.emit("response.function_call_arguments.done", map[string]any{"item_id": fcID, "output_index": st.outIndex, "arguments": args}); err != nil {
			return nil, err
		}
		done := map[string]any{"type": "function_call", "id": fcID, "call_id": callID, "name": acc.name, "arguments": args, "status": "completed"}
		if err := st.emit("response.output_item.done", map[string]any{"output_index": st.outIndex, "item": done}); err != nil {
			return nil, err
		}
		items = append(items, done)
		st.outIndex++
	}
	return items, nil
}

func (st *responsesStreamer) finish() error {
	if err := st.closeReasoning(); err != nil {
		return err
	}
	var output []any
	if st.msgOpen {
		text := st.textBuf.String()
		msgItem := map[string]any{"type": "message", "id": st.msgID, "status": "completed", "role": "assistant",
			"content": []any{map[string]any{"type": "output_text", "text": text, "annotations": []any{}}}}
		if err := st.closeMessage(); err != nil {
			return err
		}
		output = append(output, msgItem)
	}
	toolItems, err := st.emitToolItems()
	if err != nil {
		return err
	}
	output = append(output, toolItems...)

	var usage *responsesUsage
	if st.usage != nil {
		usage = &responsesUsage{InputTokens: st.usage.PromptTokens, OutputTokens: st.usage.CompletionTokens,
			TotalTokens: st.usage.PromptTokens + st.usage.CompletionTokens}
	} else {
		usage = &responsesUsage{OutputTokens: max((st.emittedBytes+3)/4, 1)}
	}
	return st.emit("response.completed", map[string]any{"response": st.skeleton("completed", output, usage)})
}

func (st *responsesStreamer) fail(msg string) error {
	return st.emit("response.failed", map[string]any{"response": st.skeleton("failed", []any{}, nil), "error": map[string]any{"message": msg}})
}

// runResponsesStream converts one upstream OpenAI SSE body into Responses SSE.
func runResponsesStream(ctx context.Context, w http.ResponseWriter, upstream io.Reader, model string, createdAt int64, logger *slog.Logger) error {
	w.Header().Set("Content-Type", "text/event-stream; charset=utf-8")
	w.Header().Set("Cache-Control", "no-cache")
	w.WriteHeader(http.StatusOK)

	st := &responsesStreamer{sw: newSSEWriter(w), logger: logger, model: model, respID: newResponsesID("resp_"), created: createdAt}
	if err := st.start(); err != nil {
		return err
	}
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
			_ = st.fail("malformed upstream chunk: " + err.Error())
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
		_ = st.fail("upstream stream aborted: " + err.Error())
		return fmt.Errorf("upstream stream aborted: %w", err)
	}
	return st.finish()
}

// synthesizeResponsesStream emits the full Responses event sequence from an
// already-complete OpenAI response — used when a stream=true upstream answered
// with plain JSON 200 instead of SSE.
func synthesizeResponsesStream(w http.ResponseWriter, oai *oaiChatResponse, model string, createdAt int64) error {
	st := &responsesStreamer{sw: newSSEWriter(w), model: model, respID: newResponsesID("resp_"), created: createdAt}
	if err := st.start(); err != nil {
		return err
	}
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
