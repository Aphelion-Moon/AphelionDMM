package search

import (
	"reflect"
	"testing"

	"sdmm/internal/dmapi/dmmap"
	"sdmm/internal/dmapi/dmmap/dmmdata/dmmprefab"
	"sdmm/internal/dmapi/dmmap/dmminstance"
	"sdmm/internal/dmapi/dmvars"
	"sdmm/internal/util"
)

func TestCurrentTilesChecksIdentityAndCoordinateBeforeReturning(t *testing.T) {
	m := &dmmap.Dmm{MaxX: 2, MaxY: 1, MaxZ: 1}
	prefab := dmmprefab.New(1, "/obj/test", dmvars.FromParent(nil))
	for x := 1; x <= 2; x++ {
		tile := &dmmap.Tile{Coord: util.Point{X: x, Y: 1, Z: 1}}
		tile.InstancesAdd(prefab)
		tile.InstancesAdd(prefab)
		m.Tiles = append(m.Tiles, tile)
	}
	a, b := m.Tiles[0].Instances()[0], m.Tiles[1].Instances()[0]
	copyOfA := a.Copy() // Same numeric/stable identity is insufficient.
	outside := dmminstance.New(util.Point{X: 3, Y: 1, Z: 1}, prefab)
	misplaced := dmminstance.New(a.Coord(), prefab)
	m.Tiles[1].Set(append(m.Tiles[1].Instances(), misplaced))
	before := m.Copy()
	for _, test := range []struct {
		name    string
		input   []*dmminstance.Instance
		want    []*dmmap.Tile
		invalid bool
	}{
		{name: "empty"},
		{name: "single", input: []*dmminstance.Instance{a}, want: []*dmmap.Tile{m.Tiles[0]}},
		{name: "ordered duplicates", input: []*dmminstance.Instance{b, a, b, m.Tiles[0].Instances()[1]}, want: []*dmmap.Tile{m.Tiles[1], m.Tiles[0]}},
		{name: "copied pointer", input: []*dmminstance.Instance{b, &copyOfA}, invalid: true},
		{name: "nil", input: []*dmminstance.Instance{a, nil}, invalid: true},
		{name: "outside", input: []*dmminstance.Instance{a, outside}, invalid: true},
		{name: "wrong tile", input: []*dmminstance.Instance{a, b, misplaced}, invalid: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			got, err := CurrentTiles(m, test.input)
			if (err != nil) != test.invalid || !reflect.DeepEqual(got, test.want) {
				t.Fatalf("tiles=%v err=%v", got, err)
			}
			if !reflect.DeepEqual(m.Copy(), before) {
				t.Fatal("validation mutated the map")
			}
		})
	}
}
