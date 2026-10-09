// APHELION EDIT ADDITION START - MERIDIAN THEME
package pmap

import (
	"fmt"
	"strings"

	"github.com/SpaiR/imgui-go"

	"sdmm/internal/app/ui/cpwsarea/wsmap/tools"
	"sdmm/internal/app/ui/uikit"
	"sdmm/internal/imguiext/style"
	w "sdmm/internal/imguiext/widget"
	"sdmm/internal/platform"
)

// statusSegment is one slot of the map status bar.
type statusSegment struct {
	text  string
	mono  bool       // paths, coordinates and sizes
	color imgui.Vec4 // zero: secondary text
	main  bool       // the action; full text colour
}

// statusSegments splits the tool context into the status bar's slots, in
// order: coordinates, state, action, target, footprint, scope, lint.
func statusSegments(context tools.ActionContext, coords string) []statusSegment {
	segments := []statusSegment{{text: coords, mono: true}}
	switch {
	case !context.Available:
		reason := context.Reason
		if reason == "" {
			reason = "action unavailable"
		}
		segments = append(segments, statusSegment{text: "Unavailable", color: style.ColorRed}, statusSegment{text: reason})
	case context.Captured:
		segments = append(segments, statusSegment{text: "Gesture", color: style.ColorGold})
	}
	action := context.Action
	if context.Cue == tools.CueHideExactType {
		action = "Hide exact type"
	} else if context.Badge != "" && context.Badge != context.Action {
		action = context.Badge + " · " + context.Action
	}
	if action == "" {
		action = "Ready"
	}
	var modifiers []string
	if context.Modifiers.Ctrl {
		modifiers = append(modifiers, "Ctrl")
	}
	if context.Modifiers.Alt {
		modifiers = append(modifiers, "Alt")
	}
	if context.Modifiers.Shift {
		modifiers = append(modifiers, "Shift")
	}
	if len(modifiers) > 0 {
		action += "  [" + strings.Join(modifiers, "+") + "]"
	}
	if context.Available {
		segments = append(segments, statusSegment{text: action, main: true})
	}
	if context.Target != "" {
		segments = append(segments, statusSegment{text: context.Target, mono: true})
	}
	if context.HasFootprint {
		width := int(context.Footprint.X2-context.Footprint.X1) + 1
		height := int(context.Footprint.Y2-context.Footprint.Y1) + 1
		if width > 1 || height > 1 {
			segments = append(segments, statusSegment{text: fmt.Sprintf("%d×%d", width, height), mono: true})
		}
	}
	if context.Scope != "" {
		segments = append(segments, statusSegment{text: context.Scope})
	}
	if context.LintWarning != "" {
		segments = append(segments, statusSegment{text: "Lint warning", color: style.ColorGold})
	}
	return segments
}

// showStatusSegments draws the status bar: segments separated by hairlines,
// the target path middle-truncated to fit, and the level controls on the
// right. Hovering shows the tool context; right-click copies the target.
func (p *PaneMap) showStatusSegments(context tools.ActionContext) {
	coords := "out of bounds"
	if !p.canvasState.HoverOutOfBounds() {
		t := p.canvasState.HoveredTile()
		coords = fmt.Sprintf("X:%03d Y:%03d", t.X, t.Y)
	}
	segments := statusSegments(context, coords)
	reserved := float32(0)
	if p.dmm.MaxZ != 1 {
		reserved = imgui.FontSize() * 7
	}
	spacing := imgui.CurrentStyle().ItemSpacing().X
	line := imgui.PackedColorFromVec4(imgui.CurrentStyle().Color(imgui.StyleColorSeparator))

	imgui.BeginGroup()
	imgui.AlignTextToFramePadding()
	for i, seg := range segments {
		if i > 0 {
			imgui.SameLine()
			pos := imgui.CursorScreenPos()
			imgui.WindowDrawList().AddLine(imgui.Vec2{X: pos.X, Y: pos.Y + 3}, imgui.Vec2{X: pos.X, Y: pos.Y + imgui.FrameHeight() - 3}, line)
			imgui.Dummy(imgui.Vec2{X: 1, Y: imgui.FrameHeight()})
			imgui.SameLine()
		}
		available := imgui.ContentRegionAvail().X - reserved - spacing
		if available < imgui.FontSize()*2 {
			break // no room for further segments
		}
		draw := func() {
			measure := func(s string) float32 { return imgui.CalcTextSize(s, false, 0).X }
			text := seg.text
			if seg.mono && strings.HasPrefix(text, "/") {
				text = uikit.MiddleTruncate(text, available, measure)
			} else {
				text = uikit.EndTruncate(text, available, measure)
			}
			switch {
			case seg.color != (imgui.Vec4{}):
				imgui.TextColored(seg.color, text)
			case seg.main:
				imgui.Text(text)
			default:
				imgui.TextDisabled(text)
			}
		}
		if seg.mono {
			uikit.Mono(draw)
		} else {
			draw()
		}
	}
	imgui.EndGroup()
	if imgui.IsItemHovered() {
		imgui.BeginTooltip()
		toolContextTooltip(context).Build()
		imgui.EndTooltip()
	}
	if context.Target != "" && imgui.BeginPopupContextItemV("copy-tool-target-path", imgui.PopupFlagsMouseButtonRight) {
		if imgui.MenuItem("Copy full target path") {
			platform.SetClipboard(context.Target)
		}
		imgui.EndPopup()
	}
	if p.dmm.MaxZ != 1 {
		imgui.SameLine()
		w.Layout{p.panelStatusLayoutLevels()}.BuildV(w.AlignRight)
	}
}

// APHELION EDIT ADDITION END
