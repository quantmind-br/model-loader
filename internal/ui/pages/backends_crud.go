package pages

import (
	"context"
	"slices"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/huh"

	"github.com/quantmind-br/model-loader/internal/domain"
	"github.com/quantmind-br/model-loader/internal/ui/components"
)

type formMode int

const (
	formModeNone formMode = iota
	formModeAdd
	formModeEdit
)

type backendDraft struct {
	ID          string
	Name        string
	Kind        string
	Executable  string
	Description string
	Tags        string
}

func (p BackendsPage) forwardToForm(msg tea.Msg) (tea.Model, tea.Cmd) {
	if p.form == nil {
		return p, nil
	}
	updated, cmd := p.form.Update(msg)
	if f, ok := updated.(*huh.Form); ok {
		p.form = f
	}
	if p.form != nil && p.form.State == huh.StateCompleted {
		return p.commitForm(cmd)
	}
	return p, cmd
}

func (p BackendsPage) commitForm(formCmd tea.Cmd) (tea.Model, tea.Cmd) {
	if p.draft == nil {
		p.form = nil
		p.formMode = formModeNone
		return p, formCmd
	}
	d := *p.draft
	mode := p.formMode
	p.form = nil
	p.formMode = formModeNone
	p.draft = nil

	var err error
	var b domain.Backend
	switch mode {
	case formModeAdd:
		b, err = p.manager.AddBackend(context.Background(), d.Name, d.Executable, domain.BackendKind(d.Kind))
		if err == nil && (d.Description != "" || d.Tags != "") {
			b, err = p.manager.UpdateBackend(b.ID, domain.Backend{Name: b.Name, Executable: b.Executable, Description: d.Description, Tags: parseTags(d.Tags)})
		}
	case formModeEdit:
		b, err = p.manager.UpdateBackend(d.ID, domain.Backend{Name: d.Name, Executable: d.Executable, Description: d.Description, Tags: parseTags(d.Tags)})
	}

	var fc tea.Cmd
	if err != nil {
		p, fc = p.withFlashError("save backend failed: " + err.Error())
		return p, tea.Batch(formCmd, fc)
	}
	p, fc = p.withFlash("saved backend " + b.ID)
	return p, tea.Batch(formCmd, p.loadCmd(), fc)
}

func (p BackendsPage) askDeleteSelected() (tea.Model, tea.Cmd) {
	b, ok := p.selectedBackend()
	if !ok {
		return p, nil
	}
	var cmd tea.Cmd
	p.deleteConfirm, cmd = setupConfirm("Delete backend "+b.Name+"?", "Delete", "Cancel",
		func() tea.Cmd { return func() tea.Msg { return backendDeleteConfirmedMsg{id: b.ID} } })
	return p, cmd
}

func (p BackendsPage) performDelete(id string) (tea.Model, tea.Cmd) {
	if p.manager == nil {
		p, fc := p.withFlashError("backend manager not available")
		return p, fc
	}
	var fc tea.Cmd
	if err := p.manager.DeleteBackend(id); err != nil {
		p, fc = p.withFlashError("delete failed: " + err.Error())
	} else {
		p, fc = p.withFlash("deleted " + id)
	}
	p.deleteConfirm = components.Confirm{}
	return p, tea.Batch(p.loadCmd(), fc)
}

func (p BackendsPage) setDefaultSelected() (tea.Model, tea.Cmd) {
	b, ok := p.selectedBackend()
	if !ok {
		return p, nil
	}
	if err := p.manager.SetDefaultBackend(b.ID); err != nil {
		p, fc := p.withFlashError("set default failed: " + err.Error())
		return p, fc
	}
	p.defaultBackendID = b.ID
	p, fc := p.withFlash("default backend " + b.ID)
	return p, fc
}

func (p BackendsPage) askRefreshSelected() (tea.Model, tea.Cmd) {
	b, ok := p.selectedBackend()
	if !ok {
		return p, nil
	}
	var cmd tea.Cmd
	p.refreshConfirm, cmd = setupConfirm("Refresh schema for "+b.Name+"?", "Refresh", "Cancel",
		func() tea.Cmd { return func() tea.Msg { return backendRefreshConfirmedMsg{id: b.ID} } })
	return p, cmd
}

func (p BackendsPage) performRefresh(id string) (tea.Model, tea.Cmd) {
	if p.manager == nil {
		p.pendingRefresh = false
		p, fc := p.withFlashError("backend manager not available")
		return p, fc
	}
	p.refreshConfirm = components.Confirm{}
	if err := p.manager.RefreshSchema(id); err != nil {
		p.pendingRefresh = false
		p, fc := p.withFlashError("refresh schema failed: " + err.Error())
		return p, fc
	}
	p.pendingRefresh = false
	p, fc := p.withFlash("schema refreshed " + id)
	return p, fc
}

func (p BackendsPage) selectedBackend() (domain.Backend, bool) {
	item, ok := p.list.SelectedItem().(backendItem)
	if !ok {
		return domain.Backend{}, false
	}
	return item.backend, true
}

func (p BackendsPage) kindOptions() []huh.Option[string] {
	kinds := p.sortedKinds()
	opts := make([]huh.Option[string], 0, len(kinds))
	for _, kind := range kinds {
		opts = append(opts, huh.NewOption(string(kind), string(kind)))
	}
	return opts
}

func (p BackendsPage) sortedKinds() []domain.BackendKind {
	if p.manager == nil {
		return nil
	}
	kinds := make([]domain.BackendKind, 0, len(p.manager.Generators()))
	for kind := range p.manager.Generators() {
		kinds = append(kinds, kind)
	}
	slices.Sort(kinds)
	return kinds
}

func (p BackendsPage) hasKind(kind domain.BackendKind) bool {
	if p.manager == nil {
		return false
	}
	_, ok := p.manager.Generators()[kind]
	return ok
}

func parseTags(s string) []string {
	parts := strings.Split(s, ",")
	tags := make([]string, 0, len(parts))
	for _, part := range parts {
		part = strings.TrimSpace(part)
		if part != "" {
			tags = append(tags, part)
		}
	}
	return tags
}

func formatBackendTime(t time.Time) string {
	if t.IsZero() {
		return "(unknown)"
	}
	return t.Format(time.RFC3339)
}
