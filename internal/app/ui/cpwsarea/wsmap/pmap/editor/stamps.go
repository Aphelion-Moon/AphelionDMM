// APHELION EDIT ADDITION START - SELECTION STAMPS
package editor

import (
	"context"
	"fmt"
	"sdmm/internal/aphelion/collab/mapadapter"
	"sdmm/internal/aphelion/editing/stamps"
	"sdmm/internal/aphelion/resources"
	"sdmm/internal/dmapi/dmmap"
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
	return stamps.CaptureWithBudget(name, environmentHash, e.dmm, coords, e.app.PathsFilter(), e.editWorkBudget())
}

func (e *Editor) StampEnvironmentMatches(stamp *stamps.Stamp) bool {
	if stamp == nil {
		return false
	}
	return e.authoritative.EnvironmentHash != "" && e.authoritative.EnvironmentHash == stamp.EnvironmentHash()
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
	lease, err := stamp.Acquire()
	if err != nil {
		return err
	}
	tileCount := lease.TileCount()
	current := e.app.PathsFilter().Copy()
	environment := e.app.LoadedEnvironment()
	budget := e.editWorkBudget()
	factory := func(ctx context.Context) ([]dmmap.Tile, func(string) bool, *resources.Reservation, error) {
		data, reservation, err := lease.PasteData(ctx, &current, environment, budget)
		if err != nil {
			return nil, nil, nil, err
		}
		filter := data.Filter.Copy()
		lease.Release()
		return data.Buffer, filter.IsVisiblePath, reservation, nil
	}
	return e.startPlacementFromFactory(tileCount, factory, lease.Release)
}

// APHELION EDIT ADDITION END
