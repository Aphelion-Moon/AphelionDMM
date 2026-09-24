// Package mapsave holds the editor's independent pre-allocation save boundary.
package mapsave

import (
	"strconv"

	"sdmm/internal/dmapi/dmmap"
	"sdmm/internal/dmapi/dmmap/dmmdata"
)

// Expected captures the intended tile stacks without reusing any dictionary key
// or content hash. Call after optional sanitization; category ordering matches
// the documented save normalization and retains order within each category.
func Expected(source *dmmap.Dmm, isTGM bool) *dmmdata.DmmData {
	d := &dmmdata.DmmData{
		MaxX: source.MaxX, MaxY: source.MaxY, MaxZ: source.MaxZ, IsTgm: isTGM,
		Dictionary: make(dmmdata.DataDictionary, len(source.Tiles)),
		Grid:       make(dmmdata.DataGrid, len(source.Tiles)),
	}
	for index, tile := range source.Tiles {
		key := dmmdata.Key(strconv.Itoa(index)) // Private labels; never serialized.
		d.Grid[tile.Coord] = key
		d.Dictionary[key] = tile.Instances().Sorted().Prefabs()
	}
	return d
}
