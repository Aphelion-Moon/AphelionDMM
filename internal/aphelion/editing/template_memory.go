package editing

import (
	"sdmm/internal/aphelion/collab/model"
	"sdmm/internal/dmapi/dmmap"
)

// EstimateMovePreparationMemory reserves a conservative allowance per selected
// tile before the asynchronous authority subset is copied.
func EstimateMovePreparationMemory(tileCount int) uint64 {
	return saturatingAdd(1<<20, saturatingMul(uint64(tileCount), 1024))
}

// EstimateMovePreparationMemoryForSource grows admission before the next
// selected tile is cloned. The base accounts for fixed session/index overhead;
// sourceBytes conservatively covers both the temporary read and owned payload.
func EstimateMovePreparationMemoryForSource(tileCount int, sourceBytes uint64) uint64 {
	return saturatingAdd(EstimateMovePreparationMemory(tileCount), saturatingMul(sourceBytes, 4))
}

func EstimateMoveTileMemory(state model.TileState) uint64 {
	bytes := uint64(256)
	for _, prefab := range state.Prefabs {
		bytes = saturatingAdd(bytes, uint64(512+len(prefab.Path)+len(prefab.StableID)))
		for name, value := range prefab.Vars {
			bytes = saturatingAdd(bytes, uint64(128+len(name)+len(value)))
		}
	}
	return bytes
}

// EstimateMovePayloadMemory covers owned visible states and their presentation
// records while a move preview or commit is live.
func EstimateMovePayloadMemory(payload *MovePayload) uint64 {
	if payload == nil {
		return 0
	}
	bytes := uint64(1 << 20)
	for _, tile := range payload.tiles {
		bytes = saturatingAdd(bytes, 256)
		bytes = saturatingAdd(bytes, EstimateMoveTileMemory(tile.original))
		for _, prefab := range tile.state.Prefabs {
			bytes = saturatingAdd(bytes, uint64(512+len(prefab.Path)+len(prefab.StableID)))
			for name, value := range prefab.Vars {
				bytes = saturatingAdd(bytes, uint64(128+len(name)+len(value)))
			}
		}
	}
	return saturatingMul(bytes, 4)
}

// EstimatePlacementSourceCopyMemory covers owned tile and instance headers
// created when an asynchronous placement assigns stable source identities.
// Prefab/value data stays shared and immutable.
func EstimatePlacementSourceCopyMemory(source []dmmap.Tile) uint64 {
	bytes := uint64(1 << 20)
	for _, tile := range source {
		bytes = saturatingAdd(bytes, 96)
		bytes = saturatingAdd(bytes, saturatingMul(uint64(len(tile.Instances())), 128))
	}
	return bytes
}

// EstimatePlacementPresentationMemory covers original/transformed instances,
// typed channel values, a previous complete appearance and its replacement.
// Variable maps are per-instance, even when their source prefab is interned.
func EstimatePlacementPresentationMemory(source []dmmap.Tile) uint64 {
	bytes := saturatingMul(EstimatePlacementSourceCopyMemory(source), 6)
	bytes = saturatingAdd(bytes, saturatingMul(estimateTemplatePayload(source), 6))
	return saturatingAdd(bytes, saturatingMul(uint64(len(source)), 512))
}
