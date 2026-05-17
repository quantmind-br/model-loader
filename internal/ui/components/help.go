package components

import "github.com/charmbracelet/glamour"

// HelpMarkdown é o conteúdo da modal de help acessível via `?` em qualquer
// página. Atualizado quando keybindings mudam.
const HelpMarkdown = `# model-loader — Keybindings

## Global

- ` + "`1`" + `–` + "`6`" + ` — switch directly to a tab
- ` + "`Tab`" + ` — next tab     ` + "`Shift+Tab`" + ` — previous tab
- ` + "`?`" + ` — toggle this help
- ` + "`q`" + ` / ` + "`Ctrl+C`" + ` — quit (background instances survive)

_Convention: lowercase keys are light/cheap actions; uppercase keys are heavy or destructive (e.g. ` + "`R`" + ` rescan walks the filesystem, ` + "`L`" + ` launches a process)._

## Profiles tab

- ` + "`n`" + ` — new profile     ` + "`d`" + ` — duplicate
- ` + "`x`" + ` — delete         ` + "`enter`" + ` — edit / submit Save
- ` + "`esc`" + ` — cancel editing (prompts to discard unsaved changes)
- ` + "`L`" + ` — launch directly from selected profile
- ` + "`e`" + ` — export all profiles to JSON bundle
- ` + "`ctrl+t`" + ` — toggle Essentials / Advanced sub-tab while editing
- ` + "`/`" + ` — filter

## Launcher tab

- ` + "`b`" + ` — toggle background/foreground (default background)
- ` + "`enter`" + ` — launch selected profile
- ` + "`k`" + ` — kill the most recent launched instance
- ` + "`r`" + ` — refresh profile list

## Monitor tab

- ` + "`v`" + ` — cycle Logs / Slots / Metrics sub-views
- ` + "`Space`" + ` — pause/resume log scroll
- ` + "`k`" + ` — kill selected instance
- ` + "`r`" + ` — restart selected instance (Kill + Launch)

## Models tab

- ` + "`R`" + ` — rescan all configured paths
- ` + "`/`" + ` — filter
- ` + "`enter`" + ` — actions: use in new profile / existing profile / reveal path

## Backends tab

- ` + "`n`" + ` — new backend
- ` + "`enter`" + ` / ` + "`e`" + ` — edit selected backend
- ` + "`x`" + ` — delete selected backend
- ` + "`D`" + ` — set selected backend as default
- ` + "`R`" + ` — refresh selected backend schema
- ` + "`P`" + ` — probe selected backend (latency / version)
- ` + "`/`" + ` — filter

## Server tab

- ` + "`s`" + ` — start HTTP proxy listener
- ` + "`x`" + ` — stop listener
- ` + "`r`" + ` — refresh status
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
