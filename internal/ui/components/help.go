package components

import "github.com/charmbracelet/glamour"

// HelpMarkdown is the help modal content accessible via `?` on any
// page. Updated when keybindings change.
const HelpMarkdown = `# model-loader — Keybindings

## Global

- ` + "`1`" + `–` + "`5`" + ` — switch directly to a tab
- ` + "`Tab`" + ` — next tab     ` + "`Shift+Tab`" + ` — previous tab
- ` + "`?`" + ` — toggle this help
- ` + "`q`" + ` / ` + "`Ctrl+C`" + ` — quit (background instances survive)
- ` + "`↑`" + `/` + "`↓`" + ` (or ` + "`k`" + `/` + "`j`" + `) — move selection in lists     ` + "`g`" + `/` + "`G`" + ` — jump to top/bottom

_Convention: lowercase keys are light/cheap actions (navigate, edit, view); uppercase keys are heavy or destructive (e.g. ` + "`R`" + ` rescan walks the filesystem, ` + "`K`" + ` kill stops a process, ` + "`X`" + ` delete removes data). Destructive uppercase keys always ask for confirmation._

## Profiles tab

- ` + "`enter`" + ` — load selected profile through the HTTP proxy (swaps out the current model)
- ` + "`e`" + ` — edit selected profile
- ` + "`n`" + ` — new profile     ` + "`d`" + ` — duplicate
- ` + "`X`" + ` — delete (confirm)
- ` + "`K`" + ` — unload the currently loaded model (confirm)
- ` + "`R`" + ` — refresh profile list
- ` + "`p`" + ` — pin selected profile
- ` + "`I`" + ` — import profiles from JSON bundle
- ` + "`u`" + ` — undo last import
- ` + "`E`" + ` — export all profiles to JSON bundle
- ` + "`ctrl+t`" + ` — cycle Essentials → Advanced → Environment → Sizing sub-tabs while editing
- ` + "`/`" + ` — filter

## Server tab

- ` + "`v`" + ` — cycle Logs / Slots / Metrics / History sub-views
- ` + "`Space`" + ` — pause/resume log scroll
- ` + "`K`" + ` — kill selected instance (unloads via the proxy when it owns it, confirm)
- ` + "`R`" + ` — restart selected instance (unload + load via the proxy, confirm)
- ` + "`h`" + ` — open history chart for the selected instance
- ` + "`1`" + ` / ` + "`2`" + ` / ` + "`3`" + ` / ` + "`4`" + ` — history chart window: 1h / 6h / 24h / 7d (only while chart is open)
- ` + "`s`" + ` — start HTTP proxy listener
- ` + "`x`" + ` — stop HTTP proxy listener

## Models tab

- ` + "`R`" + ` — rescan all configured paths
- ` + "`/`" + ` — filter
- ` + "`enter`" + ` — actions: use in new profile / existing profile / reveal path
- ` + "`s`" + ` — search Hugging Face
- ` + "`i`" + ` — show model info panel
- ` + "`X`" + ` — remove a broken search path from config (confirm; shown when a path fails to scan)
- ` + "`→`" + ` / ` + "`g`" + ` — navigate to sizing for this model

## Backends tab

- ` + "`n`" + ` — new backend
- ` + "`enter`" + ` / ` + "`e`" + ` — edit selected backend
- ` + "`X`" + ` — delete selected backend (confirm)
- ` + "`D`" + ` — set selected backend as default
- ` + "`R`" + ` — refresh selected backend schema
- ` + "`P`" + ` — probe selected backend (latency / version)
- ` + "`/`" + ` — filter

## Benchmark tab

- ` + "`b`" + ` — run a benchmark on a profile
- ` + "`enter`" + ` — open details for the selected run
- ` + "`c`" + ` — compare two runs
- ` + "`E`" + ` — export selected run to JSON
- ` + "`X`" + ` — delete selected run (confirm)
- ` + "`R`" + ` — reload runs from disk
- ` + "`h`" + ` — history chart (when runs exist)
- ` + "`/`" + ` — filter (in profile picker)
- ` + "`esc`" + ` — back / cancel
`

// RenderHelp returns HelpMarkdown rendered through glamour.
// width tells the renderer the viewport size in columns (affects wrapping).
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
