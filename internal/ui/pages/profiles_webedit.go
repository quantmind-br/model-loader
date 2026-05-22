package pages

import (
	"os/exec"
	"runtime"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/quantmind-br/model-loader/internal/service/configweb"
)

type webEditStartedMsg struct {
	session *configweb.Session
	url     string
}

type webEditDoneMsg struct {
	saved     bool
	profileID string
	err       error
}

type webEditFailedMsg struct{ err error }

// startWebEdit boots a configweb session for the given draft and returns a Cmd
// that emits webEditStartedMsg (or webEditFailedMsg).
func (p ProfilesPage) startWebEdit(draft configweb.Draft) tea.Cmd {
	return func() tea.Msg {
		sess := configweb.NewSession(configweb.Deps{
			Profiles:     p.store,
			Catalog:      p.catalogStore,
			Schemas:      p.schemaStore,
			InitialDraft: draft,
		})
		url, err := sess.Start()
		if err != nil {
			return webEditFailedMsg{err: err}
		}
		openBrowser(url)
		return webEditStartedMsg{session: sess, url: url}
	}
}

// waitForWebEdit blocks on the session result channel in a Cmd goroutine.
func waitForWebEdit(sess *configweb.Session) tea.Cmd {
	return func() tea.Msg {
		res := <-sess.Done()
		return webEditDoneMsg{saved: res.Saved, profileID: res.ProfileID, err: res.Err}
	}
}

// Cleanup cancels any active web-edit session. Called by the root model before
// tea.Quit so the configweb HTTP server is shut down and waitForWebEdit
// goroutine unblocks even when the TUI exits via ctrl+c or q.
func (p ProfilesPage) Cleanup() {
	if p.webSession != nil {
		p.webSession.Cancel()
	}
}

func openBrowser(url string) {
	var cmd string
	var args []string
	switch runtime.GOOS {
	case "darwin":
		cmd = "open"
	case "windows":
		cmd, args = "cmd", []string{"/c", "start"}
	default:
		cmd = "xdg-open"
	}
	args = append(args, url)
	_ = exec.Command(cmd, args...).Start()
}
