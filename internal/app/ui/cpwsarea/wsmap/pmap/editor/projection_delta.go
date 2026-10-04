// APHELION EDIT ADDITION START - SPARSE SESSION PROJECTION
package editor

import (
	"fmt"

	"sdmm/internal/aphelion/collab/client"
	"sdmm/internal/aphelion/collab/executor"
	"sdmm/internal/aphelion/collab/mapadapter"
	"sdmm/internal/aphelion/collab/model"
	"sdmm/internal/util"
)

type incrementalProjectionExecutor interface {
	executor.Executor
	ProjectionChanges() <-chan client.ProjectionUpdate
}

func (e *Editor) processProjectionChanges(execution incrementalProjectionExecutor) {
	var latest *client.ProjectionUpdate
	for {
		select {
		case update := <-execution.ProjectionChanges():
			if latest != nil {
				update = update.Coalesce(*latest)
			}
			latest = &update
		default:
			if latest == nil {
				return
			}
			if err := e.installProjectionChange(*latest); err != nil {
				e.collaborationErr = err
				e.reportCollaborationError("Unable to project collaborative map", err)
				return
			}
			e.revalidateSelectionMovePreview()
			return
		}
	}
}

func (e *Editor) installProjectionChange(update client.ProjectionUpdate) error {
	capture := update.Capture()
	document, revision, environment, _, err := capture.OperationBase()
	if err != nil {
		return err
	}
	if document != e.authoritative.DocumentID || environment != e.authoritative.EnvironmentHash || revision < e.authoritative.Revision {
		return fmt.Errorf("session projection does not match displayed authority")
	}
	x, y, z := capture.Dimensions()
	if update.Full() || e.networkView == nil || x != e.dmm.MaxX || y != e.dmm.MaxY || z != e.dmm.MaxZ {
		visible, err := capture.VisibleSnapshot()
		if err != nil {
			return err
		}
		if err := mapadapter.ApplyWithEnvironment(e.dmm, visible, e.app.LoadedEnvironment()); err != nil {
			return err
		}
		e.setAuthoritative(capture.AcceptedSnapshot())
		e.refreshCollaborationView(e.pMap.ActiveLevel(), nil, visible)
		e.networkView = &update
		return nil
	}
	coords := update.ChangedCoords()
	previous := e.networkView.Capture()
	byLevel := make(map[int][]util.Point)
	for _, coord := range coords {
		before, _ := previous.VisibleTile(coord)
		after, _ := capture.VisibleTile(coord)
		if err := e.applyPasteTileState(coord, after); err != nil {
			return err
		}
		e.updateAreaDelta(model.TileChange{Coord: coord, Before: before, After: after})
		accepted, exists := capture.AcceptedTile(coord)
		// Speculation may fill an absent authority coordinate. It belongs only
		// to the display until accepted; materializing an empty tile would alter
		// the sparse authority's canonical hash.
		if exists {
			e.authoritativeTiles[coord] = accepted
			if index, found := e.authoritativePositions[coord]; found {
				e.authoritative.Tiles[index].State = accepted
			} else {
				e.authoritativePositions[coord] = len(e.authoritative.Tiles)
				e.authoritative.Tiles = append(e.authoritative.Tiles, model.Tile{Coord: coord, State: accepted})
			}
		}
		byLevel[coord.Z] = append(byLevel[coord.Z], util.Point{X: coord.X, Y: coord.Y, Z: coord.Z})
	}
	if len(coords) != 0 || revision != e.authoritative.Revision {
		e.mapViewGeneration++
	}
	e.authoritative.Revision = revision
	e.networkView = &update
	if len(coords) == 0 {
		return nil
	}
	e.syncPasteSnapshot(coords)
	for level, points := range byLevel {
		e.updateBucket(level, points)
	}
	e.app.SyncPrefabs()
	e.app.SyncVarEditor()
	return nil
}

// APHELION EDIT ADDITION END
