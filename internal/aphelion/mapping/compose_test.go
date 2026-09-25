package mapping

import (
	"context"
	"os"
	"path/filepath"
	"sdmm/internal/dmapi/dmenv"
	"sdmm/internal/dmapi/dmvars"
	"sdmm/internal/util"
	"testing"
)

func TestNoopChannelsDoNotEraseObjectsOrInheritSuppressedBase(t *testing.T) {
	base := []Atom{{Path: "/turf/base"}, {Path: "/area/base"}, {Path: "/obj/base"}}
	module := []Atom{{Path: "/turf/template_noop"}, {Path: "/area/template_noop"}, {Path: "/obj/module"}}
	merged := applyCell(base, module)
	if len(merged) != 4 || merged[0].Path != "/turf/base" || merged[1].Path != "/area/base" || merged[3].Path != "/obj/module" {
		t.Fatalf("noop channels lost ordered contributions: %+v", merged)
	}
	reserved := applyCell(nil, []Atom{{Path: "/turf/fixed"}, {Path: "/area/template_noop"}, {Path: "/obj/fixed"}})
	if len(atomsFor(reserved, "area")) != 0 || len(atomsFor(reserved, "objects")) != 1 {
		t.Fatal("reserved base content leaked through area noop")
	}
}

func TestFixedReservationSuppressesRootBeforeExpansion(t *testing.T) {
	root := t.TempDir()
	for name, text := range map[string]string{
		"base.dmm":     "\"a\" = (/obj/modular_map_root{key = \"room\"; config_file = \"modules.toml\"},/obj/base,/turf/base,/area/base)\n(1,1,1) = {\"\na\n\"}\n",
		"fixed.dmm":    "\"a\" = (/obj/fixed,/turf/fixed,/area/template_noop)\n(1,1,1) = {\"\na\n\"}\n",
		"modules.toml": "directory = \"\"\n[rooms.room]\nmodules = [\"missing.dmm\"]\n",
	} {
		if err := os.WriteFile(filepath.Join(root, name), []byte(text), 0600); err != nil {
			t.Fatal(err)
		}
	}
	env := &dmenv.Dme{RootDir: root, Objects: map[string]*dmenv.Object{"/obj/modular_map_root": {Path: "/obj/modular_map_root", Vars: dmvars.FromParent(nil)}}}
	catalog := NewCatalog(env)
	defer catalog.Close()
	base, err := catalog.Load(context.Background(), filepath.Join(root, "base.dmm"))
	if err != nil {
		t.Fatal(err)
	}
	fixed, err := catalog.Load(context.Background(), filepath.Join(root, "fixed.dmm"))
	if err != nil {
		t.Fatal(err)
	}
	projection, err := catalog.Compose(context.Background(), base, Scenario{}, []FixedPlacement{{ID: "fixed", Source: fixed}})
	if err != nil {
		t.Fatal(err)
	}
	defer projection.Close()
	if len(projection.Placements) != 0 {
		t.Fatal("reserved root expanded")
	}
	cell := projection.Source.Cell(util.Point{X: 1, Y: 1, Z: 1})
	if len(cell) != 2 || len(atomsFor(cell, "area")) != 0 || atomsFor(cell, "objects")[0].Path != "/obj/fixed" {
		t.Fatalf("skipped base leaked into fixed projection: %+v", cell)
	}
	suppressed := false
	for _, d := range projection.Diagnostics {
		if d.Code == "root-suppressed" {
			suppressed = true
		}
		if d.Code == "missing-source" {
			t.Fatal("suppressed candidate was loaded")
		}
	}
	if !suppressed {
		t.Fatal("suppression provenance missing")
	}
}
