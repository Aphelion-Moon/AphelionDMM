package search

import (
	"fmt"

	"sdmm/internal/dmapi/dmmap"
	"sdmm/internal/dmapi/dmmap/dmminstance"
)

// CurrentTiles validates exact pointer membership at each target's coordinate
// and returns unique tiles in first-target order. Callers must keep the map on
// its owning thread between validation, capture and mutation; this is no cache.
func CurrentTiles(m *dmmap.Dmm, targets []*dmminstance.Instance) ([]*dmmap.Tile, error) {
	stale := func() ([]*dmmap.Tile, error) {
		return nil, fmt.Errorf("search result no longer belongs to the current map")
	}
	if len(targets) == 0 {
		return nil, nil
	}
	if m == nil {
		return stale()
	}
	// Row actions need no membership map.
	if len(targets) == 1 {
		target := targets[0]
		if target != nil && m.HasTile(target.Coord()) {
			tile := m.GetTile(target.Coord())
			for _, current := range tile.Instances() {
				if current == target {
					return []*dmmap.Tile{tile}, nil
				}
			}
		}
		return stale()
	}
	remaining := make(map[*dmminstance.Instance]*dmmap.Tile, len(targets))
	seen := make(map[*dmmap.Tile]bool)
	var tiles []*dmmap.Tile
	for _, target := range targets {
		if target == nil || !m.HasTile(target.Coord()) {
			return stale()
		}
		tile := m.GetTile(target.Coord())
		remaining[target] = tile
		if !seen[tile] {
			seen[tile] = true
			tiles = append(tiles, tile)
		}
	}
	for _, tile := range tiles {
		for _, current := range tile.Instances() {
			if remaining[current] == tile {
				delete(remaining, current)
			}
		}
	}
	if len(remaining) != 0 {
		return stale()
	}
	return tiles, nil
}
