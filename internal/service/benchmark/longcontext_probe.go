package benchmark

import (
	"context"
	"fmt"
	"math/rand"
	"regexp"
	"strings"
	"time"
)

// --- long-context needle probe --------------------------------------------

// needle is one randomized fact planted in the long-context haystack.
type needle struct {
	label string // distinguishes the three planted facts
	value string // "<City>-<4digit>", the literal the model must recover
}

var needleCities = []string{
	"Reykjavik", "Ulaanbaatar", "Montevideo", "Gaborone", "Tbilisi",
	"Ljubljana", "Windhoek", "Paramaribo", "Bishkek", "Vientiane",
}

// buildNeedles makes three randomized needles at distinct depths from a seed,
// so a run's exact needle set can be reproduced later. The three values are
// guaranteed distinct so scoreNeedles can't double-count a collision.
func buildNeedles(seed int64) []needle {
	rng := rand.New(rand.NewSource(seed))
	labels := []string{"alpha", "beta", "gamma"}
	out := make([]needle, 3)
	seen := make(map[string]bool, 3)
	for i := range out {
		var value string
		for {
			city := needleCities[rng.Intn(len(needleCities))]
			value = fmt.Sprintf("%s-%04d", city, rng.Intn(9000)+1000)
			if !seen[value] {
				break
			}
		}
		seen[value] = true
		out[i] = needle{label: labels[i], value: value}
	}
	return out
}

// needleNormRe strips everything but letters and digits for needle matching.
var needleNormRe = regexp.MustCompile(`[^a-z0-9]+`)

// normalizeNeedleText lowercases and removes separators so "Reykjavik - 1042",
// "reykjavik–1042" and "Reykjavik-1042" all compare equal.
func normalizeNeedleText(s string) string {
	return needleNormRe.ReplaceAllString(strings.ToLower(s), "")
}

// scoreNeedles returns the fraction of needle values present in the response,
// comparing separator-normalized text so formatting variants still count.
func scoreNeedles(response string, needles []needle) float64 {
	if len(needles) == 0 {
		return 0
	}
	norm := normalizeNeedleText(response)
	found := 0
	for _, n := range needles {
		if strings.Contains(norm, normalizeNeedleText(n.value)) {
			found++
		}
	}
	return float64(found) / float64(len(needles))
}

// buildMultiNeedleHaystack generates ~targetTokens of varied pseudo-code with
// the needles planted at ~25%, ~50%, ~75% depth.
func buildMultiNeedleHaystack(targetTokens int, needles []needle) string {
	charBudget := targetTokens * 4
	depths := []int{charBudget / 4, charBudget / 2, charBudget * 3 / 4}
	var b strings.Builder
	planted := make([]bool, len(needles))
	i := 0
	for b.Len() < charBudget {
		for k := range needles {
			if !planted[k] && k < len(depths) && b.Len() >= depths[k] {
				fmt.Fprintf(&b, "\n# === FILE: registry_%s.py ===\n# Internal registration table.\nMAGIC_%s_NUMBER = '%s'\n# End.\n\n",
					needles[k].label, strings.ToUpper(needles[k].label), needles[k].value)
				planted[k] = true
			}
		}
		fmt.Fprintf(&b, "\n# === FILE: module_%04d.py ===\n", i)
		fmt.Fprintf(&b, "def handler_%04d(state, payload, retries=%d):\n", i, i%7)
		fmt.Fprintf(&b, "    total = 0\n    for item in payload.get('items_%d', []):\n", i%5)
		fmt.Fprintf(&b, "        total += item.weight * %d\n", (i%9)+1)
		fmt.Fprintf(&b, "    return Result(total=total, code=%d)\n", i%256)
		i++
	}
	for k := range needles {
		if !planted[k] {
			fmt.Fprintf(&b, "\nMAGIC_%s_NUMBER = '%s'\n", strings.ToUpper(needles[k].label), needles[k].value)
		}
	}
	return b.String()
}

// buildQualityHaystack generates ~targetTokens of realistic filler from real
// arXiv abstracts with the needles planted at ~25/50/75% depth as constant
// definitions the model must recover. Falls back to the pseudo-code haystack
// when no abstracts are available.
func buildQualityHaystack(docs []ArxivDoc, targetTokens int, needles []needle) string {
	if len(docs) == 0 {
		return buildMultiNeedleHaystack(targetTokens, needles)
	}
	charBudget := targetTokens * 4
	depths := []int{charBudget / 4, charBudget / 2, charBudget * 3 / 4}
	var b strings.Builder
	planted := make([]bool, len(needles))
	i := 0
	for b.Len() < charBudget {
		for k := range needles {
			if !planted[k] && k < len(depths) && b.Len() >= depths[k] {
				fmt.Fprintf(&b, "\n# === FILE: registry_%s.py ===\n# Internal registration table.\nMAGIC_%s_NUMBER = '%s'\n# End.\n\n",
					needles[k].label, strings.ToUpper(needles[k].label), needles[k].value)
				planted[k] = true
			}
		}
		d := docs[i%len(docs)]
		fmt.Fprintf(&b, "\n# === PAPER %s [%s] ===\n## %s\n%s\n", d.ID, d.Category, d.Title, d.Abstract)
		i++
	}
	for k := range needles {
		if !planted[k] {
			fmt.Fprintf(&b, "\nMAGIC_%s_NUMBER = '%s'\n", strings.ToUpper(needles[k].label), needles[k].value)
		}
	}
	return b.String()
}

// runLongContext packs a large synthetic code corpus with three randomized
// needles planted at varied depths, then asks the model to retrieve all three.
// Score = fraction recovered; resolved = all three found.
func (r *Runner) runLongContext(ctx context.Context, base, model string) (ProblemResult, ProblemTranscript) {
	res := ProblemResult{ProblemID: "long-context-needle", ProblemName: "Long-context needle retrieval"}
	tr := ProblemTranscript{ProblemID: res.ProblemID, ProblemName: res.ProblemName}

	targetTokens := r.cfg.LongContextTokens
	if targetTokens <= 0 {
		targetTokens = 8000
	}
	seed := time.Now().UnixNano()
	res.Seed = seed
	needles := buildNeedles(seed)
	haystack := buildQualityHaystack(r.arxivDocs, targetTokens, needles)
	user := "Below is a dump of a Python codebase. Read it carefully.\n\n" + haystack +
		"\n\nQuestion: three files define a constant named MAGIC_<NAME>_NUMBER. " +
		"List all three literal values, one per line, no explanation."

	// Deep prefill costs minutes on large contexts; extend the deadline by the
	// same ~100 tok/s prefill floor runLlamaBench uses so a high
	// long_context_tokens setting doesn't guarantee a timeout (BR5).
	reqTimeout := r.cfg.Timeout + time.Duration(targetTokens/100)*time.Second
	reqCtx, cancel := context.WithTimeout(ctx, reqTimeout)
	defer cancel()
	comp, err := Complete(reqCtx, nil, base, "", ChatRequest{
		Model:       model,
		Temperature: 0,
		MaxTokens:   128,
		Messages: []ChatMessage{
			{Role: "system", Content: "You are a careful code-reading assistant. Answer literally."},
			{Role: "user", Content: user},
		},
	})
	if err != nil {
		res.Err = err.Error()
		res.FailPhase = phaseInfer
		tr.Error = err.Error()
		return res, tr
	}
	res.TTFTms = comp.TTFT.Milliseconds()
	res.TotalMs = comp.Total.Milliseconds()
	res.TokensPerSecond = comp.TokensPerSecond
	res.DecodeTPS = comp.TokensPerSecond
	res.PromptProcessingTPS = comp.PromptProcessingTPS
	res.PromptTokens = comp.PromptTokens
	res.CompletionTokens = comp.CompletionTokens
	tr.ModelResponse = comp.Content

	frac := scoreNeedles(comp.Content, needles)
	res.Score = frac
	res.Resolved = frac == 1.0
	res.Detail = fmt.Sprintf("recovered %.0f%% of needles (%d/3); prompt≈%d tok; pp %.0f t/s; tg %.0f t/s; seed=%d",
		frac*100, int(frac*3+0.5), comp.PromptTokens, comp.PromptProcessingTPS, comp.TokensPerSecond, seed)
	return res, tr
}
