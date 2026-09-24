package editing

import (
	"fmt"
	"sdmm/internal/dmapi/dmmap/dmmdata/dmmprefab"
)

// HeldPrefab preserves the original representation; turns are a bounded pose,
// not a history of edits to inherited or explicitly written overrides.
type HeldPrefab struct {
	source, value *dmmprefab.Prefab
	turns         uint8
}

func (h *HeldPrefab) SetSource(p *dmmprefab.Prefab) {
	if h.source != p {
		h.source = p
		h.value = p
		h.turns = 0
	}
}
func (h *HeldPrefab) Value() *dmmprefab.Prefab { return h.value }
func (h *HeldPrefab) Rotate(clockwise bool) error {
	if h.source == nil {
		return fmt.Errorf("no held prefab")
	}
	turns := (h.turns + 3) % 4
	if clockwise {
		turns = (h.turns + 1) % 4
	}
	value := h.source
	for n := uint8(0); n < turns; n++ {
		var err error
		value, err = rotatePrefab(value, true)
		if err != nil {
			return err
		}
	}
	h.value = value
	h.turns = turns
	return nil
}
