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
