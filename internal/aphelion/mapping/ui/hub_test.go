package mappingui

import (
	"sdmm/internal/aphelion/mapping"
	"sdmm/internal/app/command"
	"sdmm/internal/app/ui/cpwsarea/workspace"
	"sdmm/internal/util"
	"testing"
)

func TestComparisonHasNoSaveOrHistoryAuthority(t *testing.T) {
	p := New(&fixtureApp{})
	p.parentPath = "source.dmm"
	p.open = true
	p.choices["root"] = mapping.Choice{Slot: 2}
	c := &Comparison{Panel: p}
	view := workspace.New(c)
	if view.Save() || view.CommandStackId() != command.NullSpaceStackId {
		t.Fatal("comparison acquired edit authority")
	}
	view.Dispose()
	if !p.open || p.choices["root"].Slot != 2 {
		t.Fatal("closing comparison disposed source session")
	}
}

type gestureApp struct {
	fixtureApp
	submissions int
	target      util.Point
}

func (*gestureApp) MappingDocuments() []string            { return []string{"a.dmm"} }
func (*gestureApp) FrameMappingSource(string, util.Point) {}
func (a *gestureApp) MoveMappingRoot(_ mapping.Root, to util.Point, check bool) error {
	if !check {
		a.submissions++
		a.target = to
	}
	return nil
}
func (*gestureApp) OpenMappingContext(string, string, mapping.Transform) {}
func (*gestureApp) OpenMappingComparison(*Panel)                         {}

func TestAnchorDraftCancelsAndSubmitsOnlyOnceAtRelease(t *testing.T) {
	app := &gestureApp{}
	p := New(app)
	p.open = true
	p.compose = true
	p.moveArmed = true
	p.focusRoot = "root"
	root := mapping.Root{ID: "root", StableID: "stable", Local: util.Point{X: 1, Y: 1, Z: 1}, Destination: util.Point{X: 1, Y: 1, Z: 1}}
	p.current = &result{roots: []mapping.Root{root}}
	if !p.handleInline(root.Local, true, true, false, false, true, "Pick") || p.draft == nil {
		t.Fatal("root handle did not admit draft")
	}
	to := util.Point{X: 3, Y: 2, Z: 1}
	p.handleInline(to, true, false, false, false, true, "Pick")
	if app.submissions != 0 {
		t.Fatal("pointer motion submitted an operation")
	}
	p.handleInline(to, true, false, false, true, true, "Pick")
	if p.draft != nil || app.submissions != 0 {
		t.Fatal("Escape modified source")
	}
	p.handleInline(root.Local, true, true, false, false, true, "Pick")
	p.handleInline(to, true, false, true, false, true, "Pick")
	p.handleInline(to, true, false, false, false, true, "Pick")
	if app.submissions != 1 || app.target != to {
		t.Fatal("release did not submit exactly one move", app.submissions, app.target)
	}
}

func TestDerivedHitCannotClickThroughToBase(t *testing.T) {
	p := New(&fixtureApp{})
	p.open = true
	p.compose = true
	point := util.Point{X: 2, Y: 3, Z: 1}
	p.current = &result{projection: &mapping.Projection{Provenance: map[util.Point]*mapping.CellProvenance{point: {Objects: []mapping.Contribution{{Occurrence: "module"}}}}}}
	if !p.handleInline(point, true, true, false, false, true, "Delete") {
		t.Fatal("destructive hit reached underlying source")
	}
	if p.handleInline(util.Point{X: 1, Y: 1, Z: 1}, true, false, false, false, true, "Delete") {
		t.Fatal("uncontributed source cell was locked")
	}
	p.current.projection.Provenance[point] = &mapping.CellProvenance{Covered: true}
	if !p.handleInline(point, true, true, false, false, true, "Add") {
		t.Fatal("transparent/noop contribution allowed click through")
	}
}

func TestDraftIsCancelledByBindingAndFocusTransitions(t *testing.T) {
	app := &gestureApp{}
	hub := NewHub(app)
	p := hub.session("a.dmm")
	p.draft = &anchorDraft{}
	hub.CancelDraft("a.dmm")
	if p.draft != nil {
		t.Fatal("focus loss retained draft")
	}
	p.draft = &anchorDraft{}
	p.queue(nil)
	if p.draft != nil || app.submissions != 0 {
		t.Fatal("binding refresh submitted stale draft")
	}
}

func TestCompositionSessionsSurviveSidebarAndCloseIndependently(t *testing.T) {
	app := &fixtureApp{}
	hub := NewHub(app)
	a, b := hub.session("a.dmm"), hub.session("b.dmm")
	a.choices = map[string]mapping.Choice{"root": {Slot: 2}}
	hub.SetVisible(false)
	if hub.session("a.dmm") != a || a.choices["root"].Slot != 2 {
		t.Fatal("sidebar hid document state")
	}
	hub.CloseSource("b.dmm")
	if hub.session("a.dmm") != a || hub.session("b.dmm") == b {
		t.Fatal("closing one source affected another source or reused closed lifetime")
	}
}
