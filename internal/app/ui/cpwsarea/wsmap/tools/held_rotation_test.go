package tools

import (
	"github.com/SpaiR/imgui-go"
	"github.com/go-gl/glfw/v3.3/glfw"
	"runtime"
	"sdmm/internal/aphelion/editing"
	"sdmm/internal/dmapi/dm"
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

type heldPaletteEditor struct {
	*heldAddEditor
	placed *dmmprefab.Prefab
}

func (e *heldPaletteEditor) FillSelectionWithFilter(_ editing.Selection, p *dmmprefab.Prefab, _ bool, _ dm.PathsFilter) error {
	e.placed = p
	return nil
}
func (e *heldPaletteEditor) InstanceReplace(i *dmminstance.Instance, p *dmmprefab.Prefab) {
	e.placed = p
	i.SetPrefab(p)
}

func TestFillAndReplaceCommitRotatedHeldPrefab(t *testing.T) {
	for _, name := range []string{TNFill, TNReplace} {
		t.Run(name, func(t *testing.T) {
			_, base := lifecycleFixture(t)
			source := dmmprefab.New(0, "/obj/held", dmvars.Set((&dmvars.MutableVariables{}).ToImmutable(), "dir", "2"))
			owner := &heldPaletteEditor{heldAddEditor: &heldAddEditor{base, source}}
			ed = owner
			oldTool, oldName := tools[name], selectedToolName
			oldActive, oldStarted := active, startedTool
			t.Cleanup(func() { tools[name], selectedToolName = oldTool, oldName; active, startedTool = oldActive, oldStarted })
			var current Tool = newFill()
			if name == TNReplace {
				current = newReplace()
			}
			tools[name], selectedToolName, active, startedTool = current, name, false, nil
			if !CanRotateHeld() {
				t.Fatal("selected prefab cannot rotate with", name)
			}
			if err := RotateHeld(true); err != nil {
				t.Fatal(err)
			}
			point := util.Point{X: 1, Y: 1, Z: 1}
			input := ActionInput{Position: point, InBounds: true, Target: base.m.GetTile(point).Instances()[0]}
			context := current.ActionContext(input)
			if context.prefab == nil || context.prefab.Vars().ValueV("dir", "") != "8" {
				t.Fatal("action context ignored held rotation")
			}
			current.setActionContext(context)
			current.captureActionContext()
			current.onStart(point)
			if name == TNFill {
				// A turn during a captured fill must change its captured payload too.
				active, startedTool = true, current
				if err := RotateHeld(false); err != nil {
					t.Fatal(err)
				}
				if feedback := current.ActionContext(input); feedback.prefab != current.(*ToolFill).prefab {
					t.Fatal("captured fill feedback retained the previous orientation")
				}
				current.onStop(point)
				if current.(*ToolFill).fillArea != (util.Bounds{}) {
					t.Fatal("fill retained finished geometry")
				}
			}
			want := "8"
			if name == TNFill {
				want = "2"
			}
			if owner.placed == nil || owner.placed.Vars().ValueV("dir", "") != want || source.Vars().ValueV("dir", "") != "2" {
				t.Fatal("committed prefab differs from held pose or mutated palette")
			}
		})
	}
}

type heldSelectingEditor struct{ *heldAddEditor }

func (e *heldSelectingEditor) SelectHeldPrefab(p *dmmprefab.Prefab) { e.prefab = p }

func TestPaletteRotationPublishesRotatedSelection(t *testing.T) {
	_, base := lifecycleFixture(t)
	source := dmmprefab.New(0, "/obj/held", dmvars.Set((&dmvars.MutableVariables{}).ToImmutable(), "dir", "2"))
	owner := &heldSelectingEditor{&heldAddEditor{base, source}}
	ed = owner
	oldTool, oldName, oldActive, oldStarted := tools[TNAdd], selectedToolName, active, startedTool
	t.Cleanup(func() { tools[TNAdd], selectedToolName, active, startedTool = oldTool, oldName, oldActive, oldStarted })
	add := newAdd()
	tools[TNAdd], selectedToolName, active, startedTool = add, TNAdd, false, nil
	for _, want := range []string{"8", "1"} {
		if err := RotateHeld(true); err != nil {
			t.Fatal(err)
		}
		if owner.prefab == source || owner.prefab.Vars().ValueV("dir", "") != want {
			t.Fatalf("selection did not follow held rotation, want dir %s", want)
		}
		if held, _ := add.HeldPrefab(); held.Vars().ValueV("dir", "") != want {
			t.Fatalf("held value diverged from published selection, want dir %s", want)
		}
	}
}

type heldShapeEditor struct{ *heldPaletteEditor }

func (*heldShapeEditor) BrushShape() editing.ShapeDescriptor {
	return editing.ShapeDescriptor{Width: 2, Height: 1}
}

func TestAddShapeTurnUpdatesCapturedPayloadAndFeedback(t *testing.T) {
	_, base := lifecycleFixture(t)
	source := dmmprefab.New(0, "/obj/held", dmvars.Set((&dmvars.MutableVariables{}).ToImmutable(), "dir", "2"))
	owner := &heldShapeEditor{&heldPaletteEditor{heldAddEditor: &heldAddEditor{base, source}}}
	ed = owner
	oldTool, oldName, oldActive, oldStarted := tools[TNAdd], selectedToolName, active, startedTool
	t.Cleanup(func() { tools[TNAdd], selectedToolName, active, startedTool = oldTool, oldName, oldActive, oldStarted })
	add := newAdd()
	tools[TNAdd], selectedToolName, active, startedTool = add, TNAdd, true, add
	point := util.Point{X: 1, Y: 1, Z: 1}
	input := ActionInput{Position: point, InBounds: true}
	add.setActionContext(add.ActionContext(input))
	add.captureActionContext()
	add.onStart(point)
	if add.shapeStroke == nil {
		t.Fatal("fixture did not capture shape")
	}
	if err := RotateHeld(true); err != nil {
		t.Fatal(err)
	}
	if feedback := add.ActionContext(input); feedback.prefab != add.shapePrefab || feedback.prefab.Vars().ValueV("dir", "") != "8" {
		t.Fatal("shape feedback ignored rotation")
	}
	add.onStop(point)
	for add.shapeStroke != nil {
		add.process()
	}
	if owner.placed == nil || owner.placed.Vars().ValueV("dir", "") != "8" || source.Vars().ValueV("dir", "") != "2" {
		t.Fatal("shape did not commit held pose or changed source")
	}
}
