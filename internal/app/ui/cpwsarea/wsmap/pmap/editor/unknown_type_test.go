package editor

import (
	"strings"
	"testing"

	"sdmm/internal/dmapi/dmenv"
	"sdmm/internal/dmapi/dmmap"
	"sdmm/internal/dmapi/dmmap/dmmdata/dmmprefab"
	"sdmm/internal/dmapi/dmvars"
	"sdmm/internal/util"
)

func TestInstanceResetRefusesUnknownType(t *testing.T) {
	e := selectionEditor(t)
	app := &noopReportingApp{editorTestApp: e.app.(*editorTestApp)}
	e.app = app
	world := &dmvars.MutableVariables{}
	world.Put("area", "/area/foo")
	world.Put("turf", "/turf/foo")
	e.app.LoadedEnvironment().Objects["/world"] = &dmenv.Object{Path: "/world", Vars: world.ToImmutable()}
	dmmap.Init(e.app.LoadedEnvironment())
	t.Cleanup(dmmap.Free)
	tile := e.dmm.GetTile(util.Point{X: 1, Y: 1, Z: 1})
	custom := &dmvars.MutableVariables{}
	custom.Put("custom_value", "7")
	custom.Put("dir", "4")
	unknown := dmmprefab.New(dmmprefab.IdNone, "/obj/audit_unknown", custom.ToImmutable())
	tile.InstancesAdd(unknown)
	e.initializeCollaboration()
	instance := tile.Instances()[len(tile.Instances())-1]
	original := instance.Prefab()

	func() {
		defer func() {
			if failure := recover(); failure != nil {
				t.Fatalf("resetting a preserved unknown instance panicked: %v", failure)
			}
		}()
		e.InstanceReset(instance)
		e.CommitOperation("Reset Instance")
	}()

	if instance.Prefab() != original || instance.Prefab().Path() != "/obj/audit_unknown" {
		t.Fatal("refused reset replaced the unknown instance")
	}
	if instance.Prefab().Vars().ValueV("custom_value", "") != "7" || instance.Prefab().Vars().ValueV("dir", "") != "4" {
		t.Fatal("refused reset discarded unknown variables")
	}
	if len(e.pendingChanges) != 0 || app.commands.HasUndoV("test") {
		t.Fatal("refused reset captured a change or created history")
	}
	if len(app.errors) != 1 || !strings.Contains(app.errors[0].Error(), "/obj/audit_unknown") {
		t.Fatalf("refused reset did not report a clear reason: %v", app.errors)
	}
}
