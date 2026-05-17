// Package pages holds tab page implementations.
package pages

import (
	"context"
	"errors"
	"fmt"
	"os"
	"sort"
	"strings"

	"github.com/atotto/clipboard"
	"github.com/charmbracelet/bubbles/key"
	"github.com/charmbracelet/bubbles/table"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/quantmind-br/model-loader/internal/domain"
	"github.com/quantmind-br/model-loader/internal/service/downloadmgr"
	"github.com/quantmind-br/model-loader/internal/service/hfhub"
	"github.com/quantmind-br/model-loader/internal/service/modelscanner"
	"github.com/quantmind-br/model-loader/internal/service/profilestore"
	"github.com/quantmind-br/model-loader/internal/ui/components"
	"github.com/quantmind-br/model-loader/internal/ui/internal/filter"
	"github.com/quantmind-br/model-loader/internal/ui/theme"
)

// clipboardWriter is overridable in tests to bypass the system clipboard
// (which may be unavailable on CI without a display server).
var clipboardWriter = clipboard.WriteAll

var fileRemover = os.Remove

type hfSearcherAdapter struct{ client *hfhub.Client }

func (a hfSearcherAdapter) Search(ctx context.Context, query string, limit int) ([]components.SearchResult, error) {
	raw, err := a.client.Search(ctx, query, limit)
	if err != nil {
		return nil, err
	}
	out := make([]components.SearchResult, len(raw))
	for i, r := range raw {
		out[i] = components.SearchResult(r)
	}
	return out, nil
}

type hfFileListerAdapter struct{ client *hfhub.Client }

func (a hfFileListerAdapter) RepoInfo(ctx context.Context, repoID string) (*components.RepoInfo, error) {
	info, err := a.client.RepoInfo(ctx, repoID)
	if err != nil {
		return nil, err
	}
	out := &components.RepoInfo{
		ID:       info.ID,
		Tags:     append([]string(nil), info.Tags...),
		Siblings: make([]components.Sibling, len(info.Siblings)),
	}
	for i, s := range info.Siblings {
		out.Siblings[i] = components.Sibling(s)
	}
	return out, nil
}

type downloadSnapshotAdapter struct{ manager *downloadmgr.Manager }

func (a downloadSnapshotAdapter) Snapshot() []components.DownloadState {
	if a.manager == nil {
		return nil
	}
	raw := a.manager.Snapshot()
	out := make([]components.DownloadState, 0, len(raw))
	for _, st := range raw {
		out = append(out, components.DownloadState{
			ID:     string(st.ID),
			Name:   st.Spec.Filename,
			Status: downloadStatusLabel(st.Status),
			Bytes:  st.Bytes,
			Total:  st.Total,
			Err:    downloadErrString(st.Err),
		})
	}
	return out
}

func downloadStatusLabel(status downloadmgr.Status) string {
	switch status {
	case downloadmgr.StatusQueued:
		return "queued"
	case downloadmgr.StatusActive:
		return "active"
	case downloadmgr.StatusCompleted:
		return "completed"
	case downloadmgr.StatusFailed:
		return "failed"
	case downloadmgr.StatusCancelled:
		return "cancelled"
	default:
		return "unknown"
	}
}

func downloadErrString(err error) string {
	if err == nil {
		return ""
	}
	return err.Error()
}

// pathStatus tracks per-root scan progress shown above the table.
type pathStatus struct {
	state string // "scanning" | "scanned" | "error"
	count int
	err   string
}

// actionOption is one row in the inline action selector overlay.
type actionOption struct{ label, value string }

// actionMenu is the inline modal used for "what should we do with this
// model file?". Replaces the earlier huh.NewForm wrapping a single
// Select that did not reliably reach huh.StateCompleted on a single
// Enter press.
type actionMenu struct {
	title      string
	options    []actionOption
	cursor     int
	targetPath string // GGUF path the menu acts on
	stage      actionStage
}

type actionStage int

const (
	actionStageRoot actionStage = iota
)

// ModelsPage browses GGUF files discovered by ModelScanner.
type ModelsPage struct {
	scanner modelscanner.Scanner
	paths   []string
	store   profilestore.Store
	cancel  context.CancelFunc
	scanID  int // epoch; bumped each rescan to discard stale events

	files     []domain.ModelFile
	statusMap map[string]pathStatus

	table      table.Model
	width      int
	height     int
	filter     string
	filterMode bool
	flash      components.Flash

	action        *actionMenu
	deleteConfirm components.Confirm

	profilePicker           *components.ProfilePicker
	profilePickerTargetPath string

	// Hugging Face integration. Wired by builders (T13); nil-safe until then.
	hfClient       *hfhub.Client
	dlManager      *downloadmgr.Manager
	hfSearch       *components.HFSearchPicker
	hfFilePicker   *components.HFFilePicker
	downloads      *components.DownloadProgress
	downloadEvents <-chan downloadmgr.Event
	searchEpoch    int    // bumped per search keystroke for debounce
	pendingRepoID  string // carried between RepoInfo lookup and file picker open

	keys modelsKeyMap
}

func (p ModelsPage) withFlash(msg string) (ModelsPage, tea.Cmd) {
	var cmd tea.Cmd
	p.flash, cmd = p.flash.Set(msg)
	return p, cmd
}

type modelsKeyMap struct {
	Filter, Rescan, Enter, Cancel key.Binding
}

func defaultModelsKeys() modelsKeyMap {
	return modelsKeyMap{
		Filter: key.NewBinding(key.WithKeys("/"), key.WithHelp("/", "filter")),
		Rescan: key.NewBinding(key.WithKeys("R"), key.WithHelp("R", "rescan")),
		Enter:  key.NewBinding(key.WithKeys("enter"), key.WithHelp("enter", "actions")),
		Cancel: key.NewBinding(key.WithKeys("esc"), key.WithHelp("esc", "clear filter")),
	}
}

// NewModelsPage builds a page wired to a Scanner and configured search paths.
func NewModelsPage(scanner modelscanner.Scanner, paths []string) ModelsPage {
	cols := []table.Column{
		{Title: "Name", Width: 36},
		{Title: "Size", Width: 10},
		{Title: "Quant", Width: 10},
		{Title: "Params", Width: 8},
		{Title: "Path", Width: 40},
	}
	t := table.New(table.WithColumns(cols), table.WithFocused(true), table.WithHeight(12))

	statusMap := make(map[string]pathStatus, len(paths))
	for _, p := range paths {
		statusMap[p] = pathStatus{state: "scanning"}
	}
	return ModelsPage{
		scanner:   scanner,
		paths:     paths,
		statusMap: statusMap,
		table:     t,
		keys:      defaultModelsKeys(),
		flash:     components.NewFlash("models"),
	}
}

// WithProfileStore enables the "Use in existing profile" action by
// giving the page access to the profile store. Without it, that option
// is hidden from the action menu.
func (p ModelsPage) WithProfileStore(store profilestore.Store) ModelsPage {
	p.store = store
	return p
}

// WithHFClient injects a Hugging Face Hub client so the page can offer
// remote model search ("s" key in T14). Without it, the HF entry points
// stay disabled.
func (p ModelsPage) WithHFClient(c *hfhub.Client) ModelsPage {
	p.hfClient = c
	return p
}

// WithDownloadManager injects the download manager and subscribes the
// page to its event channel. Without it, download UI stays inert.
func (p ModelsPage) WithDownloadManager(m *downloadmgr.Manager) ModelsPage {
	p.dlManager = m
	if m != nil {
		p.downloadEvents = m.Subscribe()
		p.downloads = components.NewDownloadProgress(downloadSnapshotAdapter{manager: m}, p.width)
	}
	return p
}

// IsCapturingInput tells the root model when the page owns global
// keystrokes (Tab/Shift+Tab) — true while the inline action menu or
// filter input is open so cursor navigation does not leak into tab
// cycling and printable characters are not stolen by global shortcuts.
func (p ModelsPage) IsCapturingInput() bool {
	return p.action != nil || p.deleteConfirm.Active() || p.filterMode || p.profilePicker != nil ||
		(p.hfSearch != nil && p.hfSearch.IsActive()) ||
		(p.hfFilePicker != nil && p.hfFilePicker.IsActive()) ||
		(p.downloads != nil && p.downloads.IsFocusVisible())
}

// scanStartedMsg delivers the channel + cancel handle from a fresh scan
// start. State mutations happen when this message lands in Update.
// scanID matches ModelsPage.scanID at start; stale messages from a
// previous rescan are discarded by epoch comparison.
type scanStartedMsg struct {
	scanID int
	ch     <-chan domain.ScanEvent
	cancel context.CancelFunc
	err    error
}

// scanEventMsg carries one ScanEvent plus the channel for re-arming.
type scanEventMsg struct {
	scanID int
	ch     <-chan domain.ScanEvent
	evt    domain.ScanEvent
}

// scanChannelClosedMsg signals the scan goroutine finished and closed
// its channel.
type scanChannelClosedMsg struct {
	scanID int
}

// modelsReloadMsg is dispatched by Reload() when the root activates this
// tab. It triggers a silent rescan (no flash) so external filesystem
// changes surface without requiring the user to press R.
type modelsReloadMsg struct{}

// downloadEventMsg lifts a downloadmgr.Event onto the Bubble Tea bus so
// the page can react to lifecycle changes (queued → active → completed/
// failed/cancelled). Read pump lives in T13/T15.
type downloadEventMsg struct {
	event downloadmgr.Event
}

// hfSearchResultMsg delivers the outcome of a HF Hub search. epoch
// matches ModelsPage.searchEpoch at request time; stale responses from
// abandoned debounce windows are discarded by epoch comparison.
type hfSearchResultMsg struct {
	Epoch   int
	Results []components.ResultItem
	Err     error
}

// hfFileListMsg delivers the file siblings returned by RepoInfo for the
// pendingRepoID — consumed by the file picker overlay (T15).
type hfFileListMsg struct {
	Files []components.FileItem
	Err   error
}

// downloadCancelMsg requests cancellation of a download by ID. Emitted
// by the progress footer (T15 wires the Manager.Cancel call).
type downloadCancelMsg struct {
	id string
}

func (msg hfSearchResultMsg) componentMsg() components.HFSearchResultMsg {
	return components.HFSearchResultMsg(msg)
}

func (msg hfFileListMsg) componentMsg() components.HFFileListMsg {
	return components.HFFileListMsg(msg)
}

func (p ModelsPage) Init() tea.Cmd {
	return startScanCmd(p.scanner, p.paths, p.scanID)
}

// Reload implements the ui.Reloader contract. RootModel calls this on
// tab activation; we re-enter the rescan path through Update so the
// state mutations happen in a single place (beginRescan).
func (p ModelsPage) Reload() tea.Cmd {
	return func() tea.Msg { return modelsReloadMsg{} }
}

// beginRescan cancels any in-flight scan, bumps the scan epoch, resets
// per-root status to "scanning", and returns the page plus a Cmd that
// kicks off a fresh scan. When showFlash is true a "rescan started"
// message is shown — used for the manual R key, suppressed for the
// silent reload-on-focus path.
func (p ModelsPage) beginRescan(showFlash bool) (ModelsPage, tea.Cmd) {
	if p.cancel != nil {
		p.cancel()
		p.cancel = nil
	}
	p.scanID++
	p.files = nil
	for _, root := range p.paths {
		p.statusMap[root] = pathStatus{state: "scanning"}
	}
	p.refreshRows()
	cmds := []tea.Cmd{startScanCmd(p.scanner, p.paths, p.scanID)}
	if showFlash {
		var fc tea.Cmd
		p, fc = p.withFlash("rescan started")
		cmds = append(cmds, fc)
	}
	return p, tea.Batch(cmds...)
}

// startScanCmd builds a Cmd that creates ctx+cancel, kicks off the
// scanner, and delivers the channel via scanStartedMsg. The Cmd's
// closure owns the cancel until Update captures it.
func startScanCmd(scanner modelscanner.Scanner, paths []string, scanID int) tea.Cmd {
	return func() tea.Msg {
		ctx, cancel := context.WithCancel(context.Background())
		ch, err := scanner.Scan(ctx, paths)
		if err != nil {
			cancel()
			return scanStartedMsg{scanID: scanID, err: err}
		}
		return scanStartedMsg{scanID: scanID, ch: ch, cancel: cancel}
	}
}

func waitForScanEvent(ch <-chan domain.ScanEvent, scanID int) tea.Cmd {
	return func() tea.Msg {
		evt, ok := <-ch
		if !ok {
			return scanChannelClosedMsg{scanID: scanID}
		}
		return scanEventMsg{scanID: scanID, ch: ch, evt: evt}
	}
}

func (p ModelsPage) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	if p.downloadEvents != nil {
		select {
		case ev := <-p.downloadEvents:
			return p, func() tea.Msg { return downloadEventMsg{event: ev} }
		default:
		}
	}

	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		p.width, p.height = msg.Width, msg.Height
		p.table.SetHeight(msg.Height - 8)
		if p.hfSearch != nil {
			p.hfSearch.SetSize(msg.Width, msg.Height)
		}
		if p.hfFilePicker != nil {
			p.hfFilePicker.SetSize(msg.Width, msg.Height)
		}
		if p.downloads != nil {
			p.downloads.SetWidth(msg.Width)
		}
		return p, nil
	case components.FlashClearMsg:
		p.flash, _ = p.flash.Update(msg)
		return p, nil
	case scanStartedMsg:
		if msg.scanID != p.scanID {
			if msg.cancel != nil {
				msg.cancel()
			}
			return p, nil
		}
		if msg.err != nil {
			for _, root := range p.paths {
				p.statusMap[root] = pathStatus{state: "error", err: msg.err.Error()}
			}
			return p, nil
		}
		p.cancel = msg.cancel
		return p, waitForScanEvent(msg.ch, msg.scanID)
	case scanEventMsg:
		if msg.scanID != p.scanID {
			return p, waitForScanEvent(msg.ch, msg.scanID)
		}
		updated, _ := p.handleScanEvent(msg.evt)
		next := updated.(ModelsPage)
		return next, waitForScanEvent(msg.ch, msg.scanID)
	case scanChannelClosedMsg:
		return p, nil
	case modelsReloadMsg:
		next, cmd := p.beginRescan(false)
		return next, cmd
	case downloadEventMsg:
		return p.handleDownloadEvent(msg.event)
	case components.HFSearchResultMsg:
		if p.hfSearch != nil && p.hfSearch.IsActive() {
			return p, p.hfSearch.Update(msg)
		}
		return p, nil
	case hfSearchResultMsg:
		if p.hfSearch != nil && p.hfSearch.IsActive() {
			return p, p.hfSearch.Update(msg.componentMsg())
		}
		return p, nil
	case components.HFFileListMsg:
		if p.hfFilePicker != nil && p.hfFilePicker.IsActive() {
			return p, p.hfFilePicker.Update(msg)
		}
		return p, nil
	case hfFileListMsg:
		if p.hfFilePicker != nil && p.hfFilePicker.IsActive() {
			return p, p.hfFilePicker.Update(msg.componentMsg())
		}
		return p, nil
	case components.DownloadCancelMsg:
		if p.dlManager != nil {
			_ = p.dlManager.Cancel(downloadmgr.ID(msg.ID))
		}
		return p, nil
	case downloadCancelMsg:
		if p.dlManager != nil {
			_ = p.dlManager.Cancel(downloadmgr.ID(msg.id))
		}
		return p, nil
	case components.ProfilePickedMsg:
		return p.handleProfilePicked(msg)
	case components.ProfilePickerCancelledMsg:
		p.profilePicker = nil
		p.profilePickerTargetPath = ""
		return p, nil
	case modelDeleteConfirmedMsg:
		return p.performDelete(msg.path)
	case tea.KeyMsg:
		if p.profilePicker != nil {
			np, cmd := p.profilePicker.Update(msg)
			p.profilePicker = &np
			return p, cmd
		}
		if p.deleteConfirm.Active() {
			return p.updateDeleteConfirm(msg)
		}
		if p.action != nil {
			return p.updateActionMenu(msg)
		}
		return p.handleKey(msg)
	}
	return p, nil
}

// handleProfilePicked applies the selected GGUF path to the chosen
// existing profile and persists, then clears the picker state.
func (p ModelsPage) handleProfilePicked(msg components.ProfilePickedMsg) (tea.Model, tea.Cmd) {
	targetPath := p.profilePickerTargetPath
	p.profilePicker = nil
	p.profilePickerTargetPath = ""
	if p.store == nil {
		p, fc := p.withFlash("profile store not wired")
		return p, fc
	}
	pr, err := p.store.Get(msg.ID)
	if err != nil {
		p, fc := p.withFlash("load profile: " + err.Error())
		return p, fc
	}
	pr.Model = targetPath
	if err := p.store.Save(pr); err != nil {
		p, fc := p.withFlash("save profile: " + err.Error())
		return p, fc
	}
	p, fc := p.withFlash("updated " + msg.ID)
	return p, fc
}

func (p ModelsPage) handleDownloadEvent(ev downloadmgr.Event) (tea.Model, tea.Cmd) {
	if p.downloads == nil && p.dlManager != nil {
		p.downloads = components.NewDownloadProgress(downloadSnapshotAdapter{manager: p.dlManager}, p.width)
	}
	switch ev.State.Status {
	case downloadmgr.StatusCompleted:
		p, fc := p.withFlash("downloaded: " + ev.State.Spec.Filename)
		next, scanCmd := p.beginRescan(false)
		return next, tea.Batch(fc, scanCmd)
	case downloadmgr.StatusFailed:
		msg := "download failed"
		if ev.State.Err != nil {
			msg += ": " + ev.State.Err.Error()
		}
		return p.withFlash(msg)
	default:
		return p, nil
	}
}

func (p ModelsPage) openHFFilePicker(item components.ResultItem) (tea.Model, tea.Cmd) {
	if p.hfClient == nil {
		return p.withFlash("HF client not wired")
	}
	isSnapshot := !item.HasGGUFTag()
	p.pendingRepoID = item.ModelID
	p.hfFilePicker = components.NewHFFilePicker(
		hfFileListerAdapter{client: p.hfClient},
		item.ModelID,
		isSnapshot,
		p.width,
		p.height,
	)
	p.hfSearch = nil
	return p, p.hfFilePicker.Init()
}

func (p ModelsPage) startSelectedDownloads() (tea.Model, tea.Cmd) {
	if p.hfFilePicker == nil {
		return p, nil
	}
	if p.dlManager == nil {
		p.hfFilePicker = nil
		p.pendingRepoID = ""
		return p.withFlash("download manager not wired")
	}
	if len(p.paths) == 0 {
		p.hfFilePicker = nil
		p.pendingRepoID = ""
		return p.withFlash(downloadmgr.ErrNoSearchPath.Error())
	}

	files := p.hfFilePicker.SelectedFiles()
	isSnapshot := p.hfFilePicker.IsSnapshot()
	p.hfFilePicker = nil
	repoID := p.pendingRepoID
	p.pendingRepoID = ""

	started := 0
	var cmds []tea.Cmd
	for _, file := range files {
		destDir, destFile, err := downloadmgr.ResolveDest(p.paths[0], repoID, file, isSnapshot)
		if errors.Is(err, downloadmgr.ErrAlreadyExists) {
			var cmd tea.Cmd
			p.flash, cmd = p.flash.Set("already exists: " + file)
			cmds = append(cmds, cmd)
			continue
		}
		if err != nil {
			var cmd tea.Cmd
			p.flash, cmd = p.flash.Set(err.Error())
			cmds = append(cmds, cmd)
			continue
		}
		url := p.hfClient.DownloadURL(repoID, file)
		_, err = p.dlManager.Start(downloadmgr.Spec{
			RepoID:     repoID,
			Filename:   file,
			URL:        url,
			DestDir:    destDir,
			DestFile:   destFile,
			IsSnapshot: isSnapshot,
		})
		if err != nil {
			var cmd tea.Cmd
			p.flash, cmd = p.flash.Set(err.Error())
			cmds = append(cmds, cmd)
			continue
		}
		started++
	}

	p.downloads = components.NewDownloadProgress(downloadSnapshotAdapter{manager: p.dlManager}, p.width)
	p, fc := p.withFlash(fmt.Sprintf("starting %d download(s)", started))
	cmds = append(cmds, fc)
	return p, tea.Batch(cmds...)
}

func (p ModelsPage) updateDeleteConfirm(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	if msg.String() == "esc" {
		p.deleteConfirm = components.Confirm{}
		p, fc := p.withFlash("delete cancelled")
		return p, fc
	}
	var cmd tea.Cmd
	p.deleteConfirm, cmd = p.deleteConfirm.Update(msg)
	return p, cmd
}

// updateActionMenu owns the inline action selector while it is on
// screen. Up/Down move the cursor, Enter commits the highlighted option,
// Esc cancels.
func (p ModelsPage) updateActionMenu(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "esc":
		p.action = nil
		return p, nil
	case "up", "k":
		if p.action.cursor > 0 {
			p.action.cursor--
		}
		return p, nil
	case "down", "j":
		if p.action.cursor < len(p.action.options)-1 {
			p.action.cursor++
		}
		return p, nil
	case "enter":
		opt := p.action.options[p.action.cursor]
		return p.commitRootAction(opt.value, p.action.targetPath)
	}
	return p, nil
}

// commitRootAction handles the choice from the first action menu.
func (p ModelsPage) commitRootAction(choice, path string) (tea.Model, tea.Cmd) {
	switch choice {
	case "new":
		p.action = nil
		return p, func() tea.Msg { return UseInNewProfileMsg{Path: path} }
	case "reveal":
		p.action = nil
		if err := clipboardWriter(path); err != nil {
			p, fc := p.withFlash("clipboard error: " + err.Error())
			return p, fc
		}
		p, fc := p.withFlash("path copied to clipboard")
		return p, fc
	case "existing":
		p.action = nil
		if p.store == nil {
			p, fc := p.withFlash("profile store not wired")
			return p, fc
		}
		profiles, err := p.store.List()
		if err != nil {
			p, fc := p.withFlash("load profiles: " + err.Error())
			return p, fc
		}
		if len(profiles) == 0 {
			p, fc := p.withFlash("no existing profiles to update")
			return p, fc
		}
		picker := components.NewProfilePicker(profiles)
		p.profilePicker = &picker
		p.profilePickerTargetPath = path
		return p, nil
	case "delete":
		p.action = nil
		selectedName := path
		if idx := strings.LastIndex(path, "/"); idx >= 0 {
			selectedName = path[idx+1:]
		}
		p.deleteConfirm = components.NewConfirm(
			"Delete "+selectedName+" from disk?",
			path,
			func(payload any) tea.Cmd {
				ppath, _ := payload.(string)
				return func() tea.Msg { return modelDeleteConfirmedMsg{path: ppath} }
			},
			"Delete",
			"Cancel",
		)
		return p, p.deleteConfirm.Init()
	}
	p.action = nil
	return p, nil
}

func (p ModelsPage) performDelete(path string) (tea.Model, tea.Cmd) {
	if err := fileRemover(path); err != nil {
		p, fc := p.withFlash("delete failed: " + err.Error())
		return p, fc
	}
	filtered := p.files[:0]
	for _, f := range p.files {
		if f.Path != path {
			filtered = append(filtered, f)
		}
	}
	p.files = filtered
	p.refreshRows()
	p, fc := p.withFlash("deleted " + path)
	return p, fc
}

func (p ModelsPage) handleScanEvent(evt domain.ScanEvent) (tea.Model, tea.Cmd) {
	switch evt.Type {
	case domain.ScanEventFile:
		if evt.File != nil {
			p.files = append(p.files, *evt.File)
			p.refreshRows()
		}
	case domain.ScanEventProgress:
		st := p.statusMap[evt.Root]
		st.count = evt.Count
		st.state = "scanned"
		p.statusMap[evt.Root] = st
	case domain.ScanEventError:
		st := p.statusMap[evt.Root]
		st.state = "error"
		if evt.Error != nil {
			st.err = evt.Error.Error()
		}
		p.statusMap[evt.Root] = st
	case domain.ScanEventDone:
		// Channel will close right after; nothing to do.
	}
	return p, nil
}

func (p ModelsPage) visibleFiles() []domain.ModelFile {
	files := filter.ContainsFold(p.files, p.filter, func(f domain.ModelFile) string { return f.Name })
	sort.Slice(files, func(i, j int) bool { return files[i].Name < files[j].Name })
	return files
}

// refreshRows rebuilds table rows from p.files honoring the current
// filter. Sorted by name for stable display.
func (p *ModelsPage) refreshRows() {
	files := p.visibleFiles()
	rows := make([]table.Row, 0, len(files))
	for _, f := range files {
		rows = append(rows, table.Row{
			truncate(f.Name, 36),
			humanSize(f.SizeBytes),
			f.Quant,
			f.Params,
			truncate(f.Path, 40),
		})
	}
	p.table.SetRows(rows)
}

// humanSize formats bytes as "X.YG" / "X.YM".
func humanSize(n int64) string {
	switch {
	case n >= 1<<30:
		return fmt.Sprintf("%.1fG", float64(n)/float64(1<<30))
	case n >= 1<<20:
		return fmt.Sprintf("%.1fM", float64(n)/float64(1<<20))
	case n >= 1<<10:
		return fmt.Sprintf("%.1fK", float64(n)/float64(1<<10))
	default:
		return fmt.Sprintf("%dB", n)
	}
}

// handleFilterKey processes keystrokes while the filter buffer is active.
// It returns handled=true when the key is consumed by filter mode; when
// handled=false the caller falls through to the command-key dispatch
// (e.g. Enter, which acts on the current selection).
func (p ModelsPage) handleFilterKey(msg tea.KeyMsg) (handled bool, m tea.Model, cmd tea.Cmd) {
	switch {
	case key.Matches(msg, p.keys.Filter):
		p.filterMode = false
		return true, p, nil
	case key.Matches(msg, p.keys.Cancel):
		p.filterMode = false
		p.filter = ""
		p.refreshRows()
		return true, p, nil
	case key.Matches(msg, p.keys.Enter):
		// fall through to the action-menu logic in handleKey
		return false, p, nil
	default:
		if msg.String() == "backspace" {
			if len(p.filter) > 0 {
				p.filter = p.filter[:len(p.filter)-1]
				p.refreshRows()
			}
			return true, p, nil
		}
		if len(msg.Runes) == 1 {
			p.filter += string(msg.Runes)
			p.refreshRows()
			return true, p, nil
		}
		return true, p, nil
	}
}

func (p ModelsPage) handleKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	// While typing in the filter buffer, route printable runes to the filter.
	// Only Filter (toggle off), Cancel (clear), and Enter (act on selection)
	// remain active — page shortcuts like Rescan must NOT fire from rune keys.
	if p.filterMode {
		if handled, m, cmd := p.handleFilterKey(msg); handled {
			return m, cmd
		}
	}
	if p.hfSearch != nil && p.hfSearch.IsActive() {
		if key.Matches(msg, p.keys.Enter) {
			item, ok := p.hfSearch.Selected()
			if !ok {
				return p, nil
			}
			return p.openHFFilePicker(item)
		}
		return p, p.hfSearch.Update(msg)
	}
	if p.hfFilePicker != nil && p.hfFilePicker.IsActive() {
		if key.Matches(msg, p.keys.Enter) {
			return p.startSelectedDownloads()
		}
		return p, p.hfFilePicker.Update(msg)
	}
	if p.downloads != nil && p.downloads.IsFocusVisible() {
		return p, p.downloads.Update(msg)
	}

	switch {
	case key.Matches(msg, p.keys.Filter):
		p.filterMode = !p.filterMode
		return p, nil
	case key.Matches(msg, p.keys.Cancel):
		if p.filterMode || p.filter != "" {
			p.filterMode = false
			p.filter = ""
			p.refreshRows()
			return p, nil
		}
	case key.Matches(msg, p.keys.Rescan):
		if p.isScanning() {
			return p.withFlash("Scan already in progress")
		}
		next, cmd := p.beginRescan(true)
		return next, cmd
	case key.Matches(msg, p.keys.Enter):
		return p.openActionMenuForSelection()
	case msg.String() == "s":
		if !p.filterMode && p.hfClient != nil {
			p.hfSearch = components.NewHFSearchPicker(hfSearcherAdapter{client: p.hfClient}, p.width, p.height)
			return p, p.hfSearch.Init()
		}
	case msg.String() == "x":
		if p.downloads != nil && p.downloads.IsVisible() {
			return p, p.downloads.Update(msg)
		}
	}

	t, cmd := p.table.Update(msg)
	p.table = t
	return p, cmd
}

// openActionMenuForSelection builds the per-row action menu for the
// currently selected model file, returning the page unchanged when the
// table is empty or the cursor is out of range.
func (p ModelsPage) openActionMenuForSelection() (tea.Model, tea.Cmd) {
	if len(p.table.Rows()) == 0 {
		return p, nil
	}
	idx := p.table.Cursor()
	if idx < 0 {
		return p, nil
	}
	// Map row idx to file via filtered ordering. Recompute filtered
	// list to match what's displayed.
	visible := p.visibleFiles()
	if idx >= len(visible) {
		return p, nil
	}
	selected := visible[idx]
	opts := []actionOption{
		{label: "Use in new profile", value: "new"},
	}
	if p.store != nil {
		opts = append(opts, actionOption{label: "Use in existing profile", value: "existing"})
	}
	opts = append(opts, actionOption{label: "Copy path to clipboard", value: "reveal"})
	opts = append(opts, actionOption{label: "Delete file", value: "delete"})
	p.action = &actionMenu{
		title:      "Action for " + selected.Name,
		options:    opts,
		targetPath: selected.Path,
		stage:      actionStageRoot,
	}
	return p, nil
}

func (p ModelsPage) View() string {
	if p.profilePicker != nil {
		return p.profilePicker.View()
	}
	if p.deleteConfirm.Active() {
		return p.deleteConfirm.View()
	}
	if p.action != nil {
		return p.renderActionMenu()
	}
	header := theme.Title.Render("Models")
	statusLine := p.renderStatus()
	filterLine := ""
	if p.filterMode || p.filter != "" {
		filterLine = theme.Subtitle.Render(fmt.Sprintf("filter: %q", p.filter))
	}
	footer := p.flash.View()
	var content string
	if len(p.files) == 0 && (len(p.paths) == 0 || p.hasScannedRoot()) {
		emptyMsg := theme.Subtitle.Render("(no .gguf files in configured search paths — edit ~/.config/model-loader/config.toml or press [R] to rescan)")
		content = lipgloss.JoinVertical(lipgloss.Left, header, statusLine, emptyMsg, filterLine, footer)
	} else {
		content = lipgloss.JoinVertical(lipgloss.Left, header, statusLine, p.table.View(), filterLine, footer)
	}
	if p.hfSearch != nil && p.hfSearch.IsActive() {
		content = p.hfSearch.View()
	}
	if p.hfFilePicker != nil && p.hfFilePicker.IsActive() {
		content = p.hfFilePicker.View()
	}
	if p.downloads != nil && p.downloads.IsVisible() {
		progressView := p.downloads.View()
		if progressView != "" {
			content = content + "\n" + progressView
		}
	}
	return content
}

// isScanning reports whether any configured root is still being scanned.
// Used to guard duplicate manual rescans.
func (p ModelsPage) isScanning() bool {
	for _, st := range p.statusMap {
		if st.state == "scanning" {
			return true
		}
	}
	return false
}

// hasScannedRoot reports whether at least one configured root has finished
// its initial scan. Empty-state copy only renders once we know the scan is
// complete — otherwise the user might think their models are missing
// while the scanner is still walking the filesystem.
func (p ModelsPage) hasScannedRoot() bool {
	for _, st := range p.statusMap {
		if st.state == "scanned" {
			return true
		}
	}
	return false
}

// Hints implements ui.HintProvider for the Models tab.
func (p ModelsPage) Hints() string {
	if p.profilePicker != nil {
		return "[↑↓] move  [enter] select  [esc] cancel"
	}
	if p.deleteConfirm.Active() {
		return "[enter] confirm  [esc] cancel"
	}
	if p.action != nil {
		return "[↑↓] move  [enter] select  [esc] cancel"
	}
	if p.filterMode {
		return "[type] filter  [esc] clear"
	}
	hints := "[/] filter  [R] rescan  [s] search HF  [enter] actions  [esc] clear"
	if p.downloads != nil && p.downloads.IsVisible() {
		hints += "  [x] cancel dl"
	}
	return hints
}

// renderActionMenu draws the inline modal-ish overlay used for both the
// root action menu and the follow-up profile picker.
func (p ModelsPage) renderActionMenu() string {
	lines := []string{theme.Title.Render(p.action.title)}
	for i, opt := range p.action.options {
		prefix := "  "
		label := opt.label
		if i == p.action.cursor {
			prefix = "> "
			label = theme.OK.Render(label)
		}
		lines = append(lines, prefix+label)
	}
	lines = append(lines, "", theme.Subtitle.Render("[↑/↓] move  [enter] select  [esc] cancel"))
	return lipgloss.JoinVertical(lipgloss.Left, lines...)
}

func (p ModelsPage) renderStatus() string {
	if len(p.paths) == 0 {
		return theme.Subtitle.Render("no search paths configured")
	}
	parts := make([]string, 0, len(p.paths))
	for _, root := range p.paths {
		display := truncFront(root, 40)
		st := p.statusMap[root]
		var label string
		switch st.state {
		case "scanning":
			label = theme.Subtitle.Render(fmt.Sprintf("%s [scanning]", display))
		case "scanned":
			label = theme.OK.Render(fmt.Sprintf("%s [%d]", display, st.count))
		case "error":
			label = theme.Error.Render(fmt.Sprintf("%s [error: %s]", display, truncate(st.err, 30)))
		default:
			label = theme.Subtitle.Render(display)
		}
		parts = append(parts, label)
	}
	// If the joined width fits, single-line — otherwise wrap one label per
	// line so labels remain legible on narrow terminals.
	const sep = "  "
	width := 0
	for i, lbl := range parts {
		width += lipgloss.Width(lbl)
		if i > 0 {
			width += len(sep)
		}
	}
	if p.width > 0 && width > p.width-2 {
		return strings.Join(parts, "\n")
	}
	return strings.Join(parts, sep)
}

// truncFront returns s with leading characters replaced by "…" when its
// length exceeds n. Preserves the tail because that's the discriminator
// for similar-looking root paths.
func truncFront(s string, n int) string {
	if n <= 1 || len(s) <= n {
		return s
	}
	return "…" + s[len(s)-(n-1):]
}

type modelDeleteConfirmedMsg struct{ path string }

// UseInNewProfileMsg requests creating a new profile pre-filled with Path.
// Root catches this message, switches to the Profiles tab, and forwards
// it so ProfilesPage starts a new draft.
type UseInNewProfileMsg struct {
	Path string
}
