package editor

import (
	"context"
	"sdmm/internal/dmapi/dmmap"
	"sdmm/internal/dmapi/dmmap/dmmdata/dmmprefab"
	"sdmm/internal/dmapi/dmvars"
	"sdmm/internal/util"
	"testing"
)

func TestLargeFillSchedulesEnumerationAndOneHistoryUnit(t *testing.T) {
	e := selectionEditor(t)
	for x := 2; x <= 200; x++ {
		tile := &dmmap.Tile{Coord: util.Point{X: x, Y: 1, Z: 1}}
		tile.InstancesSet(e.dmm.Tiles[0].Instances().Prefabs())
		e.dmm.Tiles = append(e.dmm.Tiles, tile)
	}
	e.dmm.MaxX = 200
	e.initializeCollaboration()
	e.pMap.Snapshot().Sync()
	a := e.app.(*editorTestApp)
	a.runLater = make(chan func(), 8)
	before, err := e.SaveSnapshot(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	want, _ := before.Hash()
	values := &dmvars.MutableVariables{}
	values.Put("custom", `"raw"`)
	prefab := dmmprefab.New(dmmprefab.IdNone, "/obj/filled", values.ToImmutable())
	if !e.TryScheduleFill(util.Bounds{X1: 1, Y1: 1, X2: 200, Y2: 1}, 1, prefab, false, true) {
		t.Fatal("large fill used synchronous path")
	}
	if len(e.pendingChanges) != 0 || e.authoritative.Revision != 0 || len(e.dmm.Tiles[0].Instances()) != len(before.Tiles[0].State.Prefabs) {
		t.Fatal("fill mutated display before acceptance")
	}
	for e.localWork != nil {
		a.runScheduled(t)
	}
	if e.authoritative.Revision != 1 || !a.commands.HasUndoV("test") {
		t.Fatal("fill was not one accepted history unit")
	}
	a.commands.UndoV("test")
	for e.localWork != nil {
		a.runScheduled(t)
	}
	got, err := e.SaveSnapshot(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	hash, _ := got.Hash()
	if hash != want || a.commands.HasUndoV("test") {
		t.Fatal("fill undo did not restore exact state")
	}
}
