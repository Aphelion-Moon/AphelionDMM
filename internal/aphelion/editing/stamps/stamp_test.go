package stamps

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"sdmm/internal/dmapi/dm"
	"sdmm/internal/dmapi/dmmap"
	"sdmm/internal/dmapi/dmmap/dmmdata/dmmprefab"
	"sdmm/internal/dmapi/dmvars"
	"sdmm/internal/util"
)

func stampFixture(t *testing.T) *Stamp {
	t.Helper()
	vars := &dmvars.MutableVariables{}
	vars.Put("unknown", `"preserved"`)
	tile := &dmmap.Tile{Coord: util.Point{X: 1, Y: 1, Z: 1}}
	tile.InstancesAdd(dmmprefab.New(0, "/obj/unknown", vars.ToImmutable()))
	s, err := Capture("User name", strings.Repeat("a", 64), &dmmap.Dmm{MaxX: 1, MaxY: 1, MaxZ: 1, Tiles: []*dmmap.Tile{tile}}, []util.Point{tile.Coord}, dm.NewPathsFilterEmpty())
	if err != nil {
		t.Fatal(err)
	}
	return s
}

func TestStampRejectsMalformedOrOversizedFile(t *testing.T) {
	for _, kind := range []string{"version", "unknown field", "duplicate tile", "outside tile", "inconsistent dimensions", "empty name", "missing vars", "invalid path", "invalid environment", "trailing", "oversized"} {
		t.Run(kind, func(t *testing.T) {
			s := stampFixture(t)
			switch kind {
			case "version":
				s.data.Version++
			case "duplicate tile":
				s.data.Tiles = append(s.data.Tiles, s.data.Tiles[0])
			case "outside tile":
				s.data.Tiles[0].X = 2
			case "inconsistent dimensions":
				s.data.Width = 2
			case "empty name":
				s.data.Name = " "
			case "missing vars":
				s.data.Tiles[0].Prefabs[0].Vars = nil
			case "invalid path":
				s.data.Tiles[0].Prefabs[0].Path = ""
			case "invalid environment":
				s.data.EnvironmentHash = "not a hash"
			}
			data, err := json.Marshal(s.data)
			if err != nil {
				t.Fatal(err)
			}
			if kind == "unknown field" {
				data = append([]byte(`{"unexpected":true,`), data[1:]...)
			}
			if kind == "trailing" {
				data = append(data, []byte(` {}`)...)
			}
			if kind == "oversized" {
				data = make([]byte, MaxFileBytes+1)
			}
			path := filepath.Join(t.TempDir(), "invalid.admmstamp")
			if err := os.WriteFile(path, data, 0600); err != nil {
				t.Fatal(err)
			}
			if _, err := Load(path); err == nil {
				t.Fatal("invalid stamp accepted")
			}
		})
	}
}

func TestStampSaveAndTemplateAreIndependent(t *testing.T) {
	s := stampFixture(t)
	filter := dm.NewPathsFilterEmpty()
	filter.TogglePath("/area/hidden")
	data := s.PasteData(filter, nil)
	data.Buffer[0].Instances()[0].SetPrefab(dmmprefab.New(0, "/changed", &dmvars.Variables{}))
	data.Filter.TogglePath("/area/hidden")
	path := filepath.Join(t.TempDir(), "selection.admmstamp")
	if err := s.Save(path); err != nil {
		t.Fatal(err)
	}
	before, _ := os.ReadFile(path)
	loaded, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if got := loaded.PasteData(filter, nil); got.Buffer[0].Instances()[0].Prefab().Path() != "/obj/unknown" || got.Filter.IsVisiblePath("/area/hidden") {
		t.Fatal("template or filter aliased caller changes")
	}
	s.data.Name = ""
	if err := s.Save(path); err == nil {
		t.Fatal("invalid stamp replaced existing file")
	}
	after, _ := os.ReadFile(path)
	if string(before) != string(after) {
		t.Fatal("failed save changed existing stamp")
	}
}
