package editor

import (
	"context"
	"reflect"
	"testing"

	"sdmm/internal/dmapi/dmmap/dmmdata/dmmprefab"
	"sdmm/internal/dmapi/dmvars"
)

// Turning a light off is one undoable revision that keeps the instance's
// identity and other edits; turning it back on removes the edit again.
func TestInstanceSwitchLightTogglesLightOn(t *testing.T) {
	e := selectionEditor(t)
	env := e.app.LoadedEnvironment()
	env.Objects["/obj/foo"].Vars = dmvars.Set(env.Objects["/obj/foo"].Vars, "light_range", "3")
	instance := e.dmm.Tiles[0].Instances()[2]
	instance.SetPrefab(dmmprefab.New(dmmprefab.IdNone, "/obj/foo", dmvars.Set(dmvars.FromParent(env.Objects["/obj/foo"].Vars), "name", `"Lamp"`)))
	e.initializeCollaboration()
	e.pMap.Snapshot().Sync()
	before, _ := e.SaveSnapshot(context.Background())

	on, err := e.InstanceSwitchLight(e.dmm.Tiles[0].Instances()[2])
	if err != nil || on {
		t.Fatalf("switch = %v, %v; want off", on, err)
	}
	e.CommitOperation("Turn Light Off")
	off, _ := e.SaveSnapshot(context.Background())
	got := off.Tiles[0].State.Prefabs[2]
	if off.Revision != before.Revision+1 || !reflect.DeepEqual(got.Vars, map[string]string{"name": `"Lamp"`, "light_on": "FALSE"}) {
		t.Fatalf("revision %d -> %d, vars %v", before.Revision, off.Revision, got.Vars)
	}
	if got.StableID != before.Tiles[0].State.Prefabs[2].StableID {
		t.Fatal("switching lost the instance identity")
	}

	if on, err = e.InstanceSwitchLight(e.dmm.Tiles[0].Instances()[2]); err != nil || !on {
		t.Fatalf("switch back = %v, %v; want on", on, err)
	}
	e.CommitOperation("Turn Light On")
	back, _ := e.SaveSnapshot(context.Background())
	if vars := back.Tiles[0].State.Prefabs[2].Vars; !reflect.DeepEqual(vars, map[string]string{"name": `"Lamp"`}) {
		t.Fatalf("vars after turning on = %v", vars)
	}

	if _, err := e.InstanceSwitchLight(e.dmm.Tiles[0].Instances()[0]); err == nil {
		t.Fatal("an atom without a light was switched")
	}
}
