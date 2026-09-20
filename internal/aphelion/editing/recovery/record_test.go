package recovery

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"sdmm/internal/aphelion/collab/model"
	"sdmm/internal/dmapi/dmmap"
	"sdmm/internal/dmapi/dmmap/dmmdata/dmmprefab"
	"sdmm/internal/dmapi/dmmap/dmminstance"
	"sdmm/internal/dmapi/dmvars"
	"sdmm/internal/util"
)

func TestRecordPreservesMalformedDisplayWithoutRepair(t *testing.T) {
	coord := util.Point{X: 1, Y: 1, Z: 1}
	vars := &dmvars.MutableVariables{}
	vars.Put("unknown", `"unusual value"`)
	valid := dmminstance.New(coord, dmmprefab.New(0, "/obj/unknown", vars.ToImmutable()))
	invalid := dmminstance.New(coord, dmmprefab.New(0, "", nil))
	invalid.SetStableID("not-a-valid-id")
	tile := &dmmap.Tile{Coord: coord}
	tile.Set(dmmap.Instances{valid, nil, invalid, dmminstance.New(coord, nil)})
	dmm := &dmmap.Dmm{Name: "private name", Path: dmmap.DmmPath{Absolute: "private source"}, MaxX: 1, MaxY: 1, MaxZ: 1, Tiles: []*dmmap.Tile{nil, tile}}
	r, err := Capture(dmm, model.Snapshot{}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if valid.StableID() != "" || invalid.StableID() != "not-a-valid-id" {
		t.Fatal("inspection repaired identities")
	}
	path := filepath.Join(t.TempDir(), "retained.json")
	if err := r.Export(path); err != nil {
		t.Fatal(err)
	}
	data, _ := os.ReadFile(path)
	var a archive
	if err := json.Unmarshal(data, &a); err != nil {
		t.Fatal(err)
	}
	if a.Display[0] != nil || a.Display[1].Instances[1] != nil || a.Display[1].Instances[2].Prefab.Variables != nil || a.Display[1].Instances[3].Prefab != nil {
		t.Fatal("export normalized damaged entries")
	}
	if strings.Contains(string(data), "private") || a.Display[1].Instances[0].Prefab.Variables[0].Value != `"unusual value"` {
		t.Fatal("export leaked paths or lost unknown values")
	}
	valid.SetStableID("changed")
	if err := r.Export(path); err != nil {
		t.Fatal(err)
	}
	again, _ := os.ReadFile(path)
	if string(again) != string(data) {
		t.Fatal("export aliased later changes")
	}
	if err := r.Export(filepath.Join(t.TempDir(), "map.dmm")); err == nil {
		t.Fatal("recovery export accepted map extension")
	}
}

func TestRecordRejectsLossyJSON(t *testing.T) {
	tile := &dmmap.Tile{}
	tile.Set(dmmap.Instances{dmminstance.New(util.Point{}, dmmprefab.New(0, string([]byte{255}), &dmvars.Variables{}))})
	if _, err := Capture(&dmmap.Dmm{Tiles: []*dmmap.Tile{tile}}, model.Snapshot{}, nil); err == nil {
		t.Fatal("invalid UTF-8 silently changed during recovery capture")
	}
}
