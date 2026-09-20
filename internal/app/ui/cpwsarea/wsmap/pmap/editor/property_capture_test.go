package editor

import (
	"context"
	"reflect"
	"testing"

	"sdmm/internal/dmapi/dmenv"
	"sdmm/internal/dmapi/dmmap"
	"sdmm/internal/dmapi/dmmap/dmmdata"
	"sdmm/internal/dmapi/dmmap/dmmdata/dmmprefab"
	"sdmm/internal/dmapi/dmvars"
	"sdmm/internal/util"
)

func TestPropertyActionsCaptureAndHistory(t *testing.T) {
	for _, action := range []string{"reset", "move up", "move down", "delete matching", "replace matching", "replace tile"} {
		for _, invalid := range []bool{true, false} {
			name := action + "/valid"
			if invalid {
				name = action + "/capture failure"
			}
			t.Run(name, func(t *testing.T) {
				e := captureEditor(t)
				world := &dmvars.MutableVariables{}
				world.Put("area", "/area/foo")
				world.Put("turf", "/turf/foo")
				e.app.LoadedEnvironment().Objects["/world"] = &dmenv.Object{Path: "/world", Vars: world.ToImmutable()}
				dmmap.Init(e.app.LoadedEnvironment())
				t.Cleanup(dmmap.Free)
				origin := util.Point{X: 1, Y: 1, Z: 1}
				tile := e.dmm.GetTile(origin)
				prefab := tile.Instances()[2].Prefab()
				custom := dmmprefab.New(dmmprefab.IdNone, prefab.Path(), dmvars.Set(prefab.Vars(), "pixel_x", "16"))
				tile.InstancesAdd(custom)
				e.initializeCollaboration()
				initial, err := e.SaveSnapshot(context.Background())
				if err != nil {
					t.Fatal(err)
				}
				authority := e.executor
				if invalid {
					fault := origin
					if action == "delete matching" || action == "replace matching" {
						fault = util.Point{X: 4, Y: 2, Z: 1}
					}
					e.dmm.GetTile(fault).Instances()[2].SetStableID("invalid-property-capture")
				}
				before := e.dmm.Copy()
				switch action {
				case "reset":
					e.InstanceReset(tile.Instances()[3])
				case "move up":
					e.InstanceMoveToTop(tile.Instances()[2])
				case "move down":
					e.InstanceMoveToBottom(tile.Instances()[3])
				case "delete matching":
					e.InstancesDeleteByPrefab(prefab)
				case "replace matching":
					e.ReplacePrefab(prefab, custom)
				case "replace tile":
					e.TileReplace(origin, dmmdata.Prefabs{custom})
				}
				e.CommitOperation(action)
				app := e.app.(*noopReportingApp)
				if invalid {
					if !reflect.DeepEqual(e.dmm, &before) {
						t.Fatal("failed capture changed display contents")
					}
					if len(e.pendingChanges) != 0 {
						t.Fatal("failed action left unused captures")
					}
					after, err := authority.Snapshot(context.Background())
					if err != nil || !reflect.DeepEqual(after, initial) {
						t.Fatal("failed capture changed authority", err)
					}
					if app.commands.HasUndoV("test") || len(app.errors) != 1 {
						t.Fatal("failed action created history or lost its error report")
					}
					if _, err := e.SaveSnapshot(context.Background()); err == nil {
						t.Fatal("failed capture lost the Save guard")
					}
					if err := e.AttachCollaborationExecutor(authority); err != nil {
						t.Fatal("failed action blocked validated recovery", err)
					}
					return
				}
				committed, err := e.SaveSnapshot(context.Background())
				if err != nil || committed.Revision != initial.Revision+1 || reflect.DeepEqual(committed.Tiles, initial.Tiles) {
					t.Fatal("valid action did not commit a change", err)
				}
				app.commands.UndoV("test")
				undone, err := e.SaveSnapshot(context.Background())
				if err != nil || !reflect.DeepEqual(undone.Tiles, initial.Tiles) {
					t.Fatal("undo changed identity, order or variables", err)
				}
				app.commands.RedoV("test")
				redone, err := e.SaveSnapshot(context.Background())
				if err != nil || !reflect.DeepEqual(redone.Tiles, committed.Tiles) || len(app.errors) != 0 {
					t.Fatal("redo did not restore the exact action", err, app.errors)
				}
			})
		}
	}
}
