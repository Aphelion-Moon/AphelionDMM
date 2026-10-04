package mapping

import (
	"context"

	"sdmm/internal/aphelion/collab/engine"
	"sdmm/internal/aphelion/collab/model"
	"sdmm/internal/aphelion/editing"
	"sdmm/internal/aphelion/resources"
	"sdmm/internal/util"
)

// TemplateExportSource is a pinned, validated accepted revision. Header omits
// tile payloads; Tile returns caller-owned data, admitted using EstimatedTileBytes.
type TemplateExportSource interface {
	Header() model.Snapshot
	Tile(model.Coord) (model.TileState, bool)
	EstimatedTileBytes(model.Coord) (uint64, bool)
}

type snapshotExportSource struct {
	header model.Snapshot
	tiles  map[model.Coord]model.TileState
}

func (source snapshotExportSource) Header() model.Snapshot { return source.header }
func (source snapshotExportSource) Tile(coord model.Coord) (model.TileState, bool) {
	state, exists := source.tiles[coord]
	return model.CloneTileState(state), exists
}
func (source snapshotExportSource) EstimatedTileBytes(coord model.Coord) (uint64, bool) {
	state, exists := source.tiles[coord]
	return engine.EstimateTileStateBytes(state), exists
}

// PrepareTemplateExport retains validation for callers with a borrowed snapshot.
// The editor uses PrepareTemplateExportTiles to avoid this whole-source index.
func PrepareTemplateExport(ctx context.Context, path string, snapshot model.Snapshot, selection editing.Selection, connector *util.Point) (*AuthoringProposal, error) {
	if err := snapshot.Validate(); err != nil {
		return nil, err
	}
	lease, err := resources.DefaultBudget().Reserve(uint64(len(snapshot.Tiles)) * 128)
	if err != nil {
		return nil, err
	}
	defer lease.Release()
	source := snapshotExportSource{header: snapshot, tiles: make(map[model.Coord]model.TileState, len(snapshot.Tiles))}
	source.header.Tiles = nil
	for _, tile := range snapshot.Tiles {
		source.tiles[tile.Coord] = tile.State
	}
	return PrepareTemplateExportTiles(ctx, path, source, selection, connector)
}
