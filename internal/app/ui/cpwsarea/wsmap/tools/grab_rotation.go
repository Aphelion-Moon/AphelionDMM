// APHELION EDIT ADDITION START - SELECTION ROTATION
package tools

import (
	"fmt"
	"sdmm/internal/aphelion/editing"
	"sdmm/internal/util"
)

func (t *ToolGrab) SelectionLevel() int { return t.fillStart.Z }

// Rotate keeps selection bookkeeping in sync with a committed editor transform.
func (t *ToolGrab) Rotate(clockwise bool, transform func(util.Bounds, int, bool) (util.Bounds, error)) error {
	if !t.HasSelectedArea() || !t.Stale() {
		return fmt.Errorf("finish selecting or moving the area before rotating")
	}
	before := t.Selection()
	return t.trackSelectionMask(before, false, func() (editing.Selection, error) {
		var err error
		if owner, ok := ed.(interface {
			RotateSelectionMask(editing.Selection, bool) (util.Bounds, error)
		}); ok && before.Sparse() {
			_, err = owner.RotateSelectionMask(before, clockwise)
		} else {
			_, err = transform(t.fillArea, t.fillStart.Z, clockwise)
		}
		return before.Rotate(clockwise), err
	})
}

// APHELION EDIT ADDITION END
