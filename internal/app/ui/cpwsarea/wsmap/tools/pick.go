package tools

import (
	// APHELION EDIT ADDITION START - FILTER PROFILES
	"github.com/rs/zerolog/log"
	// APHELION EDIT ADDITION END
	"sdmm/internal/util"
)

// ToolPick can be used to select a hovered object instance.
type ToolPick struct {
	tool
}

func (ToolPick) Name() string {
	return TNPick
}

func newPick() *ToolPick {
	return &ToolPick{}
}

func (ToolPick) IgnoreBounds() bool {
	return true
}

/* APHELION EDIT REMOVAL START - FILTER PROFILES
func (ToolPick) AltBehaviour() bool { return false }
func (t ToolPick) onStart(util.Point) {
	if hoveredInstance := ed.HoveredInstance(); hoveredInstance != nil {
		ed.InstanceSelect(hoveredInstance)
	}
}
APHELION EDIT REMOVAL END */
// APHELION EDIT ADDITION START - FILTER PROFILES
func (t *ToolPick) AltBehaviour() bool {
	return t.tool.AltBehaviour()
}

func (t ToolPick) onStart(util.Point) {
	// APHELION EDIT CHANGE - SHARED TOOL FEEDBACK - ORIGINAL: hoveredInstance := ed.HoveredInstance()
	hoveredInstance := t.actionContext.targetInstance
	if hoveredInstance == nil || hoveredInstance.Prefab() == nil {
		return
	}
	// APHELION EDIT CHANGE - SHARED TOOL FEEDBACK - ORIGINAL: if t.AltBehaviour() {
	if t.actionContext.Alternate {
		hider, ok := ed.(interface{ HideExactPath(string) error })
		if !ok {
			log.Error().Msg("Pick editor does not support exact type hiding")
			return
		}
		if err := hider.HideExactPath(hoveredInstance.Prefab().Path()); err != nil {
			log.Error().Err(err).Msg("Unable to hide picked type")
		}
		return
	}
	ed.InstanceSelect(hoveredInstance)
}

// APHELION EDIT ADDITION END
