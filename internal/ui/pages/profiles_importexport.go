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
		p.logger.Warn("profiles_export_skipped", "reason", "export_dir_not_configured")
		p, fc := p.withFlashError("export failed: directory not configured (check ~/.local/state/model-loader/exports)")
		return p, fc
	}
	bundle, err := profilestore.ExportAll(p.store, p.exportDir)
	if err != nil {
		p.logger.Error("profiles_export_failed", "export_dir", p.exportDir, "err", err)
		p, fc := p.withFlashError("export failed: " + err.Error())
		return p, fc
	}
	fullPath := profilestore.ExportFilename(p.exportDir, bundle.ExportedAt)
	filename := filepath.Base(fullPath)
	p.logger.Info("profiles_export_done", "path", fullPath, "count", len(bundle.Profiles))
	// F-03 audit: show count + filename and log the full path so the user
	// can see where the bundle landed without the action looking like a
	// silent no-op.
	p, fc := p.withFlash(fmt.Sprintf("exported %d profile(s) → %s", len(bundle.Profiles), filename))
	return p, fc
}

func (p ProfilesPage) startImport() (tea.Model, tea.Cmd) {
	fp := filepicker.New()
	fp.AllowedTypes = []string{".json"}
	fp.FileAllowed = true
	fp.DirAllowed = false
	// Open where bundles are most likely to be (the export dir is where [E]
	// writes them), falling back to the profiles dir, then $HOME — instead of
	// always starting in $HOME where there's rarely a bundle to pick (UX-01).
	fp.CurrentDirectory = p.importStartDir()
	// Replace the bubbles default empty-state ("Bummer. No Files Found.") with
	// a message in the app's voice (UX-01).
	fp.Styles.EmptyDirectory = fp.Styles.EmptyDirectory.
		SetString("No .json bundles here — navigate to a folder with an exported bundle, or press esc to cancel.")
	p.importPicker = fp
	p.importPickerActive = true
	return p, p.importPicker.Init()
}

// importStartDir picks the directory the import file picker opens in:
// the configured export dir (where [E] writes bundles), then the profiles
// dir when the store is FS-backed, then the user's home directory.
func (p ProfilesPage) importStartDir() string {
	if p.exportDir != "" {
		if _, err := os.Stat(p.exportDir); err == nil {
			return p.exportDir
		}
	}
	if fsStore, ok := p.store.(*profilestore.FSStore); ok {
		if dir := fsStore.Dir(); dir != "" {
			return dir
		}
	}
	if home, err := os.UserHomeDir(); err == nil {
		return home
	}
	return "."
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
