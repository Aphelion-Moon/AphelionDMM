package tools

import (
	"sdmm/internal/util"
)

// ToolReplace can be used to replace the hovered instance with the selected prefab.
type ToolReplace struct {
	tool
}

func (ToolReplace) Name() string {
	return TNReplace
}

func newReplace() *ToolReplace {
	return &ToolReplace{}
}

func (ToolReplace) IgnoreBounds() bool {
	return true
}

func (ToolReplace) AltBehaviour() bool {
	return false
}

func (t ToolReplace) onStart(util.Point) {
	// APHELION EDIT CHANGE - SHARED TOOL FEEDBACK - ORIGINAL: if hoveredInstance := ed.HoveredInstance(); hoveredInstance != nil {
	if hoveredInstance := t.actionContext.targetInstance; hoveredInstance != nil {
		// APHELION EDIT CHANGE - SHARED TOOL FEEDBACK - ORIGINAL: if selectedPrefab, ok := ed.SelectedPrefab(); ok {
		if selectedPrefab := t.actionContext.prefab; selectedPrefab != nil {
			// APHELION EDIT CHANGE - COLLABORATION - ORIGINAL: hoveredInstance.SetPrefab(selectedPrefab)
			ed.InstanceReplace(hoveredInstance, selectedPrefab)
			ed.CommitOperation("Replace Instance")
		}
	}
}
