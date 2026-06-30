# Redesign da Aba Benchmark (TUI) — Plano de Implementação

> **For Hermes:** Use a skill subagent-driven-development para implementar este plano tarefa-a-tarefa.

**Goal:** Reconstruir a aba Benchmark do TUI em torno de um dashboard/leaderboard com wizard de execução guiado, tela de progresso rica, detalhe visual, comparação por métrica e timeline de histórico — melhorando descoberta, execução e interpretação dos resultados dos modelos.

**Architecture:** Mantém o padrão Bubble Tea value-receiver da página `BenchmarkPage`, expandindo a máquina de estados `benchView` e dividindo o código em novos arquivos `internal/ui/pages/benchmark_*.go` (lógica de view) e novos widgets puros em `internal/ui/components/` (sem importar `internal/service/`). Toda a derivação de métricas vive em helpers da página, lendo `benchmark.Run`/`Aggregate` já existentes — **nenhuma mudança no service layer**. Estilo via `internal/ui/theme` (sem cores hardcoded, com fallback `NO_COLOR`).

**Tech Stack:** Go 1.26, charmbracelet/bubbletea v1.3.10, bubbles (table/spinner), lipgloss, glamour; testes table-driven `package pages` + teatest.

---

## Contexto Atual / Premissas

Estado atual da aba (lido da árvore viva):

- **Arquivos:** `internal/ui/pages/benchmark.go` (629 linhas, View + helpers de métrica), `benchmark_update.go` (teclado), `benchmark_run.go` (execução), `benchmark_compare.go` (compare + history), `benchmark_export.go` (JSON+CSV). Testes: `benchmark_test.go`, `benchmark_detail_test.go`, `benchmark_compare_test.go`, `benchmark_export_test.go`.
- **Máquina de estados (`benchView`):** `bvList`, `bvProfilePick`, `bvModePick`, `bvRunning`, `bvRunDetail`, `bvCompare`, `bvHistory`.
- **Modos (12, via `benchmark.ModesInOrder()`):** agrupados em 5 categorias por `benchmark.CategoryOf()` — Quality (judge, math, codegen, ragas, summary), Speed (llama-bench), Robustness (longctx, instruction), Knowledge (mmlu), Agentic (terminal-bench, swe-bench-pro, deep-swe).
- **Dados por run:** `benchmark.Run` → `Aggregate` (SolveRate, AvgScore, AvgTokensPerSecond, AvgTTFTms, PeakVRAMMB, AvgGPUUtil, métricas por modo) + `[]ProblemResult` (campos estruturados `Kind/Category/Difficulty/SubScores/TPSStdDev…`). `run.Err != ""` marca runs parciais.
- **Infra de UI reutilizável:** `components.Sparkline(values, width)` (puro), `components.HistoryChart`, `components.InfoPanel`, `components.Modal`, `components.Confirm`, `components.Flash`, `components.EmptyState`, `components.FilterLine`, `bubbles/table`. Tema: `theme.Title/Subtitle/OK/Warn/Error/Selected/Pane`, `theme.ColorAccent/OK/Warn/Error/Dim`, `theme.NoColor()`.
- **Engine:** `runner.CountForMode(mode)` (contagem de problemas), `benchmark.FormatBenchProgress(index,total,name,phase)`, fases exatas `launch|infer|score|done` (o CLI casa nessas strings — **não renomear**).

**Problemas de UX identificados:**

1. `bvList` é tabela de texto plano sem filtro, sem cor por qualidade, sem ordenação, sem visão de "ranking".
2. `bvProfilePick → bvModePick` é wizard linear de 2 passos sem voltar, sem ver as duas escolhas juntas, sem resumo/estimativa antes de rodar.
3. `bvModePick` é lista de cursor plana; descrições vivem hardcoded num `map` no view (duplicação) e não há indicação de pré-requisitos (Docker/python/tb).
4. `bvRunning` é só spinner + 1 linha de status: sem barra de progresso, sem ETA, sem métricas ao vivo, sem feed de problemas resolvidos, sem confirmação de cancelar.
5. `bvRunDetail` é texto puro: breakdowns por dificuldade/categoria (dados existem) renderizam como linhas planas; sem scorecard, sem barras.
6. `bvCompare` são colunas de texto sem barras, sem indicador de vencedor (Δ), sem seletor de métrica.
7. `bvHistory` tem sparkline mas sem rótulos de eixo, sem min/max, sem toggle de métrica, sem selecionar run da timeline.
8. `Hints()` existe mas o atalho `h` (history) só aparece com runs; falta documentar atalhos contextuais novos no help global.

**Princípios de design do redesign:**

- **Dashboard-first:** ao entrar na aba, o usuário vê um leaderboard escopado por categoria + cards de resumo, não uma lista crua.
- **Like-for-like:** nunca misturar métricas de modos diferentes no mesmo ranking (escopo por modo, herdado de `openCompare`).
- **Runs parciais (`Err != ""`) nunca entram no ranking/compare** — só aparecem com marcador de aviso.
- **Degradação graciosa:** layout funciona em ≤80 colunas; barras/cores caem para ASCII sob `NO_COLOR`.
- **Sem imports de service em components;** novos widgets recebem `[]float64`/structs primitivos.

---

## Abordagem Proposta (nova máquina de estados)

```go
type benchView int

const (
    bvDashboard   benchView = iota // leaderboard + cards + filtro por categoria  (era bvList)
    bvWizard                       // profile + mode + review numa só tela (substitui bvProfilePick/bvModePick)
    bvRunning                      // progresso rico
    bvRunDetail                    // scorecards + barras + tabela de problemas
    bvCompare                      // barras lado-a-lado + seletor de métrica + Δ
    bvHistory                      // timeline + sparkline rotulada + seleção de run
)
```

O wizard consolida as duas telas de pick num único `benchView` com sub-passos internos:

```go
type benchWizardStep int

const (
    wizProfile benchWizardStep = iota
    wizMode
    wizReview
)
```

**Arquivos a criar:**
- `internal/ui/pages/benchmark_dashboard.go` — view do dashboard + derivação de linhas do leaderboard.
- `internal/ui/pages/benchmark_wizard.go` — view + teclado do wizard (3 sub-passos).
- `internal/ui/pages/benchmark_metrics.go` — helpers de métrica primária por modo (label, valor, "higher/lower is better").
- `internal/ui/components/metric_bar.go` — barra horizontal proporcional pura (novo widget).
- Testes: `benchmark_dashboard_test.go`, `benchmark_wizard_test.go`, `benchmark_metrics_test.go`, `internal/ui/components/metric_bar_test.go`.

**Arquivos a modificar:**
- `internal/ui/pages/benchmark.go` — `benchView`, `View()` switch, `Hints()`, `IsCapturingInput()`, remover `viewProfilePick`/`viewModePick`/`viewList`.
- `internal/ui/pages/benchmark_update.go` — `handleKey` switch, substituir `keyProfilePick`/`keyModePick` por `keyWizard`, ajustar `keyList`→`keyDashboard`.
- `internal/ui/pages/benchmark_run.go` — `startRun` lê escolha do wizard; progresso enriquecido.
- `internal/ui/pages/benchmark_compare.go` — barras + seletor de métrica + Δ; timeline de history.
- `internal/ui/components/help.go` — sincronizar atalhos novos.
- `internal/ui/components/help_test.go` — `TestHelpMarkdownCoversAllHints` precisa cobrir os hints novos.

---

## Plano Passo-a-Passo

As tarefas são incrementais e mantêm a página compilando/testável após cada uma. Cada tarefa segue TDD onde há lógica pura; views renderizadas são validadas com teatest/snapshot de substring.

### Task 1: Widget puro `metric_bar.go`

**Objective:** Barra horizontal proporcional reutilizável (compare/detail), com fallback ASCII sob `NO_COLOR`.

**Files:**
- Create: `internal/ui/components/metric_bar.go`
- Test: `internal/ui/components/metric_bar_test.go`

**Step 1: Escrever teste que falha**

```go
package components

import (
	"strings"
	"testing"
)

func TestMetricBar_FillProportional(t *testing.T) {
	got := MetricBar(0.5, 10)
	if n := strings.Count(got, "█"); n != 5 {
		t.Fatalf("frac 0.5 width 10 = %d filled blocks, want 5 (%q)", n, got)
	}
}

func TestMetricBar_ClampsAndZeroWidth(t *testing.T) {
	if MetricBar(2.0, 4) != strings.Repeat("█", 4) {
		t.Fatalf("frac>1 should clamp to full")
	}
	if MetricBar(0.5, 0) != "" {
		t.Fatalf("width 0 should return empty string")
	}
}
```

**Step 2: Rodar e ver falhar**

Run: `go test ./internal/ui/components -run TestMetricBar -v`
Expected: FAIL — "undefined: MetricBar"

**Step 3: Implementação mínima**

```go
package components

import (
	"strings"

	"github.com/quantmind-br/model-loader/internal/ui/theme"
)

// MetricBar renders frac (0..1) as a width-column horizontal bar. Uses '█'
// for filled and '░' for empty, degrading to '#'/'-' under NO_COLOR. frac is
// clamped to [0,1]; width<=0 returns "".
func MetricBar(frac float64, width int) string {
	if width <= 0 {
		return ""
	}
	if frac < 0 {
		frac = 0
	}
	if frac > 1 {
		frac = 1
	}
	full, empty := '█', '░'
	if theme.NoColor() {
		full, empty = '#', '-'
	}
	filled := int(frac*float64(width) + 0.5)
	if filled > width {
		filled = width
	}
	return strings.Repeat(string(full), filled) + strings.Repeat(string(empty), width-filled)
}
```

**Step 4: Rodar e ver passar**

Run: `go test ./internal/ui/components -run TestMetricBar -v`
Expected: PASS

**Step 5: Commit**

```bash
git add internal/ui/components/metric_bar.go internal/ui/components/metric_bar_test.go
git commit -m "feat(ui): add MetricBar component for benchmark redesign"
```

---

### Task 2: Helpers de métrica primária por modo

**Objective:** Centralizar "qual é a métrica principal de cada modo, seu rótulo, valor 0..1 normalizado e texto formatado" — elimina a lógica espalhada em `listQualityCell`/`compareSectionRows`/`sparkTrend`.

**Files:**
- Create: `internal/ui/pages/benchmark_metrics.go`
- Test: `internal/ui/pages/benchmark_metrics_test.go`

**Step 1: Escrever teste que falha**

```go
package pages

import (
	"testing"

	"github.com/quantmind-br/model-loader/internal/service/benchmark"
)

func TestPrimaryMetric_Judge(t *testing.T) {
	r := benchmark.Run{Mode: benchmark.ModeJudge,
		Aggregate: benchmark.Aggregate{SolveRate: 0.5, Resolved: 1, Total: 2}}
	m := primaryMetric(r)
	if m.Label != "solve" || m.Frac != 0.5 || m.Higher != true {
		t.Fatalf("judge primary = %+v", m)
	}
	if m.Text != "50%" {
		t.Fatalf("text = %q want 50%%", m.Text)
	}
}

func TestPrimaryMetric_LlamaBenchUsesThroughput(t *testing.T) {
	r := benchmark.Run{Mode: benchmark.ModeLlamaBench,
		Aggregate: benchmark.Aggregate{AvgTokensPerSecond: 42.0}}
	m := primaryMetric(r)
	if m.Label != "tok/s" || m.Higher != true {
		t.Fatalf("llama-bench primary = %+v", m)
	}
	// Frac não-normalizado isoladamente (precisa de escala externa); Raw guarda valor.
	if m.Raw != 42.0 {
		t.Fatalf("raw = %v want 42", m.Raw)
	}
}
```

**Step 2: Rodar e ver falhar**

Run: `go test ./internal/ui/pages -run TestPrimaryMetric -v`
Expected: FAIL — "undefined: primaryMetric"

**Step 3: Implementação**

```go
package pages

import (
	"fmt"

	"github.com/quantmind-br/model-loader/internal/service/benchmark"
)

// benchMetric is the primary comparison metric for a run, normalized for bars.
type benchMetric struct {
	Label  string  // "solve", "recall", "tok/s", "accuracy"…
	Frac   float64 // 0..1 for bar fill (throughput needs external scaling → 0)
	Raw    float64 // raw value (tok/s etc.) for series-max normalization
	Text   string  // pre-formatted cell ("50%", "42.0")
	Higher bool    // true = higher is better
}

// primaryMetric picks the headline metric per mode. Quality/knowledge modes
// trend on a 0..1 rate; the throughput probe trends on tok/s (Raw, scaled by
// the caller against the series max); longctx uses recall (AvgScore).
func primaryMetric(r benchmark.Run) benchMetric {
	a := r.Aggregate
	switch r.Mode {
	case benchmark.ModeLlamaBench:
		return benchMetric{Label: "tok/s", Raw: a.AvgTokensPerSecond,
			Text: fmt.Sprintf("%.1f", a.AvgTokensPerSecond), Higher: true}
	case benchmark.ModeLongContext:
		return benchMetric{Label: "recall", Frac: a.AvgScore, Raw: a.AvgScore,
			Text: fmt.Sprintf("%.0f%%", a.AvgScore*100), Higher: true}
	default:
		return benchMetric{Label: "solve", Frac: a.SolveRate, Raw: a.SolveRate,
			Text: fmt.Sprintf("%.0f%%", a.SolveRate*100), Higher: true}
	}
}
```

**Step 4: Rodar e ver passar**

Run: `go test ./internal/ui/pages -run TestPrimaryMetric -v`
Expected: PASS

**Step 5: Commit**

```bash
git add internal/ui/pages/benchmark_metrics.go internal/ui/pages/benchmark_metrics_test.go
git commit -m "feat(ui): centralize benchmark primary-metric derivation"
```

---

### Task 3: Linhas do leaderboard do dashboard

**Objective:** Construir `[]benchDashboardRow` (último run completo por (perfil,modo) no modo/categoria selecionado), com Δ vs. run anterior e trend.

**Files:**
- Create: `internal/ui/pages/benchmark_dashboard.go`
- Test: `internal/ui/pages/benchmark_dashboard_test.go`

**Step 1: Teste que falha** (TDD) — verificar que dois runs do mesmo perfil+modo colapsam num row com Δ calculado, e runs parciais são ignorados:

```go
package pages

import (
	"testing"
	"time"

	"github.com/quantmind-br/model-loader/internal/service/benchmark"
)

func TestDashboardRows_LatestPerProfileWithDelta(t *testing.T) {
	now := time.Now()
	runs := []benchmark.Run{ // newest-first (como o store entrega)
		{ID: "b", ProfileID: "p1", ProfileName: "P1", Mode: benchmark.ModeJudge,
			StartedAt: now, Aggregate: benchmark.Aggregate{SolveRate: 0.6}},
		{ID: "a", ProfileID: "p1", ProfileName: "P1", Mode: benchmark.ModeJudge,
			StartedAt: now.Add(-time.Hour), Aggregate: benchmark.Aggregate{SolveRate: 0.4}},
	}
	rows := dashboardRows(runs, benchmark.ModeJudge)
	if len(rows) != 1 {
		t.Fatalf("want 1 collapsed row, got %d", len(rows))
	}
	if rows[0].Latest.ID != "b" || rows[0].Previous == nil || rows[0].Previous.ID != "a" {
		t.Fatalf("latest/previous wrong: %+v", rows[0])
	}
	if rows[0].DeltaFrac <= 0 { // 0.6 - 0.4 = +0.2
		t.Fatalf("delta should be positive, got %v", rows[0].DeltaFrac)
	}
}

func TestDashboardRows_SkipsPartial(t *testing.T) {
	runs := []benchmark.Run{{ProfileID: "p1", Mode: benchmark.ModeJudge, Err: "boom"}}
	if rows := dashboardRows(runs, benchmark.ModeJudge); len(rows) != 0 {
		t.Fatalf("partial run must be skipped, got %d rows", len(rows))
	}
}
```

**Step 2: Rodar e ver falhar**

Run: `go test ./internal/ui/pages -run TestDashboardRows -v`
Expected: FAIL — "undefined: dashboardRows"

**Step 3: Implementar `dashboardRows` + struct + `viewDashboard`**

```go
package pages

import (
	"fmt"
	"sort"
	"strings"

	"github.com/charmbracelet/lipgloss"

	"github.com/quantmind-br/model-loader/internal/service/benchmark"
	"github.com/quantmind-br/model-loader/internal/ui/components"
	"github.com/quantmind-br/model-loader/internal/ui/theme"
)

// benchDashboardRow is one profile's latest complete run in the focused mode,
// plus the run before it (for Δ) and a trend series (oldest→newest).
type benchDashboardRow struct {
	ProfileID   string
	ProfileName string
	Latest      benchmark.Run
	Previous    *benchmark.Run
	Metric      benchMetric
	DeltaFrac   float64
	Trend       []float64
}

// dashboardRows builds the leaderboard for one mode from newest-first runs:
// the latest complete run per profile (partial runs skipped), its predecessor
// for the Δ arrow, and a chronological trend series. Sorted by primary metric
// descending (higher-is-better) so the best model floats to the top.
func dashboardRows(runs []benchmark.Run, mode benchmark.Mode) []benchDashboardRow {
	type acc struct {
		ordered []benchmark.Run // newest-first for this profile+mode
	}
	byProfile := map[string]*acc{}
	var order []string
	for _, r := range runs {
		if r.Mode != mode || r.Err != "" {
			continue
		}
		a, ok := byProfile[r.ProfileID]
		if !ok {
			a = &acc{}
			byProfile[r.ProfileID] = a
			order = append(order, r.ProfileID)
		}
		a.ordered = append(a.ordered, r)
	}
	rows := make([]benchDashboardRow, 0, len(order))
	for _, id := range order {
		a := byProfile[id]
		latest := a.ordered[0]
		row := benchDashboardRow{
			ProfileID: id, ProfileName: latest.ProfileName,
			Latest: latest, Metric: primaryMetric(latest),
		}
		if len(a.ordered) > 1 {
			prev := a.ordered[1]
			row.Previous = &prev
			row.DeltaFrac = primaryMetric(latest).Raw - primaryMetric(prev).Raw
		}
		// trend oldest→newest
		for i := len(a.ordered) - 1; i >= 0; i-- {
			row.Trend = append(row.Trend, primaryMetric(a.ordered[i]).Raw)
		}
		rows = append(rows, row)
	}
	sort.SliceStable(rows, func(i, j int) bool {
		return rows[i].Metric.Raw > rows[j].Metric.Raw
	})
	return rows
}
```

**Step 4: Rodar e ver passar**

Run: `go test ./internal/ui/pages -run TestDashboardRows -v`
Expected: PASS

**Step 5: Commit**

```bash
git add internal/ui/pages/benchmark_dashboard.go internal/ui/pages/benchmark_dashboard_test.go
git commit -m "feat(ui): build benchmark dashboard leaderboard rows"
```

---

### Task 4: Render do dashboard (cards + filtro de categoria + leaderboard + insight)

**Objective:** `viewDashboard()` compõe: faixa de resumo (total runs / completos / parciais / melhor modelo), barra de categorias (foco atual), tabela de leaderboard com `MetricBar` + seta Δ, e painel de insight do row selecionado.

**Files:**
- Modify: `internal/ui/pages/benchmark_dashboard.go` (adicionar `viewDashboard`, helpers de render)
- Modify: `internal/ui/pages/benchmark.go` (campos de estado: `focusMode benchmark.Mode`, `dashCursor int`, `catCursor int`)

**Step 1:** Adicionar à struct `BenchmarkPage` (em `benchmark.go`):

```go
	// dashboard state
	focusMode benchmark.Mode // mode whose leaderboard is shown
	dashCursor int           // selected leaderboard row
	catCursor  int           // selected category in the category bar
```

**Step 2:** Implementar `viewDashboard` (em `benchmark_dashboard.go`). Estrutura: título → faixa de resumo → barra de categorias → header da tabela → linhas (`profile  [████░░] 60% ▲  tok/s  vram  trend`) → painel de insight. Usar `components.MetricBar`, `components.Sparkline`, `theme.OK/Error/Subtitle`. Seta Δ: `▲` (verde) se `DeltaFrac>0`, `▼` (vermelho) se `<0`, `·` se zero/sem anterior. Modos throughput normalizam `Frac` pela máxima da coluna antes de chamar `MetricBar`.

```go
func (p BenchmarkPage) viewDashboard() string {
	title := theme.Title.Render("Benchmark dashboard")
	if p.runner == nil {
		return lipgloss.JoinVertical(lipgloss.Left, title,
			theme.Error.Render("benchmark dataset failed to load — see logs"))
	}
	if len(p.runs) == 0 {
		return lipgloss.JoinVertical(lipgloss.Left, title,
			components.EmptyState("No benchmark runs yet", "Press [b] to run your first benchmark"))
	}
	summary := p.renderDashSummary()
	cats := p.renderCategoryBar()
	board := p.renderLeaderboard()
	insight := p.renderInsight()
	return lipgloss.JoinVertical(lipgloss.Left, title, summary, "", cats, "", board, "", insight)
}
```

(Implementar `renderDashSummary`, `renderCategoryBar`, `renderLeaderboard`, `renderInsight` como helpers privados; `renderLeaderboard` deriva escala de throughput uma vez e reusa `MetricBar(frac, barW)`.)

**Step 3:** Teste de render (teatest ou substring) em `benchmark_dashboard_test.go`: criar página com 2 runs, chamar `View()` e afirmar que contém o nome do perfil, "%", e o rótulo do modo focado. Verificar empty-state com zero runs.

**Step 4:**

Run: `go test ./internal/ui/pages -run 'Dashboard|Benchmark' -v`
Expected: PASS

**Step 5:**

```bash
git add internal/ui/pages/benchmark_dashboard.go internal/ui/pages/benchmark.go internal/ui/pages/benchmark_dashboard_test.go
git commit -m "feat(ui): render benchmark dashboard with leaderboard and insight panel"
```

---

### Task 5: Teclado do dashboard

**Objective:** Substituir `keyList` por `keyDashboard`: `↑↓/jk` navega leaderboard, `←→` troca categoria/modo focado, `enter` detalhe, `b` abre wizard, `c` compare, `h` history, `E` export, `X` delete, `R` reload, `/` filtro (futuro).

**Files:**
- Modify: `internal/ui/pages/benchmark_update.go`
- Modify: `internal/ui/pages/benchmark.go` (`View()` chama `viewDashboard`; default do switch)

**Step 1:** Em `benchmark.go::View()`, trocar `default: body = p.viewList()` por `case bvDashboard: body = p.viewDashboard()` e tornar `bvDashboard` o default/inicial (`NewBenchmarkPage` seta `view: bvDashboard`).

**Step 2:** Em `benchmark_update.go`, renomear `keyList`→`keyDashboard`; adicionar `←/→` (e `[`/`]`) ciclando entre modos que possuem runs (derivar lista de modos presentes); `dashCursor` indexa `dashboardRows(p.runs, p.focusMode)`. `enter` seta `p.detail` do row selecionado. Atualizar `handleKey` default → `keyDashboard`. Atualizar `IsCapturingInput()` (`p.view != bvDashboard` em vez de `!= bvList`).

**Step 3:** Teste em `benchmark_update`/`benchmark_test.go`: enviar `tea.KeyMsg{Type: KeyRight}` e afirmar que `focusMode` mudou; `enter` num row abre `bvRunDetail`.

**Step 4:**

Run: `go test ./internal/ui/pages -run Benchmark -v`
Expected: PASS

**Step 5:**

```bash
git add internal/ui/pages/benchmark_update.go internal/ui/pages/benchmark.go internal/ui/pages/benchmark_test.go
git commit -m "feat(ui): dashboard keyboard navigation (mode focus, row select)"
```

---

### Task 6: Wizard de execução (profile → mode → review) — estado e teclado

**Objective:** Unificar `bvProfilePick`+`bvModePick` em `bvWizard` com 3 sub-passos e navegação para frente/trás, terminando numa tela de review com estimativa antes de rodar.

**Files:**
- Create: `internal/ui/pages/benchmark_wizard.go`
- Modify: `internal/ui/pages/benchmark.go` (estado `wizStep benchWizardStep`; remover `bvProfilePick`/`bvModePick`)
- Modify: `internal/ui/pages/benchmark_update.go` (`keyWizard` substitui `keyProfilePick`/`keyModePick`)

**Step 1:** Definir `benchWizardStep` + campos na struct (`wizStep`, reusar `profCursor`, `modeCursor`, `filter`, `filterMode`, `selectedProfileID`, `runningName`). `openProfilePick`→`openWizard` (seta `view=bvWizard`, `wizStep=wizProfile`).

**Step 2:** `keyWizard(msg)` despacha por `wizStep`:
- `wizProfile`: navegação + filtro (reaproveitar `keyProfilePickFilter`), `enter` → `wizMode`, `esc` → `bvDashboard`.
- `wizMode`: navegação por modos, `enter` → `wizReview`, `esc` → `wizProfile` (voltar!).
- `wizReview`: `enter` → `startRun()`, `esc` → `wizMode`.

**Step 3:** Teste `benchmark_wizard_test.go`: simular fluxo `b → escolhe perfil → enter → escolhe modo → enter → review → enter` e afirmar transição para `bvRunning`; afirmar que `esc` no `wizMode` volta para `wizProfile` sem perder `selectedProfileID`.

**Step 4:**

Run: `go test ./internal/ui/pages -run 'Wizard|Benchmark' -v`
Expected: PASS

**Step 5:**

```bash
git add internal/ui/pages/benchmark_wizard.go internal/ui/pages/benchmark.go internal/ui/pages/benchmark_update.go internal/ui/pages/benchmark_wizard_test.go
git commit -m "feat(ui): unified benchmark run wizard (profile→mode→review)"
```

---

### Task 7: Render do wizard (cards de modo + review)

**Objective:** `viewWizard()` por passo: lista de perfis filtráveis; **grade de cards de modo** agrupados por categoria com descrição e pré-requisitos (Docker/python/tb); tela de review mostrando perfil+modo+nº de problemas (`runner.CountForMode`) + avisos de pré-requisito.

**Files:**
- Modify: `internal/ui/pages/benchmark_wizard.go`
- Modify: `internal/ui/pages/benchmark.go` (`View()` → `case bvWizard: body = p.viewWizard()`)

**Step 1:** Mover o `map descs` hardcoded de `viewModePick` para um helper `modeDescription(m) string` e adicionar `modePrereq(m) string` (ex.: terminal-bench → "requer tb + Docker"; codegen → "requer python3"). Cards: `[Quality] Math reasoning (GSM8K)  — 12 problemas — accuracy sob quantização`.

**Step 2:** `viewReview()`: bloco com perfil, modo, categoria, contagem de problemas (`p.runner.CountForMode(benchModes[p.modeCursor])`), pré-requisitos em `theme.Warn`, e dica `[enter] run  [esc] back`.

**Step 3:** Teste: render do `wizMode` contém o cabeçalho de categoria "Quality" e a descrição; render do `wizReview` contém a contagem de problemas.

**Step 4:**

Run: `go test ./internal/ui/pages -run 'Wizard|Benchmark' -v`
Expected: PASS

**Step 5:**

```bash
git add internal/ui/pages/benchmark_wizard.go internal/ui/pages/benchmark.go internal/ui/pages/benchmark_wizard_test.go
git commit -m "feat(ui): mode cards and run-review screen in benchmark wizard"
```

---

### Task 8: Tela de progresso rica (`bvRunning`)

**Objective:** Substituir spinner+1-linha por: barra de progresso (`index/total`), fase atual rotulada, problema corrente, contagem resolvidos/total ao vivo, e confirmação ao cancelar (`esc`).

**Files:**
- Modify: `internal/ui/pages/benchmark.go` (`viewRunning`)
- Modify: `internal/ui/pages/benchmark_update.go` (`esc` em `bvRunning` arma `Confirm` "Cancel run?")
- Modify: `internal/ui/pages/benchmark_run.go` (acumular resolvidos parciais via progress, se disponível)

**Step 1:** `viewRunning` usa `components.MetricBar(float64(prog.Index)/float64(prog.Total), barW)` + `benchmark.FormatBenchProgress(...)` (sem renomear fases). Mostrar `fase: launch/infer/score/done` traduzida para rótulo amigável.

**Step 2:** `esc` em `bvRunning`: em vez de cancelar direto, armar `p.deleteConfirm`-style `Confirm` ("Cancel running benchmark?", "Cancel run", "Keep running") cujo `onYes` chama `p.runCancel()`. Atualizar `Hints()` de `bvRunning`.

**Step 3:** Teste: enviar `benchProgressMsg` com `Index:3,Total:10,Phase:"infer"` e afirmar que `View()` contém a barra e "3/10".

**Step 4:**

Run: `go test ./internal/ui/pages -run Benchmark -v`
Expected: PASS

**Step 5:**

```bash
git add internal/ui/pages/benchmark.go internal/ui/pages/benchmark_update.go internal/ui/pages/benchmark_run.go internal/ui/pages/benchmark_test.go
git commit -m "feat(ui): rich benchmark progress view with cancel confirmation"
```

---

### Task 9: Detalhe visual (`bvRunDetail`)

**Objective:** Acrescentar scorecards no topo (métrica primária + tok/s + TTFT + VRAM) com `MetricBar`, e converter breakdowns por dificuldade/categoria em barras. Manter a tabela de problemas existente (com scroll se necessário) e o aviso de run parcial.

**Files:**
- Modify: `internal/ui/pages/benchmark.go` (`viewRunDetail`, `modeDetailLines` → barras)

**Step 1:** Antes da tabela de problemas, renderizar uma faixa de "scorecards": `solve [████░] 80%`, `tok/s 42.0`, `TTFT 120ms`, `VRAM 21GB`. Reutilizar `primaryMetric(r)` (Task 2).

**Step 2:** Em `mathDifficultyBreakdown`/`mmluCategoryBreakdown`, adicionar variante que retorna pares `(label, solved, total)` e renderizar com `MetricBar(solved/total, w)`. Manter os helpers de string atuais para compatibilidade de testes (ou atualizar os asserts).

**Step 3:** Teste em `benchmark_detail_test.go`: run de math com breakdowns → `View()` contém barra e "difficulty"; run parcial → contém "run incomplete".

**Step 4:**

Run: `go test ./internal/ui/pages -run 'Detail|Benchmark' -v`
Expected: PASS

**Step 5:**

```bash
git add internal/ui/pages/benchmark.go internal/ui/pages/benchmark_detail_test.go
git commit -m "feat(ui): scorecards and metric bars in benchmark run detail"
```

---

### Task 10: Compare com barras + seletor de métrica + Δ

**Objective:** Em `viewCompare`, para cada seção de modo, ranquear perfis pela métrica primária, mostrar `MetricBar` por linha, marcar o melhor com `▲`, e permitir alternar a métrica (`m`) entre solve/tok·s/TTFT/VRAM quando aplicável ao modo.

**Files:**
- Modify: `internal/ui/pages/benchmark_compare.go`
- Modify: `internal/ui/pages/benchmark.go` (estado `compareMetric` + `Hints()` de `bvCompare`)
- Modify: `internal/ui/pages/benchmark_update.go` (`m` em `bvCompare` cicla métrica)

**Step 1:** Adicionar `compareMetric benchMetricKind` à struct. Em `bvCompare`, `m` cicla; `←/→` (opcional) muda seção focada. Atualizar `IsCapturingInput` já cobre (view != dashboard).

**Step 2:** `compareSectionRows` passa a ordenar `sec.Runs` pela métrica selecionada e renderizar barra normalizada pela máxima da seção, com `▲` no topo. Indicar "higher/lower is better" no header.

**Step 3:** Teste em `benchmark_compare_test.go`: seção com 2 perfis → linha do maior solve aparece primeiro e contém `▲`; alternar métrica para tok/s reordena.

**Step 4:**

Run: `go test ./internal/ui/pages -run 'Compare|Benchmark' -v`
Expected: PASS

**Step 5:**

```bash
git add internal/ui/pages/benchmark_compare.go internal/ui/pages/benchmark.go internal/ui/pages/benchmark_update.go internal/ui/pages/benchmark_compare_test.go
git commit -m "feat(ui): visual compare with metric selector and best-performer markers"
```

---

### Task 11: Timeline de history rotulada + seleção de run

**Objective:** Em `viewHistory`, adicionar rótulos min/max ao sparkline, toggle de métrica (`m`), e cursor para selecionar um run da timeline e abrir seu detalhe (`enter`).

**Files:**
- Modify: `internal/ui/pages/benchmark_compare.go` (`viewHistory`, `sparkTrend`)
- Modify: `internal/ui/pages/benchmark.go` (estado `histCursor`)
- Modify: `internal/ui/pages/benchmark_update.go` (`bvHistory`: `↑↓`, `enter`, `m`)

**Step 1:** `histCursor` navega `p.historyRuns`; `enter` seta `p.detail` e vai para `bvRunDetail`. `m` alterna métrica do sparkline. Adicionar rótulos `min/max` ao redor do sparkline e marcar a posição selecionada.

**Step 2:** Atualizar `bvHistory` em `handleKey` (não é mais só `esc`). Atualizar `Hints()`.

**Step 3:** Teste: `↓` move cursor; `enter` abre detail do run selecionado.

**Step 4:**

Run: `go test ./internal/ui/pages -run 'History|Benchmark' -v`
Expected: PASS

**Step 5:**

```bash
git add internal/ui/pages/benchmark_compare.go internal/ui/pages/benchmark.go internal/ui/pages/benchmark_update.go internal/ui/pages/benchmark_test.go
git commit -m "feat(ui): interactive history timeline with run selection"
```

---

### Task 12: Hints + help global + limpeza

**Objective:** Sincronizar `Hints()` com todos os atalhos novos, atualizar `components.HelpMarkdown`, remover `viewList`/`viewProfilePick`/`viewModePick` mortos, e garantir `TestHelpMarkdownCoversAllHints`.

**Files:**
- Modify: `internal/ui/pages/benchmark.go` (`Hints()`, remover views mortas)
- Modify: `internal/ui/components/help.go` (`HelpMarkdown` Benchmark)
- Modify: `internal/ui/components/help_test.go` (se necessário)

**Step 1:** Atualizar `Hints()` para `bvDashboard` (`[b] run  [←→] mode  [enter] details  [c] compare  [h] history  [E] export  [X] del  [R] reload`), `bvWizard` por passo, `bvRunning` (`[esc] cancel`), `bvCompare` (`[m] metric  [esc] back`), `bvHistory` (`[↑↓] run  [enter] details  [m] metric  [esc] back`).

**Step 2:** Atualizar a seção Benchmark do `HelpMarkdown` com os mesmos atalhos.

**Step 3:** Rodar o detector de drift help↔hints.

**Step 4:**

Run: `go test ./internal/ui/components -run TestHelpMarkdownCoversAllHints -v`
Expected: PASS

**Step 5:**

```bash
git add internal/ui/pages/benchmark.go internal/ui/components/help.go internal/ui/components/help_test.go
git commit -m "docs(ui): sync benchmark hints and help with redesigned tab"
```

---

### Task 13: Verificação final completa

**Objective:** Garantir build + suíte inteira verdes e checagem manual.

**Step 1:** Build e testes:

```bash
make build
make tests
```
Expected: build OK; `ok` em todos os pacotes (`go test ./...`).

**Step 2:** Checagem manual do TUI:

```bash
./bin/model-loader            # abre o TUI; pressionar [5] para a aba Benchmark
NO_COLOR=1 ./bin/model-loader # confirmar fallback ASCII das barras/sparklines
```
Verificar: dashboard com leaderboard, `←→` troca modo, `b` abre wizard com voltar, progresso com barra, detail com scorecards, compare com barras+`m`, history navegável.

**Step 3:** Commit final se houver ajustes:

```bash
git add -A && git commit -m "feat(ui): complete benchmark tab redesign"
```

---

## Arquivos Que Provavelmente Mudam

- Criados: `internal/ui/components/metric_bar.go` (+test), `internal/ui/pages/benchmark_dashboard.go` (+test), `internal/ui/pages/benchmark_wizard.go` (+test), `internal/ui/pages/benchmark_metrics.go` (+test).
- Modificados: `internal/ui/pages/benchmark.go`, `benchmark_update.go`, `benchmark_run.go`, `benchmark_compare.go`, `benchmark_test.go`, `benchmark_detail_test.go`, `benchmark_compare_test.go`, `internal/ui/components/help.go`, `help_test.go`.
- Service layer: **inalterado** (`internal/service/benchmark/*`).

## Testes / Validação

```bash
go test ./internal/ui/pages -run Benchmark -v
go test ./internal/ui/components -v
go test ./internal/service/benchmark -v   # garante que o engine não regrediu
make tests
make build
```

## Riscos, Tradeoffs e Questões em Aberto

**Riscos:**
- **Quebra de testes existentes:** `benchmark_*_test.go` afirmam texto de view; cada tarefa atualiza seus asserts (parte do passo, não depois).
- **Contrato da interface Page:** preservar `Init/Update/View/Reload/Hints/IsCapturingInput/OverlayView/StatusMessage`. `IsCapturingInput` deve trocar `bvList`→`bvDashboard`.
- **Fases de progresso são contrato:** `launch|infer|score|done` são casadas pelo CLI — **não renomear**, só re-rotular na exibição.
- **Largura do terminal:** compare lado-a-lado pode estourar em ≤80 cols; barras devem ter largura derivada de `p.width` (reusar a lógica de `benchListColumns`).
- **NO_COLOR:** `MetricBar` e Δ devem degradar (já previsto); evitar depender de cor para transmitir vencedor (usar `▲/▼` além da cor).
- **components sem service:** `MetricBar` recebe `float64`, nunca `benchmark.Run`.
- **Value-receiver Bubble Tea:** novos campos de estado na struct `BenchmarkPage` por valor; `Confirm` (cancelar run) segue o padrão `deleteConfirm` já existente.
- **Escopo:** 13 tarefas; implementar em fases — dashboard (1-5), wizard (6-7), progresso/detalhe (8-9), compare/history (10-11), polish (12-13). Cada fase é mergeable isoladamente.

**Tradeoffs:**
- Wizard num único `benchView` com sub-passos (vs. múltiplos estados) reduz a explosão da máquina de estados mas concentra teclado em `keyWizard`.
- `dashboardRows` recomputa a cada `View()`; para ≤dezenas de runs é barato. Cache só se profiling indicar.

**Questões em aberto:**
- Ordenação padrão do leaderboard: por último run (proposto) vs. melhor run histórico?
- Runs parciais: ocultos do leaderboard (proposto) ou mostrados com badge?
- O wizard deve estimar tempo de execução, além da contagem de problemas?
- Compare deve permitir seleção interativa de múltiplos perfis/modos, ou manter "último completo por perfil"?
- Export deve cobrir o dashboard/compare agregado, ou seguir só por run individual?
