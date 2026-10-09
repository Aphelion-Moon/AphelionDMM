package unit

import (
	"sdmm/internal/dmapi/dm"
	"sdmm/internal/dmapi/dmicon"
	"sdmm/internal/dmapi/dmmap/dmmdata/dmmprefab"
	"sdmm/internal/dmapi/dmmap/dmminstance"
	"sdmm/internal/util"
)

// Unit stores render information about specific object prefab on the map.
type Unit struct {
	sprite   *dmicon.Sprite
	instance *dmminstance.Instance

	layer      float32
	viewBounds util.Bounds

	r, g, b, a float32

	// APHELION EDIT ADDITION START - IN-GAME LOOK
	icon string // the DMI the sprite comes from, which may not be the instance's
	// APHELION EDIT ADDITION END
}

func (u Unit) Sprite() *dmicon.Sprite {
	return u.sprite
}

// APHELION EDIT ADDITION START - IN-GAME LOOK

// Icon is the DMI this unit draws from: the instance's icon, or the icon of
// an in-game part such as a spawned window.
func (u Unit) Icon() string { return u.icon }

// APHELION EDIT ADDITION END

func (u Unit) Instance() *dmminstance.Instance {
	return u.instance
}

func (u Unit) Layer() float32 {
	return u.layer
}

func (u Unit) ViewBounds() util.Bounds {
	// APHELION EDIT ADDITION START - ASYNC ICONS
	// Original return u.viewBounds used the placeholder's fixed dimensions.
	bounds := u.viewBounds
	if u.sprite != nil {
		bounds.X2 = bounds.X1 + float32(u.sprite.IconWidth())
		bounds.Y2 = bounds.Y1 + float32(u.sprite.IconHeight())
	}
	return bounds
	// APHELION EDIT ADDITION END
}

func (u Unit) R() float32 {
	return u.r
}

func (u Unit) G() float32 {
	return u.g
}

func (u Unit) B() float32 {
	return u.b
}

func (u Unit) A() float32 {
	return u.a
}

func Make(x, y int, i *dmminstance.Instance, iconSize int) Unit {
	// All vars below are built-in and expected to exist.
	icon, _ := i.Prefab().Vars().Text("icon")
	iconState, _ := i.Prefab().Vars().Text("icon_state")
	dir, _ := i.Prefab().Vars().Int("dir")
	// APHELION EDIT ADDITION START - IN-GAME LOOK
	return MakeWithAppearance(x, y, i, iconSize, icon, iconState, dir)
}

// MakeWithAppearance builds a unit drawing the given icon state instead of the
// prefab's own; placement, colour and layer still come from the prefab.
func MakeWithAppearance(x, y int, i *dmminstance.Instance, iconSize int, icon, iconState string, dir int) Unit {
	return MakePart(x, y, i, i.Prefab(), iconSize, icon, iconState, dir)
}

// MakePart draws part (an atom the instance creates in game, such as a window
// from a spawner) on the instance's tile. Picking the unit selects instance i.
func MakePart(x, y int, i *dmminstance.Instance, part *dmmprefab.Prefab, iconSize int, icon, iconState string, dir int) Unit {
	// APHELION EDIT ADDITION END
	/* APHELION EDIT REMOVAL START - BATCH UNIT PREPARATION
	pixelX, _ := i.Prefab().Vars().Int("pixel_x")
	pixelY, _ := i.Prefab().Vars().Int("pixel_y")
	stepX, _ := i.Prefab().Vars().Int("step_x")
	stepY, _ := i.Prefab().Vars().Int("step_y")
	pixelW, _ := i.Prefab().Vars().Int("pixel_w")
	pixelZ, _ := i.Prefab().Vars().Int("pixel_z")
	APHELION EDIT REMOVAL END */
	// APHELION EDIT ADDITION START - BATCH UNIT PREPARATION
	offset := PlacementOffset(part)
	// APHELION EDIT ADDITION END

	sp := dmicon.Cache.GetSpriteOrPlaceholderV(icon, iconState, dir)
	// APHELION EDIT CHANGE - BATCH UNIT PREPARATION - ORIGINAL: x1 := float32((x-1)*iconSize + pixelX + stepX + pixelW)
	x1 := float32((x-1)*iconSize + offset.X)
	// APHELION EDIT CHANGE - BATCH UNIT PREPARATION - ORIGINAL: y1 := float32((y-1)*iconSize + pixelY + stepY + pixelZ)
	y1 := float32((y-1)*iconSize + offset.Y)
	x2 := x1 + float32(sp.IconWidth())
	y2 := y1 + float32(sp.IconHeight())
	// APHELION EDIT CHANGE - IN-GAME LOOK - ORIGINAL: r, g, b, a := parseColor(i.Prefab())
	r, g, b, a := parseColor(part)

	return Unit{
		// APHELION EDIT CHANGE - IN-GAME LOOK - ORIGINAL: sp, i, countLayer(i.Prefab()),
		sp, i, countLayer(part),
		util.Bounds{X1: x1, Y1: y1, X2: x2, Y2: y2},
		// APHELION EDIT CHANGE - IN-GAME LOOK - ORIGINAL: r, g, b, a,
		r, g, b, a, icon,
	}
}

func parseColor(p *dmmprefab.Prefab) (r, g, b, a float32) {
	// Default rgba is white.
	// APHELION EDIT CHANGE - RENDER ALPHA - ORIGINAL: r, g, b, a = 1, 1, 1, 1
	r, g, b = 1, 1, 1
	if color, _ := p.Vars().Text("color"); color != "" {
		r, g, b, _ = util.ParseColor(color).RGBA()
		/* APHELION EDIT REMOVAL START - RENDER ALPHA
		alpha := p.Vars().FloatV("alpha", 255)
		a = alpha / 255 // Color = RGB from color variable + alpha variable.
		APHELION EDIT REMOVAL END */
	}
	// APHELION EDIT ADDITION START - RENDER ALPHA
	alpha := p.Vars().FloatV("alpha", 255)
	a = alpha / 255 // Color = RGB from color variable + alpha variable.
	// APHELION EDIT ADDITION END
	return r, g, b, a
}

// countLayer returns the value of combined prefab vars: plane + Layer.
func countLayer(p *dmmprefab.Prefab) float32 {
	plane, _ := p.Vars().Float("plane")
	layer, _ := p.Vars().Float("layer")

	// Layers can have essentially effect values added onto them
	// We should clip them off to reduce the max possible layer to like 4999 (likely far lower)
	const backgroundLayer = 20_000
	const topdownLayer = 10_000
	const effectsLayer = 5000
	if layer > backgroundLayer {
		layer -= backgroundLayer
	}
	if layer > topdownLayer {
		layer -= topdownLayer
	}
	if layer > effectsLayer {
		layer -= effectsLayer
	}

	layer = plane*10_000 + layer*1000

	// When mobs are on the same Layer with object they are always rendered above them (BYOND specific stuff).
	if dm.IsPath(p.Path(), "/obj") {
		layer += 100
	} else if dm.IsPath(p.Path(), "/mob") {
		layer += 10
	}

	return layer
}

// APHELION EDIT ADDITION START - BATCH UNIT PREPARATION
// PlacementOffset resolves the prefab's integer pixel and step offsets.
func PlacementOffset(p *dmmprefab.Prefab) util.Point {
	pixelX, _ := p.Vars().Int("pixel_x")
	pixelY, _ := p.Vars().Int("pixel_y")
	stepX, _ := p.Vars().Int("step_x")
	stepY, _ := p.Vars().Int("step_y")
	pixelW, _ := p.Vars().Int("pixel_w")
	pixelZ, _ := p.Vars().Int("pixel_z")
	return util.Point{X: pixelX + stepX + pixelW, Y: pixelY + stepY + pixelZ}
}

// WithPlacement reuses this unit's appearance for another instance of the same
// prefab. Offsets stay integral until final placement to preserve pixel rounding.
func (u Unit) WithPlacement(x, y int, i *dmminstance.Instance, iconSize int, offset util.Point) Unit {
	u.instance = i
	x1 := float32((x-1)*iconSize + offset.X)
	y1 := float32((y-1)*iconSize + offset.Y)
	u.viewBounds = util.Bounds{
		X1: x1, Y1: y1,
		X2: x1 + float32(u.sprite.IconWidth()),
		Y2: y1 + float32(u.sprite.IconHeight()),
	}
	return u
}

// APHELION EDIT ADDITION END
