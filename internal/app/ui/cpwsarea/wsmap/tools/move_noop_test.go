package tools

import (
	"github.com/SpaiR/imgui-go"
	"github.com/go-gl/glfw/v3.3/glfw"
	"runtime"
	"sdmm/internal/app/prefs"
	"sdmm/internal/dmapi/dmmap/dmmdata/dmmprefab"
	"sdmm/internal/dmapi/dmmap/dmminstance"
	"sdmm/internal/dmapi/dmvars"
	"sdmm/internal/util"
	"testing"
)

type offsetEditor struct {
	editor
	captures, updates int
}

func (e *offsetEditor) TryBeginTileChange(...util.Point) bool { e.captures++; return true }
func (e *offsetEditor) UpdateCanvasByCoords([]util.Point)     { e.updates++ }
func (*offsetEditor) Prefs() prefs.Prefs                      { return prefs.Prefs{} }
func (*offsetEditor) ZoomLevel() float32                      { return 2 }

func TestShiftMoveUnchangedEffectiveOffsetsDoNoWork(t *testing.T) {
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	ctx := imgui.CreateContext(nil)
	defer ctx.Destroy()
	io := imgui.CurrentIO()
	io.KeyPress(int(glfw.KeyLeftShift))
	io.SetMousePosition(imgui.Vec2{X: 10.5, Y: 10.5})
	previous := ed
	defer func() { ed = previous }()
	owner := &offsetEditor{}
	ed = owner
	prefab := dmmprefab.New(dmmprefab.IdNone, "/obj/test", (&dmvars.MutableVariables{}).ToImmutable())
	i := dmminstance.New(util.Point{X: 1, Y: 1, Z: 1}, prefab)
	move := &ToolMove{instance: i, lastMouseCoords: imgui.Vec2{X: 10, Y: 10}}
	for n := 0; n < 20; n++ {
		move.process()
	}
	if owner.captures != 0 || owner.updates != 0 || i.Prefab() != prefab {
		t.Fatal("unchanged pixel drag captures or rebuilds", owner.captures, owner.updates)
	}
}
