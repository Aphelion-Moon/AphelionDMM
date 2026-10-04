package mapping

import (
	"context"
	"path/filepath"
	"strings"
	"testing"

	"sdmm/internal/aphelion/collab/model"
	"sdmm/internal/aphelion/editing"
	"sdmm/internal/util"
)

type countedExportSource struct {
	snapshotExportSource
	reads, estimates int
}

func (source *countedExportSource) Tile(coord model.Coord) (model.TileState, bool) {
	source.reads++
	return source.snapshotExportSource.Tile(coord)
}
func (source *countedExportSource) EstimatedTileBytes(coord model.Coord) (uint64, bool) {
	source.estimates++
	return source.snapshotExportSource.EstimatedTileBytes(coord)
}

func TestTemplateExportReadsOnlySelectedPayloads(t *testing.T) {
	document, _ := model.NewDocumentID()
	source := &countedExportSource{snapshotExportSource: snapshotExportSource{
		header: model.Snapshot{ProtocolVersion: model.ProtocolVersion, SchemaVersion: model.SchemaVersion, DocumentID: document, EnvironmentHash: strings.Repeat("a", 64), MaxX: 100, MaxY: 100, MaxZ: 1},
		tiles:  make(map[model.Coord]model.TileState),
	}}
	// Source expressions use the parser representation; unquoted whitespace is stripped.
	for _, x := range []int{1, 3} {
		id, _ := model.NewStableID()
		source.tiles[model.Coord{X: x, Y: 1, Z: 1}] = model.TileState{Prefabs: []model.PrefabState{{StableID: id, Path: "/obj/unknown", Vars: map[string]string{"raw": "\"quoted value\"", "expression": "1+2"}}}}
	}
	selection, err := editing.MaskSelection([]util.Point{{X: 1, Y: 1, Z: 1}, {X: 3, Y: 1, Z: 1}})
	if err != nil {
		t.Fatal(err)
	}
	proposal, err := PrepareTemplateExportTiles(context.Background(), filepath.Join(t.TempDir(), "selected.dmm"), source, selection, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer proposal.Close()
	if source.reads != 2 || source.estimates != 2 {
		t.Fatalf("read unselected source: reads=%d estimates=%d", source.reads, source.estimates)
	}
	// Source expressions use the parser representation; unquoted whitespace is stripped.
	for _, x := range []int{1, 3} {
		prefabs := proposal.output.Dictionary[proposal.output.Grid[util.Point{X: x, Y: 1, Z: 1}]]
		if len(prefabs) != 1 || prefabs[0].Path() != "/obj/unknown" || prefabs[0].Vars().ValueV("expression", "") != "1+2" || prefabs[0].Vars().ValueV("raw", "") != "\"quoted value\"" {
			t.Fatal("selected raw prefab content changed")
		}
	}
	hole := proposal.output.Dictionary[proposal.output.Grid[util.Point{X: 2, Y: 1, Z: 1}]]
	if len(hole) != 2 || hole[0].Path() != "/turf/template_noop" || hole[1].Path() != "/area/template_noop" {
		t.Fatal("mask hole lost channel noops")
	}
	if result := proposal.Apply(context.Background()); result.Err != nil || !result.SourceWritten {
		t.Fatal(result.Err)
	}
}
