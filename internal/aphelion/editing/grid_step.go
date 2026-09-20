package editing

import "sdmm/internal/aphelion/collab/model"

// A selection must remain inside a map, so a full maximum map width cannot
// be a usable move step. Invalid or absent saved preferences retain the default.
const MaxSelectionMoveStep = model.MaxMapDimension - 1

func NormalizeSelectionMoveStep(step int) int {
	if step < 1 || step > MaxSelectionMoveStep {
		return 1
	}
	return step
}
