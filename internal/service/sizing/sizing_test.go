package sizing

import "testing"

func TestSuggestFitsAllLayers(t *testing.T) {
	r := Suggest(8e9, 32, 24e9)
	if r.NGL != 32 {
		t.Fatalf("expected NGL 32, got %d", r.NGL)
	}
	if r.Utilization >= 0.9 {
		t.Fatalf("expected Utilization < 0.9, got %f", r.Utilization)
	}
	if r.BytesPerLayer != 250e6 {
		t.Fatalf("expected BytesPerLayer 250000000, got %d", r.BytesPerLayer)
	}
}

func TestSuggestClampsToVRAM(t *testing.T) {
	r := Suggest(70e9, 80, 24e9)
	if r.NGL >= 80 {
		t.Fatalf("expected NGL < 80, got %d", r.NGL)
	}
	if r.Utilization > 0.9 {
		t.Fatalf("expected Utilization <= 0.9, got %f", r.Utilization)
	}
}

func TestFitRedWhenOver(t *testing.T) {
	if Fit(8e9, 32, 100, 24e9) != FitRed {
		t.Fatal("expected FitRed when ngl*bytesPerLayer > freeVRAM")
	}
}

func TestFitGreenWhenUnder90(t *testing.T) {
	if Fit(8e9, 32, 10, 24e9) != FitGreen {
		t.Fatal("expected FitGreen when under 90%")
	}
}

func TestFitYellowWhenBetween90And100(t *testing.T) {
	if Fit(70e9, 80, 25, 24e9) != FitYellow {
		t.Fatal("expected FitYellow when between 90% and 100%")
	}
}
