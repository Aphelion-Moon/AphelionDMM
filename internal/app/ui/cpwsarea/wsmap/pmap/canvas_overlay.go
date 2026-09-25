package pmap

import (
	// APHELION EDIT ADDITION START - FLASH LIFETIME
	"slices"
	// APHELION EDIT ADDITION END
	// APHELION EDIT ADDITION START - SHARED TOOL FEEDBACK
	"sdmm/internal/aphelion/editing"
	// APHELION EDIT ADDITION END
	"sdmm/internal/app/ui/cpwsarea/wsmap/pmap/canvas"
	"sdmm/internal/app/ui/cpwsarea/wsmap/pmap/overlay"
	"sdmm/internal/app/ui/cpwsarea/wsmap/tools"
	// APHELION EDIT REMOVAL START - CACHED AREA BORDERS
	// "sdmm/internal/dmapi/dm"
	// APHELION EDIT REMOVAL END
	"sdmm/internal/dmapi/dmmap"
	"sdmm/internal/dmapi/dmmap/dmminstance"
	"sdmm/internal/util"

	"github.com/SpaiR/imgui-go"
)

const flickDurationSec = .5

func (p *PaneMap) processCanvasOverlay() {
	p.processCanvasOverlayTools()
	p.processCanvasOverlayFlick()
	p.processCanvasOverlayAreasZones()
}

func (p *PaneMap) processCanvasOverlayTools() {
	/* APHELION EDIT REMOVAL START - SHARED TOOL FEEDBACK
	if !tools.Selected().Stale() {
		return
	}
	var (
		colInstance   util.Color
		colTileFill   util.Color
		colTileBorder util.Color
	)
	switch tools.Selected().Name() {
	case tools.TNAdd:
		colTileFill = overlay.ColorToolAddTileFill
		if !tools.Selected().AltBehaviour() {
			colTileBorder = overlay.ColorToolAddTileBorder
		} else {
			colTileBorder = overlay.ColorToolAddAltTileBorder
		}
	case tools.TNFill:
		if !tools.Selected().AltBehaviour() {
			colTileFill = overlay.ColorToolFillTileFill
		} else {
			colTileFill = overlay.ColorToolFillAltTileFill
		}
	case tools.TNGrab:
		colTileBorder = overlay.ColorToolSelectTileBorder
	case tools.TNPick:
		colInstance = overlay.ColorToolPickInstance
	case tools.TNMove:
		colInstance = overlay.ColorToolPickInstance
	case tools.TNDelete:
		if !tools.Selected().AltBehaviour() {
			colInstance = overlay.ColorToolDeleteInstance
		} else {
			colTileFill = overlay.ColorToolDeleteAltTileFill
			colTileBorder = overlay.ColorToolDeleteAltTileBorder
		}
	case tools.TNReplace:
		colInstance = overlay.ColorToolReplaceInstance
	}
	if colInstance != overlay.ColorEmpty {
		p.PushUnitHighlight(p.canvasState.HoveredInstance(), colInstance)
	}
	if !p.canvasState.HoverOutOfBounds() {
		p.PushAreaHover(p.canvasState.HoveredTileBounds(), colTileFill, colTileBorder)
	}
	APHELION EDIT REMOVAL END */
	// APHELION EDIT ADDITION START - SHARED TOOL FEEDBACK
	context := tools.CurrentActionContext()
	activeTool := tools.Tools()[context.ToolName]
	if activeTool == nil || !activeTool.Stale() {
		return
	}

	var (
		colInstance   util.Color
		colTileFill   util.Color
		colTileBorder util.Color
	)

	switch context.ToolName {
	case tools.TNAdd:
		colTileFill = overlay.ColorToolAddTileFill
		if !context.Alternate {
			colTileBorder = overlay.ColorToolAddTileBorder
		} else {
			colTileBorder = overlay.ColorToolAddAltTileBorder
		}
	case tools.TNFill:
		if !context.Alternate {
			colTileFill = overlay.ColorToolFillTileFill
		} else {
			colTileFill = overlay.ColorToolFillAltTileFill
		}
		if context.Outline {
			colTileBorder = overlay.ColorToolAddTileBorder
		}
	case tools.TNGrab:
		colTileBorder = overlay.ColorToolSelectTileBorder
		if context.Cue == tools.CueMembership {
			switch context.SelectionOperation {
			case editing.SelectionAdd:
				colTileBorder = overlay.ColorToolSelectAddBorder
			case editing.SelectionSubtract:
				colTileBorder = overlay.ColorToolSelectSubtractBorder
			case editing.SelectionIntersect:
				colTileBorder = overlay.ColorToolSelectIntersectBorder
			}
		}
	case tools.TNPick:
		colInstance = overlay.ColorToolPickInstance
		if context.Cue == tools.CueHideExactType {
			colInstance = overlay.ColorToolHideTypeInstance
		}
	case tools.TNMove:
		colInstance = overlay.ColorToolPickInstance
	case tools.TNDelete:
		if !context.Alternate {
			colInstance = overlay.ColorToolDeleteInstance
		} else {
			colTileFill = overlay.ColorToolDeleteAltTileFill
			colTileBorder = overlay.ColorToolDeleteAltTileBorder
		}
	case tools.TNReplace:
		colInstance = overlay.ColorToolReplaceInstance
	}

	if colInstance != overlay.ColorEmpty {
		instance := p.canvasState.HoveredInstance()
		if instance != nil {
			p.PushUnitHighlight(instance, colInstance)
		}
	}
	if !p.canvasState.HoverOutOfBounds() {
		p.PushAreaHover(p.canvasState.HoveredTileBounds(), colTileFill, colTileBorder)
	}
	// APHELION EDIT ADDITION END
}

func (p *PaneMap) processCanvasOverlayFlick() {
	// APHELION EDIT ADDITION START - FLASH LIFETIME
	// Compact before drawing: a delayed frame may expire several entries at once.
	// DeleteFunc also clears the backing tail so old instances can be collected.
	now := imgui.Time()
	p.editor.SetFlickAreas(slices.DeleteFunc(p.editor.FlickAreas(), func(a overlay.FlickArea) bool {
		return now-a.Time >= flickDurationSec
	}))
	p.editor.SetFlickInstance(slices.DeleteFunc(p.editor.FlickInstance(), func(i overlay.FlickInstance) bool {
		return now-i.Time >= flickDurationSec
	}))
	// APHELION EDIT ADDITION END
	// APHELION EDIT CHANGE - FLASH LIFETIME - ORIGINAL: for idx, a := range p.editor.FlickAreas() {
	for _, a := range p.editor.FlickAreas() {
		delta := imgui.Time() - a.Time
		col := flickColor(overlay.ColorFlickTileFill, delta)

		if delta < flickDurationSec {
			p.PushAreaHover(a.Area, col, overlay.ColorEmpty)
		}
		/* APHELION EDIT REMOVAL START - FLASH LIFETIME
		else {
			p.editor.SetFlickAreas(append(p.editor.FlickAreas()[:idx], p.editor.FlickAreas()[idx+1:]...))
		}
		APHELION EDIT REMOVAL END */
	}

	// APHELION EDIT CHANGE - FLASH LIFETIME - ORIGINAL: for idx, i := range p.editor.FlickInstance() {
	for _, i := range p.editor.FlickInstance() {
		delta := imgui.Time() - i.Time
		col := flickColor(overlay.ColorFlickInstance, delta)

		if delta < flickDurationSec {
			p.PushUnitHighlight(i.Instance, col)
		}
		/* APHELION EDIT REMOVAL START - FLASH LIFETIME
		else {
			p.editor.SetFlickInstance(append(p.editor.FlickInstance()[:idx], p.editor.FlickInstance()[idx+1:]...))
		}
		APHELION EDIT REMOVAL END */
	}
}

func (p *PaneMap) processCanvasOverlayAreasZones() {
	// APHELION EDIT ADDITION START - CACHED AREA BORDERS
	if !AreaBordersRendering {
		return
	}
	p.canvasOverlay.SetAreaBorders(p.areaBorders.resolve(p.editor.AreaBordersGeneration(), p.activeLevel, dmmap.WorldIconSize, p.editor.AreasZones()))
	// APHELION EDIT ADDITION END
	/* APHELION EDIT REMOVAL START - CACHED AREA BORDERS
	if !AreaBordersRendering {
		return
	}

	for _, areaZone := range p.editor.AreasZones() {
		for _, areaBorder := range areaZone.Borders {
			// Ignore area zones on other z-levels
			if areaBorder.Coord.Z != p.activeLevel {
				continue
			}

			var borders []util.Bounds

			iconSize := float32(dmmap.WorldIconSize)

			x := float32(areaBorder.Coord.X-1) * iconSize
			y := float32(areaBorder.Coord.Y-1) * iconSize

			if areaBorder.Dirs&dm.DirNorth != 0 {
				borders = append(borders, util.Bounds{X1: x, Y1: y + iconSize, X2: x + iconSize, Y2: y + iconSize})
			}
			if areaBorder.Dirs&dm.DirEast != 0 {
				borders = append(borders, util.Bounds{X1: x + iconSize, Y1: y, X2: x + iconSize, Y2: y + iconSize})
			}
			if areaBorder.Dirs&dm.DirSouth != 0 {
				borders = append(borders, util.Bounds{X1: x, Y1: y, X2: x + iconSize, Y2: y})
			}
			if areaBorder.Dirs&dm.DirWest != 0 {
				borders = append(borders, util.Bounds{X1: x, Y1: y, X2: x, Y2: y + iconSize})
			}

			p.canvasOverlay.PushAreaBorder(canvas.OverlayAreaBorder{
				Borders_: borders,
				Color_:   overlay.ColorAreaBorder,
			})
		}
	}
	APHELION EDIT REMOVAL END */
}

func (p *PaneMap) PushUnitHighlight(instance *dmminstance.Instance, color util.Color) {
	if instance != nil {
		p.canvasOverlay.PushUnit(canvas.HighlightUnit{
			Id_:    instance.Id(),
			Color_: color,
		})
	}
}

func (p *PaneMap) PushAreaHover(bounds util.Bounds, fillColor, borderColor util.Color) {
	p.canvasOverlay.PushArea(canvas.OverlayArea{
		Bounds_:      bounds,
		FillColor_:   fillColor,
		BorderColor_: borderColor,
	})
}

func flickColor(col util.Color, delta float64) util.Color {
	return util.MakeColor(
		col.R(),
		col.G(),
		col.B(),
		col.A()-float32(delta/flickDurationSec),
	)
}
