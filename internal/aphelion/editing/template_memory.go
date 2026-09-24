package editing

import "sdmm/internal/dmapi/dmmap"

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
