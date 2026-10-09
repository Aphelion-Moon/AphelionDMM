package cpvareditor

import (
	"runtime"
	"testing"

	"github.com/SpaiR/imgui-go"

	"sdmm/internal/app/config"
	"sdmm/internal/dmapi/dmvars"
)

type colorPickApp struct {
	App
	cfg *vareditorConfig
}

func (a *colorPickApp) ConfigFind(string) config.Config { return a.cfg }

func TestColorPickKeepsStyleAndStartsWhiteOnNull(t *testing.T) {
	pick := newColorPick("light_color", `"#FFA62B"`)
	if pick.value() != `"#FFA62B"` {
		t.Fatalf("unchanged pick = %s", pick.value())
	}
	pick.current = [3]float32{0, 0, 1}
	if pick.value() != `"#0000FF"` {
		t.Fatalf("blue pick over upper-case value = %s", pick.value())
	}
	if blank := newColorPick("color", dmvars.NullValue); blank.value() != `"#ffffff"` {
		t.Fatalf("null starts at %s, want white", blank.value())
	}
	if matrix := newColorPick("color", "list(1,0,0, 0,1,0, 0,0,1)"); matrix.value() != `"#ffffff"` {
		t.Fatalf("matrix starts at %s, want white", matrix.value())
	}
}

// The dialog renders inside the variable row's ID scope with its swatch grids
// and picker; a headless frame catches ID-stack or popup misuse.
func TestColorDialogRendersHeadless(t *testing.T) {
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	ctx := imgui.CreateContext(nil)
	defer ctx.Destroy()
	io := imgui.CurrentIO()
	io.SetIniFilename("")
	io.SetDisplaySize(imgui.Vec2{X: 1024, Y: 768})
	io.Fonts().TextureDataRGBA32()

	v := &VarEditor{app: &colorPickApp{cfg: &vareditorConfig{RecentColors: []string{`"#d1dfff"`, "null"}}}}
	frame := func(open bool) bool {
		imgui.NewFrame()
		imgui.Begin("Variables")
		if open {
			v.colorPick = newColorPick("light_color", `"#d1dfff"`)
			imgui.OpenPopup(colorPopup)
		}
		v.showColorSwatch("light_color", `"#d1dfff"`)
		visible := imgui.IsPopupOpen(colorPopup)
		imgui.End()
		imgui.EndFrame()
		return visible
	}
	frame(true)
	if !frame(false) {
		t.Fatal("colour dialog did not stay open")
	}
	if v.colorPick.varName != "light_color" {
		t.Fatalf("pick = %+v", v.colorPick)
	}
}
