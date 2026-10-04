// Package renderprep bounds appearance reuse to a single synchronous geometry rebuild.
package renderprep

import (
	"sdmm/internal/app/render/bucket/level/chunk/unit"
	"sdmm/internal/dmapi/dmmap/dmmdata/dmmprefab"
	"sdmm/internal/dmapi/dmmap/dmminstance"
	"sdmm/internal/util"
)

const maxUnitPrefabs = 128

type preparedUnit struct {
	prototype unit.Unit
	offset    util.Point
}

// UnitBatch reuses immutable prefab appearances within one chunk rebuild.
// Its zero value is ready to use. Do not retain it across edits or icon lifetimes.
type UnitBatch struct {
	prefabs map[*dmmprefab.Prefab]preparedUnit
}

func (b *UnitBatch) Make(x, y int, instance *dmminstance.Instance, iconSize int) unit.Unit {
	prefab := instance.Prefab()
	if prepared, ok := b.prefabs[prefab]; ok {
		return prepared.prototype.WithPlacement(x, y, instance, iconSize, prepared.offset)
	}
	result := unit.Make(x, y, instance, iconSize)
	if len(b.prefabs) < maxUnitPrefabs {
		if b.prefabs == nil {
			b.prefabs = make(map[*dmmprefab.Prefab]preparedUnit, 32)
		}
		b.prefabs[prefab] = preparedUnit{prototype: result, offset: unit.PlacementOffset(prefab)}
	}
	return result
}
