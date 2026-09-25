package tools

import (
	"sdmm/internal/dmapi/dmmap/dmminstance"
	"sdmm/internal/util"
	"testing"
)

type pickFilterEditor struct {
	editor
	hovered  *dmminstance.Instance
	hides    []string
	selected *dmminstance.Instance
	reads    int
}

func (e *pickFilterEditor) HoveredInstance() *dmminstance.Instance { e.reads++; return e.hovered }
func (e *pickFilterEditor) HideExactPath(path string) error {
	e.hides = append(e.hides, path)
	e.hovered = nil
	return nil
}
func (e *pickFilterEditor) InstanceSelect(i *dmminstance.Instance) { e.selected = i }
func TestAltPickHidesResolvedTypeOnlyOnce(t *testing.T) {
	_, base := lifecycleFixture(t)
	instance := base.m.GetTile(util.Point{X: 1, Y: 1, Z: 1}).Instances()[0]
	e := &pickFilterEditor{hovered: instance}
	ed = e
	pick := newPick()
	// APHELION EDIT CHANGE - SHARED TOOL FEEDBACK - ORIGINAL: pick.setAltBehaviour(true)
	pick.setActionContext(pick.ActionContext(ActionInput{
		Modifiers: ToolModifiers{Alt: true},
		Position:  util.Point{X: 1, Y: 1, Z: 1},
		InBounds:  true,
		Target:    instance,
	}))
	pick.onStart(util.Point{})
	for i := 0; i < 10; i++ {
		pick.onMove(util.Point{})
		pick.process()
	}
	pick.onStop(util.Point{})
	// APHELION EDIT ADDITION START - SHARED TOOL FEEDBACK
	// The test supplies the resolved target in the action context, so handler
	// dispatch must not re-read the editor hover after resolving that input.
	// APHELION EDIT ADDITION END
	// APHELION EDIT CHANGE - SHARED TOOL FEEDBACK - ORIGINAL: if e.reads != 1 || len(e.hides) != 1 || e.hides[0] != instance.Prefab().Path() || e.selected != nil {
	if e.reads != 0 || len(e.hides) != 1 || e.hides[0] != instance.Prefab().Path() || e.selected != nil {
		t.Fatal("Alt-Pick re-resolved hover or selected instead of hiding exact type")
	}
	// APHELION EDIT CHANGE - SHARED TOOL FEEDBACK - ORIGINAL: pick.setAltBehaviour(false)
	pick.setActionContext(pick.ActionContext(ActionInput{Target: instance, InBounds: true}))
	e.hovered = instance
	pick.onStart(util.Point{})
	if e.selected != instance || len(e.hides) != 1 {
		t.Fatal("normal Pick changed visibility")
	}
}
