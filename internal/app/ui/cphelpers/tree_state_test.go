package cphelpers

import (
	"runtime"
	"testing"

	"github.com/SpaiR/imgui-go"
)

// Groups must keep their open state across frames: the tree is rebuilt every
// frame, and an ID taken from the node's address made a clicked group close
// again on the next frame (play-test: "flickers but doesn't open").
func TestHelperGroupsStayOpenAfterClick(t *testing.T) {
	p, _, _ := fixture(t)
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	ctx := imgui.CreateContext(nil)
	defer ctx.Destroy()
	io := imgui.CurrentIO()
	io.SetIniFilename("")
	io.SetDisplaySize(imgui.Vec2{X: 800, Y: 600})
	io.Fonts().TextureDataRGBA32()

	// Records the "airlock/access" group's header rectangle each frame.
	var header imgui.Vec2
	var open bool
	probe = func(key string, isOpen bool, min, max imgui.Vec2) {
		if key == "airlock/access" {
			header, open = imgui.Vec2{X: min.X + 4, Y: (min.Y + max.Y) / 2}, isOpen
		}
	}
	defer func() { probe = nil }()

	frame := func(click bool) {
		if click {
			io.SetMousePosition(header)
			io.SetMouseButtonDown(0, true)
		}
		imgui.NewFrame()
		imgui.SetNextWindowPos(imgui.Vec2{})
		imgui.SetNextWindowSize(imgui.Vec2{X: 600, Y: 500})
		imgui.Begin("Mapping Helpers")
		p.Process(0)
		imgui.End()
		imgui.EndFrame()
		if click {
			io.SetMouseButtonDown(0, false)
		}
	}
	frame(false)
	frame(false)
	if open {
		t.Fatal("access group open before any click")
	}
	frame(true)
	for i := 0; i < 4; i++ {
		frame(false)
	}
	if !open {
		t.Fatal("clicked group did not stay open")
	}

	// A new search opens matching groups once; the user can then close them.
	p.query = "role1"
	frame(false)
	if !open {
		t.Fatal("search did not open the matching group")
	}
	frame(true)
	for i := 0; i < 3; i++ {
		frame(false)
	}
	if open {
		t.Fatal("search kept forcing the group open after the user closed it")
	}
}
