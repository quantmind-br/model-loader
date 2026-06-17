package benchmark

import (
	"context"
	"fmt"
	"math"
	"strconv"
	"strings"
)

// --- llama-bench throughput probe ------------------------------------------

// tpPreset is one parsed throughput configuration: pp tokens in, tg tokens out.
type tpPreset struct {
	PromptTokens int
	GenTokens    int
}

func (p tpPreset) name() string { return fmt.Sprintf("pp %d / tg %d", p.PromptTokens, p.GenTokens) }
func (p tpPreset) id() string   { return fmt.Sprintf("tp-%d-%d", p.PromptTokens, p.GenTokens) }

// defaultPresets must stay in sync with the benchmark.llamabench.presets default in internal/config/config.go.
var defaultPresets = []tpPreset{
	{PromptTokens: 128, GenTokens: 512}, // chat-like: short prefill, long gen
	{PromptTokens: 512, GenTokens: 128},
	{PromptTokens: 2048, GenTokens: 256},
	{PromptTokens: 4096, GenTokens: 256},
	{PromptTokens: 8192, GenTokens: 128}, // RAG-like: long prefill, short gen
	{PromptTokens: 16384, GenTokens: 64}, // extreme RAG
}

// parsePresets parses "pp/tg" strings into tpPresets. Empty input → defaults.
func parsePresets(raw []string) ([]tpPreset, error) {
	if len(raw) == 0 {
		return append([]tpPreset(nil), defaultPresets...), nil
	}
	out := make([]tpPreset, 0, len(raw))
	for _, s := range raw {
		parts := strings.SplitN(strings.TrimSpace(s), "/", 2)
		if len(parts) != 2 {
			return nil, fmt.Errorf("invalid llama-bench preset %q (want \"pp/tg\", e.g. \"512/128\")", s)
		}
		pp, err := strconv.Atoi(strings.TrimSpace(parts[0]))
		if err != nil || pp <= 0 {
			return nil, fmt.Errorf("invalid prompt size in preset %q (want \"pp/tg\" with positive ints)", s)
		}
		tg, err := strconv.Atoi(strings.TrimSpace(parts[1]))
		if err != nil || tg <= 0 {
			return nil, fmt.Errorf("invalid gen size in preset %q (want \"pp/tg\" with positive ints)", s)
		}
		out = append(out, tpPreset{PromptTokens: pp, GenTokens: tg})
	}
	return out, nil
}

// runLlamaBench measures generation throughput for one preset: it sends a
// fixed-size prompt asking for tg tokens, repeated r.reps times, and averages
// TTFT / tokens-per-second across the successful repetitions. The model field,
// streaming and timing all come from Complete, so it works against any
// OpenAI-compatible backend (llama-server / vLLM / SGLang).
func (r *Runner) runLlamaBench(ctx context.Context, base, model string, ps tpPreset) (ProblemResult, ProblemTranscript) {
	res := ProblemResult{ProblemID: ps.id(), ProblemName: ps.name()}
	tr := ProblemTranscript{ProblemID: ps.id(), ProblemName: ps.name()}

	prompt := buildFixedPrompt(ps.PromptTokens)
	msgs := []ChatMessage{
		{Role: "system", Content: "You are a verbose writing assistant. Continue at length."},
		{Role: "user", Content: prompt},
	}

	for w := 0; w < r.warmup; w++ {
		select {
		case <-ctx.Done():
			res.Err = ctx.Err().Error()
			tr.Error = res.Err
			return res, tr
		default:
		}
		warmCtx, warmCancel := context.WithTimeout(ctx, r.cfg.Timeout)
		_, _ = Complete(warmCtx, nil, base, "", ChatRequest{
			Model: model, Temperature: 0, MaxTokens: ps.GenTokens, IgnoreEOS: true, Messages: msgs,
		})
		warmCancel()
	}

	var ttftSum, totalSum, ppTpsSum float64
	var ppSum, tgSum, short int
	var tpsSamples []float64
	var fromServer bool
	var lastContent string
	for i := 0; i < r.reps; i++ {
		select {
		case <-ctx.Done():
			res.Err = ctx.Err().Error()
			tr.Error = res.Err
			return res, tr
		default:
		}
		reqCtx, cancel := context.WithTimeout(ctx, r.cfg.Timeout)
		comp, err := Complete(reqCtx, nil, base, "", ChatRequest{
			Model:       model,
			Temperature: 0,
			MaxTokens:   ps.GenTokens,
			IgnoreEOS:   true, // force exactly tg tokens so samples stay comparable
			Messages:    msgs,
		})
		cancel()
		if err != nil {
			res.Err = err.Error()
			tr.Error = err.Error()
			return res, tr
		}
		lastContent = comp.Content
		// Only average full-length samples: a backend that ignored ignore_eos and
		// stopped early would otherwise skew tok/s and make the preset
		// incomparable across profiles.
		if comp.CompletionTokens < ps.GenTokens {
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
		// Every sample stopped before tg tokens — the backend doesn't honor
		// ignore_eos, so this preset can't be measured reliably here.
		res.Detail = fmt.Sprintf("no full-length sample: all %d stopped before %d gen tokens (backend ignored ignore_eos?)", short, ps.GenTokens)
		return res, tr
	}
	n := float64(ok)
	mean, stddev, minTPS, maxTPS := tpsStats(tpsSamples)
	res.Resolved = true
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
	res.Detail = fmt.Sprintf("pp≈%d tg=%d; tok/s %.1f ±%.1f [%.1f–%.1f]; TTFT %dms (n=%d)",
		res.PromptTokens, ps.GenTokens, mean, stddev, minTPS, maxTPS, res.TTFTms, ok)
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

// buildFixedPrompt generates ~promptTokens of deterministic filler prose
// (≈4 chars/token) to drive a fixed prompt-processing load.
func buildFixedPrompt(promptTokens int) string {
	charBudget := promptTokens * 4
	var b strings.Builder
	b.WriteString("Summarize and then continue the following technical log in detail.\n\n")
	i := 0
	for b.Len() < charBudget {
		fmt.Fprintf(&b, "event %04d: subsystem %d processed batch of %d items in %dms; status=ok retries=%d\n",
			i, i%13, (i%97)+1, (i*7)%500, i%4)
		i++
	}
	return b.String()
}
