package window_test

import (
	"context"
	"testing"

	"github.com/SpaiR/imgui-go"
	"github.com/go-gl/glfw/v3.3/glfw"
	"sdmm/internal/aphelion/collab/engine"
	"sdmm/internal/aphelion/collab/executor"
	"sdmm/internal/aphelion/collab/model"
	"sdmm/internal/app/config"
	"sdmm/internal/app/ui/cpvareditor"
	"sdmm/internal/app/ui/cpwsarea/wsmap/pmap/editor"
	"sdmm/internal/app/ui/cpwsarea/wsmap/tools"
	"sdmm/internal/app/window"
	"sdmm/internal/dmapi/dmmap/dmmdata/dmmprefab"
	"sdmm/internal/dmapi/dmmap/dmminstance"
	"sdmm/internal/util"
)

// Shift-dragging with the Move tool changes pixel offsets while the tile stays
// fixed. The drawn geometry must follow the pointer on the frame that moves it,
// before release, and must never keep a unit at an earlier pose.
func TestShiftMoveDragUpdatesRenderedUnitBeforeRelease(t *testing.T) {
	ws, app := newMouseNetworkWorkspace(t)
	e := ws.Map().Editor()
	r := ws.Map().Canvas().Render()
	origin := util.Point{X: 1, Y: 1, Z: 1}
	moved := e.Dmm().GetTile(origin).Instances()[2]
	dragged := moved
	frame := mouseWorkspaceFrame(t, ws, app.mouse)

	assertUnit := func(step string, wantX1, wantY1 float32) {
		t.Helper()
		// Undo restores authority, which may rebuild the instance object.
		moved = e.Dmm().GetTile(origin).Instances()[2]
		units := r.UnitBounds(1, moved.Id())
		if len(units) != 1 {
			t.Fatalf("%s: %d rendered units for the instance, want exactly 1 (stale ghost): %v", step, len(units), units)
		}
		if units[0].X1 != wantX1 || units[0].Y1 != wantY1 {
			t.Fatalf("%s: rendered at (%v,%v), want (%v,%v); prefab pixel_x=%q pixel_y=%q",
				step, units[0].X1, units[0].Y1, wantX1, wantY1,
				moved.Prefab().Vars().ValueV("pixel_x", ""), moved.Prefab().Vars().ValueV("pixel_y", ""))
		}
		if moved.Coord() != origin {
			t.Fatalf("%s: Shift-drag changed the tile to %v", step, moved.Coord())
		}
	}

	tools.SetSelected(tools.TNMove)
	frame(false, 1, 1)
	frame(false, 1, 1)
	ws.Map().CanvasState().SetHoveredInstance(moved)
	frame(true, 1, 1)
	frame(true, 1, 1)
	assertUnit("gesture start", 0, 0)

	io := imgui.CurrentIO()
	io.KeyPress(int(glfw.KeyLeftShift))
	// Each step is a 32 px pointer move; the gesture is still held (no release).
	frame(true, 2, 1)
	assertUnit("pointer +32 px", 32, 0)
	frame(true, 3, 1)
	assertUnit("pointer +64 px", 64, 0)
	frame(true, 3, 2)
	assertUnit("pointer +64 px, +32 px up", 64, 32)
	frame(true, 1, 1)
	assertUnit("pointer returned", 0, 0)
	frame(true, 4, 1)
	assertUnit("pointer +96 px", 96, 0)
	io.KeyRelease(int(glfw.KeyLeftShift))
	frame(false, 4, 1)
	frame(false, 4, 1)
	assertUnit("after release", 96, 0)

	app.commands.UndoV(ws.CommandStackId())
	window.DrainFrameJobsForTest()
	e.ProcessCollaborationUpdates()
	frame(false, 4, 1)
	assertUnit("after undo", 0, 0)
	if stale := r.UnitBounds(1, dragged.Id()); moved.Id() != dragged.Id() && len(stale) != 0 {
		t.Fatalf("undo left a stale unit for the dragged instance: %v", stale)
	}
	if len(app.errors) != 0 {
		t.Fatalf("unexpected errors: %v", app.errors)
	}
}

type offsetVarApp struct {
	*mouseNetworkApp
	current *editor.Editor
}

func (a *offsetVarApp) CurrentEditor() *editor.Editor  { return a.current }
func (*offsetVarApp) DoSelectPrefab(*dmmprefab.Prefab) {}
func (*offsetVarApp) ConfigFind(string) config.Config  { return nil }

// Variable-editor offset changes must refresh the sprite for the instance and
// for every instance of an edited prefab, including edits that restore the
// value the map was opened with.
func TestVariableEditorOffsetEditsRefreshRenderedUnits(t *testing.T) {
	for _, attach := range []bool{false, true} {
		name := "default executor"
		if attach {
			name = "attached local authority"
		}
		t.Run(name, func(t *testing.T) { variableEditorOffsetEdits(t, attach) })
	}
}

func variableEditorOffsetEdits(t *testing.T, attach bool) {
	ws, app := newMouseNetworkWorkspace(t)
	e := ws.Map().Editor()
	if attach {
		initial, err := e.CollaborationSnapshot(context.Background())
		if err != nil {
			t.Fatal(err)
		}
		document, err := engine.NewDocument(initial)
		if err != nil {
			t.Fatal(err)
		}
		actor, err := model.NewActorID()
		if err != nil {
			t.Fatal(err)
		}
		authority, err := executor.NewLocal(document, actor)
		if err != nil {
			t.Fatal(err)
		}
		if err := e.AttachCollaborationExecutor(authority); err != nil {
			t.Fatal(err)
		}
	}
	r := ws.Map().Canvas().Render()
	frame := mouseWorkspaceFrame(t, ws, app.mouse)
	vars := cpvareditor.NewForVerification(&offsetVarApp{mouseNetworkApp: app, current: e})
	origin := util.Point{X: 1, Y: 1, Z: 1}
	settle := func() {
		t.Helper()
		window.DrainFrameJobsForTest()
		e.ProcessCollaborationUpdates()
		frame(false, 1, 1)
		window.DrainFrameJobsForTest()
	}
	assertAt := func(step string, point util.Point, wantX1, wantY1 float32) {
		t.Helper()
		instance := e.Dmm().GetTile(point).Instances()[2]
		units := r.UnitBounds(1, instance.Id())
		wantX1 += float32((point.X - 1) * 32)
		wantY1 += float32((point.Y - 1) * 32)
		if len(units) != 1 || units[0].X1 != wantX1 || units[0].Y1 != wantY1 {
			t.Fatalf("%s: tile %v units=%v, want one unit at (%v,%v); pixel_x=%q pixel_y=%q", step, point, units, wantX1, wantY1,
				instance.Prefab().Vars().ValueV("pixel_x", ""), instance.Prefab().Vars().ValueV("pixel_y", ""))
		}
	}
	instance := func() *dmminstance.Instance { return e.Dmm().GetTile(origin).Instances()[2] }

	// The fixture starts without pixel offsets, so a null value restores the
	// opened state.
	for _, edit := range []struct {
		name, value string
		x, y        float32
	}{
		{"pixel_x", "40", 40, 0},
		{"pixel_x", "-40", -40, 0},
		{"pixel_y", "-17", -40, -17},
		{"pixel_x", "", 0, -17},
		{"pixel_y", "", 0, 0},
		{"pixel_x", "41", 41, 0},
	} {
		vars.SetInstanceVariableForVerification(instance(), edit.name, edit.value)
		settle()
		assertAt("instance "+edit.name+"="+edit.value, origin, edit.x, edit.y)
	}
	if len(app.errors) != 0 {
		t.Fatalf("unexpected errors: %v", app.errors)
	}

	// Prefab edit: every instance sharing the prefab moves. Tile 1 carries its own
	// instance override from above and is edited as its own prefab.
	shared := e.Dmm().GetTile(util.Point{X: 4, Y: 1, Z: 1}).Instances()[2].Prefab()
	vars.SetPrefabVariableForVerification(shared, "pixel_x", "40")
	settle()
	for _, point := range []util.Point{{X: 4, Y: 1, Z: 1}, {X: 2, Y: 3, Z: 1}, {X: 4, Y: 4, Z: 1}} {
		assertAt("prefab pixel_x=40", point, 40, 0)
	}
	if len(app.errors) != 0 {
		t.Fatalf("unexpected errors: %v", app.errors)
	}
}
