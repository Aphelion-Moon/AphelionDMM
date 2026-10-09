// APHELION EDIT ADDITION START - LIGHTING PREVIEW
package pmap

import (
	"fmt"
	"image/color"
	"math"
	"time"

	"sdmm/internal/aphelion/lighting"
	"sdmm/internal/aphelion/lighting/maplight"
	"sdmm/internal/aphelion/resources"
	"sdmm/internal/dmapi/dmmap"
	"sdmm/internal/util"

	"github.com/SpaiR/imgui-go"
)

const (
	lightingMarkerLimit    = 2000
	lightingMarkerRadius   = 4
	lightingFrameBudget    = 2 * time.Millisecond
	lightingStatusPopupFmt = "Lighting skipped atoms##%p"
)

// lightingMarker is the primitive geometry for one emitter, in map pixels with
// y up. It contains no art: a point, a range ring and optional cone edges.
type lightingMarker struct {
	CX, CY       float64
	Ring         float64
	Cone         bool
	EdgeA, EdgeB [2]float64
}

func lightingMarkerFor(s lighting.Source, tile float64) lightingMarker {
	m := lightingMarker{
		CX:   (float64(s.X) + 0.5 + s.ShiftX) * tile,
		CY:   (float64(s.Y) + 0.5 + s.ShiftY) * tile,
		Ring: math.Min(s.Range, maplight.MaxRange) * tile,
	}
	angle := s.Angle
	switch s.System {
	case lighting.SystemOverlay, lighting.SystemOverlayDirectional:
		angle = 360
	case lighting.SystemOverlayBeam:
		angle = 45
	}
	if angle <= 0 || angle >= 360 {
		return m
	}
	m.Cone = true
	centre := s.Dir.Angle()
	edge := func(deg float64) [2]float64 {
		rad := deg * math.Pi / 180
		return [2]float64{m.CX + math.Sin(rad)*m.Ring, m.CY + math.Cos(rad)*m.Ring}
	}
	m.EdgeA, m.EdgeB = edge(centre-angle/2), edge(centre+angle/2)
	return m
}

func lightingStatusText(rep maplight.Report, busy bool) string {
	text := fmt.Sprintf("Lighting (approximate): %d sources, %d skipped", rep.Sources, rep.Skipped)
	if rep.Capped {
		text += fmt.Sprintf(" (capped at %d)", maplight.SourceCap)
	}
	if busy {
		text += " updating..."
	}
	return text
}

type lightingPane struct {
	ctl *maplight.Controller
}

// attachLighting wires the display-change observer of the current renderer.
func (p *PaneMap) attachLighting() {
	p.canvas.Render().SetTileObserver(func(level int, points []util.Point) {
		if p.lighting.ctl != nil {
			p.lighting.ctl.MarkDirty(level, points)
		}
	})
}

func (p *PaneMap) closeLighting() {
	if p.lighting.ctl != nil {
		p.lighting.ctl.Close()
		p.lighting.ctl = nil
	}
	p.canvas.Render().SetLighting(nil, 0)
}

// processLighting advances the preview and hands the current frame to the
// renderer. Capture work is bounded by the shared frame allowance; computation
// runs on a worker.
func (p *PaneMap) processLighting() {
	set := maplight.Current()
	r := p.canvas.Render()
	if p.lighting.ctl == nil {
		if !set.Enabled {
			r.SetLighting(nil, 0)
			return
		}
		p.lighting.ctl = maplight.NewController()
	}
	started := time.Now()
	defer resources.ChargeFrameWork(started)
	budget := resources.FrameWorkRemaining(lightingFrameBudget)
	if budget <= 0 {
		budget = time.Microsecond // always make progress, one small batch per frame
	}
	version, _ := p.editor.MapViewVersion()
	frame := p.lighting.ctl.Tick(maplight.Input{
		Dmm: p.dmm, Level: p.activeLevel, Env: p.app.LoadedEnvironment(),
		Settings: set, ViewVersion: version, Budget: budget,
	})
	if frame == nil {
		r.SetLighting(nil, 0)
		return
	}
	r.SetLighting(frame, set.Strength())
}

// showLightingOverlay draws source markers with the ImGui draw list. Markers sit
// above the lit canvas, so they are never darkened.
func (p *PaneMap) showLightingOverlay() {
	set := maplight.Current()
	if !set.Enabled || !set.ShowSources || p.lighting.ctl == nil {
		return
	}
	sources := p.lighting.ctl.Sources()
	if len(sources) == 0 {
		return
	}
	drawList := imgui.WindowDrawList()
	canvasMin, canvasMax := p.canvasControl.PosMin(), p.canvasControl.PosMax()
	drawList.PushClipRect(canvasMin, canvasMax)
	defer drawList.PopClipRect()
	camera := p.canvas.Render().Camera
	tile := float64(dmmap.WorldIconSize)
	toScreen := func(x, y float64) imgui.Vec2 {
		return imgui.Vec2{
			X: canvasMin.X + (float32(x)+camera.ShiftX)*camera.Scale,
			Y: canvasMax.Y - (float32(y)+camera.ShiftY)*camera.Scale,
		}
	}
	outline := imgui.Packed(color.RGBA{A: 230})
	drawn := 0
	for _, s := range sources {
		m := lightingMarkerFor(s, tile)
		c := toScreen(m.CX, m.CY)
		reach := float32(m.Ring) * camera.Scale
		if c.X+reach < canvasMin.X || c.X-reach > canvasMax.X || c.Y+reach < canvasMin.Y || c.Y-reach > canvasMax.Y {
			continue
		}
		if drawn++; drawn > lightingMarkerLimit {
			break
		}
		tint := func(alpha uint8) imgui.PackedColor {
			return imgui.Packed(color.RGBA{R: uint8(s.Color.R * 255), G: uint8(s.Color.G * 255), B: uint8(s.Color.B * 255), A: alpha})
		}
		drawList.AddCircleV(c, reach, tint(70), 48, 1)
		if m.Cone {
			drawList.AddLineV(c, toScreen(m.EdgeA[0], m.EdgeA[1]), tint(110), 1)
			drawList.AddLineV(c, toScreen(m.EdgeB[0], m.EdgeB[1]), tint(110), 1)
		}
		drawList.AddCircleFilledV(c, lightingMarkerRadius+1, outline, 12)
		drawList.AddCircleFilledV(c, lightingMarkerRadius, tint(255), 12)
	}
}

// showLightingStatus shows the source/skip line and, on demand, the aggregated
// list of skipped atoms.
func (p *PaneMap) showLightingStatus() {
	set := maplight.Current()
	if !set.Enabled || p.lighting.ctl == nil {
		return
	}
	rep := p.lighting.ctl.Report()
	text := lightingStatusText(rep, p.lighting.ctl.Busy())
	popup := fmt.Sprintf(lightingStatusPopupFmt, p)
	pos := imgui.Vec2{X: p.pos.X + panelPadding, Y: p.pos.Y + p.size.Y - p.panelBottomSize.Y - panelPadding*2}
	imgui.SetNextWindowPosV(pos, imgui.ConditionAlways, imgui.Vec2{Y: 1})
	imgui.SetNextWindowBgAlpha(panelAlpha)
	if imgui.BeginV(fmt.Sprintf("lighting-status-%p", p), nil, panelFlags|imgui.WindowFlagsNoNavInputs|imgui.WindowFlagsNoNavFocus) {
		imgui.Text(text)
		if imgui.IsItemHovered() {
			imgui.SetTooltip("Approximate editor-only preview of in-game lighting.\nIt is never saved, never shared, and never changes the map.\nMulti-z light, power state and directional opacity are not modelled.")
		}
		if rep.Skipped > 0 {
			imgui.SameLine()
			if imgui.SmallButton("details##lighting") {
				imgui.OpenPopup(popup)
			}
		}
		if imgui.BeginPopupV(popup, imgui.WindowFlagsNone) {
			imgui.Text("Atoms that look like lights but were not used")
			imgui.Separator()
			for _, g := range rep.Groups {
				label := "skipped"
				if g.Unparsable {
					label = "unparsable"
				}
				imgui.Text(fmt.Sprintf("%dx %s - %s (%s)", g.Count, g.Path, g.Reason, label))
			}
			imgui.EndPopup()
		}
	}
	imgui.End()
}

// APHELION EDIT ADDITION END
