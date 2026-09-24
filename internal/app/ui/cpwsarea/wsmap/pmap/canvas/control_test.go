package canvas

import (
	"runtime"
	"testing"

	"github.com/SpaiR/imgui-go"
)

func TestCanvasClickUsesButtonIdentifiers(t *testing.T) {
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	for button := 0; button < 5; button++ {
		ctx := imgui.CreateContext(nil)
		io := imgui.CurrentIO()
		io.SetIniFilename("")
		io.SetDisplaySize(imgui.Vec2{X: 100, Y: 100})
		io.Fonts().TextureDataRGBA32()
		io.SetMouseButtonDown(button, true)
		imgui.NewFrame()
		left, right := 0, 0
		c := &Control{active: true, onLmbClick: func() { left++ }, onRmbClick: func() { right++ }}
		c.processMouseClick()
		if c.Clicked() != (button < 3) || c.Touched() != (button < 3) {
			t.Errorf("button %d: clicked=%v touched=%v", button, c.Clicked(), c.Touched())
		}
		if (left == 1) != (button == 0) || (right == 1) != (button == 1) {
			t.Errorf("button %d dispatched left=%d right=%d", button, left, right)
		}
		imgui.EndFrame()
		ctx.Destroy()
	}
}
