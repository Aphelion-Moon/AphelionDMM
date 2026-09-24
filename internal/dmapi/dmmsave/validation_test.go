package dmmsave

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"sdmm/internal/dmapi/dmenv"
	"sdmm/internal/dmapi/dmmap"
	"sdmm/internal/dmapi/dmmap/dmmdata"
	"sdmm/internal/dmapi/dmmap/dmmdata/dmmprefab"
	"sdmm/internal/dmapi/dmvars"
	"sdmm/internal/util"
)

func TestSaveRejectsFaultyKeyAllocation(t *testing.T) {
	for _, format := range []Format{FormatDM, FormatTGM} {
		t.Run(fmt.Sprint(format), func(t *testing.T) {
			sp := validationProcess(t, format)
			sp.handleReusedKeys()
			if err := sp.handleLocationsWithoutKeys(); err != nil {
				t.Fatal(err)
			}
			// Simulate a serializer allocator bug after independent input capture.
			sp.output.Grid[util.Point{X: 2, Y: 1, Z: 1}] = sp.output.Grid[util.Point{X: 1, Y: 1, Z: 1}]
			// The lower serialization guard accepts this internally consistent but
			// wrong allocation. Only the independent expected input can reject it.
			control := *sp.output
			control.Filepath = filepath.Join(filepath.Dir(sp.output.Filepath), "corrupt-control.dmm")
			if err := control.Save(); err != nil {
				t.Fatalf("control is not a valid serialized map: %v", err)
			}
			if err := sp.save(); err == nil {
				t.Fatal("faulty key assignment replaced destination")
			}
			got, err := os.ReadFile(sp.output.Filepath)
			if err != nil || string(got) != "original target" {
				t.Fatalf("target changed: %q, %v", got, err)
			}
			stages, err := filepath.Glob(filepath.Join(filepath.Dir(sp.output.Filepath), ".*.tmp-*"))
			if err != nil || len(stages) != 0 {
				t.Fatalf("staging files remain: %v %v", stages, err)
			}
		})
	}
}

func validationProcess(t *testing.T, format Format) *saveProcess {
	t.Helper()
	dir := t.TempDir()
	backup := filepath.Join(dir, "backup.dmm")
	if err := os.WriteFile(backup, []byte("\"a\"=(/obj/one)\n\"b\"=(/obj/two)\n(1,1,1)={\"\nab\n\"}\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	data, err := dmmdata.New(backup)
	if err != nil {
		t.Fatal(err)
	}
	dmm := &dmmap.Dmm{MaxX: 2, MaxY: 1, MaxZ: 1, Backup: backup}
	for x := 1; x <= 2; x++ {
		coord := util.Point{X: x, Y: 1, Z: 1}
		tile := &dmmap.Tile{Coord: coord}
		tile.InstancesSet(data.Dictionary[data.Grid[coord]])
		dmm.Tiles = append(dmm.Tiles, tile)
	}
	target := filepath.Join(dir, "target.dmm")
	if err := os.WriteFile(target, []byte("original target"), 0o600); err != nil {
		t.Fatal(err)
	}
	sp, err := makeSaveProcess(Config{Format: format}, &dmenv.Dme{}, dmm, target)
	if err != nil {
		t.Fatal(err)
	}
	return sp
}

func TestSaveValidationPreservesRawOverridesAndSanitizeChoice(t *testing.T) {
	for _, format := range []Format{FormatDM, FormatTGM} {
		for _, sanitize := range []bool{false, true} {
			t.Run(fmt.Sprintf("%d/%t", format, sanitize), func(t *testing.T) {
				sp := validationProcess(t, format)
				vars := &dmvars.MutableVariables{}
				vars.Put("dir", "2")
				vars.Put("raw", `list("a" = 12)`)
				sp.dmm.Tiles[0].Instances()[0].SetPrefab(dmmprefab.New(0, "/obj/one", vars.ToImmutable()))
				// Unknown type: even sanitize=true must preserve opaque overrides.
				if err := SaveV(sp.dme, sp.dmm, sp.output.Filepath, Config{Format: format, SanitizeVariables: sanitize}); err != nil {
					t.Fatal(err)
				}
				got, err := dmmdata.New(sp.output.Filepath)
				if err != nil {
					t.Fatal(err)
				}
				prefab := got.Dictionary[got.Grid[util.Point{X: 1, Y: 1, Z: 1}]][0]
				if prefab.Vars().ValueV("raw", "") != `list("a" = 12)` || prefab.Vars().ValueV("dir", "") != "2" {
					t.Fatal("unknown raw overrides changed")
				}
			})
		}
	}
}

func TestSaveValidationHonorsKnownDefaultSanitization(t *testing.T) {
	for _, format := range []Format{FormatDM, FormatTGM} {
		for _, sanitize := range []bool{false, true} {
			t.Run(fmt.Sprintf("%d/%t", format, sanitize), func(t *testing.T) {
				sp := validationProcess(t, format)
				vars := &dmvars.MutableVariables{}
				vars.Put("dir", "2")
				sp.dme.Objects = map[string]*dmenv.Object{"/obj/one": {Path: "/obj/one", Vars: vars.ToImmutable()}}
				sp.dmm.Tiles[0].Instances()[0].SetPrefab(dmmprefab.New(0, "/obj/one", vars.ToImmutable()))
				if err := SaveV(sp.dme, sp.dmm, sp.output.Filepath, Config{Format: format, SanitizeVariables: sanitize}); err != nil {
					t.Fatal(err)
				}
				got, err := dmmdata.New(sp.output.Filepath)
				if err != nil {
					t.Fatal(err)
				}
				_, exists := got.Dictionary[got.Grid[util.Point{X: 1, Y: 1, Z: 1}]][0].Vars().Value("dir")
				if exists == sanitize {
					t.Fatalf("sanitize=%t, override exists=%t", sanitize, exists)
				}
				if sp.dmm.Tiles[0].Instances()[0].Prefab().Vars().Len() != 1 {
					t.Fatal("save mutated input")
				}
			})
		}
	}
}
