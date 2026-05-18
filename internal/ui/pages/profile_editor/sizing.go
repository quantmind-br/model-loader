package profile_editor

import (
	"fmt"
	"os"
	"strconv"
	"strings"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/quantmind-br/model-loader/internal/service/sizing"
	"github.com/quantmind-br/model-loader/internal/ui/theme"
)

type SizingTab struct {
	active         bool
	modelSizeBytes uint64
	totalLayers    uint64
	freeVRAMBytes  uint64
	ngl            uint64
	onSuggest      func(uint64)
}

func newSizingTabForDraft(d Draft) SizingTab {
	var modelSizeBytes uint64
	if info, err := os.Stat(d.Model); err == nil {
		modelSizeBytes = uint64(info.Size())
	}
	var totalLayers uint64
	if ngl, err := strconv.ParseUint(d.NGL, 10, 64); err == nil {
		totalLayers = ngl
	}
	return SizingTab{
		active:         true,
		modelSizeBytes: modelSizeBytes,
		totalLayers:    totalLayers,
		ngl:            totalLayers,
	}
}

func NewSizingTab(modelSizeBytes, totalLayers, freeVRAMBytes uint64, onSuggest func(uint64)) SizingTab {
	return SizingTab{
		active:         true,
		modelSizeBytes: modelSizeBytes,
		totalLayers:    totalLayers,
		freeVRAMBytes:  freeVRAMBytes,
		onSuggest:      onSuggest,
	}
}

func (s SizingTab) Active() bool { return s.active }
func (s *SizingTab) SetActive(v bool) { s.active = v }

func (s SizingTab) Update(msg tea.Msg) (SizingTab, tea.Cmd) {
	if km, ok := msg.(tea.KeyMsg); ok {
		switch km.String() {
		case "s":
			if s.onSuggest != nil {
				res := sizing.Suggest(s.modelSizeBytes, s.totalLayers, s.freeVRAMBytes)
				s.ngl = res.NGL
				return s, func() tea.Msg { return suggestAppliedMsg{ngl: res.NGL} }
			}
		case "left":
			if s.ngl > 0 {
				s.ngl--
			}
			return s, nil
		case "right":
			if s.ngl < s.totalLayers {
				s.ngl++
			}
			return s, nil
		}
	}
	return s, nil
}

type suggestAppliedMsg struct{ ngl uint64 }

func (s SizingTab) View() string {
	if !s.active {
		return ""
	}
	var b strings.Builder
	b.WriteString(theme.Title.Render("Sizing") + "\n\n")
	b.WriteString(fmt.Sprintf("Free VRAM: %.2f GB\n\n", float64(s.freeVRAMBytes)/1e9))
	b.WriteString(fmt.Sprintf("Model size: %.2f GB\n", float64(s.modelSizeBytes)/1e9))
	b.WriteString(fmt.Sprintf("Total layers: %d\n\n", s.totalLayers))

	res := sizing.Suggest(s.modelSizeBytes, s.totalLayers, s.freeVRAMBytes)
	b.WriteString(fmt.Sprintf("Suggested NGL: %d (%.0f%% util)\n", res.NGL, res.Utilization*100))
	b.WriteString(fmt.Sprintf("Bytes per layer: %d\n\n", res.BytesPerLayer))

	b.WriteString(fmt.Sprintf("Current n-gpu-layers: %d / %d\n", s.ngl, s.totalLayers))
	status := sizing.Fit(s.modelSizeBytes, s.totalLayers, s.ngl, s.freeVRAMBytes)
	var indicator string
	switch status {
	case sizing.FitGreen:
		indicator = theme.OK.Render("● FIT")
	case sizing.FitYellow:
		indicator = theme.Warn.Render("● TIGHT")
	case sizing.FitRed:
		indicator = theme.Error.Render("● OVER")
	}
	b.WriteString(indicator + "\n\n")
	b.WriteString(theme.Subtitle.Render("Estimate — verify with first run") + "\n\n")
	b.WriteString("[←→] adjust  [s] suggest")
	return b.String()
}
