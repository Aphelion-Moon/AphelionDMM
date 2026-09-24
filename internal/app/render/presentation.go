// APHELION EDIT ADDITION START - PLACEMENT PRESENTATION
package render

import (
	"sdmm/internal/app/render/brush"
	"sdmm/internal/app/render/bucket/level/chunk/unit"
	"sdmm/internal/dmapi/dmicon"
	"sdmm/internal/dmapi/dmmap/dmminstance"
	"sdmm/internal/util"
	"sort"
)

// Appearance retains existing sprite/animation facilities without retaining a
// live document instance. It can never enter committed picking or properties.
type Appearance struct {
	Coord      util.Point
	Path       string
	Bounds     util.Bounds
	Sprite     *dmicon.Sprite
	Layer      float32
	R, G, B, A float32
}

func PrepareAppearance(coord util.Point, instance *dmminstance.Instance, iconSize int) Appearance {
	u := unit.Make(coord.X, coord.Y, instance, iconSize)
	return Appearance{Coord: util.Point{X: coord.X - 1, Y: coord.Y - 1}, Path: instance.Prefab().Path(), Bounds: u.ViewBounds(), Sprite: u.Sprite(), Layer: u.Layer(), R: u.R(), G: u.G(), B: u.B(), A: u.A()}
}

// Presentation is owned by the UI thread. Sprites are prepared once; translating
// Anchor does not touch base chunks, instances or the sprite cache.
type Presentation struct {
	Anchor     util.Point
	IconSize   int
	Layers     []float32
	groups     map[float32][]*appearanceGroup
	groupIndex map[appearanceGroupKey]*appearanceGroup
	Suppress   func(unit.Unit) bool
	Visible    func(Appearance) bool
	Ready      bool
}

type appearanceGroupKey struct {
	layer float32
	x, y  int
}
type appearanceGroup struct {
	bounds  util.Bounds
	sprites []Appearance
}

func (p *Presentation) Add(a Appearance) {
	if p.groups == nil {
		p.groups = make(map[float32][]*appearanceGroup)
		p.groupIndex = make(map[appearanceGroupKey]*appearanceGroup)
	}
	if _, exists := p.groups[a.Layer]; !exists {
		p.Layers = append(p.Layers, a.Layer)
	}
	key := appearanceGroupKey{layer: a.Layer, x: a.Coord.X / 32, y: a.Coord.Y / 32}
	group := p.groupIndex[key]
	if group == nil {
		group = &appearanceGroup{bounds: a.Bounds}
		p.groupIndex[key] = group
		p.groups[a.Layer] = append(p.groups[a.Layer], group)
	} else {
		group.bounds = util.Bounds{X1: min(group.bounds.X1, a.Bounds.X1), Y1: min(group.bounds.Y1, a.Bounds.Y1), X2: max(group.bounds.X2, a.Bounds.X2), Y2: max(group.bounds.Y2, a.Bounds.Y2)}
	}
	group.sprites = append(group.sprites, a)
}

func (p *Presentation) Finish() {
	sort.Slice(p.Layers, func(i, j int) bool { return p.Layers[i] < p.Layers[j] })
	p.Ready = true
	p.groupIndex = nil
}

func (r *Render) SetPresentation(p *Presentation) { r.presentation = p }

func (p *Presentation) drawLayer(layer float32, view util.Bounds) {
	dx, dy := float32((p.Anchor.X-1)*p.IconSize), float32((p.Anchor.Y-1)*p.IconSize)
	for _, group := range p.groups[layer] {
		if !dmicon.Cache.ExpandPendingBounds(group.bounds).Plus(dx, dy).ContainsV(view) {
			continue
		}
		for _, a := range group.sprites {
			bounds := a.Bounds.Plus(dx, dy)
			bounds.X2 = bounds.X1 + float32(a.Sprite.IconWidth())
			bounds.Y2 = bounds.Y1 + float32(a.Sprite.IconHeight())
			if !bounds.ContainsV(view) || p.Visible != nil && !p.Visible(a) {
				continue
			}
			brush.RectTexturedV(bounds.X1, bounds.Y1, bounds.X2, bounds.Y2, a.R, a.G, a.B, a.A, a.Sprite.Texture(), a.Sprite.U1, a.Sprite.V1, a.Sprite.U2, a.Sprite.V2)
		}
	}
}

// Merging sorted layer streams keeps ghost sprites between retained base
// layers; a translucent overlay after the base would give the wrong result.
func eachPresentationLayer(base, ghost []float32, visit func(float32)) {
	i, j := 0, 0
	for i < len(base) || j < len(ghost) {
		var layer float32
		if j == len(ghost) || i < len(base) && base[i] < ghost[j] {
			layer = base[i]
			i++
		} else if i == len(base) || ghost[j] < base[i] {
			layer = ghost[j]
			j++
		} else {
			layer = base[i]
			i++
			j++
		}
		visit(layer)
	}
}

// APHELION EDIT ADDITION END
