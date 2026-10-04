// APHELION EDIT ADDITION START - STROKE PICKING
package render

import (
	"sdmm/internal/app/render/bucket/level/chunk/unit"
	"sdmm/internal/dmapi/dmicon"
	"sdmm/internal/dmapi/dmmap/dmminstance"
)

// PickAt walks draw order backwards so the first eligible opaque hit is topmost.
// It excludes presentation sprites and does not invoke mutating hover callbacks.
func (r *Render) PickAt(x, y, level int, eligible func(*dmminstance.Instance) bool) *dmminstance.Instance {
	if !r.LevelReady(level) {
		return nil
	}
	visible := r.bucket.Level(level)
	if visible == nil {
		return nil
	}
	for layerIndex := len(visible.Layers) - 1; layerIndex >= 0; layerIndex-- {
		layer := visible.Layers[layerIndex]
		chunks := visible.ChunksByLayers[layer]
		for chunkIndex := len(chunks) - 1; chunkIndex >= 0; chunkIndex-- {
			chunk := chunks[chunkIndex]
			if !dmicon.Cache.ExpandPendingBounds(chunk.ViewBounds).Contains(float32(x), float32(y)) {
				continue
			}
			units := chunk.UnitsByLayers[layer]
			for unitIndex := len(units) - 1; unitIndex >= 0; unitIndex-- {
				u := units[unitIndex]
				// Most units in an intersecting chunk do not touch this pixel.
				if !u.ViewBounds().Contains(float32(x), float32(y)) {
					continue
				}
				if eligible(u.Instance()) && UnitContainsPixel(u, x, y) {
					return u.Instance()
				}
			}
		}
	}
	return nil
}

func UnitContainsPixel(u unit.Unit, x, y int) bool {
	if !u.ViewBounds().Contains(float32(x), float32(y)) || u.Sprite() == nil || u.Sprite().Image() == nil || u.A() <= 0 {
		return false
	}
	xOffset := int(float32(x)-u.ViewBounds().X1) + u.Sprite().X1
	yOffset := u.Sprite().IconHeight() - 1 - int(float32(y)-u.ViewBounds().Y1) + u.Sprite().Y1
	_, _, _, a := u.Sprite().Image().At(xOffset, yOffset).RGBA()
	return a != 0
}

// APHELION EDIT ADDITION END
