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
}

func (c *areaBorderCache) resolve(generation uint64, level, iconSize int, zones []editor.AreaZone) []render.AreaBorder {
	if c.valid && c.generation == generation && c.level == level && c.iconSize == iconSize {
		return c.borders
	}
	var lines []util.Bounds
	size := float32(iconSize)
	for _, zone := range zones {
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
	}
	c.valid, c.generation, c.level, c.iconSize = true, generation, level, iconSize
	c.borders = []render.AreaBorder{canvas.OverlayAreaBorder{Borders_: lines, Color_: overlay.ColorAreaBorder}}
	return c.borders
}

// APHELION EDIT ADDITION END
