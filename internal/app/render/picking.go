// APHELION EDIT ADDITION START - STROKE PICKING
package render

import (
	"sdmm/internal/app/render/bucket/level/chunk/unit"
	"sdmm/internal/dmapi/dmicon"
	"sdmm/internal/dmapi/dmmap/dmminstance"
)

// PickAt follows the same layer/chunk/unit order as drawing. It deliberately
// excludes presentation sprites and does not invoke mutating hover callbacks.
func (r *Render) PickAt(x, y, level int, eligible func(*dmminstance.Instance) bool) *dmminstance.Instance {
	visible := r.bucket.Level(level)
	if visible == nil {
		return nil
	}
	var picked *dmminstance.Instance
	for _, layer := range visible.Layers {
		for _, chunk := range visible.ChunksByLayers[layer] {
			if !dmicon.Cache.ExpandPendingBounds(chunk.ViewBounds).Contains(float32(x), float32(y)) {
				continue
			}
			for _, u := range chunk.UnitsByLayers[layer] {
				if eligible(u.Instance()) && UnitContainsPixel(u, x, y) {
					picked = u.Instance()
				}
			}
		}
	}
	return picked
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
