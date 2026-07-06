package pages

import (
	"strings"

	"github.com/charmbracelet/lipgloss"

	"github.com/quantmind-br/model-loader/internal/service/backendcatalog"
	"github.com/quantmind-br/model-loader/internal/ui/components"
	"github.com/quantmind-br/model-loader/internal/ui/theme"
)

func (p BackendsPage) View() string {
	if p.webEditing {
		return p.renderWebEditModal()
	}
	if p.form != nil {
		return components.Modal("Backend", p.form.View(), p.width, p.height)
	}

	mode, listW, detailW := theme.ResponsiveSplit(p.width)
	leftContent := p.list.View()
	if len(p.list.Items()) == 0 {
		leftContent = components.EmptyState("No backends yet", "Press [n] to add one")
	}
	left := lipgloss.NewStyle().Width(listW).Render(leftContent)
	rightContent := p.detailView(detailW)
	if p.pendingRefresh {
		rightContent = components.LoadingLine(p.spinnerModel, "Refreshing schema", 0) + "\n" + rightContent
	}
	if p.pendingProbe {
		rightContent = components.LoadingLine(p.spinnerModel, "Probing backends", 0) + "\n" + rightContent
	}
	right := lipgloss.NewStyle().Width(detailW).Render(rightContent)
	var body string
	if mode == theme.LayoutStacked {
		rule := theme.Subtitle.Render(strings.Repeat("─", max(1, listW)))
		leftH := lipgloss.Height(left)
		right = lipgloss.NewStyle().Width(detailW).MaxHeight(max(3, p.height-2-leftH-1)).Render(rightContent)
		body = lipgloss.JoinVertical(lipgloss.Left, left, rule, right)
	} else {
		leftH := len(strings.Split(left, "\n"))
		rightH := len(strings.Split(right, "\n"))
		divH := max(1, max(leftH, rightH))
		divLine := lipgloss.NewStyle().Foreground(theme.ColorDim).Render("│")
		divider := strings.Repeat(divLine+"\n", divH-1) + divLine
		body = lipgloss.JoinHorizontal(lipgloss.Top, left, divider, right)
	}

	if v := p.flash.View(); v != "" {
		body = lipgloss.JoinVertical(lipgloss.Left, body, v)
	}
	return body
}

// OverlayView routes the delete/refresh confirms through the shared centered
// Modal overlay (the same path Profiles/Server/Benchmark use) instead of
// returning the Modal straight from View(). Returning it from View() let
// theme.ClampBody re-wrap the already-bordered box, which overflowed 80
// columns and doubled the line spacing (RENDER-01).
func (p BackendsPage) OverlayView() Overlay {
	if p.refreshConfirm.Active() {
		content := components.Modal("Refresh schema", p.refreshConfirm.View(), p.width, p.height)
		return Overlay{Content: content, Width: p.width, Height: p.height, Active: true}
	}
	if p.deleteConfirm.Active() {
		content := components.Modal("Delete backend", p.deleteConfirm.View(), p.width, p.height)
		return Overlay{Content: content, Width: p.width, Height: p.height, Active: true}
	}
	return Overlay{}
}

func (p BackendsPage) detailView(w int) string {
	if len(p.list.Items()) == 0 {
		return components.EmptyState("No backends yet", "Press [n] to add one")
	}
	sel, ok := p.selectedBackend()
	if !ok {
		return ""
	}
	defaultMark := ""
	if sel.ID == p.defaultBackendID {
		defaultMark = " " + theme.OK.Render("default")
	}
	tags := strings.Join(sel.Tags, ", ")
	if tags == "" {
		tags = "(none)"
	}
	desc := sel.Description
	if desc == "" {
		desc = "(none)"
	}
	probeLine := ""
	if r, ok := p.probeResults[sel.ID]; ok {
		statusStyle := theme.Subtitle
		switch r.status {
		case backendcatalog.ProbeStatusOK:
			statusStyle = theme.OK
		case backendcatalog.ProbeStatusErr:
			statusStyle = theme.Error
		}
		probeLine = "\nProbe:       " + statusStyle.Render(string(r.status))
		if r.latency > 0 {
			probeLine += " (" + r.latency.String() + ")"
		}
	}
	labelStyle := lipgloss.NewStyle().Width(13).Foreground(theme.ColorDim)
	valW := max(6, w-13)

	row := func(label, value string) string {
		return lipgloss.JoinHorizontal(lipgloss.Top, labelStyle.Render(label), truncate(value, valW))
	}

	var b strings.Builder
	if p.defaultBackendID == "" {
		b.WriteString(components.EmptyState("No default backend set", "Press [D] to set a backend as default") + "\n\n")
	}
	b.WriteString(theme.Title.Render(sel.Name) + defaultMark + "\n")
	b.WriteString(theme.Subtitle.Render(string(sel.Kind)) + "\n\n")
	b.WriteString(row("ID:", sel.ID) + "\n")
	b.WriteString(row("Kind:", string(sel.Kind)) + "\n")
	b.WriteString(row("Executable:", sel.Executable) + "\n")
	b.WriteString(row("SchemaRef:", sel.SchemaRef) + "\n")
	b.WriteString(row("Description:", desc) + "\n")
	b.WriteString(row("Tags:", tags) + "\n")
	b.WriteString(row("Created:", formatBackendTime(sel.Meta.CreatedAt)) + "\n")
	b.WriteString(row("Updated:", formatBackendTime(sel.Meta.UpdatedAt)) + "\n")
	b.WriteString(probeLine)
	return b.String()
}
