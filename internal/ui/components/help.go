package components

import "github.com/charmbracelet/glamour"

// HelpMarkdown é o conteúdo da modal de help acessível via `?` em qualquer
// página. Atualizado quando keybindings mudam.
const HelpMarkdown = `# model-loader — Keybindings

## Global

- ` + "`1`" + `–` + "`4`" + ` — switch directly to a tab
- ` + "`Tab`" + ` — next tab     ` + "`Shift+Tab`" + ` — previous tab
- ` + "`?`" + ` — toggle this help
- ` + "`q`" + ` / ` + "`Ctrl+C`" + ` — quit (background instances survive)

_Convention: lowercase keys are light/cheap actions; uppercase keys are heavy or destructive (e.g. ` + "`R`" + ` rescan walks the filesystem, ` + "`L`" + ` launches a process)._

## Profiles tab

- ` + "`enter`" + ` — launch selected profile
- ` + "`E`" + ` — edit selected profile
- ` + "`n`" + ` — new profile     ` + "`d`" + ` — duplicate
- ` + "`x`" + ` — delete
- ` + "`b`" + ` — toggle background/foreground (default background)
- ` + "`k`" + ` — kill the most recent launched instance
- ` + "`r`" + ` — refresh profile list
- ` + "`p`" + ` — pin selected profile
- ` + "`I`" + ` — import profiles from JSON bundle
- ` + "`u`" + ` — undo last import
- ` + "`e`" + ` — export all profiles to JSON bundle
- ` + "`ctrl+t`" + ` — cycle Essentials → Advanced → Environment → Sizing sub-tabs while editing
- ` + "`/`" + ` — filter

## Server tab

- ` + "`v`" + ` — cycle Logs / Slots / Metrics / History sub-views
- ` + "`Space`" + ` — pause/resume log scroll
- ` + "`k`" + ` — kill selected instance
- ` + "`r`" + ` — restart selected instance (Kill + Launch)
- ` + "`H`" + ` — open history chart for the selected instance
- ` + "`1`" + ` / ` + "`2`" + ` / ` + "`3`" + ` / ` + "`4`" + ` — history chart window: 1h / 6h / 24h / 7d (only while chart is open)
- ` + "`s`" + ` — start HTTP proxy listener
- ` + "`x`" + ` — stop HTTP proxy listener

## Models tab

- ` + "`R`" + ` — rescan all configured paths
- ` + "`/`" + ` — filter
- ` + "`enter`" + ` — actions: use in new profile / existing profile / reveal path
- ` + "`s`" + ` — search Hugging Face
- ` + "`i`" + ` — show model info panel
- ` + "`→`" + ` / ` + "`g`" + ` — navigate to sizing for this model

## Backends tab

- ` + "`n`" + ` — new backend
- ` + "`enter`" + ` / ` + "`e`" + ` — edit selected backend
- ` + "`x`" + ` — delete selected backend
- ` + "`D`" + ` — set selected backend as default
- ` + "`R`" + ` — refresh selected backend schema
- ` + "`P`" + ` — probe selected backend (latency / version)
- ` + "`/`" + ` — filter
`

// RenderHelp retorna o markdown HelpMarkdown renderizado via glamour.
// width informa ao renderer o tamanho da viewport em colunas (afeta wrap).
func RenderHelp(width int) (string, error) {
	return RenderContextualHelp(width, "")
}

// RenderContextualHelp renders HelpMarkdown plus an optional active-page
// context section. activeContext is appended as a "Current Page" heading
// when non-empty.
func RenderContextualHelp(width int, activeContext string) (string, error) {
	if width <= 0 {
		width = 80
	}
	content := HelpMarkdown
	if activeContext != "" {
		content += "\n## Current Page\n\n" + activeContext + "\n"
	}
	r, err := glamour.NewTermRenderer(
		glamour.WithAutoStyle(),
		glamour.WithWordWrap(width),
	)
	if err != nil {
		return "", err
	}
	return r.Render(content)
}
