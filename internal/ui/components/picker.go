// Package components contains reusable UI building blocks.
package components

import (
	"context"
	"fmt"
	"sort"
	"strings"

	"github.com/charmbracelet/bubbles/key"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/quantmind-br/model-loader/internal/domain"
	"github.com/quantmind-br/model-loader/internal/ui/internal/filter"
	"github.com/quantmind-br/model-loader/internal/ui/theme"
)

// ModelScanner is the minimal Scanner contract the picker depends on.
// Mirrors modelscanner.Scanner; redeclared locally to avoid the
// components package importing service/.
type ModelScanner interface {
	Scan(ctx context.Context, paths []string) (<-chan domain.ScanEvent, error)
}

// ModelPickedMsg is emitted when the user selects a model.
type ModelPickedMsg struct {
	Path string
}

// ModelPickerCancelledMsg is emitted on Esc / cancel.
type ModelPickerCancelledMsg struct{}

// ModelPicker is a modal overlay listing GGUF files.
// Streaming pattern mirrors ModelsPage: Init returns a Cmd that creates
// ctx+channel+cancel and delivers them via PickerScanStartedMsg.
type ModelPicker struct {
	scanner ModelScanner
	paths   []string
	cancel  context.CancelFunc

	files      []domain.ModelFile
	filtered   []domain.ModelFile
	cursor     int
	filter     string
	filterMode bool
	scanning   bool
	err        string // populated when Scan returns an error

	width  int
	height int
}

// NewModelPicker constructs a picker bound to a Scanner and search paths.
func NewModelPicker(scanner ModelScanner, paths []string) ModelPicker {
	return ModelPicker{scanner: scanner, paths: paths, scanning: true}
}

// PickerScanStartedMsg delivers the scan channel + cancel handle.
type PickerScanStartedMsg struct {
	Ch     <-chan domain.ScanEvent
	Cancel context.CancelFunc
	Err    error
}

// PickerScanEventMsg carries one ScanEvent + the channel for re-arm.
type PickerScanEventMsg struct {
	Ch  <-chan domain.ScanEvent
	Evt domain.ScanEvent
}

// PickerScanClosedMsg signals the scan channel closed.
type PickerScanClosedMsg struct{}

// Init returns a Cmd that starts the scan goroutine and emits
// PickerScanStartedMsg.
func (m ModelPicker) Init() tea.Cmd {
	scanner := m.scanner
	paths := m.paths
	return func() tea.Msg {
		ctx, cancel := context.WithCancel(context.Background())
		ch, err := scanner.Scan(ctx, paths)
		if err != nil {
			cancel()
			return PickerScanStartedMsg{Err: err}
		}
		return PickerScanStartedMsg{Ch: ch, Cancel: cancel}
	}
}

func pickerWaitForEvent(ch <-chan domain.ScanEvent) tea.Cmd {
	return func() tea.Msg {
		evt, ok := <-ch
		if !ok {
			return PickerScanClosedMsg{}
		}
		return PickerScanEventMsg{Ch: ch, Evt: evt}
	}
}

// pickerKeys for the modal.
type pickerKeys struct {
	Up, Down, Enter, Esc, Filter, Backspace, PgUp, PgDn, Home, End, GTop, GBottom key.Binding
}

func defaultPickerKeys() pickerKeys {
	return pickerKeys{
		Up:        key.NewBinding(key.WithKeys("up", "k")),
		Down:      key.NewBinding(key.WithKeys("down", "j")),
		Enter:     key.NewBinding(key.WithKeys("enter")),
		Esc:       key.NewBinding(key.WithKeys("esc")),
		Filter:    key.NewBinding(key.WithKeys("/")),
		Backspace: key.NewBinding(key.WithKeys("backspace")),
		PgUp:      key.NewBinding(key.WithKeys("pgup")),
		PgDn:      key.NewBinding(key.WithKeys("pgdown")),
		Home:      key.NewBinding(key.WithKeys("home")),
		End:       key.NewBinding(key.WithKeys("end")),
		GTop:      key.NewBinding(key.WithKeys("g")),
		GBottom:   key.NewBinding(key.WithKeys("G")),
	}
}

// Update is a thin dispatcher: each typed-message arm delegates to a
// private handle<MsgType> method.
func (m ModelPicker) Update(msg tea.Msg) (ModelPicker, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		return m.handleResize(msg)
	case PickerScanStartedMsg:
		return m.handleScanStarted(msg)
	case PickerScanEventMsg:
		return m.handleScanEvent(msg)
	case PickerScanClosedMsg:
		return m.handleScanClosed(msg)
	case tea.KeyMsg:
		return m.handleKey(msg)
	}
	return m, nil
}

func (m ModelPicker) handleResize(msg tea.WindowSizeMsg) (ModelPicker, tea.Cmd) {
	m.width, m.height = msg.Width, msg.Height
	return m, nil
}

func (m ModelPicker) handleScanStarted(msg PickerScanStartedMsg) (ModelPicker, tea.Cmd) {
	if msg.Err != nil {
		m.scanning = false
		m.err = msg.Err.Error()
		return m, nil
	}
	m.cancel = msg.Cancel
	return m, pickerWaitForEvent(msg.Ch)
}

func (m ModelPicker) handleScanEvent(msg PickerScanEventMsg) (ModelPicker, tea.Cmd) {
	switch msg.Evt.Type {
	case domain.ScanEventFile:
		if msg.Evt.File != nil {
			m.files = append(m.files, *msg.Evt.File)
			m.applyFilter()
		}
	case domain.ScanEventError:
		if msg.Evt.Error != nil {
			m.err = msg.Evt.Error.Error()
		}
	case domain.ScanEventDone:
		m.scanning = false
	}
	return m, pickerWaitForEvent(msg.Ch)
}

func (m ModelPicker) handleScanClosed(_ PickerScanClosedMsg) (ModelPicker, tea.Cmd) {
	m.scanning = false
	return m, nil
}

// handleKey dispatches one key event. Esc / Enter terminate the picker;
// '/' toggles filter mode; while filter mode is on printable runes append
// to the filter; otherwise arrows / pgup-down / home-end / g / G move
// the cursor.
func (m ModelPicker) handleKey(msg tea.KeyMsg) (ModelPicker, tea.Cmd) {
	keys := defaultPickerKeys()
	if key.Matches(msg, keys.Esc) {
		if m.cancel != nil {
			m.cancel()
		}
		return m, func() tea.Msg { return ModelPickerCancelledMsg{} }
	}
	if key.Matches(msg, keys.Enter) {
		if len(m.filtered) == 0 {
			return m, nil
		}
		path := m.filtered[m.cursor].Path
		if m.cancel != nil {
			m.cancel()
		}
		return m, func() tea.Msg { return ModelPickedMsg{Path: path} }
	}
	if key.Matches(msg, keys.Filter) {
		m.filterMode = !m.filterMode
		return m, nil
	}
	if m.filterMode {
		return m.handleFilterKey(msg, keys)
	}
	return m.handleNavKey(msg, keys)
}

// handleFilterKey runs while filter mode is on: backspace pops a char,
// printable runes append. Anything else is swallowed.
func (m ModelPicker) handleFilterKey(msg tea.KeyMsg, keys pickerKeys) (ModelPicker, tea.Cmd) {
	if key.Matches(msg, keys.Backspace) {
		if len(m.filter) > 0 {
			m.filter = m.filter[:len(m.filter)-1]
			m.applyFilter()
		}
		return m, nil
	}
	if len(msg.Runes) == 1 {
		m.filter += string(msg.Runes)
		m.applyFilter()
		return m, nil
	}
	return m.handleNavKey(msg, keys)
}

// handleNavKey runs cursor navigation: up/down, pgup/pgdn, home/end,
// and (only outside filter mode) g / G.
func (m ModelPicker) handleNavKey(msg tea.KeyMsg, keys pickerKeys) (ModelPicker, tea.Cmd) {
	switch {
	case key.Matches(msg, keys.Up):
		if m.cursor > 0 {
			m.cursor--
		}
	case key.Matches(msg, keys.Down):
		if m.cursor < len(m.filtered)-1 {
			m.cursor++
		}
	case key.Matches(msg, keys.PgUp):
		m.cursor -= 10
		if m.cursor < 0 {
			m.cursor = 0
		}
	case key.Matches(msg, keys.PgDn):
		m.cursor += 10
		if m.cursor > len(m.filtered)-1 {
			m.cursor = len(m.filtered) - 1
		}
		if m.cursor < 0 {
			m.cursor = 0
		}
	case key.Matches(msg, keys.Home) || (!m.filterMode && key.Matches(msg, keys.GTop)):
		m.cursor = 0
	case key.Matches(msg, keys.End) || (!m.filterMode && key.Matches(msg, keys.GBottom)):
		if len(m.filtered) > 0 {
			m.cursor = len(m.filtered) - 1
		}
	}
	return m, nil
}

func (m *ModelPicker) applyFilter() {
	matched := filter.ContainsFold(m.files, m.filter, func(f domain.ModelFile) string { return f.Name })
	// Clone into the persistent m.filtered buffer so the subsequent sort
	// never mutates m.files (ContainsFold returns the input slice header
	// when filter is empty).
	m.filtered = append(m.filtered[:0], matched...)
	sort.Slice(m.filtered, func(i, j int) bool { return m.filtered[i].Name < m.filtered[j].Name })
	if m.cursor >= len(m.filtered) {
		m.cursor = 0
	}
}

// Cancel returns the cancel func for the in-flight scan (or nil).
// Callers may invoke it to abort the scan when closing the picker.
func (m ModelPicker) Cancel() context.CancelFunc {
	return m.cancel
}

// View renders the picker as a centered overlay box.
func (m ModelPicker) View() string {
	title := theme.Title.Render("Pick a model")
	hint := "[↑↓] move  [/] filter  [enter] select  [esc] cancel"
	if m.filterMode {
		hint = "[type] add  [backspace] del  [/] exit filter  [enter] select"
	}
	statusLine := ""
	if m.scanning {
		statusLine = theme.Subtitle.Render(fmt.Sprintf("scanning… %d found", len(m.files)))
	} else {
		statusLine = theme.Subtitle.Render(fmt.Sprintf("%d models", len(m.files)))
	}
	filterLine := FilterLine(m.filterMode, m.filter)
	boxW := pickerBoxWidth(m.width)
	nameW, quantW, paramsW, pathW := pickerColumnWidths(boxW)
	rowFmt := fmt.Sprintf("%%-%ds  %%-%ds  %%-%ds  %%-%ds", nameW, quantW, paramsW, pathW)
	rows := make([]string, 0, len(m.filtered))
	for i, f := range m.filtered {
		row := fmt.Sprintf(rowFmt, truncatePath(f.Name, nameW), truncatePath(f.Quant, quantW), truncatePath(f.Params, paramsW), truncatePath(f.Path, pathW))
		if i == m.cursor {
			row = theme.Selected.Render("> " + row)
		} else {
			row = "  " + row
		}
		rows = append(rows, row)
	}
	body := strings.Join(rows, "\n")
	help := theme.Subtitle.Render(hint)
	parts := []string{title}
	if m.err != "" {
		parts = append(parts, theme.Error.Render("scan error: "+m.err))
	}
	parts = append(parts, statusLine, filterLine, body, help)
	box := lipgloss.JoinVertical(lipgloss.Left, parts...)
	return theme.Pane.Width(boxW).Render(box)
}

// pickerBoxWidth picks the picker overlay width from the terminal width.
// Targets min(width-4, 120) with a 24-column floor so narrow terminals
// still produce a usable layout.
func pickerBoxWidth(termWidth int) int {
	const ceil = 120
	w := termWidth - 4
	if w > ceil {
		w = ceil
	}
	if w < 24 {
		w = 24
	}
	return w
}

// pickerColumnWidths splits the box width across name / quant / params /
// path columns. Quant + params have fixed widths; the rest is split 1:2
// between name and path.
func pickerColumnWidths(boxW int) (name, quant, params, path int) {
	const (
		quantW  = 8
		paramsW = 6
		gutter  = 8 // 4 separators × 2 spaces
	)
	rest := boxW - quantW - paramsW - gutter
	if rest < 16 {
		rest = 16
	}
	name = rest / 3
	if name < 8 {
		name = 8
	}
	path = rest - name
	if path < 8 {
		path = 8
	}
	return name, quantW, paramsW, path
}

func truncatePath(s string, max int) string {
	if theme.RuneWidth(s) <= max {
		return s
	}
	return theme.TruncateRuneWidth(s, max, "…")
}
