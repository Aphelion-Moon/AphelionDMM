package pmap

import (
	"fmt"
	"github.com/SpaiR/imgui-go"
	"runtime"
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
