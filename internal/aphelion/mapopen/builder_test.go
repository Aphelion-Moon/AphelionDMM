package mapopen

import (
	"context"
	"sdmm/internal/dmapi/dmenv"
	"sdmm/internal/dmapi/dmmap"
	"sdmm/internal/dmapi/dmmap/dmmdata"
	"sdmm/internal/dmapi/dmmap/dmmdata/dmmprefab"
	"sdmm/internal/dmapi/dmvars"
	"sdmm/internal/util"
	"testing"
	"time"
)

func TestBuilderYieldsAndPreservesUnknownDuplicateInstances(t *testing.T) {
	dmmap.PrefabStorage.Free()
	t.Cleanup(dmmap.PrefabStorage.Free)
	prefab := dmmprefab.New(42, "/obj/unknown", dmvars.FromParent(nil))
	data := &dmmdata.DmmData{Filepath: "map.dmm", MaxX: 2, MaxY: 1, MaxZ: 1, Dictionary: dmmdata.DataDictionary{"a": {prefab, prefab}}, Grid: dmmdata.DataGrid{{X: 1, Y: 1, Z: 1}: "a", {X: 2, Y: 1, Z: 1}: "a"}}
	builder := NewBuilder(&dmenv.Dme{Objects: map[string]*dmenv.Object{}}, data, "backup")
	if builder.InternStep(1, time.Now().Add(time.Second)) {
		t.Fatal("intern ignored quota")
	}
	if _, _, err := builder.Build(context.Background()); err == nil {
		t.Fatal("partial dictionary accepted")
	}
	for !builder.InternStep(1, time.Now().Add(time.Second)) {
	}
	m, unknown, err := builder.Build(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(unknown) != 1 || m.Backup != "backup" || len(m.GetTile(util.Point{X: 2, Y: 1, Z: 1}).Instances()) != 2 {
		t.Fatal("map content changed")
	}
	if m.Tiles[0].Instances()[0].Id() == m.Tiles[1].Instances()[0].Id() {
		t.Fatal("instances aliased")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, _, err := builder.Build(ctx); err == nil {
		t.Fatal("cancel ignored")
	}
}
