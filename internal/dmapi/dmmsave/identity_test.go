package dmmsave

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"sdmm/internal/aphelion/mapsave"
	"sdmm/internal/dmapi/dmenv"
	"sdmm/internal/dmapi/dmmap"
	"sdmm/internal/dmapi/dmmap/dmmdata"
	"sdmm/internal/dmapi/dmmap/dmmdata/dmmprefab"
	"sdmm/internal/dmapi/dmvars"
	"sdmm/internal/util"
)

func TestSaveKeyCacheResolvesCollision(t *testing.T) {
	a := dmmdata.Prefabs{dmmprefab.New(42, "/obj/a", &dmvars.Variables{})}
	b := dmmdata.Prefabs{dmmprefab.New(42, "/obj/b", &dmvars.Variables{})}
	index := mapsave.NewContentIndex(nil)
	index.Add(42, "a", a)
	index.Add(42, "b", b)
	key, ok := index.Find(42, b)
	if !ok || key != "b" {
		t.Fatalf("collision resolved to %q, %t", key, ok)
	}
}

// APHELION EDIT ADDITION START - SAVE_INDEX
func TestSaveReusesOriginalLocationKeysForDistinctChangedStacks(t *testing.T) {
	dir := t.TempDir()
	backup := filepath.Join(dir, "input.dmm")
	input := "\"a\"=(/obj/one)\n\"b\"=(/obj/two)\n(1,1,1)={\"\nab\"}\n"
	if err := os.WriteFile(backup, []byte(input), 0o600); err != nil {
		t.Fatal(err)
	}
	data, err := dmmdata.New(backup)
	if err != nil {
		t.Fatal(err)
	}
	dme := &dmenv.Dme{RootDir: dir}
	dmm, _ := dmmap.New(dme, data, backup)
	for index, path := range []string{"/obj/changed_one", "/obj/changed_two"} {
		tile := dmm.Tiles[index]
		old := tile.Instances()[0].Prefab()
		tile.Instances()[0].SetPrefab(dmmprefab.New(dmmprefab.IdNone, path, old.Vars()))
	}
	output := filepath.Join(dir, "output.dmm")
	sp, err := makeSaveProcess(Config{Format: FormatDM}, dme, dmm, output)
	if err != nil {
		t.Fatal(err)
	}
	sp.handleReusedKeys()
	if err := sp.handleLocationsWithoutKeys(); err != nil {
		t.Fatal(err)
	}
	if got := sp.output.Grid[util.Point{X: 1, Y: 1, Z: 1}]; got != "a" {
		t.Fatalf("first changed stack reused key %q, want original location key a", got)
	}
	if got := sp.output.Grid[util.Point{X: 2, Y: 1, Z: 1}]; got != "b" {
		t.Fatalf("second changed stack reused key %q, want original location key b", got)
	}
}

// APHELION EDIT ADDITION END

func TestSaveVPreservesAmbiguousContent(t *testing.T) {
	for _, format := range []Format{FormatDM, FormatTGM} {
		t.Run(fmt.Sprint(format), func(t *testing.T) {
			dmmap.PrefabStorage.Free()
			t.Cleanup(dmmap.PrefabStorage.Free)
			dir := t.TempDir()
			backup := filepath.Join(dir, "input.dmm")
			input := "\"a\"=(/obj/unknown{a = 12},/obj/unknown{a = 12},/turf,/area)\n\"b\"=(/obj/unknown{a1 = 2},/obj/unknown{a1 = 2},/turf,/area)\n(1,1,1)={\"\nab\n\"}\n"
			if err := os.WriteFile(backup, []byte(input), 0o600); err != nil {
				t.Fatal(err)
			}
			data, err := dmmdata.New(backup)
			if err != nil {
				t.Fatal(err)
			}
			dme := &dmenv.Dme{RootDir: dir}
			dmm, _ := dmmap.New(dme, data, backup)
			out := filepath.Join(dir, "output.dmm")
			if err := SaveV(dme, dmm, out, Config{Format: format}); err != nil {
				t.Fatal(err)
			}
			got, err := dmmdata.New(out)
			if err != nil {
				t.Fatal(err)
			}
			for x, name := range []string{"a", "a1"} {
				p := got.Dictionary[got.Grid[util.Point{X: x + 1, Y: 1, Z: 1}]]
				if len(p) != 4 || p[0].Path() != "/obj/unknown" || p[1].Path() != "/obj/unknown" {
					t.Fatalf("lost instances: %v", p)
				}
				want := "12"
				if x == 1 {
					want = "2"
				}
				for _, instance := range p[:2] {
					if value, ok := instance.Vars().Value(name); !ok || value != want {
						t.Fatalf("tile %d lost %s=%s", x+1, name, want)
					}
				}
			}
		})
	}
}
