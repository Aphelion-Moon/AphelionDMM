package chunk

import (
	"sdmm/internal/app/render/bucket/level/chunk/unit"
	"sdmm/internal/dmapi/dmmap"
	// APHELION EDIT ADDITION START - OCCURRENCE GEOMETRY
	"sdmm/internal/dmapi/dmmap/dmminstance"
	// APHELION EDIT ADDITION END
	"sdmm/internal/util"

	"github.com/rs/zerolog/log"
)

// Size is a maximum number of tiles per axis for a single Chunk.
const Size = 24

// Chunk stores the actual data to render.
// It stores two types of bounds: view and map.
// View bounds are a visual bounds which are used to ignore the chunk if it's out of the user viewport.
// Map bounds are coordinate points of tiles in the chunk.
type Chunk struct {
	ViewBounds, MapBounds util.Bounds
	// APHELION EDIT ADDITION START - RENDER CULLING
	baseViewBounds util.Bounds
	// APHELION EDIT ADDITION END

	UnitsByLayers map[float32][]unit.Unit
	// APHELION EDIT ADDITION START - RETAINED SUBMISSIONS
	revision uint64
	// APHELION EDIT ADDITION END
}

// APHELION EDIT ADDITION START - RETAINED SUBMISSIONS
// Revision changes whenever Update replaces this chunk's unit geometry.
func (c *Chunk) Revision() uint64 { return c.revision }

// APHELION EDIT ADDITION END

func New(x1, y1, x2, y2, iconSize float32) *Chunk {
	return &Chunk{
		ViewBounds: util.Bounds{
			X1: (x1 - 1) * iconSize,
			Y1: (y1 - 1) * iconSize,
			X2: x2 * iconSize,
			Y2: y2 * iconSize,
		},
		// APHELION EDIT ADDITION START - RENDER CULLING
		baseViewBounds: util.Bounds{X1: (x1 - 1) * iconSize, Y1: (y1 - 1) * iconSize, X2: x2 * iconSize, Y2: y2 * iconSize},
		// APHELION EDIT ADDITION END
		MapBounds: util.Bounds{
			X1: x1,
			Y1: y1,
			X2: x2,
			Y2: y2,
		},
	}
}

// Update will update internal data of the current chunk.
// Basically, we will create units for every tile in the chunk.
// APHELION EDIT CHANGE - OCCURRENCE GEOMETRY - ORIGINAL: func (c *Chunk) Update(dmm *dmmap.Dmm, level int) {
func (c *Chunk) Update(dmm *dmmap.Dmm, level int, filters ...func(*dmminstance.Instance) bool) {
	// Create a storage for our units by Layers with initial capacity.
	// Inner slices are created with initial capacity as well.
	unitsByLayers := make(map[float32][]unit.Unit, len(c.UnitsByLayers))
	for layer := range c.UnitsByLayers {
		unitsByLayers[layer] = make([]unit.Unit, 0, len(c.UnitsByLayers[layer]))
	}
	// APHELION EDIT ADDITION START - RENDER CULLING
	viewBounds := c.baseViewBounds
	// APHELION EDIT ADDITION END

	for x := c.MapBounds.X1; x <= c.MapBounds.X2; x++ {
		for y := c.MapBounds.Y1; y <= c.MapBounds.Y2; y++ {
			x, y := int(x), int(y)
			for _, i := range dmm.GetTile(util.Point{X: x, Y: y, Z: level}).Instances() {
				// APHELION EDIT ADDITION START - OCCURRENCE GEOMETRY
				if len(filters) > 0 && filters[0] != nil && !filters[0](i) {
					continue
				}
				// APHELION EDIT ADDITION END
				u := unit.Make(x, y, i, dmmap.WorldIconSize)
				unitsByLayers[u.Layer()] = append(unitsByLayers[u.Layer()], u)
				// APHELION EDIT ADDITION START - RENDER CULLING
				viewBounds = includeViewBounds(viewBounds, u.ViewBounds())
				// APHELION EDIT ADDITION END
			}
		}
	}

	c.UnitsByLayers = unitsByLayers
	// APHELION EDIT ADDITION START - RENDER CULLING
	c.ViewBounds = viewBounds
	// APHELION EDIT ADDITION END
	// APHELION EDIT ADDITION START - RETAINED SUBMISSIONS
	c.revision++
	// APHELION EDIT ADDITION END
	// APHELION EDIT CHANGE - QUIET FRAME WORK - ORIGINAL: log.Printf("chunk level [%d] updated: %v", level, c.MapBounds)
	log.Debug().Int("level", level).Interface("bounds", c.MapBounds).Msg("chunk updated")
}

// APHELION EDIT ADDITION START - RENDER CULLING
func includeViewBounds(bounds, addition util.Bounds) util.Bounds {
	if addition.X1 < bounds.X1 {
		bounds.X1 = addition.X1
	}
	if addition.Y1 < bounds.Y1 {
		bounds.Y1 = addition.Y1
	}
	if addition.X2 > bounds.X2 {
		bounds.X2 = addition.X2
	}
	if addition.Y2 > bounds.Y2 {
		bounds.Y2 = addition.Y2
	}
	return bounds
}

// APHELION EDIT ADDITION END
