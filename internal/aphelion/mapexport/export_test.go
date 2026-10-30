package mapexport

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"sdmm/internal/aphelion/collab/model"
	"sdmm/internal/aphelion/diskversion"
	"sdmm/internal/aphelion/editing"
	"sdmm/internal/dmapi/dmenv"
	"sdmm/internal/dmapi/dmmap/dmmdata"
	"sdmm/internal/util"
)

const testEnvironmentHash = "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"

// sourceSnapshot builds a maxX x maxY x maxZ map whose every tile holds an area,
// a turf naming its coordinate, and (on tile 3,2,z) an unknown object with vars.
func sourceSnapshot(t *testing.T, maxX, maxY, maxZ int) model.Snapshot {
	t.Helper()
	documentID, err := model.NewDocumentID()
	if err != nil {
		t.Fatal(err)
	}
	snapshot := model.Snapshot{
		ProtocolVersion: model.ProtocolVersion,
		SchemaVersion:   model.SchemaVersion,
		DocumentID:      documentID,
		EnvironmentHash: testEnvironmentHash,
		MaxX:            maxX,
		MaxY:            maxY,
		MaxZ:            maxZ,
	}
	// Reverse order: the crop must not depend on snapshot tile ordering.
	for z := maxZ; z >= 1; z-- {
		for y := maxY; y >= 1; y-- {
			for x := maxX; x >= 1; x-- {
				prefabs := []model.PrefabState{
					{StableID: newStableID(t), Path: "/area/test", Vars: map[string]string{}},
					{StableID: newStableID(t), Path: "/turf/floor", Vars: map[string]string{"name": fmt.Sprintf("\"floor %d,%d,%d\"", x, y, z)}},
				}
				if x == 3 && y == 2 {
					prefabs = append(prefabs, model.PrefabState{
						StableID: newStableID(t),
						Path:     "/obj/aphelion_export_unknown_type",
						Vars:     map[string]string{"dir": "4", "custom_var": "\"kept\"", "list_var": "list(\"a\" = 1)"},
					})
				}
				snapshot.Tiles = append(snapshot.Tiles, model.Tile{
					Coord: model.Coord{X: x, Y: y, Z: z},
					State: model.TileState{Prefabs: prefabs},
				})
			}
		}
	}
	if err := snapshot.Validate(); err != nil {
		t.Fatal(err)
	}
	return snapshot
}

func newStableID(t *testing.T) model.StableID {
	t.Helper()
	id, err := model.NewStableID()
	if err != nil {
		t.Fatal(err)
	}
	return id
}

func tileAt(t *testing.T, snapshot model.Snapshot, x, y, z int) model.Tile {
	t.Helper()
	for _, tile := range snapshot.Tiles {
		if tile.Coord == (model.Coord{X: x, Y: y, Z: z}) {
			return tile
		}
	}
	t.Fatalf("tile (%d,%d,%d) missing", x, y, z)
	return model.Tile{}
}

var testFiller = []model.PrefabState{
	{Path: "/area/space", Vars: map[string]string{}},
	{Path: "/turf/space", Vars: map[string]string{}},
}

func TestCropRectangleMatchesSelectionDimensionsAndContents(t *testing.T) {
	source := sourceSnapshot(t, 6, 5, 2)
	selection := editing.RectangleSelection(util.Bounds{X1: 2, Y1: 2, X2: 4, Y2: 4}, 2)
	region, err := SelectionRegion(selection)
	if err != nil {
		t.Fatal(err)
	}
	cropped, err := Crop(source, region, testFiller)
	if err != nil {
		t.Fatal(err)
	}
	if cropped.MaxX != 3 || cropped.MaxY != 3 || cropped.MaxZ != 1 {
		t.Fatalf("dimensions = %dx%dx%d, want 3x3x1", cropped.MaxX, cropped.MaxY, cropped.MaxZ)
	}
	if len(cropped.Tiles) != 9 {
		t.Fatalf("tiles = %d, want 9", len(cropped.Tiles))
	}
	if err := cropped.Validate(); err != nil {
		t.Fatalf("cropped snapshot invalid: %v", err)
	}
	for x := 1; x <= 3; x++ {
		for y := 1; y <= 3; y++ {
			got := tileAt(t, cropped, x, y, 1)
			want := tileAt(t, source, x+1, y+1, 2)
			if len(got.State.Prefabs) != len(want.State.Prefabs) {
				t.Fatalf("(%d,%d) prefabs = %d, want %d", x, y, len(got.State.Prefabs), len(want.State.Prefabs))
			}
			for i := range want.State.Prefabs {
				g, w := got.State.Prefabs[i], want.State.Prefabs[i]
				if g.Path != w.Path || g.StableID != w.StableID || fmt.Sprint(g.Vars) != fmt.Sprint(w.Vars) {
					t.Fatalf("(%d,%d) prefab %d = %+v, want %+v", x, y, i, g, w)
				}
			}
		}
	}
	// The crop owns its variable maps.
	tileAt(t, cropped, 2, 1, 1).State.Prefabs[2].Vars["dir"] = "8"
	if tileAt(t, source, 3, 2, 2).State.Prefabs[2].Vars["dir"] != "4" {
		t.Fatal("crop shares variable maps with the source snapshot")
	}
}

func TestCropSparseSelectionFillsHolesWithFiller(t *testing.T) {
	source := sourceSnapshot(t, 5, 5, 1)
	selection, err := editing.MaskSelection([]util.Point{{X: 2, Y: 2, Z: 1}, {X: 4, Y: 3, Z: 1}})
	if err != nil {
		t.Fatal(err)
	}
	region, err := SelectionRegion(selection)
	if err != nil {
		t.Fatal(err)
	}
	cropped, err := Crop(source, region, testFiller)
	if err != nil {
		t.Fatal(err)
	}
	if cropped.MaxX != 3 || cropped.MaxY != 2 || cropped.MaxZ != 1 || len(cropped.Tiles) != 6 {
		t.Fatalf("dimensions = %dx%dx%d with %d tiles, want 3x2x1 with 6", cropped.MaxX, cropped.MaxY, cropped.MaxZ, len(cropped.Tiles))
	}
	if err := cropped.Validate(); err != nil {
		t.Fatal(err)
	}
	if got := tileAt(t, cropped, 1, 1, 1).State.Prefabs[1].Vars["name"]; got != "\"floor 2,2,1\"" {
		t.Fatalf("selected tile name = %s", got)
	}
	if got := tileAt(t, cropped, 3, 2, 1).State.Prefabs[1].Vars["name"]; got != "\"floor 4,3,1\"" {
		t.Fatalf("selected tile name = %s", got)
	}
	hole := tileAt(t, cropped, 2, 1, 1).State.Prefabs
	if len(hole) != 2 || hole[0].Path != "/area/space" || hole[1].Path != "/turf/space" {
		t.Fatalf("unselected cell = %+v, want filler", hole)
	}
}

func TestCropRejectsEmptyAndOutOfBoundsRegions(t *testing.T) {
	source := sourceSnapshot(t, 3, 3, 1)
	if _, err := SelectionRegion(editing.Selection{}); err == nil {
		t.Fatal("empty selection accepted")
	}
	outside := editing.RectangleSelection(util.Bounds{X1: 2, Y1: 2, X2: 4, Y2: 3}, 1)
	region, err := SelectionRegion(outside)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := Crop(source, region, testFiller); err == nil {
		t.Fatal("selection beyond map bounds accepted")
	}
	wrongLevel := editing.RectangleSelection(util.Bounds{X1: 1, Y1: 1, X2: 2, Y2: 2}, 2)
	region, err = SelectionRegion(wrongLevel)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := Crop(source, region, testFiller); err == nil {
		t.Fatal("selection on a missing level accepted")
	}
}

func writeBackup(t *testing.T, dir, name, content string) string {
	t.Helper()
	path := filepath.Join(dir, name)
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

const dmBackupCRLF = "\"a\" = (/area/test,/turf/floor)\r\n\r\n(1,1,1) = {\"\r\na\r\n\"}\r\n"

const tgmBackup = "//MAP CONVERTED BY dmm2tgm.py THIS HEADER COMMENT PREVENTS RECONVERSION, DO NOT REMOVE\n" +
	"\"a\" = (\n/area/test,\n/turf/floor)\n\n(1,1,1) = {\"\na\n\"}\n"

func exportFixture(t *testing.T, backupContent string) (model.Snapshot, string, string, string) {
	t.Helper()
	dir := t.TempDir()
	source := writeBackup(t, dir, "source.dmm", backupContent)
	backup := writeBackup(t, dir, "source.backup.dmm", backupContent)
	snapshot := sourceSnapshot(t, 6, 5, 2)
	region, err := SelectionRegion(editing.RectangleSelection(util.Bounds{X1: 2, Y1: 1, X2: 5, Y2: 3}, 1))
	if err != nil {
		t.Fatal(err)
	}
	cropped, err := Crop(snapshot, region, testFiller)
	if err != nil {
		t.Fatal(err)
	}
	return cropped, source, backup, filepath.Join(dir, "export.dmm")
}

func TestWriteRoundTripsExportedSelection(t *testing.T) {
	cropped, source, backup, destination := exportFixture(t, dmBackupCRLF)
	state, err := Write(&dmenv.Dme{}, cropped, Target{Path: destination, SourcePath: source, SourceBackup: backup, Expected: diskversion.Absent()})
	if err != nil {
		t.Fatal(err)
	}
	if !state.Exists() {
		t.Fatal("written disk state does not exist")
	}
	saved, err := dmmdata.New(destination)
	if err != nil {
		t.Fatalf("parse exported map: %v", err)
	}
	if saved.MaxX != 4 || saved.MaxY != 3 || saved.MaxZ != 1 {
		t.Fatalf("exported dimensions = %dx%dx%d, want 4x3x1", saved.MaxX, saved.MaxY, saved.MaxZ)
	}
	if saved.IsTgm {
		t.Fatal("DMM source exported as TGM")
	}
	raw, err := os.ReadFile(destination)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(raw), "\r\n") || strings.Count(string(raw), "\n") != strings.Count(string(raw), "\r\n") {
		t.Fatal("exported map did not keep the source CRLF line endings")
	}
	// (3,2,1) in the source is (2,2,1) in the export and holds the unknown object.
	prefabs := saved.Dictionary[saved.Grid[util.Point{X: 2, Y: 2, Z: 1}]]
	var found bool
	for _, prefab := range prefabs {
		if prefab.Path() != "/obj/aphelion_export_unknown_type" {
			continue
		}
		found = true
		for name, want := range map[string]string{"dir": "4", "custom_var": "\"kept\"", "list_var": "list(\"a\" = 1)"} {
			if got, ok := prefab.Vars().ExplicitValue(name); !ok || got != want {
				t.Fatalf("unknown object var %s = %q (%t), want %q", name, got, ok, want)
			}
		}
	}
	if !found {
		t.Fatalf("unknown type was not exported: %v", prefabs)
	}
	for x := 1; x <= 4; x++ {
		for y := 1; y <= 3; y++ {
			want := fmt.Sprintf("\"floor %d,%d,1\"", x+1, y)
			var got string
			for _, prefab := range saved.Dictionary[saved.Grid[util.Point{X: x, Y: y, Z: 1}]] {
				if prefab.Path() == "/turf/floor" {
					got, _ = prefab.Vars().ExplicitValue("name")
				}
			}
			if got != want {
				t.Fatalf("export (%d,%d) turf name = %s, want %s", x, y, got, want)
			}
		}
	}
	sourceContent, err := os.ReadFile(source)
	if err != nil || string(sourceContent) != dmBackupCRLF {
		t.Fatalf("source map changed: %q %v", sourceContent, err)
	}
}

func TestWriteKeepsTGMFormat(t *testing.T) {
	cropped, source, backup, destination := exportFixture(t, tgmBackup)
	if _, err := Write(&dmenv.Dme{}, cropped, Target{Path: destination, SourcePath: source, SourceBackup: backup, Expected: diskversion.Absent()}); err != nil {
		t.Fatal(err)
	}
	saved, err := dmmdata.New(destination)
	if err != nil {
		t.Fatal(err)
	}
	if !saved.IsTgm {
		t.Fatal("TGM source exported as DMM")
	}
}

func TestWriteRefusesTheSourceMap(t *testing.T) {
	cropped, source, backup, _ := exportFixture(t, dmBackupCRLF)
	destinations := []string{source, backup, filepath.Join(filepath.Dir(source), ".", filepath.Base(source))}
	if runtime.GOOS == "windows" {
		destinations = append(destinations, strings.ToUpper(source))
	}
	for _, destination := range destinations {
		_, err := Write(&dmenv.Dme{}, cropped, Target{Path: destination, SourcePath: source, SourceBackup: backup, Expected: diskversion.Absent()})
		if !errors.Is(err, ErrSourceDestination) {
			t.Fatalf("Write(%q) err = %v, want ErrSourceDestination", destination, err)
		}
	}
	content, err := os.ReadFile(source)
	if err != nil || string(content) != dmBackupCRLF {
		t.Fatalf("source map changed: %q %v", content, err)
	}
}

func TestWriteRefusesAnExistingDestinationWithoutItsState(t *testing.T) {
	cropped, source, backup, destination := exportFixture(t, dmBackupCRLF)
	writeBackup(t, filepath.Dir(destination), filepath.Base(destination), "existing")
	if _, err := Write(&dmenv.Dme{}, cropped, Target{Path: destination, SourcePath: source, SourceBackup: backup, Expected: diskversion.Absent()}); err == nil {
		t.Fatal("existing destination replaced without confirmation state")
	}
	if content, _ := os.ReadFile(destination); string(content) != "existing" {
		t.Fatalf("destination changed: %q", content)
	}
	state, err := diskversion.Capture(destination)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := Write(&dmenv.Dme{}, cropped, Target{Path: destination, SourcePath: source, SourceBackup: backup, Expected: state}); err != nil {
		t.Fatalf("confirmed replace failed: %v", err)
	}
	if saved, err := dmmdata.New(destination); err != nil || saved.MaxX != 4 {
		t.Fatalf("confirmed replace did not write export: %v", err)
	}
}
