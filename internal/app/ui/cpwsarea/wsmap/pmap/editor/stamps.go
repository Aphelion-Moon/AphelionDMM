// APHELION EDIT ADDITION START - SELECTION STAMPS
package editor

import (
	"fmt"
	"sdmm/internal/aphelion/collab/mapadapter"
	"sdmm/internal/aphelion/editing/stamps"
	"sdmm/internal/util"
)

func (e *Editor) CaptureStamp(name string, coords []util.Point) (*stamps.Stamp, error) {
	if !e.CanStartMapEdit() || len(e.unresolvedSubmissions) != 0 {
		return nil, fmt.Errorf("finish the current edit before capturing a stamp")
	}
	if pending, ok := e.executor.(pendingExecutor); ok && pending.HasUnacknowledgedOperations() {
		return nil, fmt.Errorf("wait for acknowledgement before capturing a stamp")
	}
	environmentHash, err := mapadapter.EnvironmentHash(e.app.LoadedEnvironment())
	if err != nil {
		return nil, err
	}
	return stamps.Capture(name, environmentHash, e.dmm, coords, e.app.PathsFilter())
}

func (e *Editor) StampEnvironmentMatches(stamp *stamps.Stamp) bool {
	if stamp == nil {
		return false
	}
	hash, err := mapadapter.EnvironmentHash(e.app.LoadedEnvironment())
	return err == nil && hash == stamp.EnvironmentHash()
}

// StartStamp does not change the clipboard or commit an edit. Explicit consent
// to a different environment permits only preview; ordinary placement validation
// and authority still decide whether a resulting map operation can commit.
func (e *Editor) StartStamp(stamp *stamps.Stamp, allowDifferentEnvironment bool) error {
	if stamp == nil || !e.CanStartMapEdit() {
		return fmt.Errorf("load a stamp and finish or cancel the current edit before placing")
	}
	hash, err := mapadapter.EnvironmentHash(e.app.LoadedEnvironment())
	if err != nil {
		return err
	}
	if hash != stamp.EnvironmentHash() && !allowDifferentEnvironment {
		return fmt.Errorf("stamp comes from a different environment; review and acknowledge before previewing")
	}
	return e.startPlacement(stamp.PasteData(e.app.PathsFilter(), e.app.LoadedEnvironment()))
}

// APHELION EDIT ADDITION END
