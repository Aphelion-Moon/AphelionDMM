package pmap

import "github.com/SpaiR/imgui-go"

const (
	panelPadding  float32 = 5
	panelAlpha    float32 = .75
	panelRounding float32 = 1

	panelFlags = imgui.WindowFlagsNoResize | imgui.WindowFlagsAlwaysAutoResize |
		imgui.WindowFlagsNoTitleBar | imgui.WindowFlagsNoMove |
		imgui.WindowFlagsNoSavedSettings | imgui.WindowFlagsNoDocking | imgui.WindowFlagsNoFocusOnAppearing
)

type panelPos int

const (
	pPosTop panelPos = iota
	pPosRightTop
	pPosRightBottom
	pPosBottom
)

func (p *PaneMap) showPanel(id string, panelPos panelPos, content func()) {
	p.showPanelV(id, panelPos, true, content)
}

func (p *PaneMap) showPanelV(id string, panelPos panelPos, visible bool, content func()) {
	if !visible {
		return
	}

	var pos, size imgui.Vec2

	switch panelPos {
	case pPosTop:
		pos = p.pos.Plus(imgui.Vec2{X: panelPadding, Y: panelPadding})
		size = imgui.Vec2{X: p.size.X - panelPadding*2}
	case pPosRightTop:
		// APHELION EDIT ADDITION START - STABLE TOOLBAR
		// A fixed, pane-bounded width avoids a first-frame auto-size/pivot delay.
		size.X = min(p.size.X-panelPadding*2, 24*imgui.FontSize())
		// APHELION EDIT ADDITION END
		// APHELION EDIT CHANGE - STABLE TOOLBAR - ORIGINAL: x := imgui.ContentRegionAvail().X - p.panelRightTopSize.X - panelPadding
		x := p.size.X - size.X - panelPadding
		// APHELION EDIT CHANGE - STABLE TOOLBAR - ORIGINAL: y := p.panelBottomSize.Y + panelPadding*2
		y := p.panelTopSize.Y + panelPadding*2
		pos = p.pos.Plus(imgui.Vec2{X: x, Y: y})
	case pPosRightBottom:
		x := imgui.ContentRegionAvail().X - p.panelRightBottomSize.X - panelPadding
		y := imgui.ContentRegionAvail().Y - p.panelRightBottomSize.Y - p.panelBottomSize.Y - panelPadding*2
		pos = p.pos.Plus(imgui.Vec2{X: x, Y: y})
	case pPosBottom:
		y := imgui.ContentRegionAvail().Y - p.panelBottomSize.Y - panelPadding
		pos = p.pos.Plus(imgui.Vec2{X: panelPadding, Y: y})
		size = imgui.Vec2{X: p.size.X - panelPadding*2}
	}

	imgui.SetNextWindowPos(pos)
	imgui.SetNextWindowSize(size)
	imgui.SetNextWindowBgAlpha(panelAlpha)
	imgui.PushStyleVarFloat(imgui.StyleVarWindowRounding, panelRounding)

	if imgui.BeginV(id, nil, panelFlags) {
		imgui.PopStyleVar()

		p.updateShortcutsState()
		p.focused = p.focused || imgui.IsWindowFocusedV(imgui.FocusedFlagsRootAndChildWindows)

		content()

		switch panelPos {
		case pPosTop:
			p.panelTopSize = imgui.WindowSize()
			// APHELION EDIT ADDITION START - STABLE TOOLBAR
			// Auto-resize reports last frame's height until layout completes.
			// Settings need this frame's occupied content, including wrapped tools.
			p.panelTopSize.Y = imgui.CursorPosY() + imgui.CurrentStyle().WindowPadding().Y
			// APHELION EDIT ADDITION END
		case pPosRightTop:
			p.panelRightTopSize = imgui.WindowSize()
		case pPosRightBottom:
			p.panelRightBottomSize = imgui.WindowSize()
		case pPosBottom:
			p.panelBottomSize = imgui.WindowSize()
		}
	} else {
		imgui.PopStyleVar()
	}
	imgui.End()
}
