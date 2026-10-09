package tools

import (
	"strings"
	"testing"

	"sdmm/internal/aphelion/disposals"
	"sdmm/internal/dmapi/dmenv"
	"sdmm/internal/dmapi/dmmap"
	"sdmm/internal/dmapi/dmmap/dmmdata/dmmprefab"
	"sdmm/internal/dmapi/dmvars"
	"sdmm/internal/util"
)

type brushEditor struct {
	editor
	m       *dmmap.Dmm
	commits int
}

func (e *brushEditor) Dmm() *dmmap.Dmm                       { return e.m }
func (e *brushEditor) TryBeginTileChange(...util.Point) bool { return true }
func (e *brushEditor) UpdateCanvasByCoords([]util.Point)     {}
func (e *brushEditor) CommitOperation(string)                { e.commits++ }

func brushFixture(t *testing.T) *brushEditor {
	t.Helper()
	objects := map[string]*dmenv.Object{}
	define := func(path string, kv ...string) {
		v := &dmvars.MutableVariables{}
		for i := 0; i+1 < len(kv); i += 2 {
			v.Put(kv[i], kv[i+1])
		}
		objects[path] = &dmenv.Object{Path: path, Vars: v.ToImmutable()}
	}
	define("/obj/pipe/supply")
	define("/obj/structure/cable")
	define(disposals.Segment, "initialize_dirs", "4")
	define(disposals.Junction, "initialize_dirs", "6")
	define(disposals.Trunk, "initialize_dirs", "0")
	define("/obj/machinery/disposal/bin")
	define("/world", "icon_size", "32", "area", "/area", "turf", "/turf")
	define("/area")
	define("/turf")
	dmmap.Init(&dmenv.Dme{Objects: objects})
	dmmap.PrefabStorage.Free()
	t.Cleanup(func() { dmmap.PrefabStorage.Free(); dmmap.Free() })
	m := &dmmap.Dmm{MaxX: 4, MaxY: 4, MaxZ: 1}
	for y := 1; y <= 4; y++ {
		for x := 1; x <= 4; x++ {
			m.Tiles = append(m.Tiles, &dmmap.Tile{Coord: util.Point{X: x, Y: y, Z: 1}})
		}
	}
	e := &brushEditor{m: m}
	previous := ed
	ed = e
	t.Cleanup(func() { ed = previous })
	return e
}

func paths(tile *dmmap.Tile) string {
	var out []string
	for _, i := range tile.Instances() {
		out = append(out, i.Prefab().Path()+":"+i.Prefab().Vars().ValueV("dir", "-"))
	}
	return strings.Join(out, " ")
}

func TestBrushLaysBundleAndDisposalsAsOneEdit(t *testing.T) {
	e := brushFixture(t)
	bin, _ := dmmap.PrefabStorage.InitialV("/obj/machinery/disposal/bin")
	e.m.GetTile(util.Point{X: 1, Y: 1, Z: 1}).InstancesAdd(bin)
	supply, _ := dmmap.PrefabStorage.InitialV("/obj/pipe/supply")
	cable, _ := dmmap.PrefabStorage.InitialV("/obj/structure/cable")
	e.m.GetTile(util.Point{X: 2, Y: 1, Z: 1}).InstancesAdd(supply) // already there
	route := disposals.Extend(nil, util.Point{X: 1, Y: 1, Z: 1})
	route = disposals.Extend(route, util.Point{X: 2, Y: 2, Z: 1}) // diagonal: becomes (2,1) then (2,2)
	if err := LayBrush(e.m, route, []*dmmprefab.Prefab{supply, cable}, true); err != nil {
		t.Fatal(err)
	}
	if e.commits != 1 {
		t.Fatalf("commits = %d", e.commits)
	}
	at := func(x, y int) string { return paths(e.m.GetTile(util.Point{X: x, Y: y, Z: 1})) }
	if got := at(1, 1); !strings.Contains(got, disposals.Trunk+":4") {
		t.Fatalf("bin tile = %s", got)
	}
	if got := at(2, 1); strings.Count(got, "/obj/pipe/supply") != 1 || !strings.Contains(got, disposals.Segment+":9") { // bend west|north
		t.Fatalf("corner tile = %s", got)
	}
	if got := at(2, 2); !strings.Contains(got, "/obj/structure/cable") {
		t.Fatalf("end tile = %s", got)
	}
	// A run through the bin cannot be planned; nothing changes.
	before := at(1, 1) + "|" + at(1, 2)
	err := LayBrush(e.m, []util.Point{{X: 1, Y: 2, Z: 1}, {X: 1, Y: 1, Z: 1}, {X: 2, Y: 1, Z: 1}}, []*dmmprefab.Prefab{cable}, true)
	if err == nil || e.commits != 1 || at(1, 1)+"|"+at(1, 2) != before {
		t.Fatalf("failed plan changed the map: err=%v commits=%d", err, e.commits)
	}
}
