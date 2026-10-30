// APHELION EDIT ADDITION START - EXPORT SELECTION
package pmap

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	"sdmm/internal/aphelion/collab/model"
	"sdmm/internal/aphelion/diskversion"
	"sdmm/internal/aphelion/editing"
	"sdmm/internal/aphelion/mapexport"
	"sdmm/internal/aphelion/resources"
	"sdmm/internal/dmapi/dmenv"
	"sdmm/internal/dmapi/dmmap/dmmdata"
	"sdmm/internal/util"
)

type fixedExportCapture struct{ snapshot model.Snapshot }

func (c fixedExportCapture) Snapshot() model.Snapshot { return c.snapshot }
func (c fixedExportCapture) EstimatedBytes() uint64   { return 4096 }

func exportTestSnapshot(t *testing.T) model.Snapshot {
	t.Helper()
	documentID, err := model.NewDocumentID()
	if err != nil {
		t.Fatal(err)
	}
	snapshot := model.Snapshot{
		ProtocolVersion: model.ProtocolVersion, SchemaVersion: model.SchemaVersion, DocumentID: documentID,
		EnvironmentHash: "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb", MaxX: 4, MaxY: 4, MaxZ: 1,
	}
	for y := 1; y <= 4; y++ {
		for x := 1; x <= 4; x++ {
			area, _ := model.NewStableID()
			turf, _ := model.NewStableID()
			snapshot.Tiles = append(snapshot.Tiles, model.Tile{Coord: model.Coord{X: x, Y: y, Z: 1}, State: model.TileState{Prefabs: []model.PrefabState{
				{StableID: area, Path: "/area/test", Vars: map[string]string{}},
				{StableID: turf, Path: "/turf/unknown_export_floor", Vars: map[string]string{"dir": "2"}},
			}}})
		}
	}
	return snapshot
}

func TestRunSelectionExportWritesCroppedMapAndProtectsSource(t *testing.T) {
	dir := t.TempDir()
	content := "\"a\" = (/area/test,/turf/unknown_export_floor)\n\n(1,1,1) = {\"\na\n\"}\n"
	source := filepath.Join(dir, "source.dmm")
	if err := os.WriteFile(source, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	region, err := mapexport.SelectionRegion(editing.RectangleSelection(util.Bounds{X1: 2, Y1: 3, X2: 4, Y2: 4}, 1))
	if err != nil {
		t.Fatal(err)
	}
	capture := fixedExportCapture{snapshot: exportTestSnapshot(t)}
	budget := resources.NewFixedBudget(1 << 20)

	err = runSelectionExport(capture, region, nil, &dmenv.Dme{}, mapexport.Target{Path: source, SourcePath: source, SourceBackup: source, Expected: diskversion.Absent()}, budget)
	if !errors.Is(err, mapexport.ErrSourceDestination) {
		t.Fatalf("export over source err = %v", err)
	}

	destination := filepath.Join(dir, "piece.dmm")
	err = runSelectionExport(capture, region, nil, &dmenv.Dme{}, mapexport.Target{Path: destination, SourcePath: source, SourceBackup: source, Expected: diskversion.Absent()}, budget)
	if err != nil {
		t.Fatal(err)
	}
	if budget.Used() != 0 {
		t.Fatalf("export reservation leaked %d bytes", budget.Used())
	}
	saved, err := dmmdata.New(destination)
	if err != nil {
		t.Fatal(err)
	}
	if saved.MaxX != 3 || saved.MaxY != 2 || saved.MaxZ != 1 {
		t.Fatalf("exported dimensions = %dx%dx%d, want 3x2x1", saved.MaxX, saved.MaxY, saved.MaxZ)
	}
	if after, _ := os.ReadFile(source); string(after) != content {
		t.Fatal("source map changed")
	}
}

// APHELION EDIT ADDITION END
