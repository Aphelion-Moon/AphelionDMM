// APHELION EDIT ADDITION START - EDIT STATUS
package pmap

import (
	"fmt"
	"github.com/SpaiR/imgui-go"
	"sdmm/internal/app/ui/cpwsarea/wsmap/tools"
	"time"
)

type editBubbleState struct {
	busy  bool
	since time.Time
}

func (p *PaneMap) showEditStatus() {
	message, recovery, busy := p.editor.EditStatus(tools.OwnsGesture(p.editor))
	if message == "" && p.canvas.Render().LevelLoading() {
		message = fmt.Sprintf("Preparing Z %d view…", p.activeLevel)
		if p.canvas.Render().GeometryWaiting() {
			message = "View preparation is waiting for render memory. Map data remains available."
		}
	}
	if !busy && !recovery && activePane == p && p.editor.HasPastePlacement() {
		if grab, ok := tools.Selected().(*tools.ToolGrab); ok && grab.PlacementError() != nil {
			message = grab.PlacementError().Error()
		}
	}
	if busy && !p.editBubble.busy {
		p.editBubble.since = time.Now()
	}
	p.editBubble.busy = busy
	lintReplace := false
	if message == "" {
		message, lintReplace = p.editor.PlacementLintNotice()
	}
	if message == "" {
		message = p.editor.VisibilityStatus()
	}
	if message == "" || busy && time.Since(p.editBubble.since) < 200*time.Millisecond {
		return
	}
	p.drawEditStatusV(message, recovery, lintReplace)
}

func (p *PaneMap) drawEditStatus(message string, recovery bool) {
	p.drawEditStatusV(message, recovery, false)
}

// drawEditStatusV also offers the placement-lint one-click actions.
func (p *PaneMap) drawEditStatusV(message string, recovery, lintReplace bool) {
	width := min(float32(360), max(float32(60), p.size.X-panelPadding*2))
	if p.showSettings {
		width = min(width, max(float32(60), p.size.X-p.panelRightTopSize.X-panelPadding*3))
	}
	pos := p.pos.Plus(imgui.Vec2{X: panelPadding, Y: p.panelTopSize.Y + panelPadding*2})
	imgui.SetNextWindowPos(pos)
	imgui.SetNextWindowSize(imgui.Vec2{X: width})
	imgui.SetNextWindowBgAlpha(panelAlpha)
	// The body is display-only and never occupies a mouse or keyboard hit area.
	if imgui.BeginV(fmt.Sprintf("edit-status-%p", p), nil, panelFlags|imgui.WindowFlagsNoInputs) {
		imgui.PushTextWrapPosV(imgui.CursorPosX() + max(float32(1), imgui.ContentRegionAvail().X))
		imgui.Text(message)
		imgui.PopTextWrapPos()
	}
	bottom := imgui.WindowPos().Y + imgui.WindowSize().Y
	imgui.End()
	if lintReplace && !recovery {
		imgui.SetNextWindowPos(imgui.Vec2{X: pos.X, Y: bottom + 2})
		imgui.SetNextWindowSize(imgui.Vec2{})
		imgui.PushStyleVarVec2(imgui.StyleVarWindowPadding, imgui.Vec2{})
		if imgui.BeginV(fmt.Sprintf("edit-lint-actions-%p", p), nil, panelFlags|imgui.WindowFlagsNoNavInputs|imgui.WindowFlagsNoNavFocus) {
			if imgui.Button("Replace existing") {
				p.editor.ReplaceLintConflicts()
			}
			imgui.SameLine()
			if imgui.Button("Dismiss") {
				p.editor.DismissPlacementLint()
			}
		}
		imgui.End()
		imgui.PopStyleVar()
		return
	}
	if !recovery {
		return
	}
	// Only the button window receives input; the surrounding message stays
	// click-through. Showing a fault does not focus or activate this window.
	imgui.SetNextWindowPos(imgui.Vec2{X: pos.X, Y: bottom + 2})
	imgui.SetNextWindowSize(imgui.Vec2{})
	imgui.PushStyleVarVec2(imgui.StyleVarWindowPadding, imgui.Vec2{})
	if imgui.BeginV(fmt.Sprintf("edit-recovery-action-%p", p), nil, panelFlags|imgui.WindowFlagsNoNavInputs|imgui.WindowFlagsNoNavFocus) {
		if imgui.Button("Inspect retained edit") {
			p.OpenLocalRecovery()
		}
	}
	imgui.End()
	imgui.PopStyleVar()
}

// APHELION EDIT ADDITION END
