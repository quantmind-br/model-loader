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
func (r *Runner) runLlamaBench(ctx context.Context, base, model string, ps tpPreset) (ProblemResult, ProblemTranscript) {
	res := ProblemResult{ProblemID: ps.id(), ProblemName: ps.name()}
	tr := ProblemTranscript{ProblemID: ps.id(), ProblemName: ps.name()}

	ctxTok := r.runCtxTokens
	if ctxTok <= 0 {
		ctxTok = 8192 // unknown context → conservative default
	}
	promptTokens := ctxTok * ps.FillPct / 100
	if maxPrompt := ctxTok - ps.GenTokens - 64; maxPrompt > 0 && promptTokens > maxPrompt {
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

	for range r.warmup {
		select {
		case <-ctx.Done():
			res.Err = ctx.Err().Error()
			tr.Error = res.Err
			return res, tr
		default:
		}
		warmCtx, warmCancel := context.WithTimeout(ctx, repTimeout)
		_, _ = Complete(warmCtx, nil, base, "", ChatRequest{
			Model: model, Temperature: 0, MaxTokens: ps.GenTokens, IgnoreEOS: true, Messages: msgs,
		})
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
	for range r.reps {
		select {
		case <-ctx.Done():
			res.Err = ctx.Err().Error()
			tr.Error = res.Err
			return res, tr
		default:
		}
		reqCtx, cancel := context.WithTimeout(ctx, repTimeout)
		comp, err := Complete(reqCtx, nil, base, "", ChatRequest{
			Model:       model,
			Temperature: 0,
			MaxTokens:   ps.GenTokens,
			IgnoreEOS:   true, // ask for exactly tg tokens where honored; real code still hits EOS earlier
			Messages:    msgs,
		})
		cancel()
		if err != nil {
			res.Err = err.Error()
			tr.Error = err.Error()
			return res, tr
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
	res.CompletionTokens = tgSum / ok
	res.Detail = fmt.Sprintf("fill %d%% (≈%d of %d ctx) tg=%d; tok/s %.1f ±%.1f [%.1f–%.1f]; TTFT %dms (n=%d)",
		ps.FillPct, promptTokens, ctxTok, ps.GenTokens, mean, stddev, minTPS, maxTPS, res.TTFTms, ok)
	if fromServer {
		res.Detail += " (server timings)"
	}
	if short > 0 {
		res.Detail += fmt.Sprintf("; %d short dropped", short)
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
