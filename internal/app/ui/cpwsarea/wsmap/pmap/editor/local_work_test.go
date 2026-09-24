package editor

import (
	"context"
	"sdmm/internal/aphelion/collab/engine"
	"sdmm/internal/aphelion/collab/model"
	"sdmm/internal/aphelion/editing"
	"sdmm/internal/aphelion/resources"
	"sdmm/internal/util"
	"testing"
	"time"
)

func TestLocalWorkOwnsDocumentUntilPublication(t *testing.T) {
	e := selectionEditor(t)
	a := e.app.(*editorTestApp)
	a.runLater = make(chan func(), 8)
	started, release := make(chan struct{}), make(chan struct{})
	done := 0
	before := model.CloneTileState(e.authoritative.Tiles[0].State)
	after := model.CloneTileState(before)
	after.Prefabs[2].Vars["dir"] = "4"
	err := e.startLocalWork(e.executor.(localEditExecutor), true, 4096, func(ctx context.Context, _ *resources.Reservation) ([]model.TileChange, error) {
		close(started)
		<-release
		return []model.TileChange{{Coord: e.authoritative.Tiles[0].Coord, Before: before, After: after}}, nil
	}, func(_ engine.LocalAcceptance, _ []model.TileChange, err error) {
		if err != nil {
			t.Error(err)
		}
		done++
	})
	if err != nil {
		t.Fatal(err)
	}
	select {
	case <-started:
	case <-time.After(time.Second):
		t.Fatal("worker did not start")
	}
	if _, err := e.SaveSnapshot(context.Background()); err == nil {
		t.Fatal("save entered unfinished work")
	}
	if e.TryBeginTileChange(util.Point{X: 1, Y: 1, Z: 1}) || e.CanChangeMapSize() {
		t.Fatal("conflicting edit entered worker-owned document")
	}
	if _, ready := e.MapViewVersion(); ready {
		t.Fatal("query exposed incomplete authority")
	}
	area := util.Bounds{X1: 1, Y1: 1, X2: 1, Y2: 1}
	if _, err := e.RotateSelection(area, 1, true); err == nil {
		t.Error("rotation entered unfinished work")
	}
	if _, err := e.MirrorSelection(area, 1, editing.MirrorHorizontal); err == nil {
		t.Error("mirror entered unfinished work")
	}
	assertEditorDirection(t, e.dmm, "2")
	if e.authoritative.Revision != 0 {
		t.Fatal("revision advanced before acceptance")
	}
	close(release)
	for done == 0 {
		a.runScheduled(t)
	}
	if done != 1 || e.localWork != nil || e.editWorkBudget().Used() != 0 {
		t.Fatal("completion or lease leaked")
	}
	if e.authoritative.Revision != 1 {
		t.Fatal("accepted revision not installed")
	}
	assertEditorDirection(t, e.dmm, "4")
}

func TestCloseAfterLocalAcceptanceFencesDisplayAndReleasesWork(t *testing.T) {
	e := selectionEditor(t)
	a := e.app.(*editorTestApp)
	a.runLater = make(chan func(), 8)
	before := model.CloneTileState(e.authoritative.Tiles[0].State)
	after := model.CloneTileState(before)
	after.Prefabs[2].Vars["dir"] = "4"
	coord := e.authoritative.Tiles[0].Coord
	execution := e.executor.(localEditExecutor)
	done := 0
	err := e.startLocalWork(execution, true, 16384, func(context.Context, *resources.Reservation) ([]model.TileChange, error) {
		return []model.TileChange{{Coord: coord, Before: before, After: after}}, nil
	}, func(_ engine.LocalAcceptance, _ []model.TileChange, err error) {
		if err == nil {
			t.Error("closed work reported display publication")
		}
		done++
	})
	if err != nil {
		t.Fatal(err)
	}
	var publish func()
	select {
	case publish = <-a.runLater:
	case <-time.After(time.Second):
		t.Fatal("acceptance did not queue")
	}
	version, err := execution.LocalVersion(context.Background())
	if err != nil || version.Revision != 1 {
		t.Fatal("test did not reach accepted boundary", err)
	}
	e.Close()
	publish()
	publish() // A duplicated/stale completion cannot install or notify twice.
	if done != 1 || e.editWorkBudget().Used() != 0 {
		t.Fatal("work lifetime or completion leaked", done)
	}
	assertEditorDirection(t, e.dmm, "2")
}
