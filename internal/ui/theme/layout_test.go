package theme

import "testing"

func TestBodyHeight(t *testing.T) {
	if h := BodyHeight(24); h != 22 {
		t.Errorf("BodyHeight(24) = %d, want 22", h)
	}
	if h := BodyHeight(2); h != 0 {
		t.Errorf("BodyHeight(2) = %d, want 0", h)
	}
	if h := BodyHeight(0); h != 0 {
		t.Errorf("BodyHeight(0) = %d, want 0", h)
	}
	if h := BodyHeight(1); h != 0 {
		t.Errorf("BodyHeight(1) = %d, want 0", h)
	}
}

func TestSplitTwoPanes(t *testing.T) {
	l, r := SplitTwoPanes(100)
	if l != 49 || r != 49 {
		t.Errorf("SplitTwoPanes(100) = (%d, %d), want (49, 49)", l, r)
	}

	l, r = SplitTwoPanes(30)
	if l != 20 || r != 20 {
		t.Errorf("SplitTwoPanes(30) = (%d, %d), want (20, 20)", l, r)
	}

	l, r = SplitTwoPanes(0)
	if l != 20 || r != 20 {
		t.Errorf("SplitTwoPanes(0) = (%d, %d), want (20, 20)", l, r)
	}
}

func TestResponsiveSplit(t *testing.T) {
	tests := []struct {
		name      string
		width     int
		wantMode  LayoutMode
		wantLeft  int
		wantRight int
		labelsMin int
	}{
		{"60col stacked", 60, LayoutStacked, 58, 58, MinPaneWidth},
		{"80col stacked", 80, LayoutStacked, 78, 78, MinPaneWidth},
		{"120col 60_40", 120, LayoutSplit6040, 70, 48, MinPaneWidth},
		{"160col 60_40 boundary", 160, LayoutSplit6040, 94, 64, MinPaneWidth},
		{"200col 50_50", 200, LayoutSplit5050, 99, 99, MinPaneWidth},
		{"0col stacked min", 0, LayoutStacked, MinPaneWidth, MinPaneWidth, MinPaneWidth},
		{"narrow tiny min", 25, LayoutStacked, 23, 23, MinPaneWidth},
		{"narrow below min", 18, LayoutStacked, MinPaneWidth, MinPaneWidth, MinPaneWidth},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mode, l, r := ResponsiveSplit(tt.width)
			if mode != tt.wantMode {
				t.Errorf("ResponsiveSplit(%d) mode = %v, want %v", tt.width, mode, tt.wantMode)
			}
			if l != tt.wantLeft {
				t.Errorf("ResponsiveSplit(%d) leftWidth = %d, want %d", tt.width, l, tt.wantLeft)
			}
			if r != tt.wantRight {
				t.Errorf("ResponsiveSplit(%d) rightWidth = %d, want %d", tt.width, r, tt.wantRight)
			}
			if l < tt.labelsMin || r < tt.labelsMin {
				t.Errorf("ResponsiveSplit(%d) pane widths (%d,%d) below min %d", tt.width, l, r, tt.labelsMin)
			}
		})
	}
}
