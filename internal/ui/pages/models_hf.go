package pages

import (
	tea "github.com/charmbracelet/bubbletea"

	"github.com/quantmind-br/model-loader/internal/ui/components"
)

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

func (msg hfSearchResultMsg) componentMsg() components.HFSearchResultMsg {
	return components.HFSearchResultMsg(msg)
}

func (msg hfFileListMsg) componentMsg() components.HFFileListMsg {
	return components.HFFileListMsg(msg)
}

func (p ModelsPage) openHFFilePicker(item components.ResultItem) (tea.Model, tea.Cmd) {
	if p.hfClient == nil {
		return p.withFlashError("HF client not wired")
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
