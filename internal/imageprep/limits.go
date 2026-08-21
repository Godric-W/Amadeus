package imageprep

import "math"

const patchSize = 32

func OutputDimensions(width, height int, limits PreparationLimits) (int, int) {
	width = max(width, 1)
	height = max(height, 1)
	if dimensionsFit(width, height, limits) {
		return width, height
	}

	scale := math.Min(float64(limits.MaxDimension)/float64(max(width, height)), 1)
	width = max(int(math.Round(float64(width)*scale)), 1)
	height = max(int(math.Round(float64(height)*scale)), 1)
	if dimensionsFit(width, height, limits) {
		return width, height
	}

	widthFloat := float64(width)
	heightFloat := float64(height)
	patchFloat := float64(patchSize)
	scale = math.Sqrt(patchFloat * patchFloat * float64(limits.MaxPatches) / widthFloat / heightFloat)
	patchesWide := widthFloat * scale / patchFloat
	patchesHigh := heightFloat * scale / patchFloat
	if patchesWide > 0 && patchesHigh > 0 {
		scale *= math.Min(math.Floor(patchesWide)/patchesWide, math.Floor(patchesHigh)/patchesHigh)
	}
	return max(int(math.Floor(widthFloat*scale)), 1), max(int(math.Floor(heightFloat*scale)), 1)
}

func dimensionsFit(width, height int, limits PreparationLimits) bool {
	patchesWide := (width + patchSize - 1) / patchSize
	patchesHigh := (height + patchSize - 1) / patchSize
	return width <= limits.MaxDimension && height <= limits.MaxDimension && patchesWide*patchesHigh <= limits.MaxPatches
}

func PatchCount(width, height int) int {
	return ((width + patchSize - 1) / patchSize) * ((height + patchSize - 1) / patchSize)
}
