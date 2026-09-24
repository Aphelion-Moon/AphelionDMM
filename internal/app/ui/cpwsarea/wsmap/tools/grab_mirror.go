// APHELION EDIT ADDITION START - SELECTION MIRROR
package tools

import (
	"fmt"

	"sdmm/internal/aphelion/editing"
	"sdmm/internal/util"
)

func (t *ToolGrab) Mirror(axis editing.MirrorAxis, transform func(util.Bounds, int, editing.MirrorAxis) (util.Bounds, error)) error {
	if !t.HasSelectedArea() || !t.Stale() {
		return fmt.Errorf("finish selecting or moving the area before mirroring")
	}
	before := t.Selection()
	return t.trackSelectionMask(before, false, func() (editing.Selection, error) {
		var err error
		if owner, ok := ed.(interface {
			MirrorSelectionMask(editing.Selection, editing.MirrorAxis) (util.Bounds, error)
		}); ok && before.Sparse() {
			_, err = owner.MirrorSelectionMask(before, axis)
		} else {
			_, err = transform(t.fillArea, t.fillStart.Z, axis)
		}
		return before.Mirror(axis), err
	})
}

// APHELION EDIT ADDITION END
