package search

import (
	"reflect"
	"sdmm/internal/dmapi/dmmap"
	"sdmm/internal/dmapi/dmmap/dmmdata/dmmprefab"
	"sdmm/internal/dmapi/dmvars"
	"sdmm/internal/util"
	"testing"
	"time"
)

func TestCursorBoundsScanAndPublicationPreservingOrder(t *testing.T) {
	m := &dmmap.Dmm{}
	for n := 0; n < 30; n++ {
		tile := &dmmap.Tile{Coord: util.Point{X: n + 1, Y: 1, Z: 1}}
		tile.InstancesAdd(dmmprefab.New(uint64(n%3+1), "/obj/test", dmvars.FromParent(nil)))
		m.Tiles = append(m.Tiles, tile)
	}
	ids := []uint64{3, 1, 2}
	cursor := NewCursor(m, ids)
	if cursor.Step(4, time.Now().Add(time.Second)) {
		t.Fatal("whole query completed inside a four-item quota")
	}
	if cursor.Result() != nil {
		t.Fatal("partial result published")
	}
	for n := 0; n < 100 && !cursor.Step(4, time.Now().Add(time.Second)); n++ {
	}
	if !reflect.DeepEqual(cursor.Result(), ByPrefabIDs(m, ids)) {
		t.Fatal("bounded query lost grouping/map order")
	}
}
