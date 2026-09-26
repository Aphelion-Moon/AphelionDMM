package mappingui

import (
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"sdmm/internal/aphelion/mapping"
	"sdmm/internal/aphelion/resources"
	"sdmm/internal/util"
)

func TestPreviewDenialKeepsDisplayedChoiceUntilDismissed(t *testing.T) {
	p := New(&fixtureApp{})
	p.open, p.compose = true, true
	p.generation = 3
	p.current = &result{projection: &mapping.Projection{Scenario: mapping.Scenario{Choices: map[string]mapping.Choice{"root": {Slot: 0}}}}}
	displayed := p.current
	p.choices["root"] = mapping.Choice{Slot: 2}
	p.operation.target = "preview to C"
	p.results = make(chan result, 1)
	p.results <- result{catalog: mapping.NewCatalog(nil), request: request{generation: 3, scenario: mapping.Scenario{Choices: map[string]mapping.Choice{"root": {Slot: 2}}}}, err: &resources.AdmissionError{Needed: 1024}}
	p.advance()
	for range 5 {
		p.advance()
	}
	if p.current != displayed || p.operation.failed == nil || p.choiceFulfilled("root") || p.pending != nil || p.results != nil {
		t.Fatal("denied request replaced the displayed scenario or silently retried")
	}
	p.dismissRequest()
	if p.current != displayed || !p.choiceFulfilled("root") || p.operation.err != nil || p.pending != nil {
		t.Fatal("dismiss did not restore the displayed scenario without a rebuild")
	}
}

func TestSupersededAlternativeCompletionCannotChangeStatus(t *testing.T) {
	p := New(&fixtureApp{})
	p.open, p.generation = true, 3
	p.operation.target = "preview to C"
	p.results = make(chan result, 1)
	p.results <- result{catalog: mapping.NewCatalog(nil), request: request{generation: 2}, err: errors.New("B failed late")}
	p.advance()
	if p.operation.target != "preview to C" || p.operation.err != nil || p.current != nil {
		t.Fatal("superseded B changed C's outcome")
	}
}

func TestHiddenPreviewRetainsScenarioAndHostCloseRetiresContext(t *testing.T) {
	a := &fixtureApp{}
	h := NewHub(a)
	p := h.session("host.dmm")
	p.open, p.previewVisible = true, true
	p.current = &result{catalog: mapping.NewCatalog(nil)}
	p.contextPath, p.contextRoot = "dirty-source.dmm", "nested"
	p.choices["root"] = mapping.Choice{Slot: 2}
	p.setPreviewVisible(false)
	p.setPreviewVisible(true)
	if p.choices["root"].Slot != 2 || p.contextPath != "dirty-source.dmm" || p.pending != nil || p.generation != 0 {
		t.Fatal("visibility changed navigation or scenario, or queued work")
	}
	h.CloseSource("host.dmm")
	if p.contextPath != "" || h.unavailable[viewKey("dirty-source.dmm")] == "" || len(a.opened) != 0 {
		t.Fatal("host close lost independent-source recovery")
	}
	if replacement := h.session("host.dmm"); replacement == p || replacement.contextPath != "" {
		t.Fatal("same path revived dead context lifetime")
	}
}

func TestRemovedChildSelectsSurvivingAncestor(t *testing.T) {
	p := New(&fixtureApp{})
	parent := mapping.Root{ID: "parent"}
	child := mapping.Root{ID: "child", Parent: "parent"}
	p.focusRoot = child.ID
	p.contextPath = "dirty-child.dmm"
	p.open = true
	p.current = &result{catalog: mapping.NewCatalog(nil), roots: []mapping.Root{child, parent}}
	p.results = make(chan result, 1)
	p.results <- result{catalog: mapping.NewCatalog(nil), roots: []mapping.Root{parent}}
	p.advance()
	if p.focusRoot != parent.ID || !strings.Contains(p.status, "disappeared") || p.contextPath != "dirty-child.dmm" {
		t.Fatal("removed occurrence lost ancestor recovery or its independent source")
	}
}

func TestInspectLoadedRootsDoesNotRequestComposition(t *testing.T) {
	p := New(&fixtureApp{})
	p.open, p.compose = true, true
	p.current = &result{}
	for i := range 20 {
		p.current.roots = append(p.current.roots, mapping.Root{ID: fmt.Sprint(i), Destination: util.Point{X: i + 1, Y: 1, Z: 1}})
	}
	for _, root := range p.current.roots {
		if !p.handleInline(root.Destination, true, true, false, false, true, "Pick") {
			t.Fatal("inspection was not consumed")
		}
		if p.focusRoot != root.ID || p.pending != nil || p.generation != 0 {
			t.Fatal("inspection queued a rebuild or disagreed with selection", root.ID)
		}
	}
}

func TestOverlappingOccurrencePickingRequiresChoice(t *testing.T) {
	p := New(&fixtureApp{})
	p.open, p.compose = true, true
	point := util.Point{X: 1, Y: 1, Z: 1}
	p.current = &result{roots: []mapping.Root{{ID: "a", Destination: point}, {ID: "b", Parent: "a", Destination: point}}}
	p.handleInline(point, true, true, false, false, true, "Pick")
	if p.focusRoot != "" || len(p.contributors) != 2 || p.pending != nil {
		t.Fatal("overlap silently chose an occurrence", p.focusRoot, p.contributors)
	}
}

type revisionApp struct {
	fixtureApp
	key, active string
	captures    int
	ready       bool
}

func (a *revisionApp) ActiveMappingPath() string          { return a.active }
func (a *revisionApp) MappingRevisionKey([]string) string { return a.key }
func (a *revisionApp) CaptureMappingSources() map[string]mapping.AcceptedSource {
	a.captures++
	return nil
}
func (a *revisionApp) MappingRefreshReady([]string) bool { return a.ready }

func TestHiddenRevisionInvalidationDefersCaptureAndStrokeCoalesces(t *testing.T) {
	a := &revisionApp{key: "r2", active: "host.dmm", ready: true}
	p := New(a)
	p.open, p.managed = true, true
	p.parentPath = "host.dmm"
	p.previewVisible = false
	p.current = &result{dependencies: []string{"source.dmm"}}
	p.observeSources()
	p.nextRevisionCheck = time.Time{}
	p.observeSources()
	if !p.stale || a.captures != 0 {
		t.Fatal("hidden refresh captured sources")
	}
	p.previewVisible = true
	a.ready = false
	p.observeSources()
	if a.captures != 0 {
		t.Fatal("unfinished stroke captured sources")
	}
	a.ready = true
	p.observeSources()
	if a.captures != 1 || p.pending == nil || !p.pending.refresh {
		t.Fatal("completed visible action did not refresh once")
	}
}

func TestAcceptedRefreshDoesNotRetryFailedAlternative(t *testing.T) {
	a := &revisionApp{key: "r2", active: "host.dmm", ready: true}
	p := New(a)
	p.open = true
	p.parentPath = "host.dmm"
	p.current = &result{projection: &mapping.Projection{Scenario: mapping.Scenario{Choices: map[string]mapping.Choice{"root": {Slot: 0}}, Excluded: map[string]bool{"other": true}}}}
	p.choices["root"] = mapping.Choice{Slot: 2}
	p.operation = previewOperation{err: errors.New("denied"), failed: &request{scenario: mapping.Scenario{Choices: map[string]mapping.Choice{"root": {Slot: 2}}}}}
	p.observeSources()
	p.nextRevisionCheck = time.Time{}
	p.observeSources()
	if p.pending == nil || p.pending.scenario.Choices["root"].Slot != 0 || !p.pending.scenario.Excluded["other"] || p.choices["root"].Slot != 2 || p.operation.failed == nil {
		t.Fatal("source refresh lost displayed/failed separation")
	}
}

type navigationApp struct {
	fixtureApp
	valid func() bool
	done  func(error)
}

func (a *navigationApp) OpenMappingSource(_, _ string, _ mapping.Transform, valid func() bool, done func(error)) {
	a.valid, a.done = valid, done
}
func TestSourceActivationRequiresAcknowledgementAndRejectsLateCompletion(t *testing.T) {
	a := &navigationApp{}
	p := New(a)
	p.open = true
	p.current = &result{}
	placement := mapping.Placement{Root: mapping.Root{ID: "root"}, Source: mapping.Identity{Path: "source.dmm"}}
	p.enterPlacement(placement, true)
	if p.contextPath != "" {
		t.Fatal("opening source became editable before activation")
	}
	a.done(errors.New("open failed"))
	if p.contextPath != "" || p.sourceStatus == "" {
		t.Fatal("failure became an editing context")
	}
	p.enterPlacement(placement, true)
	done := a.done
	p.clearContext()
	done(nil)
	if p.contextPath != "" || p.pending != nil {
		t.Fatal("late source activation revived old context")
	}
}

func TestNewPreviewRequestSupersedesOpeningSource(t *testing.T) {
	a := &navigationApp{}
	p := New(a)
	p.open = true
	p.current = &result{}
	p.enterPlacement(mapping.Placement{Root: mapping.Root{ID: "A"}, Source: mapping.Identity{Path: "A.dmm"}}, true)
	p.queue(nil)
	newer := p.pending
	if a.valid() {
		t.Fatal("new preview did not fence source activation")
	}
	a.done(nil)
	if p.contextPath != "" || p.pending != newer {
		t.Fatal("late source activation replaced newer preview")
	}
}

func TestHidePreviewRetiresAnchorLayerDemand(t *testing.T) {
	p := New(&fixtureApp{})
	p.open = true
	p.moveRoot = "root"
	p.moveArmed = true
	p.draft = &anchorDraft{}
	p.setPreviewVisible(false)
	if p.moveRoot != "" || p.moveArmed || p.draft != nil {
		t.Fatal("hidden preview retained anchor demand")
	}
}

func TestContainingSourceCannotBypassFailedAlternative(t *testing.T) {
	a := &navigationApp{}
	p := New(a)
	p.open = true
	root := mapping.Root{ID: "child", Parent: "parent"}
	p.focusRoot = root.ID
	p.current = &result{projection: &mapping.Projection{Placements: []mapping.Placement{{Root: mapping.Root{ID: "parent"}, Source: mapping.Identity{Path: "parent.dmm"}}}}}
	p.operation.err = errors.New("preview denied")
	p.enterContainingSource(root)
	if a.done != nil || len(a.opened) != 0 {
		t.Fatal("failed preview still navigated its old containing source")
	}
}

func TestUnfulfilledAlternativeCannotOpenDisplayedSource(t *testing.T) {
	app := &fixtureApp{}
	p := New(app)
	p.focusRoot = "root"
	p.choices["root"] = mapping.Choice{Slot: 2}
	p.current = &result{projection: &mapping.Projection{
		Scenario:   mapping.Scenario{Choices: map[string]mapping.Choice{"root": {Slot: 0}}},
		Placements: []mapping.Placement{{Root: mapping.Root{ID: "root"}, Source: mapping.Identity{Path: "old.dmm"}}},
	}}
	p.openSelectedSource()
	if len(app.opened) != 0 || p.contextPath != "" {
		t.Fatal("unfulfilled alternative opened the old source")
	}
}

func TestReturnClearsContextWithoutClosingSource(t *testing.T) {
	app := &fixtureApp{}
	p := New(app)
	p.parentPath, p.contextPath, p.contextRoot = "host.dmm", "dirty.dmm", "root"
	p.returnToParent()
	if p.contextPath != "" || p.contextRoot != "" {
		t.Fatal("return retained a false editing context")
	}
	if len(app.opened) != 1 || app.opened[0] != "host.dmm" {
		t.Fatal("return did not navigate to the host")
	}
}
