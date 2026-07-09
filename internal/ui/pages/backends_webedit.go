package pages

import (
	tea "github.com/charmbracelet/bubbletea"

	"github.com/quantmind-br/model-loader/internal/domain"
	"github.com/quantmind-br/model-loader/internal/service/configweb"
)

type backendWebEditStartedMsg struct {
	session *configweb.Session
	url     string
}

type backendWebEditDoneMsg struct {
	saved     bool
	backendID string
	err       error
}

type backendWebEditFailedMsg struct{ err error }

// startAdd opens the browser-based backend editor seeded with a new draft.
func (p BackendsPage) startAdd() (tea.Model, tea.Cmd) {
	if p.manager == nil {
		p, fc := p.withFlashError("backend manager not available")
		return p, fc
	}
	if len(p.kindOptions()) == 0 {
		p, fc := p.withFlashError("no backend generators registered")
		return p, fc
	}
	opts := p.sortedKinds()
	kind := domain.BackendKindLlamaServer
	if !p.hasKind(kind) && len(opts) > 0 {
		kind = opts[0]
	}
	d := configweb.BackendDraft{
		IsNew: true,
		Kind:  kind,
	}
	p.webEditing = true
	return p, p.startBackendWebEdit(d)
}

// startEditSelected opens the browser-based backend editor seeded with the
// highlighted backend.
func (p BackendsPage) startEditSelected() (tea.Model, tea.Cmd) {
	b, ok := p.selectedBackend()
	if !ok {
		return p, nil
	}
	d := configweb.BackendDraft{
		ID:          b.ID,
		OrigID:      b.ID,
		IsNew:       false,
		Name:        b.Name,
		Kind:        b.Kind,
		Executable:  b.Executable,
		Description: b.Description,
		Tags:        b.Tags,
	}
	p.webEditing = true
	return p, p.startBackendWebEdit(d)
}

func (p BackendsPage) startBackendWebEdit(draft configweb.BackendDraft) tea.Cmd {
	return func() tea.Msg {
		sess := configweb.NewSession(configweb.Deps{
			Catalog:             p.catalogStore,
			Schemas:             p.schemaStore,
			Manager:             p.manager,
			InitialBackendDraft: draft,
		})
		url, err := sess.Start()
		if err != nil {
			return backendWebEditFailedMsg{err: err}
		}
		openBrowser(url + "/backend/")
		return backendWebEditStartedMsg{session: sess, url: url}
	}
}

func waitForBackendWebEdit(sess *configweb.Session) tea.Cmd {
	return func() tea.Msg {
		res := <-sess.Done()
		return backendWebEditDoneMsg{saved: res.Saved, backendID: res.BackendID, err: res.Err}
	}
}

// handleWebEditStarted records the live session/url and begins waiting for
// its completion.
func (p BackendsPage) handleWebEditStarted(m backendWebEditStartedMsg) (tea.Model, tea.Cmd) {
	p.webSession = m.session
	p.webURL = m.url
	return p, waitForBackendWebEdit(m.session)
}

// handleWebEditDone clears the web-edit state and reloads, flashing the
// save/cancel/error outcome.
func (p BackendsPage) handleWebEditDone(m backendWebEditDoneMsg) (tea.Model, tea.Cmd) {
	p.webEditing = false
	p.webSession = nil
	p.webURL = ""
	var fc tea.Cmd
	if m.err != nil {
		p, fc = p.withFlashError("backend edit failed: " + m.err.Error())
	} else if m.saved {
		p, fc = p.withFlash("saved backend " + m.backendID)
	} else {
		p, fc = p.withFlash("backend edit cancelled")
	}
	return p, tea.Batch(p.loadCmd(), fc)
}

// handleWebEditFailed clears the web-edit state after a session start error.
func (p BackendsPage) handleWebEditFailed(m backendWebEditFailedMsg) (tea.Model, tea.Cmd) {
	p.webEditing = false
	p.webSession = nil
	p.webURL = ""
	p, fc := p.withFlashError("backend edit failed: " + m.err.Error())
	return p, fc
}

// handleWebEditKey handles keystrokes while the browser editor modal is up:
// esc cancels the session, everything else is swallowed.
func (p BackendsPage) handleWebEditKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	if msg.String() == "esc" {
		p.cleanupBackendWebEdit()
		p.webEditing = false
		p.webSession = nil
		p.webURL = ""
		p, fc := p.withFlash("backend edit cancelled")
		return p, fc
	}
	return p, nil
}

func (p BackendsPage) renderWebEditModal() string {
	return "\n  Editing backend in browser…\n\n  " + p.webURL + "\n\n  Save or cancel on the page. (esc cancels)\n"
}

func (p BackendsPage) Cleanup() {
	if p.webSession != nil {
		p.webSession.Cancel()
	}
	if p.probeCancel != nil {
		p.probeCancel() // stop any in-flight probe producer on quit (audit N-C14)
	}
}

func (p BackendsPage) cleanupBackendWebEdit() {
	if p.webSession != nil {
		p.webSession.Cancel()
	}
}
