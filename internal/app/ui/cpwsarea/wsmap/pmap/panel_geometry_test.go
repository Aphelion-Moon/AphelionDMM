package pmap

import (
	"fmt"
	"github.com/SpaiR/imgui-go"
	"runtime"
	// APHELION EDIT ADDITION START - U2 ACCEPTANCE GEOMETRY
	"sdmm/internal/aphelion/editing"
	"sdmm/internal/app/ui/cpwsarea/wsmap/pmap/canvas"
	"sdmm/internal/app/ui/cpwsarea/wsmap/pmap/editor"
	"sdmm/internal/dmapi/dmmap"
	"sdmm/internal/util"
	// APHELION EDIT ADDITION END
	"sdmm/internal/app/ui/cpwsarea/wsmap/tools"
	"testing"
)

func TestSettingsPanelTracksCurrentToolbarHeightAndRightEdge(t *testing.T) {
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	previous := tools.Selected().Name()
	tools.SetSelected(tools.TNAdd)
	defer tools.SetSelected(previous)
	for _, scale := range []float32{1, 1.5, 2} {
		for _, width := range []float32{240, 700} {
			t.Run(fmt.Sprintf("%.1f-%g", scale, width), func(t *testing.T) {
				ctx := imgui.CreateContext(nil)
				defer ctx.Destroy()
				io := imgui.CurrentIO()
				io.SetIniFilename("")
				io.SetDisplaySize(imgui.Vec2{X: 1000, Y: 900})
				io.SetFontGlobalScale(scale)
				io.Fonts().TextureDataRGBA32()
				imgui.CurrentStyle().ScaleAllSizes(scale)
				p := &PaneMap{pos: imgui.Vec2{X: 20, Y: 20}, size: imgui.Vec2{X: width, Y: 700}}
				for frame := 0; frame < 3; frame++ {
					imgui.NewFrame()
					imgui.SetNextWindowPos(imgui.Vec2{})
					imgui.SetNextWindowSize(imgui.Vec2{X: 1000, Y: 900})
					imgui.Begin("parent")
					var toolbarBottom float32
					p.showPanel("tools", pPosTop, func() { p.showToolsPanel(); toolbarBottom = imgui.CursorScreenPos().Y })
					// APHELION EDIT ADDITION START - TWO-ROW TOOLBAR CONTRACT
					maxToolbarHeight := 4*imgui.FontSize() + 2*imgui.CurrentStyle().WindowPadding().Y
					if p.panelTopSize.Y > maxToolbarHeight {
						t.Errorf("toolbar grew beyond two compact rows at font scale %.1f and pane width %g: height %g > %g", scale, width, p.panelTopSize.Y, maxToolbarHeight)
					}
					// APHELION EDIT ADDITION END
					p.showPanel("settings", pPosRightTop, func() {
						imgui.Text("Settings")
						pos, size := imgui.WindowPos(), imgui.WindowSize()
						if pos.Y < toolbarBottom {
							t.Errorf("frame %d settings overlap occupied tools: %g < %g", frame, pos.Y, toolbarBottom)
						}
						if frame > 0 && pos.X+size.X > p.pos.X+width+1 {
							t.Errorf("settings outside pane right edge")
						}
					})
					imgui.End()
					imgui.EndFrame()
				}
			})
		}
	}
}

// APHELION EDIT ADDITION START - U2 ACCEPTANCE GEOMETRY
func TestToolbarAndStatusFitAcceptedViewportsAndNarrowSplit(t *testing.T) {
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	previous := tools.Selected().Name()
	tools.SetSelected(tools.TNAdd)
	defer tools.SetSelected(previous)

	cases := []struct {
		name                 string
		width, height, scale float32
		paneWidth            float32
	}{
		{name: "1366x768-default", width: 1366, height: 768, scale: 1, paneWidth: 1200},
		{name: "1920x1080-150-percent", width: 1920, height: 1080, scale: 1.5, paneWidth: 1700},
		{name: "narrow-split-150-percent", width: 1920, height: 1080, scale: 1.5, paneWidth: 480},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			ctx := imgui.CreateContext(nil)
			defer ctx.Destroy()
			io := imgui.CurrentIO()
			io.SetIniFilename("")
			io.SetDisplaySize(imgui.Vec2{X: test.width, Y: test.height})
			io.SetFontGlobalScale(test.scale)
			io.Fonts().TextureDataRGBA32()
			imgui.CurrentStyle().ScaleAllSizes(test.scale)
			p := &PaneMap{
				pos:         imgui.Vec2{X: 20, Y: 20},
				size:        imgui.Vec2{X: test.paneWidth, Y: test.height - 48},
				dmm:         &dmmap.Dmm{MaxX: 1, MaxY: 1, MaxZ: 1},
				canvasState: canvas.NewState(1, 1, 32),
				activeLevel: 1,
				editor:      &editor.Editor{},
			}
			p.editor.WorkingSelection().Set(editing.RectangleSelection(util.Bounds{X1: 1, Y1: 1, X2: 500, Y2: 500}, 1))
			p.editor.WorkingSelection().Restrict = true
			var toolbarRight, statusRight float32
			for frame := 0; frame < 2; frame++ {
				imgui.NewFrame()
				imgui.SetNextWindowPos(imgui.Vec2{})
				imgui.SetNextWindowSize(imgui.Vec2{X: test.width, Y: test.height})
				imgui.Begin("toolbar-acceptance-parent")
				p.showPanel("u2-tools", pPosTop, func() {
					p.showToolsPanel()
					toolbarRight = imgui.ItemRectMax().X
				})
				p.showPanel("u2-status", pPosBottom, func() {
					p.showStatusPanel()
					statusRight = imgui.ItemRectMax().X
				})
				imgui.End()
				imgui.EndFrame()
			}

			maxToolbarHeight := 4*imgui.FontSize() + 2*imgui.CurrentStyle().WindowPadding().Y
			if p.panelTopSize.Y > maxToolbarHeight {
				t.Errorf("toolbar height %.1f exceeds two-row bound %.1f", p.panelTopSize.Y, maxToolbarHeight)
			}
			maxStatusHeight := 2*imgui.FontSize() + 2*imgui.CurrentStyle().WindowPadding().Y
			if p.panelBottomSize.Y > maxStatusHeight {
				t.Errorf("status strip occupies more than one line: height %.1f exceeds %.1f", p.panelBottomSize.Y, maxStatusHeight)
			}
			paneRight := p.pos.X + p.size.X - panelPadding
			if toolbarRight > paneRight+1 || statusRight > paneRight+1 {
				t.Errorf("toolbar/status controls exceed narrow pane: right %.1f/%.1f > %.1f", toolbarRight, statusRight, paneRight)
			}
			chrome := p.panelTopSize.Y + p.panelBottomSize.Y + 4*panelPadding
			if chrome > p.size.Y*0.2 {
				t.Errorf("toolbar and status leave less than 80%% pane height: chrome %.1f of %.1f", chrome, p.size.Y)
			}
			t.Logf("toolbar/status geometry: window=%dx%d scale=%.1f pane=%.1fx%.1f top=%.1f bottom=%.1f chrome=%.1f (%.1f%%) right=%.1f/%.1f limit=%.1f", int(test.width), int(test.height), test.scale, p.size.X, p.size.Y, p.panelTopSize.Y, p.panelBottomSize.Y, chrome, chrome/p.size.Y*100, toolbarRight, statusRight, paneRight)
		})
	}
}

// APHELION EDIT ADDITION END
