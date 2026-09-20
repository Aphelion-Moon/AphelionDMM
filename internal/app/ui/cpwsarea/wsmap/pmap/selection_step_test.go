package pmap

import (
	"sdmm/internal/app/prefs"
	"sdmm/internal/util"
	"testing"
)

type selectionStepApp struct {
	App
	step int
}

func (app *selectionStepApp) Prefs() prefs.Prefs {
	return prefs.Prefs{Editor: prefs.Editor{SelectionMoveStep: app.step}}
}

func TestSelectionNudgeReadsCurrentGridStep(t *testing.T) {
	app := &selectionStepApp{step: 4}
	pane := &PaneMap{app: app}
	for _, direction := range selectionNudges {
		want := util.Point{X: direction.shift.X * 4, Y: direction.shift.Y * 4}
		if got := pane.selectionNudgeShift(direction.shift); got != want {
			t.Fatalf("shift = %v, want %v", got, want)
		}
	}
	app.step = 8
	if got := pane.selectionNudgeShift(util.Point{X: -1}); got != (util.Point{X: -8}) {
		t.Fatal("changed preference did not take effect")
	}
	app.step = 0
	if got := pane.selectionNudgeShift(util.Point{Y: 1}); got != (util.Point{Y: 1}) {
		t.Fatal("old preference did not preserve one-tile default")
	}
}
