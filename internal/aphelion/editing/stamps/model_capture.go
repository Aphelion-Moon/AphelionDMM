package stamps

import (
	"context"
	"fmt"
	"maps"
	"sdmm/internal/aphelion/collab/model"
	"sdmm/internal/aphelion/editing"
	"sdmm/internal/aphelion/resources"
	"sdmm/internal/dmapi/dm"
	"sdmm/internal/util"
	"sort"
	"strings"
)

// CaptureModel reads only selected tiles from a pinned immutable authority view.
// Call on a worker; no live DMM instance pointers may enter the source callback.
func CaptureModel(ctx context.Context, name, environment string, selection editing.Selection, filter *dm.PathsFilter, source func(model.Coord) (model.TileState, bool), budget *resources.Budget) (*Stamp, error) {
	if selection.Len() == 0 || filter == nil {
		return nil, fmt.Errorf("select tiles to capture")
	}
	reservation, err := budgetOrDefault(budget).Reserve(uint64(selection.Len())*256 + 1024)
	if err != nil {
		return nil, err
	}
	d := document{Format: "aphelion-selection-stamp", Version: 1, Name: strings.TrimSpace(name), EnvironmentHash: environment, HiddenPaths: filter.HiddenPaths()}
	a := selection.Bounds()
	d.Width, d.Height = int(a.X2-a.X1)+1, int(a.Y2-a.Y1)+1
	needed := uint64(selection.Len())*256 + 1024
	selection.Visit(func(p util.Point) {
		if err != nil {
			return
		}
		if err = ctx.Err(); err != nil {
			return
		}
		state, ok := source(model.Coord{X: p.X, Y: p.Y, Z: p.Z})
		if !ok {
			err = fmt.Errorf("stamp source tile is unavailable")
			return
		}
		needed += editing.EstimateMoveTileMemory(state) * 4
		if err = reservation.Resize(needed); err != nil {
			return
		}
		t := tile{X: p.X - int(a.X1) + 1, Y: p.Y - int(a.Y1) + 1}
		for _, p := range state.Prefabs {
			if filter.IsVisiblePath(p.Path) {
				t.Prefabs = append(t.Prefabs, prefab{Path: p.Path, Vars: maps.Clone(p.Vars)})
			}
		}
		d.Tiles = append(d.Tiles, t)
	})
	if err == nil {
		err = d.validate()
	}
	if err != nil {
		reservation.Release()
		return nil, err
	}
	sort.Slice(d.Tiles, func(i, j int) bool {
		if d.Tiles[i].Y != d.Tiles[j].Y {
			return d.Tiles[i].Y < d.Tiles[j].Y
		}
		return d.Tiles[i].X < d.Tiles[j].X
	})
	s := &Stamp{data: d, reservation: reservation}
	s.preview = s.buildPreview()
	return s, nil
}
