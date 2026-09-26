// APHELION EDIT ADDITION START - GEOMETRY CAPACITY
package render

import (
	"sdmm/internal/aphelion/resources"
	"sdmm/internal/app/render/bucket/level/chunk"
	"sdmm/internal/dmapi/dmmap"
	"sdmm/internal/util"
)

// Disposable geometry has a process-wide cap independent of authoritative map
// storage. Leave room for icons, edit proposals and temporary rebuilds.
var geometryCapacity = resources.NewHostBudget(256<<20, 8)
var geometryOwners = map[*Render]struct{}{}

type geometryAllocation struct {
	lease    *resources.Reservation
	chunks   map[util.Point]uint64
	metadata uint64
	total    uint64
}

func (r *Render) releaseGeometry() {
	for _, entry := range r.geometry {
		entry.lease.Release()
	}
	r.geometry = nil
	delete(geometryOwners, r)
}

func (r *Render) evictGeometry(z int) {
	// Retained submissions reference these chunks; drop them before requeueing.
	r.clearRetainedScene()
	if entry := r.geometry[z]; entry != nil {
		entry.lease.Release()
		delete(r.geometry, z)
	}
	r.bucket.DropLevel(z)
	delete(r.levelReady, z)
	if r.levelBuildDmm != nil {
		r.levelBuilds[z] = &levelBuild{dmm: r.levelBuildDmm, level: z, generation: r.levelBuildGeneration}
	}
}

func (r *Render) admitGeometry(dmm *dmmap.Dmm, z int, points []util.Point, metadataOnly bool) bool {
	if r.geometry == nil {
		r.geometry = map[int]*geometryAllocation{}
		geometryOwners[r] = struct{}{}
	}
	entry := r.geometry[z]
	if entry == nil {
		width, height := (dmm.MaxX+chunk.Size)/(chunk.Size+1), (dmm.MaxY+chunk.Size)/(chunk.Size+1)
		entry = &geometryAllocation{chunks: map[util.Point]uint64{}, metadata: uint64(width*height)*512 + 1024}
		entry.total = entry.metadata
	}
	updates := map[util.Point]uint64{}
	estimate := func(point util.Point) {
		x, y := 1+(point.X-1)/(chunk.Size+1)*(chunk.Size+1), 1+(point.Y-1)/(chunk.Size+1)*(chunk.Size+1)
		key := util.Point{X: x, Y: y, Z: z}
		if _, seen := updates[key]; seen {
			return
		}
		bytes := uint64(256)
		for cx := x; cx <= min(x+chunk.Size, dmm.MaxX); cx++ {
			for cy := y; cy <= min(y+chunk.Size, dmm.MaxY); cy++ {
				// Covers units, layer indexes and old/new arrays during replacement.
				for _, instance := range dmm.GetTile(util.Point{X: cx, Y: cy, Z: z}).Instances() {
					if r.bucket.InstanceFilter == nil || r.bucket.InstanceFilter(instance) {
						bytes += 256
					}
				}
			}
		}
		updates[key] = bytes
	}
	if !metadataOnly {
		if len(points) == 0 {
			for x := 1; x <= dmm.MaxX; x += chunk.Size + 1 {
				for y := 1; y <= dmm.MaxY; y += chunk.Size + 1 {
					estimate(util.Point{X: x, Y: y, Z: z})
				}
			}
		} else {
			for _, point := range points {
				if point.Z == z && dmm.HasTile(point) {
					estimate(point)
				}
			}
		}
	}
	needed := entry.total
	for key, bytes := range updates {
		needed = needed - entry.chunks[key] + bytes
	}
	admit := func() bool {
		var err error
		if entry.lease == nil {
			entry.lease, err = geometryCapacity.Reserve(needed)
		} else {
			err = entry.lease.Resize(needed)
		}
		return err == nil
	}
	for !admit() {
		var victim *Render
		victimZ := 0
		priority := -1
		for owner := range geometryOwners {
			for level := range owner.geometry {
				if owner == r && level == z {
					continue
				}
				p := owner.levelBuildPriority(level)
				if owner == r && p <= r.levelBuildPriority(z) {
					continue
				}
				if owner != r {
					if !owner.geometryDrawn.Before(r.geometryDrawn) {
						continue
					}
					p += 30000
				}
				if p > priority {
					victim, victimZ, priority = owner, level, p
				}
			}
		}
		if victim == nil {
			r.geometryWaiting = true
			return false
		}
		victim.evictGeometry(victimZ)
	}
	r.geometryWaiting = false
	entry.total = needed
	for key, bytes := range updates {
		entry.chunks[key] = bytes
	}
	r.geometry[z] = entry
	return true
}

func (r *Render) GeometryWaiting() bool { return r.geometryWaiting }

// APHELION EDIT ADDITION END
