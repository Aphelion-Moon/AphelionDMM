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

func TestInternDeadlineProgressBeyondOldCapAndCancellation(t *testing.T) {
	dmmap.PrefabStorage.Free()
	t.Cleanup(dmmap.PrefabStorage.Free)
	prefabs := make([]*dmmprefab.Prefab, 600)
	for i := range prefabs {
		prefabs[i] = dmmprefab.New(0, "/obj/unknown", dmvars.FromParent(nil))
	}
	data := &dmmdata.DmmData{Dictionary: dmmdata.DataDictionary{"a": prefabs}}
	b := NewBuilder(&dmenv.Dme{Objects: map[string]*dmenv.Object{}}, data, "")
	tick := time.Unix(0, 0)
	b.now = func() time.Time { tick = tick.Add(time.Nanosecond); return tick }
	first := b.InternUntil(context.Background(), tick.Add(8*time.Nanosecond))
	if first.Done || first.Reason != "deadline" || b.member == 0 {
		t.Fatalf("deadline did not yield with progress: %+v", first)
	}
	member := b.member
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	stopped := b.InternUntil(ctx, tick.Add(time.Second))
	if stopped.Reason != "cancelled" || b.member != member {
		t.Fatal("cancelled slice changed interning state")
	}
	last := b.InternUntil(context.Background(), tick.Add(time.Second))
	if !last.Done || last.Items <= 256 || last.Reason != "complete" {
		t.Fatalf("adequate slice retained the old cap: %+v", last)
	}
}
