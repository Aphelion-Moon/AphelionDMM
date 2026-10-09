// APHELION EDIT ADDITION START - OFFSET PREVIEW INSPECTION
package render

import "sdmm/internal/util"

// UnitBounds lists the world-space bounds built for an instance on a level, one
// entry per unit holding it. It reads CPU geometry only, so tests can verify
// live previews, and stale duplicates, without drawing.
func (r *Render) UnitBounds(level int, instanceID uint64) []util.Bounds {
	built := r.bucket.Level(level)
	if built == nil {
		return nil
	}
	var found []util.Bounds
	for _, c := range built.Chunks {
		for _, units := range c.UnitsByLayers {
			for _, u := range units {
				if u.Instance().Id() == instanceID {
					found = append(found, u.ViewBounds())
				}
			}
		}
	}
	return found
}

// APHELION EDIT ADDITION END
