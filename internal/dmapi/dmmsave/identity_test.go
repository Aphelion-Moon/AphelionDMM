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

func TestSaveKeyCacheResolvesCollision(t *testing.T) {
	a := dmmdata.Prefabs{dmmprefab.New(42, "/obj/a", &dmvars.Variables{})}
	b := dmmdata.Prefabs{dmmprefab.New(42, "/obj/b", &dmvars.Variables{})}
	data := &dmmdata.DmmData{Dictionary: dmmdata.DataDictionary{"a": a, "b": b}}
	key, ok := findKeyByTileContent(data, map[uint64]dmmdata.Key{b.Hash(): "a"}, b)
	if !ok || key != "b" {
		t.Fatalf("collision resolved to %q, %t", key, ok)
	}
}

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
