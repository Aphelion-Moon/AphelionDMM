package rendercache

import (
	"math"
	"sort"

	"sdmm/internal/app/render/bucket/level/chunk/unit"
	"sdmm/internal/dmapi/dmicon"
)

// Dependencies describe only appearances emitted into this submission.
type Dependencies struct {
	IconLifetime uint64
	Icons        map[string]uint64
}

// GetWithDependencies rechecks stale global policies without uploading unchanged
// geometry. Chunk changes and texture disposal always invalidate the entry.
func (c *Cache) GetWithDependencies(key Key, versions Versions, visible func(unit.Unit) bool, icons *dmicon.IconsCache) (*Entry, bool) {
	return c.get(key, versions, func(entry *Entry) bool {
		if entry.Versions.Chunk != versions.Chunk || entry.dependencies == nil {
			return false
		}
		if entry.Versions.Appearance != versions.Appearance {
			if icons == nil || entry.dependencies.IconLifetime != icons.Lifetime() {
				return false
			}
			for icon, revision := range entry.dependencies.Icons {
				if icons.IconRevision(icon) != revision {
					return false
				}
			}
		}
		if entry.Versions.Policy != versions.Policy {
			if !entry.indexComplete || key.Chunk == nil {
				return false
			}
			count := 0
			// Include previously hidden candidates so an unhide cannot reuse an
			// empty or incomplete submission.
			for _, u := range key.Chunk.UnitsByLayers[math.Float32frombits(key.Layer)] {
				id := u.Instance().Id()
				index := sort.Search(len(entry.unitIDs), func(i int) bool { return entry.unitIDs[i] >= id })
				wasVisible := index < len(entry.unitIDs) && entry.unitIDs[index] == id
				isVisible := visible == nil || visible(u)
				if wasVisible != isVisible {
					return false
				}
				if isVisible {
					count++
				}
			}
			if count != len(entry.unitIDs) {
				return false
			}
		}
		return true
	})
}
