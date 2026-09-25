// APHELION EDIT ADDITION START - DETERMINISTIC RANDOM FILL
package editor

import (
	"context"
	"fmt"
	"math"
	"sdmm/internal/aphelion/editing"
	"sdmm/internal/aphelion/resources"
	"sdmm/internal/app/ui/cpwsarea/wsmap/tools"
	"sdmm/internal/dmapi/dm"
	"sdmm/internal/dmapi/dmmap"
	"sdmm/internal/dmapi/dmmap/dmmdata/dmmprefab"
	"sdmm/internal/dmapi/dmvars"
	"sdmm/internal/util"
)

type randomFillDefinition struct {
	selection editing.Selection
	palette   editing.RandomPalette
	density   float64
	anchor    util.Point
	filter    dm.PathsFilter
}

func (e *Editor) RandomFillPreview() bool { return e.paste != nil && e.paste.preserveEmpty }
func (e *Editor) RerollRandomFill(seed uint64) error {
	if !e.RandomFillPreview() || e.randomFill == nil {
		return nil
	}
	if e.PastePlacementPending() {
		return fmt.Errorf("random Fill is already being confirmed")
	}
	definition := *e.randomFill
	target, orientation := e.paste.target, e.paste.orientation
	e.CancelPastePlacement()
	tools.Tools()[tools.TNGrab].(*tools.ToolGrab).CancelPlacement()
	if err := e.StartRandomFillWithFilter(definition.selection, definition.palette, seed, definition.density, definition.anchor, definition.filter); err != nil {
		return err
	}
	e.paste.target = target
	e.paste.orientation = orientation
	e.paste.request++
	return nil
}

// StartRandomFill prepares an immutable sparse payload through the same worker,
// presentation and confirmation owner as a stamp. Peers receive explicit tiles.
func (e *Editor) StartRandomFill(selection editing.Selection, palette editing.RandomPalette, seed uint64, density float64, anchor util.Point) error {
	return e.StartRandomFillWithFilter(selection, palette, seed, density, anchor, e.app.PathsFilter().Copy())
}

func (e *Editor) StartRandomFillWithFilter(selection editing.Selection, palette editing.RandomPalette, seed uint64, density float64, anchor util.Point, filter dm.PathsFilter) error {
	if math.IsNaN(density) || density < 0 || density > 1 {
		return fmt.Errorf("density must be between 0 and 1")
	}
	compiled, err := palette.Compile()
	if err != nil {
		return err
	}
	if selection.Len() == 0 || density == 0 {
		return nil
	}
	if !e.CanStartMapEdit() {
		return fmt.Errorf("finish or cancel the current edit before random Fill")
	}
	filter = filter.Copy()
	maxTileBytes := uint64(1024)
	for _, entry := range palette.Entries {
		bytes := uint64(len(entry.Prefab.Path) + 1024)
		for key, value := range entry.Prefab.Vars {
			bytes = addWorkBytes(bytes, uint64(len(key)+len(value)+128))
		}
		maxTileBytes = max(maxTileBytes, bytes)
	}
	budget := e.editWorkBudget()
	factory := func(ctx context.Context) ([]dmmap.Tile, func(string) bool, *resources.Reservation, error) {
		if maxTileBytes > (^uint64(0)-1024)/uint64(selection.Len()) {
			return nil, nil, nil, fmt.Errorf("random Fill exceeds memory capacity")
		}
		reservation, err := budget.Reserve(uint64(selection.Len())*maxTileBytes + 1024)
		if err != nil {
			return nil, nil, nil, err
		}
		tiles := make([]dmmap.Tile, 0, selection.Len())
		selection.Visit(func(coord util.Point) {
			if err != nil {
				return
			}
			if err = ctx.Err(); err != nil {
				return
			}
			tile := dmmap.Tile{Coord: coord}
			if value, chosen := compiled.Choose(seed, coord.Minus(anchor), density); chosen && filter.IsVisiblePath(value.Path) {
				vars := &dmvars.MutableVariables{}
				for key, v := range value.Vars {
					vars.Put(key, v)
				}
				tile.InstancesAdd(dmmprefab.New(0, value.Path, vars.ToImmutable()))
			}
			tiles = append(tiles, tile)
		})
		if err != nil {
			reservation.Release()
			return nil, nil, nil, err
		}
		return tiles, filter.IsVisiblePath, reservation, nil
	}
	bounds := selection.Bounds()
	target := util.Point{X: int(bounds.X1), Y: int(bounds.Y1), Z: selection.Level()}
	if err = e.beginPasteProposalFromFactory(selection.Len(), factory, nil, target); err != nil {
		return err
	}
	e.paste.preserveEmpty = true
	e.randomFill = &randomFillDefinition{selection: selection, palette: palette.Clone(), density: density, anchor: anchor, filter: filter}
	grab := tools.SetSelected(tools.TNGrab).(*tools.ToolGrab)
	if !grab.StartPreparedPlacement(e, target) {
		e.discardPasteWithoutRestore()
		return fmt.Errorf("unable to start random Fill preview")
	}
	return nil
}

// APHELION EDIT ADDITION END
