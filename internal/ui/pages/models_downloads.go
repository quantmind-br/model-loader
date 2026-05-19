package pages

import (
	"errors"
	"fmt"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/quantmind-br/model-loader/internal/service/downloadmgr"
	"github.com/quantmind-br/model-loader/internal/ui/components"
)

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
	case downloadmgr.StatusAbandoned:
		return "abandoned"
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

func (p ModelsPage) startSelectedDownloads() (tea.Model, tea.Cmd) {
	if p.hfFilePicker == nil {
		return p, nil
	}
	if p.dlManager == nil {
		p.hfFilePicker = nil
		p.pendingRepoID = ""
		return p.withFlashError("download manager not wired")
	}
	if len(p.paths) == 0 {
		p.hfFilePicker = nil
		p.pendingRepoID = ""
		return p.withFlashError(downloadmgr.ErrNoSearchPath.Error())
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
			p.flash, cmd = p.flash.SetError(err.Error())
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
			p.flash, cmd = p.flash.SetError(err.Error())
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
