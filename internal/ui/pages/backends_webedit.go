package pages

import (
	tea "github.com/charmbracelet/bubbletea"

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

func (p BackendsPage) Cleanup() {
	if p.webSession != nil {
		p.webSession.Cancel()
	}
}

func (p BackendsPage) cleanupBackendWebEdit() {
	if p.webSession != nil {
		p.webSession.Cancel()
	}
}
