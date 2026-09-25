// APHELION EDIT ADDITION START - PERSISTENT SELECTION
package psettings

import (
	"sdmm/internal/aphelion/editing"
	"sdmm/internal/app/render/bucket/level/chunk/unit"
	"sdmm/internal/dmapi/dm"
	"sdmm/internal/util"
)

func (p *Panel) screenshotSelection() editing.Selection {
	if owner, ok := p.editor.(interface {
		WorkingSelection() *editing.WorkingSelection
	}); ok {
		return owner.WorkingSelection().Get(p.editor.ActiveLevel())
	}
	return editing.Selection{}
}

type screenshotPolicy struct {
	selection editing.Selection
	filter    dm.PathsFilter
}

func (p screenshotPolicy) includes(coord util.Point, path string) bool {
	return p.filter.IsVisiblePath(path) && (p.selection.Len() == 0 || p.selection.Contains(coord))
}
func (p screenshotPolicy) ProcessUnit(u unit.Unit) bool {
	return p.includes(u.Instance().Coord(), u.Instance().Prefab().Path())
}

// APHELION EDIT ADDITION END
