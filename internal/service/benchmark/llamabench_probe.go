package benchmark

import (
	"context"
	"fmt"
	"math"
	"strconv"
	"strings"
	"time"
)

// --- llama-bench throughput probe ------------------------------------------

// tpPreset is one throughput configuration: fill FillPct% of the profile's
// effective context with real content, then generate GenTokens.
type tpPreset struct {
	FillPct   int // 1..100, percent of effective context to prefill
	GenTokens int
}

func (p tpPreset) name() string { return fmt.Sprintf("fill %d%% / tg %d", p.FillPct, p.GenTokens) }
func (p tpPreset) id() string   { return fmt.Sprintf("fill-%d-%d", p.FillPct, p.GenTokens) }

// defaultPresets must stay in sync with the benchmark.llamabench.presets default in internal/config/config.go.
var defaultPresets = []tpPreset{
	{FillPct: 5, GenTokens: 256}, // near-empty KV: best-case decode
	{FillPct: 25, GenTokens: 256},
	{FillPct: 50, GenTokens: 256},
	{FillPct: 90, GenTokens: 128}, // near-full KV: worst-case decode
}

// parsePresets parses "<fill>%/<tg>" strings into tpPresets. Empty input → defaults.
func parsePresets(raw []string) ([]tpPreset, error) {
	if len(raw) == 0 {
		return append([]tpPreset(nil), defaultPresets...), nil
	}
	out := make([]tpPreset, 0, len(raw))
	for _, s := range raw {
		parts := strings.SplitN(strings.TrimSpace(s), "/", 2)
		if len(parts) != 2 {
			return nil, fmt.Errorf("invalid llama-bench preset %q (want \"<fill>%%/<tg>\", e.g. \"50%%/256\")", s)
		}
		fillStr := strings.TrimSpace(parts[0])
		if !strings.HasSuffix(fillStr, "%") {
			return nil, fmt.Errorf("invalid llama-bench preset %q (want \"<fill>%%/<tg>\", e.g. \"50%%/256\")", s)
		}
		fill, err := strconv.Atoi(strings.TrimSpace(strings.TrimSuffix(fillStr, "%")))
		if err != nil || fill < 1 || fill > 100 {
			return nil, fmt.Errorf("invalid fill percent in preset %q (want 1..100)", s)
		}
		tg, err := strconv.Atoi(strings.TrimSpace(parts[1]))
		if err != nil || tg <= 0 {
			return nil, fmt.Errorf("invalid gen tokens in preset %q (want a positive int after \"%%/\")", s)
		}
		out = append(out, tpPreset{FillPct: fill, GenTokens: tg})
	}
	return out, nil
}

// runLlamaBench measures generation throughput for one preset: it prefills the
// profile's context to FillPct% with real code content, asks for tg tokens,
// repeats r.reps times, and averages TTFT / tokens-per-second across the
// successful repetitions. The model field, streaming and timing all come from
// Complete, so it works against any OpenAI-compatible backend
// (llama-server / vLLM / SGLang).
//
// Prompt sizing: buildCodeContext writes promptTokens*4 chars and assumes
// ~4 chars/token, but real tokenizers (o200k_base, llama.cpp's BPE, Qwen's
// tiktoken) measure **~3 chars/token** on Python code — a ~25% overestimate.
// At 90% fill on a 262144-ctx profile that pushes the actual request above
// max_model_len and the backend rejects with 400. The cap below reserves room
// for the chat template (system prompt + role markers, ~200 tokens across
// Qwen/Llama/Gemma/OAI templates), the requested completion, and the
// tokenizer underestimate.
func (r *Runner) runLlamaBench(ctx context.Context, base, model string, ps tpPreset) (ProblemResult, ProblemTranscript) {
	res := ProblemResult{ProblemID: ps.id(), ProblemName: ps.name()}
	tr := ProblemTranscript{ProblemID: ps.id(), ProblemName: ps.name()}

	ctxTok := r.runCtxTokens
	if ctxTok <= 0 {
		ctxTok = 8192 // unknown context → conservative default
	}
	// Reserve room for completion + chat-template overhead (system prompt +
	// structural tokens like `<|im_start|>user\n…<|im_end|>`). 256 covers
	// Qwen3.5 (~120 tok), Llama 3 (~180), Gemma (~90) with margin.
	const chatTemplateSlack = 256
	maxPrompt := ctxTok - ps.GenTokens - chatTemplateSlack
	if maxPrompt < 64 {
		maxPrompt = 64
	}
	// buildCodeContext writes promptTokens*4 chars; real tokenizers measure
	// ~3 chars/token on Python code, so the produced prompt is ~33% larger
	// than this budget. Scale the budget down by 3/4 so the actual tokenized
	// prompt fits in maxPrompt after we apply the completion+template cap below.
	target := ctxTok * ps.FillPct / 100
	promptTokens := target * 3 / 4
	if promptTokens > maxPrompt {
		promptTokens = maxPrompt
	}
	if promptTokens < 64 {
		promptTokens = 64
	}
	// Deep prefill costs minutes on large contexts (dflash 128k ≈ 10 min);
	// extend the per-rep deadline by a ~100 tok/s prefill floor on top of the
	// base timeout so big fills don't spuriously time out.
	repTimeout := r.cfg.Timeout + time.Duration(promptTokens/100)*time.Second

	prompt := buildCodeContext(r.codeGenProblems, promptTokens)
	msgs := []ChatMessage{
		{Role: "system", Content: "You are an expert programmer. Continue writing clean, idiomatic code."},
		{Role: "user", Content: prompt},
	}

	var warmupFailed int
	for range r.warmup {
		select {
		case <-ctx.Done():
			res.Err = ctx.Err().Error()
			res.FailPhase = phaseInfer
			tr.Error = res.Err
			return res, tr
		default:
		}
		warmCtx, warmCancel := context.WithTimeout(ctx, repTimeout)
		if _, err := Complete(warmCtx, nil, base, "", ChatRequest{
			Model: model, MaxTokens: ps.GenTokens, IgnoreEOS: true, Messages: msgs,
		}); err != nil {
			// A failing warmup predicts failing reps; keep a trace instead of
			// discarding it (BR6) — it lands in Detail below.
			warmupFailed++
		}
		warmCancel()
	}

	// A sample counts if it reaches tg tokens or at least this floor — enough for
	// a stable decode-rate estimate. Real-content generations end at EOS well
	// before tg, so a full-tg gate would drop every one on backends that ignore
	// ignore_eos (e.g. the native dflash_server).
	minSample := ps.GenTokens
	if minSample > 32 {
		minSample = 32
	}

	var ttftSum, totalSum, ppTpsSum float64
	var ppSum, tgSum, short int
	var tpsSamples []float64
	var fromServer bool
	var lastContent string
	var repErrs int
	var lastRepErr string
	for range r.reps {
		select {
		case <-ctx.Done():
			res.Err = ctx.Err().Error()
			res.FailPhase = phaseInfer
			tr.Error = res.Err
			return res, tr
		default:
		}
		reqCtx, cancel := context.WithTimeout(ctx, repTimeout)
		comp, err := Complete(reqCtx, nil, base, "", ChatRequest{
			Model:       model,
			MaxTokens:   ps.GenTokens,
			OnDelta:     r.streamHeartbeat(res.ProblemID, res.ProblemName),
			IgnoreEOS:   true, // ask for exactly tg tokens where honored; real code still hits EOS earlier
			Messages:    msgs,
		})
		cancel()
		if err != nil {
			// A cancelled run still aborts; any other rep failure degrades to
			// the remaining samples instead of voiding reps already measured
			// (BR6). The preset only errors when nothing was measured.
			if ctx.Err() != nil {
				res.Err = ctx.Err().Error()
				res.FailPhase = phaseInfer
				tr.Error = res.Err
				return res, tr
			}
			repErrs++
			lastRepErr = err.Error()
			continue
		}
		lastContent = comp.Content
		// Average samples that reached tg tokens OR generated enough for a stable
		// decode-rate estimate (minSample). Decode tok/s is a per-token rate from
		// server timings, valid for any sufficiently long run — see minSample above.
		if comp.CompletionTokens < minSample {
			short++
			continue
		}
		ttftSum += float64(comp.TTFT.Milliseconds())
		tpsSamples = append(tpsSamples, comp.TokensPerSecond)
		ppTpsSum += comp.PromptProcessingTPS
		totalSum += float64(comp.Total.Milliseconds())
		ppSum += comp.PromptTokens
		tgSum += comp.CompletionTokens
		if comp.TimingsFromServer {
			fromServer = true
		}
	}

	tr.ModelResponse = lastContent
	ok := len(tpsSamples)
	if ok == 0 {
		if repErrs > 0 {
			// Nothing measured and at least one rep failed outright: the
			// preset is an error, not a zero measurement (BR6).
			res.Err = fmt.Sprintf("%d/%d reps failed (last: %s)", repErrs, r.reps, firstLine(lastRepErr))
			res.FailPhase = phaseInfer
			tr.Error = res.Err
			return res, tr
		}
		// Every sample was too short to measure (empty / near-empty generation).
		res.Detail = fmt.Sprintf("fill %d%%: no measurable sample: all %d generated < %d tokens", ps.FillPct, short, minSample)
		return res, tr
	}
	n := float64(ok)
	mean, stddev, minTPS, maxTPS := tpsStats(tpsSamples)
	res.Resolved = true
	res.FillPct = ps.FillPct
	res.TTFTms = int64(ttftSum / n)
	res.TokensPerSecond = mean
	res.DecodeTPS = mean
	res.TPSStdDev = stddev
	res.TPSMin = minTPS
	res.TPSMax = maxTPS
	res.PromptProcessingTPS = ppTpsSum / n
	res.TotalMs = int64(totalSum / n)
	res.PromptTokens = ppSum / ok
	// Display the *target* fill (the preset's intent), not the clamped budget —
	// the budget shrinks by ~25% to compensate for the buildCodeContext
	// chars/token underestimate and chat-template overhead. PromptTokens /
	// CompletionTokens below are the **measured** server-side counts.
	res.Detail = fmt.Sprintf("fill %d%% (≈%d of %d ctx, budget %d) tg=%d; tok/s %.1f ±%.1f [%.1f–%.1f]; TTFT %dms (n=%d)",
		ps.FillPct, target, ctxTok, promptTokens, ps.GenTokens, mean, stddev, minTPS, maxTPS, res.TTFTms, ok)
	if fromServer {
		res.Detail += " (server timings)"
	}
	if short > 0 {
		res.Detail += fmt.Sprintf("; %d short dropped", short)
	}
	if repErrs > 0 {
		res.Detail += fmt.Sprintf("; %d reps failed (last: %s)", repErrs, firstLine(lastRepErr))
	}
	if warmupFailed > 0 {
		res.Detail += fmt.Sprintf("; %d warmup failures", warmupFailed)
	}
	return res, tr
}

// tpsStats returns mean, population standard deviation, min and max of the
// per-rep tok/s samples.
func tpsStats(samples []float64) (mean, stddev, min, max float64) {
	n := float64(len(samples))
	if n == 0 {
		return 0, 0, 0, 0
	}
	min, max = samples[0], samples[0]
	var sum float64
	for _, s := range samples {
		sum += s
		if s < min {
			min = s
		}
		if s > max {
			max = s
		}
	}
	mean = sum / n
	var varSum float64
	for _, s := range samples {
		d := s - mean
		varSum += d * d
	}
	stddev = math.Sqrt(varSum / n)
	return mean, stddev, min, max
}

// buildCodeContext concatenates real HumanEval items (prompt + canonical
// solution) until ~promptTokens (≈4 chars/token), cycling the pool. Real code
// makes speculative-decode acceptance representative of real workloads, unlike
// pseudo-random filler which collapses draft acceptance to ~1.
func buildCodeContext(problems []CodeGenProblem, promptTokens int) string {
	charBudget := promptTokens * 4
	var b strings.Builder
	b.WriteString("Continue this Python module. Keep implementing well-typed, documented functions in the same style.\n\n")
	if len(problems) == 0 { // unit-test fallback: deterministic code-shaped filler
		for i := 0; b.Len() < charBudget; i++ {
			fmt.Fprintf(&b, "def func_%d(x: int) -> int:\n    \"\"\"Return x plus %d.\"\"\"\n    return x + %d\n\n", i, i, i)
		}
		return b.String()
	}
	for i := 0; b.Len() < charBudget; i++ {
		p := problems[i%len(problems)]
		fmt.Fprintf(&b, "%s%s\n\n", p.Prompt, p.CanonicalSolution)
	}
	return b.String()
}
