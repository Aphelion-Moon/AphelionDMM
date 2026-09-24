package tools

import (
	"github.com/SpaiR/imgui-go"
	"github.com/go-gl/glfw/v3.3/glfw"
	"runtime"
	"sdmm/internal/dmapi/dmmap/dmmdata/dmmprefab"
	"sdmm/internal/dmapi/dmmap/dmminstance"
	"sdmm/internal/dmapi/dmvars"
	"sdmm/internal/util"
	"testing"
)

type heldAddEditor struct {
	*lifecycleEditor
	prefab *dmmprefab.Prefab
}

func (e *heldAddEditor) SelectedPrefab() (*dmmprefab.Prefab, bool) { return e.prefab, e.prefab != nil }
func (*heldAddEditor) TryBeginTileChange(...util.Point) bool       { return true }

func TestAddPlacesRotatedOwnedPrefabAndResetsOnSourceChange(t *testing.T) {
	_, base := lifecycleFixture(t)
	vars := &dmvars.MutableVariables{}
	vars.Put("dir", "2")
	source := dmmprefab.New(0, "/obj/held", vars.ToImmutable())
	owner := &heldAddEditor{base, source}
	ed = owner
	tile := base.m.Tiles[0]
	for _, path := range []string{"/area/base", "/turf/base"} {
		tile.InstancesAdd(dmmprefab.New(0, path, (&dmvars.MutableVariables{}).ToImmutable()))
	}
	add := newAdd()
	if _, ok := add.HeldPrefab(); !ok {
		t.Fatal("no held prefab")
	}
	if err := add.held.Rotate(true); err != nil {
		t.Fatal(err)
	}
	add.onMove(tile.Coord)
	placed := tile.Instances()[len(tile.Instances())-1].Prefab()
	if placed.Vars().ValueV("dir", "") != "8" || source.Vars().ValueV("dir", "") != "2" {
		t.Fatal("placement ignored rotation or changed palette")
	}
	owner.prefab = dmmprefab.New(0, "/obj/other", source.Vars())
	next, _ := add.HeldPrefab()
	if next != owner.prefab {
		t.Fatal("new palette source inherited old rotation")
	}
}

func TestHeldInstanceRotationRebasesOffsetDragAndRetainsIdentity(t *testing.T) {
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	ctx := imgui.CreateContext(nil)
	defer ctx.Destroy()
	io := imgui.CurrentIO()
	io.KeyPress(int(glfw.KeyLeftShift))
	io.SetMousePosition(imgui.Vec2{X: 17, Y: 23})
	previous := ed
	defer func() { ed = previous }()
	owner := &offsetEditor{}
	ed = owner
	vars := &dmvars.MutableVariables{}
	vars.Put("dir", "2")
	vars.Put("pixel_x", "3")
	vars.Put("pixel_y", "4")
	source := dmmprefab.New(0, "/obj/held", vars.ToImmutable())
	instance := dmminstance.New(util.Point{X: 1, Y: 1, Z: 1}, source)
	instance.SetStableID("owned-instance")
	move := &ToolMove{instance: instance}
	if err := move.rotateHeld(true); err != nil {
		t.Fatal(err)
	}
	rotated := instance.Prefab()
	captures, updates := owner.captures, owner.updates
	move.process()
	if instance.Prefab() != rotated || owner.captures != captures || owner.updates != updates {
		t.Fatal("next sample restored pre-rotation offsets")
	}
	for range 3 {
		if err := move.rotateHeld(true); err != nil {
			t.Fatal(err)
		}
	}
	if instance.Prefab() != source || instance.StableID() != "owned-instance" {
		t.Fatal("four turns changed raw values or identity")
	}
}
