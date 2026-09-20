package window_test

import (
	"context"
	"reflect"
	"testing"

	"github.com/SpaiR/imgui-go"
	"github.com/go-gl/glfw/v3.3/glfw"
	"sdmm/internal/app/ui/cpwsarea/wsmap/tools"
	"sdmm/internal/dmapi/dmmap/dmmdata/dmmprefab"
	"sdmm/internal/dmapi/dmvars"
	"sdmm/internal/util"
)

func TestBrushCaptureAndHistory(t *testing.T) {
	for _, action := range []struct {
		name         string
		tool         string
		alt, outline bool
	}{
		{name: "add", tool: tools.TNAdd},
		{name: "fill", tool: tools.TNFill},
		{name: "outline fill", tool: tools.TNFill, outline: true},
		{name: "delete instance", tool: tools.TNDelete},
		{name: "delete tiles", tool: tools.TNDelete, alt: true},
		{name: "replace", tool: tools.TNReplace},
		{name: "delete selection", tool: tools.TNGrab},
	} {
		for _, invalid := range []bool{true, false} {
			name := action.name + "/valid"
			if invalid {
				name = action.name + "/capture failure"
			}
			t.Run(name, func(t *testing.T) {
				ws, app := newMouseNetworkWorkspace(t)
				e := ws.Map().Editor()
				initial, err := e.SaveSnapshot(context.Background())
				if err != nil {
					t.Fatal(err)
				}
				origin := util.Point{X: 1, Y: 1, Z: 1}
				instance := e.Dmm().GetTile(origin).Instances()[2]
				app.selectedPrefab = dmmprefab.New(dmmprefab.IdNone, instance.Prefab().Path(), dmvars.Set(instance.Prefab().Vars(), "dir", "4"))
				if invalid {
					fault := origin
					if action.tool == tools.TNFill || action.tool == tools.TNGrab {
						fault = util.Point{X: 3, Y: 2, Z: 1}
					}
					e.Dmm().GetTile(fault).Instances()[2].SetStableID("invalid-brush-capture")
				}
				before := e.Dmm().Copy()
				frame := mouseWorkspaceFrame(t, ws, app.mouse)
				tools.SetSelected(action.tool)
				frame(false, 1, 1)
				frame(false, 1, 1)
				ws.Map().CanvasState().SetHoveredInstance(instance)
				io := imgui.CurrentIO()
				if action.alt {
					io.KeyPress(int(glfw.KeyLeftAlt))
				}
				if action.outline {
					io.KeyPress(int(glfw.KeyLeftControl))
				}
				frame(true, 1, 1)
				frame(true, 1, 1)
				frame(true, 3, 3)
				frame(false, 3, 3)
				frame(false, 3, 3)
				io.KeyRelease(int(glfw.KeyLeftAlt))
				io.KeyRelease(int(glfw.KeyLeftControl))
				if action.tool == tools.TNGrab {
					tools.Selected().(*tools.ToolGrab).SelectArea([]util.Point{origin, {X: 3, Y: 2, Z: 1}})
					e.TileDeleteSelected()
					e.CommitOperation("Delete selected tiles")
				}
				if invalid {
					if !reflect.DeepEqual(e.Dmm(), &before) {
						t.Fatal("failed capture partially changed the map")
					}
					if _, err := e.SaveSnapshot(context.Background()); err == nil {
						t.Fatal("failed capture allowed Save")
					}
					if app.commands.HasUndoV(ws.CommandStackId()) || len(app.errors) != 1 {
						t.Fatalf("failed capture created history or reported %d errors", len(app.errors))
					}
					return
				}
				committed, err := e.SaveSnapshot(context.Background())
				if err != nil || committed.Revision != initial.Revision+1 || reflect.DeepEqual(committed.Tiles, initial.Tiles) {
					t.Fatal("valid brush did not commit one changed operation", err)
				}
				if action.tool == tools.TNFill {
					center := e.Dmm().GetTile(util.Point{X: 2, Y: 2, Z: 1}).Instances()
					want := 4
					if action.outline {
						want = 3
					}
					if len(center) != want {
						t.Fatal("fill changed the outline/interior behavior")
					}
				}
				app.commands.UndoV(ws.CommandStackId())
				undone, err := e.SaveSnapshot(context.Background())
				if err != nil || !reflect.DeepEqual(undone.Tiles, initial.Tiles) {
					t.Fatal("undo did not restore exact tiles", err)
				}
				app.commands.RedoV(ws.CommandStackId())
				redone, err := e.SaveSnapshot(context.Background())
				if err != nil || !reflect.DeepEqual(redone.Tiles, committed.Tiles) || len(app.errors) != 0 {
					t.Fatal("redo did not restore exact tiles", err, app.errors)
				}
			})
		}
	}
}
