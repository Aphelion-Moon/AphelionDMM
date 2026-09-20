package cpvareditor

import (
	"context"
	"testing"

	"sdmm/internal/app/command"
	"sdmm/internal/app/config"
	"sdmm/internal/app/ui/cpwsarea/wsmap"
	"sdmm/internal/app/ui/cpwsarea/wsmap/pmap/editor"
	"sdmm/internal/dmapi/dmenv"
	"sdmm/internal/dmapi/dmmap"
	"sdmm/internal/dmapi/dmmap/dmmdata/dmmprefab"
	"sdmm/internal/dmapi/dmvars"
	"sdmm/internal/util"
)

type captureVariableApp struct {
	wsmap.App
	current     *editor.Editor
	environment *dmenv.Dme
	commands    *command.Storage
	errors      []error
	selections  int
}

func (a *captureVariableApp) CurrentEditor() *editor.Editor    { return a.current }
func (a *captureVariableApp) LoadedEnvironment() *dmenv.Dme    { return a.environment }
func (a *captureVariableApp) CommandStorage() *command.Storage { return a.commands }
func (*captureVariableApp) ConfigFind(string) config.Config    { return nil }
func (a *captureVariableApp) DoSelectPrefab(*dmmprefab.Prefab) { a.selections++ }
func (a *captureVariableApp) ReportCollaborationError(_ string, err error) {
	a.errors = append(a.errors, err)
}

func TestVariableCaptureFailurePreservesPrefabSession(t *testing.T) {
	dmmap.PrefabStorage.Free()
	t.Cleanup(dmmap.PrefabStorage.Free)
	objects := make(map[string]*dmenv.Object)
	for _, path := range []string{"/world", "/area/test", "/turf/test", "/obj/test"} {
		vars := &dmvars.MutableVariables{}
		vars.Put("custom_value", "0")
		if path == "/world" {
			vars.Put("area", "/area/test")
			vars.Put("turf", "/turf/test")
		}
		objects[path] = &dmenv.Object{Path: path, Vars: vars.ToImmutable()}
	}
	a := &captureVariableApp{environment: &dmenv.Dme{Objects: objects}, commands: command.NewStorage()}
	dmmap.Init(a.environment)
	t.Cleanup(dmmap.Free)
	tile := &dmmap.Tile{Coord: util.Point{X: 1, Y: 1, Z: 1}}
	tile.InstancesAdd(dmmap.BaseArea)
	tile.InstancesAdd(dmmap.BaseTurf)
	prefab := dmmap.PrefabStorage.Get("/obj/test", dmvars.Set(dmvars.FromParent(objects["/obj/test"].Vars), "custom_value", "7"))
	tile.InstancesAdd(prefab)
	m := &dmmap.Dmm{Path: dmmap.DmmPath{Absolute: "capture"}, MaxX: 1, MaxY: 1, MaxZ: 1, Tiles: []*dmmap.Tile{tile}}
	a.commands.SetStack("capture")
	a.current = editor.New(a, nil, m)
	if _, err := a.current.SaveSnapshot(context.Background()); err != nil {
		t.Fatal(err)
	}
	instance := tile.Instances()[2]
	instance.SetStableID("invalid-variable-capture")
	v := &VarEditor{app: a, instance: instance, prefab: prefab, sessionPrefabId: prefab.Id()}
	v.setInstanceVariable("custom_value", "8")
	if instance.Prefab() != prefab || v.prefab != prefab || v.sessionPrefabId != prefab.Id() {
		t.Fatal("failed capture changed the instance or variable-edit session")
	}
	if cached, exists := dmmap.PrefabStorage.GetById(prefab.Id()); !exists || cached != prefab {
		t.Fatal("failed capture deleted the session prefab")
	}
	if a.selections != 0 || len(a.errors) != 1 || a.commands.HasUndoV("capture") {
		t.Fatal("failed capture selected a replacement, lost its report or created undo")
	}
	if _, err := a.current.SaveSnapshot(context.Background()); err == nil {
		t.Fatal("failed capture allowed Save")
	}
}
