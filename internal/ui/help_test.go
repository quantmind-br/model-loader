// TUI-RESP: Step 8 — help viewport sizing in small windows. Floors must
// guarantee the inner viewport fits inside the modal box even at the
// absolute minimum terminal size (20x6).

package ui

import "testing"

func TestHelpViewportSize(t *testing.T) {
	tests := []struct {
		name         string
		w, h         int
		wantW, wantH int
	}{
		{name: "wide terminal", w: 120, h: 40, wantW: 104, wantH: 32},
		{name: "minimum floor width", w: 32, h: 20, wantW: 16, wantH: 12},
		{name: "at min terminal 20x6", w: 20, h: 6, wantW: 16, wantH: 3},
		{name: "below width floor", w: 10, h: 20, wantW: 16, wantH: 12},
		{name: "below height floor", w: 120, h: 4, wantW: 104, wantH: 3},
		{name: "zero height", w: 120, h: 0, wantW: 104, wantH: 3},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gotW, gotH := helpViewportSize(tt.w, tt.h)
			if gotW != tt.wantW || gotH != tt.wantH {
				t.Errorf("helpViewportSize(%d, %d) = (%d, %d), want (%d, %d)",
					tt.w, tt.h, gotW, gotH, tt.wantW, tt.wantH)
			}
		})
	}
}
