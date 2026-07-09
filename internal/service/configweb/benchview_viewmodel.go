package configweb

import (
	"fmt"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/quantmind-br/model-loader/internal/service/benchmark"
)

// benchBaseVM carries the fields every benchview page template needs: the
// document title and which nav item ("runs" | "compare" | "live") is active.
type benchBaseVM struct {
	Title  string
	Active string
}

// benchStat is one labelled value in the run detail summary grid.
type benchStat struct {
	Label string
	Value string
}

// benchCardVM is one aggregate metric rendered as a labelled CSS bar. Frac is a
// clamped 0..100 percentage the template applies directly as a width.
type benchCardVM struct {
	Label string
	Text  string
	Frac  int
}

// benchProblemVM is one per-problem row in the run detail table.
type benchProblemVM struct {
	Name    string
	Outcome string // "pass" | "fail" | "error"
	Score   string
	TokS    string
	TTFT    string
	Detail  string
}

// benchTranscriptVM is one problem's captured I/O excerpt, shown in the run
// detail transcript <details> when transcripts were saved for the run.
type benchTranscriptVM struct {
	Name     string
	Response string
	Judge    []string
	Error    string
}

// benchListRowVM is one row of the run list.
type benchListRowVM struct {
	ID         string
	When       string
	Profile    string
	Mode       string
	Metric     string
	MetricText string
	TokS       string
	Partial    bool
}

type benchListVM struct {
	benchBaseVM
	Rows  []benchListRowVM
	Empty bool
}

// benchDetailVM is the full run detail page.
type benchDetailVM struct {
	benchBaseVM
	ID          string
	Profile     string
	Mode        string
	StartedAt   string
	Model       string
	Quant       string
	CacheK      string
	CacheV      string
	Ctx         int
	Instance    string
	Err         string
	Stats       []benchStat
	Cards       []benchCardVM
	ModeLines   []string
	Problems    []benchProblemVM
	Transcript  []benchTranscriptVM
	HasTranscript bool
}

// benchCompareRowVM is one profile row inside a compare section.
type benchCompareRowVM struct {
	Profile    string
	Best       bool
	MetricText string
	Frac       int
	TokS       string
	TTFT       string
	VRAM       string
	Quant      string
}

type benchCompareSectionVM struct {
	Mode        string
	MetricLabel string
	Rows        []benchCompareRowVM
}

type benchCompareVM struct {
	benchBaseVM
	Sections []benchCompareSectionVM
	Empty    bool
}

// benchActivityVM is one line of the live activity log.
type benchActivityVM struct {
	Time  string
	Glyph string
	Class string
	Text  string
}

// benchLiveVM is the /live/fragment payload. Running=false renders the "no run
// in progress" state.
type benchLiveVM struct {
	benchBaseVM
	Running    bool
	Completed  bool
	Profile    string
	Mode       string
	Elapsed    string
	HasBar     bool
	Frac       int
	Index      int
	Total      int
	Pass       int
	Fail       int
	Errored    int
	HasNow     bool
	NowItem    string
	NowPhase   string
	NowElapsed string
	NowStream  string
	Activity   []benchActivityVM
	StaleClass string
	StaleText  string
	Watchdog   string
}

// benchPrimary is the headline comparison metric for a run, mirroring the TUI's
// primaryMetric so all three surfaces agree on the leaderboard axis.
type benchPrimary struct {
	Label string
	Frac  float64 // 0..1 for rate modes
	Raw   float64 // raw value (tok/s) for throughput normalization
	Text  string
	Rate  bool // Raw already normalized to [0,1]
}

// primaryMetricOf picks the headline metric per mode: throughput trends on
// tok/s, longctx on recall, everything else on solve rate.
func primaryMetricOf(r benchmark.Run) benchPrimary {
	a := r.Aggregate
	switch r.Mode {
	case benchmark.ModeLlamaBench:
		return benchPrimary{Label: "tok/s", Raw: a.AvgTokensPerSecond,
			Text: fmt.Sprintf("%.1f", a.AvgTokensPerSecond)}
	case benchmark.ModeLongContext:
		return benchPrimary{Label: "recall", Frac: a.AvgScore, Raw: a.AvgScore,
			Text: fmt.Sprintf("%.0f%%", a.AvgScore*100), Rate: true}
	default:
		return benchPrimary{Label: "solve", Frac: a.SolveRate, Raw: a.SolveRate,
			Text: fmt.Sprintf("%.0f%%", a.SolveRate*100), Rate: true}
	}
}

// buildListVM renders the run list, newest-first (the store already sorts).
func buildListVM(runs []benchmark.Run) benchListVM {
	vm := benchListVM{benchBaseVM: benchBaseVM{Title: "Benchmark runs", Active: "runs"}}
	for _, r := range runs {
		m := primaryMetricOf(r)
		vm.Rows = append(vm.Rows, benchListRowVM{
			ID:         r.ID,
			When:       r.StartedAt.Format("2006-01-02 15:04"),
			Profile:    profileLabel(r),
			Mode:       r.Mode.Title(),
			Metric:     m.Label,
			MetricText: m.Text,
			TokS:       fmt.Sprintf("%.1f", r.Aggregate.AvgTokensPerSecond),
			Partial:    r.Err != "",
		})
	}
	vm.Empty = len(vm.Rows) == 0
	return vm
}

// buildDetailVM assembles the run detail page. allRuns feeds the data-relative
// ceilings; tr (may be nil) is the transcript when it was saved.
func buildDetailVM(r benchmark.Run, allRuns []benchmark.Run, tr []benchmark.ProblemTranscript) benchDetailVM {
	a := r.Aggregate
	instance := "launched fresh"
	if r.ReusedInstance {
		instance = "reused (warm — numbers may include other traffic)"
	}
	vm := benchDetailVM{
		benchBaseVM: benchBaseVM{Title: profileLabel(r) + " — " + r.Mode.Title(), Active: "runs"},
		ID:          r.ID,
		Profile:     profileLabel(r),
		Mode:        r.Mode.Title(),
		StartedAt:   r.StartedAt.Format("2006-01-02 15:04:05"),
		Model:       r.Profile.Model,
		Quant:       orDash(r.Profile.Quantization),
		CacheK:      orDash(r.Profile.CacheTypeK),
		CacheV:      orDash(r.Profile.CacheTypeV),
		Ctx:         r.Profile.CtxSize,
		Instance:    instance,
		Err:         r.Err,
	}

	vm.Stats = []benchStat{
		{"solve", fmt.Sprintf("%.0f%% (%d/%d)", a.SolveRate*100, a.Resolved, a.Total)},
		{"avg score", fmt.Sprintf("%.2f", a.AvgScore)},
		{"tok/s", fmt.Sprintf("%.1f", a.AvgTokensPerSecond)},
		{"TTFT", fmt.Sprintf("%.0fms", a.AvgTTFTms)},
		{"tokens in/out", fmt.Sprintf("%d/%d", a.TotalPromptTokens, a.TotalCompletionTokens)},
		{"peak VRAM", fmt.Sprintf("%dMB", a.PeakVRAMMB)},
		{"GPU", fmt.Sprintf("%.0f%%", a.AvgGPUUtil)},
	}
	if a.Errored > 0 {
		vm.Stats = append(vm.Stats, benchStat{"errored", fmt.Sprintf("%d", a.Errored)})
	}

	ceil := benchmark.CeilingsFor(allRuns, r.Mode)
	vm.Cards = detailCards(r, ceil)
	vm.ModeLines = modeDetailLines(r)

	for _, pr := range r.Problems {
		detail := pr.Detail
		if pr.Err != "" {
			detail = "err: " + pr.Err
		}
		vm.Problems = append(vm.Problems, benchProblemVM{
			Name:    pr.ProblemName,
			Outcome: outcomeOf(pr),
			Score:   fmt.Sprintf("%.2f", pr.Score),
			TokS:    fmt.Sprintf("%.1f", pr.TokensPerSecond),
			TTFT:    fmt.Sprintf("%dms", pr.TTFTms),
			Detail:  detail,
		})
	}

	for _, t := range tr {
		vm.Transcript = append(vm.Transcript, benchTranscriptVM{
			Name:     t.ProblemName,
			Response: truncRunes(t.ModelResponse, 4000),
			Judge:    t.JudgeRaw,
			Error:    t.Error,
		})
	}
	vm.HasTranscript = len(vm.Transcript) > 0
	return vm
}

// detailCards builds the primary + tok/s + TTFT + VRAM cards, normalized by the
// data-relative ceilings so bars stay meaningful across hardware.
func detailCards(r benchmark.Run, ceil benchmark.Ceilings) []benchCardVM {
	a := r.Aggregate
	m := primaryMetricOf(r)

	pf := m.Frac
	if !m.Rate && ceil.TPS > 0 {
		pf = m.Raw / ceil.TPS
	}
	cards := []benchCardVM{{Label: m.Label, Text: m.Text, Frac: clampPct(pf)}}

	tps := a.AvgTokensPerSecond
	tf := 0.0
	if ceil.TPS > 0 {
		tf = tps / ceil.TPS
	}
	cards = append(cards, benchCardVM{Label: "tok/s", Text: fmt.Sprintf("%.1f", tps), Frac: clampPct(tf)})

	// TTFT is lower-is-better: invert against the ceiling so a fast run fills
	// more bar. Unknown (<=0) shows empty rather than a misleading full bar.
	ttft := a.AvgTTFTms
	ttftf := 0.0
	if ttft > 0 && ceil.TTFTms > 0 {
		ttftf = 1 - ttft/ceil.TTFTms
	}
	cards = append(cards, benchCardVM{Label: "TTFT", Text: fmt.Sprintf("%.0fms", ttft), Frac: clampPct(ttftf)})

	vram := float64(a.PeakVRAMMB)
	vf := 0.0
	if ceil.VRAMMB > 0 {
		vf = vram / ceil.VRAMMB
	}
	cards = append(cards, benchCardVM{Label: "VRAM", Text: fmt.Sprintf("%.1fGB", vram/1024), Frac: clampPct(vf)})
	return cards
}

// buildCompareVM groups the latest COMPLETE run per (mode, profile) into one
// section per mode, ranked by the mode's primary metric with a best-row marker.
// Mirrors the TUI compare grouping (partial runs skipped).
func buildCompareVM(runs []benchmark.Run) benchCompareVM {
	vm := benchCompareVM{benchBaseVM: benchBaseVM{Title: "Compare profiles", Active: "compare"}}
	// runs is newest-first; first hit per (mode, profile) wins.
	seen := map[string]bool{}
	byMode := map[benchmark.Mode][]benchmark.Run{}
	for _, r := range runs {
		if r.Err != "" {
			continue
		}
		key := string(r.Mode) + "|" + r.ProfileID
		if seen[key] {
			continue
		}
		seen[key] = true
		byMode[r.Mode] = append(byMode[r.Mode], r)
	}

	emit := func(m benchmark.Mode, secRuns []benchmark.Run) {
		sort.SliceStable(secRuns, func(i, j int) bool {
			return primaryMetricOf(secRuns[i]).Raw > primaryMetricOf(secRuns[j]).Raw
		})
		ceil := benchmark.CeilingsFor(runs, m)
		sec := benchCompareSectionVM{Mode: m.Title(), MetricLabel: primaryMetricOf(secRuns[0]).Label}
		for i, r := range secRuns {
			p := primaryMetricOf(r)
			frac := p.Frac
			if !p.Rate && ceil.TPS > 0 {
				frac = p.Raw / ceil.TPS
			}
			sec.Rows = append(sec.Rows, benchCompareRowVM{
				Profile:    profileLabel(r),
				Best:       i == 0 && len(secRuns) > 1,
				MetricText: p.Text,
				Frac:       clampPct(frac),
				TokS:       fmt.Sprintf("%.1f", r.Aggregate.AvgTokensPerSecond),
				TTFT:       fmt.Sprintf("%.0fms", r.Aggregate.AvgTTFTms),
				VRAM:       fmt.Sprintf("%dMB", r.Aggregate.PeakVRAMMB),
				Quant:      orDash(r.Profile.Quantization),
			})
		}
		vm.Sections = append(vm.Sections, sec)
	}

	for _, m := range benchmark.ModesInOrder() {
		if sr := byMode[m]; len(sr) > 0 {
			emit(m, sr)
			delete(byMode, m)
		}
	}
	// Legacy/unknown modes still deserve a section.
	for m, sr := range byMode {
		emit(m, sr)
	}
	vm.Empty = len(vm.Sections) == 0
	return vm
}

// buildLiveVM renders the live monitor fragment from a snapshot taken at now.
func buildLiveVM(snap benchmark.FeedSnapshot, now time.Time) benchLiveVM {
	completed := snap.Phase == "done"
	vm := benchLiveVM{
		benchBaseVM: benchBaseVM{Title: "Live run", Active: "live"},
		Running:     true,
		Completed:   completed,
		Profile:     snapProfileLabel(snap),
		Mode:        snap.Mode.Title(),
		Elapsed:     fmtDur(now.Sub(snap.StartedAt)),
		Index:       snap.Index,
		Total:       snap.Total,
		Pass:        snap.Pass,
		Fail:        snap.Fail,
		Errored:     snap.Errored,
	}
	if snap.Total > 0 {
		vm.HasBar = true
		vm.Frac = clampPct(float64(snap.Index) / float64(snap.Total))
	}
	if !completed && snap.Current != nil {
		vm.HasNow = true
		vm.NowItem = itemLabel(snap.Current.ID, snap.Current.Name)
		vm.NowPhase = snap.Current.Phase
		vm.NowElapsed = fmtDur(now.Sub(snap.Current.StartedAt))
		vm.NowStream = latestStream(snap.Activity)
	}

	// Last 30 activity entries (newest last in the ring).
	act := snap.Activity
	if len(act) > 30 {
		act = act[len(act)-30:]
	}
	for _, e := range act {
		glyph, class := activityGlyph(e)
		vm.Activity = append(vm.Activity, benchActivityVM{
			Time:  e.At.Format("15:04:05"),
			Glyph: glyph,
			Class: class,
			Text:  e.Text,
		})
	}

	if completed {
		// UIUX-025: a done-phase run is terminal, not hung — show a settled
		// completion line and never escalate staleness or the kill watchdog.
		vm.StaleClass = "ok"
		vm.StaleText = "run complete"
	} else {
		level, since := snap.Staleness(now)
		vm.StaleText = benchmark.StalenessLabel(level, since)
		switch level {
		case benchmark.StaleStalled:
			vm.StaleClass = "err"
		case benchmark.StaleQuiet:
			vm.StaleClass = "warn"
		default:
			vm.StaleClass = "dim"
		}
		if snap.StallTimeout > 0 {
			if rem := snap.StallTimeout - now.Sub(snap.LastItemDone); rem > 0 {
				vm.Watchdog = "watchdog kill in " + fmtDur(rem)
			}
		}
	}
	return vm
}

// --- mode-specific detail lines (mirrors the TUI modeDetailLines, +DeepSWE) --

var mathDifficultyRe = regexp.MustCompile(`difficulty (\d+)\)`)

// modeDetailLines returns the mode-specific breakdown lines shown below the
// generic summary, matching the TUI set and adding the DeepSWE case.
func modeDetailLines(r benchmark.Run) []string {
	a := r.Aggregate
	var lines []string
	if a.AvgPromptProcessingTPS > 0 || a.AvgDecodeTPS > 0 {
		lines = append(lines, fmt.Sprintf("prefill %.1f tok/s   decode %.1f tok/s", a.AvgPromptProcessingTPS, a.AvgDecodeTPS))
	}
	if r.Mode == benchmark.ModeLlamaBench {
		if v := llamaBenchVarianceLine(r.Problems); v != "" {
			lines = append(lines, v)
		}
	}
	switch r.Mode {
	case benchmark.ModeMathBench:
		lines = append(lines, fmt.Sprintf("math accuracy %.0f%%", a.MathAccuracy*100))
		if b := mathDifficultyBreakdown(r.Problems); b != "" {
			lines = append(lines, "  "+b)
		}
	case benchmark.ModeCodeGenBench:
		executed, skipped := 0, 0
		for _, pr := range r.Problems {
			if pr.Err != "" {
				skipped++
			} else {
				executed++
			}
		}
		if executed == 0 {
			lines = append(lines, "code generation skipped (python3 not available)")
		} else {
			lines = append(lines, fmt.Sprintf("code pass rate %.0f%% (%d executed, %d skipped)", a.CodePassRate*100, executed, skipped))
		}
	case benchmark.ModeInstBench:
		lines = append(lines, fmt.Sprintf("instruction — format %.0f%%   refusal %.0f%%   consistency %.2f",
			a.InstFormatRate*100, a.InstRefusalRate*100, a.InstConsistency))
	case benchmark.ModeMMLUBench:
		lines = append(lines, fmt.Sprintf("MMLU accuracy %.0f%%", a.MMLUAccuracy*100))
		if b := mmluCategoryBreakdown(r.Problems); b != "" {
			lines = append(lines, "  "+b)
		}
	case benchmark.ModeRagasBench:
		lines = append(lines, fmt.Sprintf("RAG — faithfulness %.0f%%   relevancy %.0f%%   precision %.0f%%",
			a.RagasFaithfulness*100, a.RagasRelevancy*100, a.RagasPrecision*100))
	case benchmark.ModeSummaryBench:
		lines = append(lines, fmt.Sprintf("summarization coherence %.0f%%", a.SummaryCoherence*100))
	case benchmark.ModeTerminalBench:
		lines = append(lines, fmt.Sprintf("terminal-bench accuracy %.0f%% (%d/%d tasks resolved)",
			a.TerminalBenchAccuracy*100, a.Resolved, a.Total))
	case benchmark.ModeSweBenchPro:
		lines = append(lines, fmt.Sprintf("swe-bench-pro accuracy %.0f%% (%d/%d instances resolved)",
			a.SweBenchProAccuracy*100, a.Resolved, a.Total))
	case benchmark.ModeDeepSWE:
		lines = append(lines, fmt.Sprintf("deep-swe accuracy %.0f%% (%d/%d tasks resolved)",
			a.DeepSWEAccuracy*100, a.Resolved, a.Total))
	}
	return lines
}

func llamaBenchVarianceLine(problems []benchmark.ProblemResult) string {
	parts := make([]string, 0, len(problems))
	for _, pr := range problems {
		if pr.TPSMax == 0 {
			continue
		}
		parts = append(parts, fmt.Sprintf("%s ±%.1f [%.1f–%.1f]", pr.ProblemName, pr.TPSStdDev, pr.TPSMin, pr.TPSMax))
	}
	if len(parts) == 0 {
		return ""
	}
	return "tok/s spread: " + strings.Join(parts, "   ")
}

func mathDifficultyBreakdown(problems []benchmark.ProblemResult) string {
	type tally struct{ solved, total int }
	bands := map[string]*tally{}
	var order []string
	for _, pr := range problems {
		var d string
		if pr.Difficulty > 0 {
			d = fmt.Sprintf("%d", pr.Difficulty)
		} else if m := mathDifficultyRe.FindStringSubmatch(pr.Detail); m != nil {
			d = m[1]
		} else {
			continue
		}
		t, ok := bands[d]
		if !ok {
			t = &tally{}
			bands[d] = t
			order = append(order, d)
		}
		t.total++
		if pr.Resolved {
			t.solved++
		}
	}
	sort.Strings(order)
	parts := make([]string, 0, len(order))
	for _, d := range order {
		t := bands[d]
		parts = append(parts, fmt.Sprintf("difficulty %s: %d/%d", d, t.solved, t.total))
	}
	return strings.Join(parts, "   ")
}

func mmluCategoryBreakdown(problems []benchmark.ProblemResult) string {
	type tally struct{ solved, total int }
	cats := map[string]*tally{}
	var order []string
	for _, pr := range problems {
		cat := pr.Category
		if cat == "" {
			cat = parseMMLUCategory(pr.Detail)
		}
		if cat == "" {
			continue
		}
		t, ok := cats[cat]
		if !ok {
			t = &tally{}
			cats[cat] = t
			order = append(order, cat)
		}
		t.total++
		if pr.Resolved {
			t.solved++
		}
	}
	sort.Strings(order)
	parts := make([]string, 0, len(order))
	for _, c := range order {
		t := cats[c]
		parts = append(parts, fmt.Sprintf("%s: %d/%d", c, t.solved, t.total))
	}
	return strings.Join(parts, "   ")
}

func parseMMLUCategory(detail string) string {
	const pfx = "category="
	if !strings.HasPrefix(detail, pfx) {
		return ""
	}
	rest := detail[len(pfx):]
	if i := strings.Index(rest, " expected"); i >= 0 {
		return rest[:i]
	}
	return ""
}

// --- small shared helpers --------------------------------------------------

// outcomeOf maps a problem result to the shared pass/fail/error vocabulary.
func outcomeOf(pr benchmark.ProblemResult) string {
	switch {
	case pr.Err != "":
		return "error"
	case pr.Resolved:
		return "pass"
	default:
		return "fail"
	}
}

// activityGlyph selects the outcome glyph + CSS class for one activity entry,
// following the shared glyph rules (✓/✗/! for finished items, → for starts,
// · for harness/stream lines, • for phase transitions).
func activityGlyph(e benchmark.ActivityEntry) (glyph, class string) {
	switch e.Kind {
	case "item":
		switch e.Outcome {
		case "pass":
			return "✓", "ok"
		case "fail":
			return "✗", "err"
		case "error":
			return "!", "warn"
		default:
			return "→", "dim"
		}
	case "harness", "stream":
		return "·", "dim"
	case "phase":
		return "•", "phase"
	}
	return " ", "dim"
}

// latestStream returns the newest stream heartbeat text in the ring, if any.
func latestStream(act []benchmark.ActivityEntry) string {
	for i := len(act) - 1; i >= 0; i-- {
		if act[i].Kind == "stream" {
			return act[i].Text
		}
	}
	return ""
}

func profileLabel(r benchmark.Run) string {
	if r.ProfileName != "" {
		return r.ProfileName
	}
	return r.ProfileID
}

func snapProfileLabel(s benchmark.FeedSnapshot) string {
	if s.ProfileName != "" {
		return s.ProfileName
	}
	return s.ProfileID
}

func itemLabel(id, name string) string {
	if name != "" {
		return name
	}
	return id
}

func orDash(s string) string {
	if s == "" {
		return "-"
	}
	return s
}

// clampPct converts a 0..1 fraction into a clamped 0..100 integer percentage.
func clampPct(f float64) int {
	if f < 0 {
		f = 0
	}
	if f > 1 {
		f = 1
	}
	return int(f*100 + 0.5)
}

// fmtDur renders a duration truncated to whole seconds ("12m40s"), guarding
// against a negative value from clock skew.
func fmtDur(d time.Duration) string {
	if d < 0 {
		d = 0
	}
	return d.Truncate(time.Second).String()
}

// truncRunes clips s to at most max runes, appending an ellipsis when it cut.
func truncRunes(s string, max int) string {
	if max <= 0 {
		return ""
	}
	r := []rune(s)
	if len(r) <= max {
		return s
	}
	if max == 1 {
		return "…"
	}
	return string(r[:max-1]) + "…"
}
