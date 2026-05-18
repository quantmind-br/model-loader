package sizing

type FitStatus int

const (
	FitGreen FitStatus = iota
	FitYellow
	FitRed
)

type SuggestResult struct {
	NGL           uint64
	Utilization   float64
	BytesPerLayer uint64
}

func Suggest(modelSizeBytes, totalLayers, freeVRAMBytes uint64) SuggestResult {
	if totalLayers == 0 {
		return SuggestResult{}
	}
	bytesPerLayer := modelSizeBytes / totalLayers
	if bytesPerLayer == 0 {
		return SuggestResult{}
	}
	maxFit := (freeVRAMBytes * 9) / 10
	maxNGL := maxFit / bytesPerLayer
	if maxNGL > totalLayers {
		maxNGL = totalLayers
	}
	utilization := 0.0
	if freeVRAMBytes > 0 {
		utilization = float64(maxNGL*bytesPerLayer) / float64(freeVRAMBytes)
	}
	return SuggestResult{
		NGL:           maxNGL,
		Utilization:   utilization,
		BytesPerLayer: bytesPerLayer,
	}
}

func Fit(modelSizeBytes, totalLayers, ngl, freeVRAMBytes uint64) FitStatus {
	if totalLayers == 0 || freeVRAMBytes == 0 {
		return FitRed
	}
	bytesPerLayer := modelSizeBytes / totalLayers
	used := ngl * bytesPerLayer
	if used > freeVRAMBytes {
		return FitRed
	}
	if used <= (freeVRAMBytes*9)/10 {
		return FitGreen
	}
	return FitYellow
}
