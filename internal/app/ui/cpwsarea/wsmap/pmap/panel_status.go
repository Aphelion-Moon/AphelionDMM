package pmap

import (
	"fmt"
	// APHELION EDIT ADDITION START - SHARED TOOL STATUS
	"strings"

	"github.com/SpaiR/imgui-go"
	// APHELION EDIT ADDITION END

	"sdmm/internal/app/ui/cpwsarea/wsmap/tools"
	"sdmm/internal/app/ui/shortcut"
	"sdmm/internal/imguiext/icon"
	w "sdmm/internal/imguiext/widget"
	"sdmm/internal/platform"
)

// APHELION EDIT ADDITION START - SHARED TOOL STATUS
/* APHELION EDIT REMOVAL START - SHARED TOOL STATUS
func (p *PaneMap) showStatusPanel() {
	w.Layout{
		p.panelStatusLayoutStatus(),
		w.SameLine(),
		w.Custom(func() {
			if p.dmm.MaxZ != 1 {
				w.Layout{
					p.panelStatusLayoutLevels(),
				}.BuildV(w.AlignRight)
			}
		}),
	}.Build()
}

func (p *PaneMap) panelStatusLayoutStatus() (layout w.Layout) {
	if p.canvasState.HoverOutOfBounds() {
		layout = append(layout, w.TextFrame("out of bounds"))
	} else {
		t := p.canvasState.HoveredTile()
		layout = append(layout, w.TextFrame(fmt.Sprintf("X:%03d Y:%03d", t.X, t.Y)))
	}
	layout = append(layout, w.Tooltip(w.Text("Tile coordinates of the mouse")))
	if isQuickToolToggled() && !tools.Selected().AltBehaviour() {
		if hoveredInstance := p.canvasState.HoveredInstance(); hoveredInstance != nil {
			layout = append(layout, w.TextFrame(hoveredInstance.Prefab().Path()))
		}
	} else if tool, ok := tools.Selected().(*tools.ToolGrab); ok && tool.HasSelectedArea() {
		bounds := tool.Bounds()
		layout = append(layout,
			w.TextFrame(fmt.Sprintf("W:%d H:%d", int(bounds.X2-bounds.X1)+1, int(bounds.Y2-bounds.Y1)+1)),
			w.Tooltip(w.Text("Grab area size")),
			w.TextFrame(bounds.String()),
			w.Tooltip(w.Text("Grab area bounds")),
		)
	}
	return w.Layout{w.Line(layout...)}
}

func isQuickToolToggled() bool {
	return tools.IsSelected(tools.TNPick) || tools.IsSelected(tools.TNDelete) || tools.IsSelected(tools.TNReplace)
}
APHELION EDIT REMOVAL END */

func statusToolSummary(context tools.ActionContext) string {
	label := context.Action
	if context.Cue == tools.CueHideExactType {
		label = "Hide exact type"
	} else if context.Badge != "" && context.Badge != context.Action {
		label = context.Badge + " · " + context.Action
	}
	if label == "" {
		label = "Ready"
	}
	if !context.Available {
		reason := context.Reason
		if reason == "" {
			reason = "action unavailable"
		}
		label = "Unavailable · " + reason
	} else if context.Captured {
		label = "Gesture · " + label
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
		label += " [" + strings.Join(modifiers, "+") + "]"
	}
	if context.Target != "" {
		label += " · " + shortTargetPath(context.Target, 42)
	}
	if context.HasFootprint {
		width := int(context.Footprint.X2-context.Footprint.X1) + 1
		height := int(context.Footprint.Y2-context.Footprint.Y1) + 1
		if width > 1 || height > 1 {
			label += fmt.Sprintf(" · %dx%d", width, height)
		}
	}
	if context.Scope != "" {
		label += " · " + context.Scope
	}
	return label
}

func actionContextNotice(context tools.ActionContext) (string, string) {
	if !context.Available {
		if context.Reason == "" {
			return "Unavailable", "Action is unavailable"
		}
		return "Unavailable", context.Reason
	}
	if context.Reason != "" {
		return "Details", context.Reason
	}
	return "", ""
}

func shortTargetPath(full string, maxLength int) string {
	if len(full) <= maxLength || maxLength < 8 {
		return full
	}
	return "…" + full[len(full)-maxLength+1:]
}

func toolContextTooltip(context tools.ActionContext) w.Layout {
	return w.Layout{w.Custom(func() {
		if context.IsHeld {
			imgui.Text("Held tool: " + context.HeldTool)
		} else if context.PersistentTool != "" {
			imgui.Text("Persistent tool: " + context.PersistentTool)
		}
		if context.GestureTool != "" {
			imgui.Text("Gesture owner: " + context.GestureTool)
		}
		imgui.Text("Action: " + context.Action)
		if context.ModifierHelp != "" {
			imgui.Text("Modifiers: " + context.ModifierHelp)
		}
		if context.Scope != "" {
			imgui.Text("Scope: " + context.Scope)
		}
		if context.Target != "" {
			imgui.Text("Target: " + context.Target)
			imgui.Text("Right-click the status line to copy the full target path")
		}
		if heading, message := actionContextNotice(context); heading != "" {
			imgui.Text(heading + ": " + message)
		}
		if context.Cue == tools.CueHideExactType {
			imgui.Text("Non-destructive: this hides the exact type throughout the local filter")
		}
		if context.AreaQuery {
			imgui.Text("Area query: candidate membership is separate from the retained selection")
		}
	})}
}

func (p *PaneMap) showStatusPanel() {
	context := tools.CurrentActionContext()
	status := statusToolSummary(context)
	if p.canvasState.HoverOutOfBounds() {
		status = "out of bounds · " + status
	} else {
		point := p.canvasState.HoveredTile()
		status = fmt.Sprintf("X:%03d Y:%03d · %s", point.X, point.Y, status)
	}
	reserved := float32(95)
	if p.dmm.MaxZ == 1 {
		reserved = 12
	}
	status = truncateToWidth(status, max(float32(24), imgui.ContentRegionAvail().X-reserved))
	w.Layout{w.TextFrame(status), w.Tooltip(toolContextTooltip(context))}.Build()
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

func (p *PaneMap) panelStatusLayoutLevels() (layout w.Layout) {
	return w.Layout{
		w.TextFrame(fmt.Sprintf("Z:%d", p.activeLevel)),
		w.Tooltip(w.Text("Current Z-level")).OnHover(true),
		w.SameLine(),
		w.Disabled(!p.hasPreviousLevel(), w.Layout{
			w.Button(icon.ArrowDownward, p.doPreviousLevel).
				// APHELION EDIT CHANGE - RESOLVED LEVEL SHORTCUT - ORIGINAL: .Tooltip(fmt.Sprintf("Previous z-level (%s)", shortcut.Combine(platform.KeyModName(), "Down"))).
				Tooltip(fmt.Sprintf("Previous z-level (%s)", shortcut.Label("pmap#doPreviousLevel"))).
				Round(true),
		}),
		w.SameLine(),
		w.Disabled(!p.hasNextLevel(), w.Layout{
			w.Button(icon.ArrowUpward, p.doNextLevel).
				// APHELION EDIT CHANGE - RESOLVED LEVEL SHORTCUT - ORIGINAL: .Tooltip(fmt.Sprintf("Next z-level (%s)", shortcut.Combine(platform.KeyModName(), "Up"))).
				Tooltip(fmt.Sprintf("Next z-level (%s)", shortcut.Label("pmap#doNextLevel"))).
				Round(true),
		}),
	}
}
