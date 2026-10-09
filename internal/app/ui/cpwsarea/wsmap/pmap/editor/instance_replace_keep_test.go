package editor

import (
	"context"
	"reflect"
	"testing"

	"sdmm/internal/dmapi/dmenv"
	"sdmm/internal/dmapi/dmmap/dmmdata/dmmprefab"
	"sdmm/internal/dmapi/dmvars"
)

// The play-test case: a windoor whose dir was edited is swapped for the
// directional helper, keeping its name and access but not the redundant dir.
func TestReplaceKeepingEditsSwapsTypeAndKeepsMeaningfulEdits(t *testing.T) {
	e := selectionEditor(t)
	env := e.app.LoadedEnvironment()
	helper := &dmvars.MutableVariables{}
	helper.Put("dir", "8")
	env.Objects["/obj/foo"].Vars = dmvars.Set(env.Objects["/obj/foo"].Vars, "name", `"foo"`)
	env.Objects["/obj/foo/west"] = &dmenv.Object{Path: "/obj/foo/west", Vars: helper.ToImmutable()}
	env.Objects["/obj/foo/west"].Vars.LinkParent(env.Objects["/obj/foo"].Vars)

	instance := e.dmm.Tiles[0].Instances()[2]
	edited := dmvars.Set(dmvars.Set(instance.Prefab().Vars(), "dir", "8"), "name", `"Reception Desk"`)
	instance.SetPrefab(dmmprefab.New(dmmprefab.IdNone, "/obj/foo", edited))
	e.initializeCollaboration()
	e.pMap.Snapshot().Sync()
	before, _ := e.SaveSnapshot(context.Background())

	selected := dmmprefab.New(dmmprefab.IdNone, "/obj/foo/west", dmvars.FromParent(env.Objects["/obj/foo/west"].Vars))
	dropped, err := e.InstanceReplaceKeepingEdits(e.dmm.Tiles[0].Instances()[2], selected)
	if err != nil {
		t.Fatal(err)
	}
	e.CommitOperation("Replace Instance, Keep Edits")
	after, _ := e.SaveSnapshot(context.Background())
	if after.Revision != before.Revision+1 {
		t.Fatalf("revision %d -> %d", before.Revision, after.Revision)
	}
	got := after.Tiles[0].State.Prefabs[2]
	if got.Path != "/obj/foo/west" || !reflect.DeepEqual(got.Vars, map[string]string{"name": `"Reception Desk"`}) {
		t.Fatalf("replaced = %+v", got)
	}
	if got.StableID != before.Tiles[0].State.Prefabs[2].StableID {
		t.Fatal("replacement lost the instance identity")
	}
	if !reflect.DeepEqual(dropped, []string{"dir"}) {
		t.Fatalf("dropped = %v", dropped)
	}
	if _, err := e.InstanceReplaceKeepingEdits(e.dmm.Tiles[0].Instances()[2], dmmprefab.New(dmmprefab.IdNone, "/turf/foo", nil)); err == nil {
		t.Fatal("cross-base replacement accepted")
	}
}
