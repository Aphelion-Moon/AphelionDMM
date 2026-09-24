package pmap

import (
	"github.com/SpaiR/imgui-go"
	"runtime"
	"testing"
)

func TestStatusBodyDoesNotTakeMapFocusOrPointer(t *testing.T) {
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	ctx := imgui.CreateContext(nil)
	defer ctx.Destroy()
	io := imgui.CurrentIO()
	io.SetIniFilename("")
	io.SetDisplaySize(imgui.Vec2{X: 640, Y: 480})
	io.Fonts().TextureDataRGBA32()
	p := &PaneMap{pos: imgui.Vec2{X: 20, Y: 20}, size: imgui.Vec2{X: 500, Y: 400}, panelTopSize: imgui.Vec2{Y: 30}}
	io.SetMousePosition(imgui.Vec2{X: 50, Y: 75})
	for frame := 0; frame < 3; frame++ {
		imgui.NewFrame()
		imgui.SetNextWindowPos(imgui.Vec2{})
		imgui.SetNextWindowSize(imgui.Vec2{X: 640, Y: 480})
		imgui.Begin("map owner")
		imgui.SetWindowFocus()
		p.drawEditStatus("Applying edit…", false)
		if !imgui.IsWindowFocused() {
			t.Error("status stole map focus")
		}
		if frame > 0 && !imgui.IsWindowHovered() {
			t.Error("status body intercepted map pointer")
		}
		imgui.End()
		imgui.EndFrame()
	}
}
