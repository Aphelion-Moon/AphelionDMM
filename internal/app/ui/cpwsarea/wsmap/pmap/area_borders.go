// APHELION EDIT ADDITION START - CACHED AREA BORDERS
package pmap

import (
	"sdmm/internal/app/render"
	"sdmm/internal/app/ui/cpwsarea/wsmap/pmap/canvas"
	"sdmm/internal/app/ui/cpwsarea/wsmap/pmap/editor"
	"sdmm/internal/app/ui/cpwsarea/wsmap/pmap/overlay"
	"sdmm/internal/dmapi/dm"
	"sdmm/internal/util"
)

type areaBorderCache struct {
	valid           bool
	generation      uint64
	level, iconSize int
	borders         []render.AreaBorder
	zones           map[string]cachedAreaBorders
}

type cachedAreaBorders struct {
	generation, seen uint64
	border           *canvas.OverlayAreaBorder
}

func (c *areaBorderCache) resolve(generation uint64, level, iconSize int, zones []editor.AreaZone) []render.AreaBorder {
	if c.valid && c.generation == generation && c.level == level && c.iconSize == iconSize {
		return c.borders
	}
	if !c.valid || c.level != level || c.iconSize != iconSize {
		c.zones = make(map[string]cachedAreaBorders, len(zones))
	}
	// Keep prior frame views immutable while retaining unchanged area geometry.
	borders := make([]render.AreaBorder, 0, len(zones))
	for _, zone := range zones {
		revision := zone.Generation
		if revision == 0 {
			// Unversioned callers retain the original global invalidation contract.
			revision = generation
		}
		cached, exists := c.zones[zone.Name]
		if !exists || cached.generation != revision {
			capacity := 0
			if cached.border != nil {
				capacity = len(cached.border.Borders_)
			}
			cached.border = &canvas.OverlayAreaBorder{Borders_: buildAreaBorders(zone, level, iconSize, capacity), Color_: overlay.ColorAreaBorder}
			cached.generation = revision
		}
		cached.seen = generation
		c.zones[zone.Name] = cached
		borders = append(borders, cached.border)
	}
	for name, cached := range c.zones {
		if cached.seen != generation {
			delete(c.zones, name)
		}
	}
	c.valid, c.generation, c.level, c.iconSize = true, generation, level, iconSize
	c.borders = borders
	return c.borders
}

func buildAreaBorders(zone editor.AreaZone, level, iconSize, capacity int) []util.Bounds {
	lines := make([]util.Bounds, 0, capacity)
	size := float32(iconSize)
	for _, border := range zone.Borders {
		if border.Coord.Z != level {
			continue
		}
		x, y := float32(border.Coord.X-1)*size, float32(border.Coord.Y-1)*size
		if border.Dirs&dm.DirNorth != 0 {
			lines = append(lines, util.Bounds{X1: x, Y1: y + size, X2: x + size, Y2: y + size})
		}
		if border.Dirs&dm.DirEast != 0 {
			lines = append(lines, util.Bounds{X1: x + size, Y1: y, X2: x + size, Y2: y + size})
		}
		if border.Dirs&dm.DirSouth != 0 {
			lines = append(lines, util.Bounds{X1: x, Y1: y, X2: x + size, Y2: y})
		}
		if border.Dirs&dm.DirWest != 0 {
			lines = append(lines, util.Bounds{X1: x, Y1: y, X2: x, Y2: y + size})
		}
	}
	return lines
}

// APHELION EDIT ADDITION END
