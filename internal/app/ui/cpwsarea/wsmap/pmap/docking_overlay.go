// APHELION EDIT ADDITION START - DOCKING OVERLAY
package pmap

import (
	"image/color"

	"sdmm/internal/aphelion/docking"
	"sdmm/internal/app/ui/cpwsarea/wsmap/pmap/editor"
	"sdmm/internal/dmapi/dm"
	"sdmm/internal/dmapi/dmmap"
	"sdmm/internal/util"

	"github.com/SpaiR/imgui-go"
)

const (
	dockingOverlayWidth     = 2
	dockingHatchSpacing     = 8
	dockingHatchMaxLines    = 256
	dockingUnknownSizeLabel = "unknown size"
)

var (
	dockingStationaryColor = imgui.Packed(color.RGBA{R: 80, G: 200, B: 255, A: 255})
	dockingMobileColor     = imgui.Packed(color.RGBA{R: 255, G: 190, B: 60, A: 255})
	dockingOtherColor      = imgui.Packed(color.RGBA{R: 200, G: 200, B: 200, A: 255})
	dockingWarningColor    = imgui.Packed(color.RGBA{R: 235, G: 40, B: 40, A: 255})
	dockingTextShadow      = imgui.Packed(color.RGBA{A: 255})
)

// dockingOverlayCache holds the analysed footprints for one map view version.
// It is recomputed only when the map view generation, level or map changes.
type dockingOverlayCache struct {
	valid      bool
	dmm        *dmmap.Dmm
	editor     *editor.Editor
	generation uint64
	level      int
	entries    []docking.Entry
}

// buildDockingEntries analyses every docking port on one level. Ports are read
// through Variables.Value, which includes values inherited from the environment.
func buildDockingEntries(dmm *dmmap.Dmm, level int) []docking.Entry {
	if dmm == nil || level < 1 || level > dmm.MaxZ {
		return nil
	}
	var inputs []docking.Input
	for y := 1; y <= dmm.MaxY; y++ {
		for x := 1; x <= dmm.MaxX; x++ {
			for _, instance := range dmm.GetTile(util.Point{X: x, Y: y, Z: level}).Instances() {
				prefab := instance.Prefab()
				if !docking.IsPort(prefab.Path()) {
					continue
				}
				inputs = append(inputs, docking.Input{Path: prefab.Path(), X: x, Y: y, Var: prefab.Vars().Value})
			}
		}
	}
	if len(inputs) == 0 {
		return nil
	}
	return docking.Analyze(inputs, docking.Level{
		MaxX: dmm.MaxX,
		MaxY: dmm.MaxY,
		TurfPaths: func(x, y int) []string {
			if x < 1 || y < 1 || x > dmm.MaxX || y > dmm.MaxY {
				return nil
			}
			var paths []string
			for _, instance := range dmm.GetTile(util.Point{X: x, Y: y, Z: level}).Instances() {
				if path := instance.Prefab().Path(); dm.IsPath(path, "/turf") {
					paths = append(paths, path)
				}
			}
			return paths
		},
	})
}

func (c *dockingOverlayCache) resolve(dmm *dmmap.Dmm, ed *editor.Editor, level int) []docking.Entry {
	var generation uint64
	ready := true
	if ed != nil {
		generation, ready = ed.MapViewVersion()
	}
	same := c.valid && c.dmm == dmm && c.editor == ed && c.level == level && c.generation == generation
	if same || (c.valid && !ready && c.dmm == dmm && c.level == level) {
		return c.entries
	}
	c.valid, c.dmm, c.editor, c.generation, c.level = true, dmm, ed, generation, level
	c.entries = buildDockingEntries(dmm, level)
	return c.entries
}

func (p *PaneMap) showDockingOverlay() {
	if !docking.Enabled() || p.dmm == nil {
		return
	}
	entries := p.dockingOverlay.resolve(p.dmm, p.editor, p.activeLevel)
	if len(entries) == 0 {
		return
	}
	drawList := imgui.WindowDrawList()
	canvasMin, canvasMax := p.canvasControl.PosMin(), p.canvasControl.PosMax()
	drawList.PushClipRect(canvasMin, canvasMax)
	defer drawList.PopClipRect()
	camera := p.canvas.Render().Camera
	toScreen := func(tileX, tileY int) imgui.Vec2 { // lower-left corner of the tile
		return imgui.Vec2{
			X: canvasMin.X + (float32((tileX-1)*dmmap.WorldIconSize)+camera.ShiftX)*camera.Scale,
			Y: canvasMax.Y - (float32((tileY-1)*dmmap.WorldIconSize)+camera.ShiftY)*camera.Scale,
		}
	}
	for _, entry := range entries {
		base := dockingKindColor(entry.Kind)
		if !entry.Known {
			lo := toScreen(entry.X, entry.Y)
			text := dockingUnknownSizeLabel
			if entry.Label != "" {
				text = entry.Label + " (" + dockingUnknownSizeLabel + ")"
			}
			at := imgui.Vec2{X: lo.X + 3, Y: lo.Y - float32(dmmap.WorldIconSize)*camera.Scale + 2}
			drawList.AddText(at.Plus(imgui.Vec2{X: 1, Y: 1}), dockingTextShadow, text)
			drawList.AddText(at, dockingWarningColor, text)
			continue
		}
		lo := toScreen(entry.Rect.MinX, entry.Rect.MinY)
		hi := toScreen(entry.Rect.MaxX+1, entry.Rect.MaxY+1)
		min := imgui.Vec2{X: lo.X, Y: hi.Y}
		max := imgui.Vec2{X: hi.X, Y: lo.Y}
		outline := base
		if entry.Warnings != 0 {
			outline = dockingWarningColor
			dockingHatch(drawList, min, max, canvasMin, canvasMax)
		}
		drawList.AddRectV(min, max, outline, 0, imgui.DrawFlagsNone, dockingOverlayWidth)
		if entry.Warnings != 0 {
			// Keep the kind colour visible as an inner outline.
			drawList.AddRectV(min.Plus(imgui.Vec2{X: 3, Y: 3}), max.Minus(imgui.Vec2{X: 3, Y: 3}), base, 0, imgui.DrawFlagsNone, 1)
		}
		text := dockingEntryText(entry)
		if text != "" {
			at := min.Plus(imgui.Vec2{X: 3, Y: 2})
			drawList.AddText(at.Plus(imgui.Vec2{X: 1, Y: 1}), dockingTextShadow, text)
			textColor := base
			if entry.Warnings != 0 {
				textColor = dockingWarningColor
			}
			drawList.AddText(at, textColor, text)
		}
	}
}

func dockingKindColor(kind docking.Kind) imgui.PackedColor {
	switch kind {
	case docking.KindStationary:
		return dockingStationaryColor
	case docking.KindMobile:
		return dockingMobileColor
	}
	return dockingOtherColor
}

// dockingEntryText returns the label plus warning text for one footprint.
func dockingEntryText(entry docking.Entry) string {
	text := entry.Label
	add := func(s string) {
		if text != "" {
			text += " | "
		}
		text += s
	}
	if entry.Warnings&docking.WarnEdge != 0 {
		add("crosses map edge")
	}
	if entry.Warnings&docking.WarnOverlap != 0 {
		add("overlaps another port")
	}
	if entry.Warnings&docking.WarnNonSpace != 0 {
		add("non-space turfs")
	}
	return text
}

// dockingHatch draws diagonal hatch lines inside the rectangle, clipped to the
// canvas and bounded in count so a zoomed-in footprint stays cheap.
func dockingHatch(drawList imgui.DrawList, min, max, canvasMin, canvasMax imgui.Vec2) {
	clipMin := imgui.Vec2{X: maxf(min.X, canvasMin.X), Y: maxf(min.Y, canvasMin.Y)}
	clipMax := imgui.Vec2{X: minf(max.X, canvasMax.X), Y: minf(max.Y, canvasMax.Y)}
	if clipMin.X >= clipMax.X || clipMin.Y >= clipMax.Y {
		return
	}
	drawList.PushClipRect(clipMin, clipMax)
	defer drawList.PopClipRect()
	hatch := imgui.Packed(color.RGBA{R: 235, G: 40, B: 40, A: 110})
	height := clipMax.Y - clipMin.Y
	for i, offset := 0, clipMin.X-height; offset < clipMax.X && i < dockingHatchMaxLines; i, offset = i+1, offset+dockingHatchSpacing {
		drawList.AddLine(imgui.Vec2{X: offset, Y: clipMax.Y}, imgui.Vec2{X: offset + height, Y: clipMin.Y}, hatch)
	}
}

func maxf(a, b float32) float32 {
	if a > b {
		return a
	}
	return b
}

func minf(a, b float32) float32 {
	if a < b {
		return a
	}
	return b
}

// APHELION EDIT ADDITION END
