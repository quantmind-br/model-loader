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

// sseWriter serializes Anthropic SSE frames ("event: <t>\ndata: <json>\n\n"),
// flushing after every frame so tokens reach the client immediately (the
// translation-layer equivalent of the reverse proxy's FlushInterval: -1).
type sseWriter struct {
	w  http.ResponseWriter
	fl http.Flusher
}

func newSSEWriter(w http.ResponseWriter) *sseWriter {
	fl, _ := w.(http.Flusher) // nil-tolerant: exotic wrappers degrade to unflushed writes
	return &sseWriter{w: w, fl: fl}
}

func (sw *sseWriter) writeEvent(event string, payload any) error {
	data, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	if _, err := fmt.Fprintf(sw.w, "event: %s\ndata: %s\n\n", event, data); err != nil {
		return err
	}
	if sw.fl != nil {
		sw.fl.Flush()
	}
	return nil
}

// writeData emits an event-less SSE frame ("data: <json>\n\n") — the Gemini
// stream shape (no `event:` line, no [DONE] terminator).
func (sw *sseWriter) writeData(payload any) error {
	data, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	if _, err := fmt.Fprintf(sw.w, "data: %s\n\n", data); err != nil {
		return err
	}
	if sw.fl != nil {
		sw.fl.Flush()
	}
	return nil
}

// anthropicStreamer converts one upstream OpenAI SSE stream into the
// Anthropic event sequence. Reasoning/text blocks stream incrementally; tool
// calls are accumulated and emitted whole at finish (see tools below).
type anthropicStreamer struct {
	sw     *sseWriter
	logger *slog.Logger
	model  string // client-requested model echoed in message_start

	nextIndex          int    // next Anthropic content block index
	curKind            string // "" | "thinking" | "text" (tool_use is buffered)
	finishReason       string // last non-empty finish_reason seen
	usage              *oaiUsage
	emittedBytes       int // fallback output-token estimate when usage never arrives
	warnedExtraChoices bool

	// Tool calls are accumulated across deltas — id, name, and argument
	// fragments can arrive in any order and split across chunks — and emitted
	// whole at finish (following CLIProxyAPI). This lets us repair arguments
	// with fixJSON and never open a block with a missing name or id.
	tools      map[int]*toolAccum
	toolOrder  []int // tool indices in first-seen order
	lastToolIx int   // index reused when a fragment omits tool_calls[].index
}

type toolAccum struct {
	id   string
	name string
	args strings.Builder
}

func (st *anthropicStreamer) start() error {
	skeleton := anthropicMessageResponse{
		ID:      newAnthropicMessageID(),
		Type:    "message",
		Role:    "assistant",
		Model:   st.model,
		Content: []any{},
	}
	if err := st.sw.writeEvent("message_start", sseMessageStart{Type: "message_start", Message: skeleton}); err != nil {
		return err
	}
	return st.sw.writeEvent("ping", ssePing{Type: "ping"})
}

func (st *anthropicStreamer) closeBlock() error {
	if st.curKind == "" {
		return nil
	}
	st.curKind = ""
	return st.sw.writeEvent("content_block_stop",
		sseContentBlockStop{Type: "content_block_stop", Index: st.nextIndex - 1})
}

// ensureBlock opens a thinking/text block, closing whatever block is open.
func (st *anthropicStreamer) ensureBlock(kind string) error {
	if st.curKind == kind {
		return nil
	}
	if err := st.closeBlock(); err != nil {
		return err
	}
	var block any
	if kind == "thinking" {
		block = anthropicThinkingBlock{Type: "thinking", Thinking: ""}
	} else {
		block = anthropicTextBlock{Type: "text", Text: ""}
	}
	if err := st.sw.writeEvent("content_block_start",
		sseContentBlockStart{Type: "content_block_start", Index: st.nextIndex, ContentBlock: block}); err != nil {
		return err
	}
	st.curKind = kind
	st.nextIndex++
	return nil
}

// emitAccumulatedTools writes one full tool_use block per accumulated call
// (start → single input_json_delta → stop), in first-seen order. The id is
// sanitized (or generated), and the argument buffer is run through fixJSON so
// single-quoted / lightly-malformed arguments still parse client-side.
func (st *anthropicStreamer) emitAccumulatedTools() error {
	for _, ix := range st.toolOrder {
		acc := st.tools[ix]
		block := anthropicToolUseBlock{Type: "tool_use", ID: sanitizeToolID(acc.id), Name: acc.name, Input: map[string]any{}}
		if err := st.sw.writeEvent("content_block_start",
			sseContentBlockStart{Type: "content_block_start", Index: st.nextIndex, ContentBlock: block}); err != nil {
			return err
		}
		args := strings.TrimSpace(acc.args.String())
		if args == "" {
			args = "{}"
		} else {
			args = fixJSON(args)
		}
		if err := st.sw.writeEvent("content_block_delta",
			sseContentBlockDelta{Type: "content_block_delta", Index: st.nextIndex, Delta: sseInputJSONDelta{Type: "input_json_delta", PartialJSON: args}}); err != nil {
			return err
		}
		if err := st.sw.writeEvent("content_block_stop",
			sseContentBlockStop{Type: "content_block_stop", Index: st.nextIndex}); err != nil {
			return err
		}
		st.nextIndex++
	}
	return nil
}

func (st *anthropicStreamer) writeDelta(delta any) error {
	return st.sw.writeEvent("content_block_delta",
		sseContentBlockDelta{Type: "content_block_delta", Index: st.nextIndex - 1, Delta: delta})
}

func (st *anthropicStreamer) onChunk(c *oaiStreamChunk) error {
	if c.Usage != nil {
		st.usage = c.Usage // last one wins (final include_usage chunk has empty choices)
	}
	if len(c.Choices) == 0 {
		return nil
	}
	if len(c.Choices) > 1 && !st.warnedExtraChoices {
		st.warnedExtraChoices = true
		st.logger.Warn("anthropic_stream_extra_choices_ignored", "n", len(c.Choices))
	}
	ch := c.Choices[0]

	// Order matters: llama.cpp interleaves reasoning before content — a chunk
	// carrying both must close the thinking block before opening text.
	if rc := ch.Delta.ReasoningContent; rc != "" {
		if err := st.ensureBlock("thinking"); err != nil {
			return err
		}
		st.emittedBytes += len(rc)
		if err := st.writeDelta(sseThinkingDelta{Type: "thinking_delta", Thinking: rc}); err != nil {
			return err
		}
	}
	if tx := ch.Delta.Content; tx != "" {
		if err := st.ensureBlock("text"); err != nil {
			return err
		}
		st.emittedBytes += len(tx)
		if err := st.writeDelta(sseTextDelta{Type: "text_delta", Text: tx}); err != nil {
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
		st.finishReason = *ch.FinishReason // finalize later: the usage chunk still follows
	}
	return nil
}

func (st *anthropicStreamer) finish() error {
	if err := st.closeBlock(); err != nil {
		return err
	}
	if err := st.emitAccumulatedTools(); err != nil {
		return err
	}
	if len(st.toolOrder) > 0 && st.finishReason == "" {
		st.finishReason = "tool_calls" // tool calls seen but the backend sent no finish_reason
	}
	var usage anthropicUsage
	if st.usage != nil {
		usage = anthropicUsage{InputTokens: st.usage.PromptTokens, OutputTokens: st.usage.CompletionTokens}
	} else {
		usage.OutputTokens = max((st.emittedBytes+3)/4, 1)
	}
	if err := st.sw.writeEvent("message_delta", sseMessageDelta{
		Type:  "message_delta",
		Delta: sseMessageDeltaBody{StopReason: mapFinishReason(st.finishReason)},
		Usage: usage,
	}); err != nil {
		return err
	}
	return st.sw.writeEvent("message_stop", sseMessageStop{Type: "message_stop"})
}

// fail emits the mid-stream error frame. No message_stop follows an error.
func (st *anthropicStreamer) fail(msg string) error {
	return st.sw.writeEvent("error", anthropicErrorEnvelope{
		Type:  "error",
		Error: anthropicErrorBody{Type: "api_error", Message: msg},
	})
}

// runAnthropicStream converts one upstream OpenAI SSE body into Anthropic SSE
// frames on w. ctx is the client request context: a canceled ctx means the
// client went away and the stream ends silently (no error frame).
func runAnthropicStream(ctx context.Context, w http.ResponseWriter, upstream io.Reader, requestedModel string, logger *slog.Logger) error {
	w.Header().Set("Content-Type", "text/event-stream; charset=utf-8")
	w.Header().Set("Cache-Control", "no-cache")
	w.WriteHeader(http.StatusOK)

	st := &anthropicStreamer{sw: newSSEWriter(w), logger: logger, model: requestedModel}
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
			return err // client write failure; caller's resp.Body.Close cancels upstream
		}
	}
	if err := scanner.Err(); err != nil {
		if ctx.Err() != nil {
			return ctx.Err() // client disconnected: end silently
		}
		_ = st.fail("upstream stream aborted: " + err.Error())
		return fmt.Errorf("upstream stream aborted: %w", err)
	}
	// Clean EOF without [DONE]: treat as completion.
	return st.finish()
}

// synthesizeAnthropicStream emits the full Anthropic frame sequence from an
// already-complete message — used when a stream=true upstream answered with a
// plain JSON 200 instead of SSE.
func synthesizeAnthropicStream(sw *sseWriter, resp *anthropicMessageResponse) error {
	skeleton := *resp
	skeleton.Content = []any{}
	skeleton.StopReason = nil
	skeleton.StopSequence = nil
	skeleton.Usage = anthropicUsage{}
	if err := sw.writeEvent("message_start", sseMessageStart{Type: "message_start", Message: skeleton}); err != nil {
		return err
	}
	if err := sw.writeEvent("ping", ssePing{Type: "ping"}); err != nil {
		return err
	}
	for i, blk := range resp.Content {
		var start, delta any
		switch b := blk.(type) {
		case anthropicThinkingBlock:
			start = anthropicThinkingBlock{Type: "thinking", Thinking: ""}
			delta = sseThinkingDelta{Type: "thinking_delta", Thinking: b.Thinking}
		case anthropicTextBlock:
			start = anthropicTextBlock{Type: "text", Text: ""}
			delta = sseTextDelta{Type: "text_delta", Text: b.Text}
		case anthropicToolUseBlock:
			start = anthropicToolUseBlock{Type: "tool_use", ID: b.ID, Name: b.Name, Input: map[string]any{}}
			args, err := json.Marshal(b.Input)
			if err != nil {
				args = []byte("{}")
			}
			delta = sseInputJSONDelta{Type: "input_json_delta", PartialJSON: string(args)}
		default:
			continue
		}
		if err := sw.writeEvent("content_block_start",
			sseContentBlockStart{Type: "content_block_start", Index: i, ContentBlock: start}); err != nil {
			return err
		}
		if err := sw.writeEvent("content_block_delta",
			sseContentBlockDelta{Type: "content_block_delta", Index: i, Delta: delta}); err != nil {
			return err
		}
		if err := sw.writeEvent("content_block_stop",
			sseContentBlockStop{Type: "content_block_stop", Index: i}); err != nil {
			return err
		}
	}
	stop := "end_turn"
	if resp.StopReason != nil {
		stop = *resp.StopReason
	}
	if err := sw.writeEvent("message_delta", sseMessageDelta{
		Type:  "message_delta",
		Delta: sseMessageDeltaBody{StopReason: stop},
		Usage: resp.Usage,
	}); err != nil {
		return err
	}
	return sw.writeEvent("message_stop", sseMessageStop{Type: "message_stop"})
}
