package pages

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/charmbracelet/bubbles/filepicker"
	tea "github.com/charmbracelet/bubbletea"

	"github.com/quantmind-br/model-loader/internal/domain"
	"github.com/quantmind-br/model-loader/internal/service/profilestore"
	"github.com/quantmind-br/model-loader/internal/ui/components"
)

func (p ProfilesPage) exportProfiles() (tea.Model, tea.Cmd) {
	if p.exportDir == "" {
		p, fc := p.withFlashError("export directory not configured")
		return p, fc
	}
	bundle, err := profilestore.ExportAll(p.store, p.exportDir)
	if err != nil {
		p, fc := p.withFlashError("export failed: " + err.Error())
		return p, fc
	}
	filename := filepath.Base(profilestore.ExportFilename(p.exportDir, bundle.ExportedAt))
	p, fc := p.withFlash("exported to " + filename)
	return p, fc
}

func (p ProfilesPage) startImport() (tea.Model, tea.Cmd) {
	fp := filepicker.New()
	fp.AllowedTypes = []string{".json"}
	fp.FileAllowed = true
	fp.DirAllowed = false
	if home, err := os.UserHomeDir(); err == nil {
		fp.CurrentDirectory = home
	}
	p.importPicker = fp
	p.importPickerActive = true
	return p, p.importPicker.Init()
}

func (p ProfilesPage) startImportWithPath(path string) (tea.Model, tea.Cmd) {
	p.conflictModal = components.NewConflictModal(path,
		func(mode profilestore.ConflictMode) tea.Cmd {
			return p.importBundleCmd(path, mode)
		},
		func() tea.Cmd {
			_, fc := p.withFlash("import cancelled")
			return fc
		},
	)
	return p, p.conflictModal.Init()
}

func (p ProfilesPage) importBundleCmd(path string, mode profilestore.ConflictMode) tea.Cmd {
	return func() tea.Msg {
		res, err := profilestore.ImportBundle(p.store, path, mode)
		if err != nil {
			return components.FlashClearMsg{}
		}
		return importDoneMsg{Result: res}
	}
}

func (p ProfilesPage) handleImportDone(msg importDoneMsg) (tea.Model, tea.Cmd) {
	p, fc := p.withFlash(fmt.Sprintf("imported: +%d ~%d !%d ->%d", msg.Result.Added, msg.Result.Skipped, msg.Result.Renamed, msg.Result.Replaced))
	return p, tea.Batch(fc, p.loadCmd())
}

func (p ProfilesPage) startUndo() (tea.Model, tea.Cmd) {
	if _, isCorrupt := p.list.SelectedItem().(corruptItem); isCorrupt {
		return p, nil
	}
	sel, ok := p.list.SelectedItem().(item)
	if !ok {
		return p, nil
	}
	fsStore, ok := p.store.(*profilestore.FSStore)
	if !ok {
		p, fc := p.withFlash("undo not available for this store")
		return p, fc
	}
	prev, found, err := profilestore.LoadPrevious(fsStore.Dir(), sel.p.ID)
	if err != nil || !found {
		p, fc := p.withFlash("No previous version available")
		return p, fc
	}
	var diffs []components.FieldDiff
	if prev.Name != sel.p.Name {
		diffs = append(diffs, components.FieldDiff{Key: "Name", OldValue: prev.Name, NewValue: sel.p.Name})
	}
	if prev.Model != sel.p.Model {
		diffs = append(diffs, components.FieldDiff{Key: "Model", OldValue: prev.Model, NewValue: sel.p.Model})
	}
	p.undoModal = components.NewUndoModal(diffs,
		func() tea.Cmd {
			return p.restorePreviousCmd(prev)
		},
		func() tea.Cmd {
			_, fc := p.withFlash("undo cancelled")
			return fc
		},
	)
	return p, p.undoModal.Init()
}

func (p ProfilesPage) restorePreviousCmd(prev domain.Profile) tea.Cmd {
	return func() tea.Msg {
		if err := p.store.Save(prev); err != nil {
			return components.FlashClearMsg{}
		}
		return undoDoneMsg{}
	}
}

func (p ProfilesPage) handleUndoDone(_ undoDoneMsg) (tea.Model, tea.Cmd) {
	p, fc := p.withFlash("undo complete")
	return p, tea.Batch(fc, p.loadCmd())
}
