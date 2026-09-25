package tools

import (
	"strings"
	"testing"

	"sdmm/internal/aphelion/collab/model"
	"sdmm/internal/aphelion/editing"
	"sdmm/internal/app/prefs"
	"sdmm/internal/dmapi/dmmap/dmmdata/dmmprefab"
	"sdmm/internal/dmapi/dmmap/dmminstance"
	"sdmm/internal/util"
)

type compositionRootActionEditor struct {
	*lifecycleEditor
	root *dmminstance.Instance
}

func (e compositionRootActionEditor) IsCompositionRoot(instance *dmminstance.Instance) bool {
	return instance == e.root
}

func (e *lifecycleEditor) SelectedPrefab() (*dmmprefab.Prefab, bool) { return nil, false }
func (e *lifecycleEditor) Prefs() prefs.Prefs                        { return prefs.Prefs{} }
func (e *lifecycleEditor) HoveredInstance() *dmminstance.Instance {
	if e.m == nil || cs == nil {
		return nil
	}
	point := cs.HoveredTile()
	if !e.m.HasTile(point) {
		return nil
	}
	instances := e.m.GetTile(point).Instances()
	if len(instances) == 0 {
		return nil
	}
	return instances[0]
}

type teardownQueryGuardEditor struct{ *lifecycleEditor }

func (teardownQueryGuardEditor) SelectedPrefab() (*dmmprefab.Prefab, bool) {
	panic("tool reset queried the selected prefab during teardown")
}

func actionContextForTest(current Tool) ActionContext {
	switch typed := current.(type) {
	case *ToolAdd:
		return typed.actionContext
	case *ToolFill:
		return typed.actionContext
	case *ToolGrab:
		return typed.actionContext
	case *ToolMove:
		return typed.actionContext
	case *ToolPick:
		return typed.actionContext
	case *ToolDelete:
		return typed.actionContext
	case *ToolReplace:
		return typed.actionContext
	default:
		return ActionContext{}
	}
}

type randomFillActionEditor struct {
	*lifecycleEditor
	settings *editing.MapperSettings
}

func (e randomFillActionEditor) RandomFillSettings() *editing.MapperSettings { return e.settings }
func (randomFillActionEditor) StartRandomFill(editing.Selection, editing.RandomPalette, uint64, float64, util.Point) error {
	return nil
}

type gestureProbeTool struct {
	tool
	name      string
	processes int
	starts    int
	moves     int
	stops     int
}

func (t *gestureProbeTool) Name() string { return t.name }
func (t *gestureProbeTool) process()     { t.processes++ }
func (t *gestureProbeTool) ActionContext(input ActionInput) ActionContext {
	return actionContext(t.name, "probe", "probe scope", input)
}
func (t *gestureProbeTool) onStart(util.Point) { t.starts++ }
func (t *gestureProbeTool) onMove(util.Point)  { t.moves++ }
func (t *gestureProbeTool) onStop(util.Point)  { t.stops++ }

type gestureProbeCanvas struct {
	point    util.Point
	dragging bool
}

func (c *gestureProbeCanvas) Dragging() bool              { return c.dragging }
func (c *gestureProbeCanvas) HoverOutOfBounds() bool      { return false }
func (c *gestureProbeCanvas) HoveredTile() util.Point     { return c.point }
func (c *gestureProbeCanvas) LastHoveredTile() util.Point { return c.point }

func TestHeldToolSwitchKeepsGestureWithItsOriginalOwner(t *testing.T) {
	_, owner := lifecycleFixture(t)
	oldTools, oldName, oldEditor := tools, selectedToolName, ed
	oldPersistent, oldHeld := persistentToolName, heldToolName
	oldControl, oldCanvas, oldActive, oldStarted := cc, cs, active, startedTool
	t.Cleanup(func() {
		tools, selectedToolName, ed = oldTools, oldName, oldEditor
		persistentToolName, heldToolName = oldPersistent, oldHeld
		cc, cs, active, startedTool = oldControl, oldCanvas, oldActive, oldStarted
	})

	gestureOwner := &gestureProbeTool{name: "Gesture owner"}
	heldTool := &gestureProbeTool{name: "Held Pick"}
	tools = map[string]Tool{gestureOwner.name: gestureOwner, heldTool.name: heldTool}
	selectedToolName, persistentToolName, heldToolName, ed = gestureOwner.name, gestureOwner.name, "", owner
	canvas := &gestureProbeCanvas{point: util.Point{X: 2, Y: 1, Z: 1}, dragging: true}
	cc, cs, active, startedTool = canvas, canvas, true, gestureOwner

	SetHeldSelected(heldTool.name)
	if gestureOwner.stops != 0 {
		t.Fatalf("switching to a held tool stopped the in-progress gesture %d times", gestureOwner.stops)
	}
	if PersistentToolName() != gestureOwner.name || HeldToolName() != heldTool.name {
		t.Fatalf("persistent/held tool state = %q/%q", PersistentToolName(), HeldToolName())
	}
	RestorePersistentSelection()
	if Selected().Name() != gestureOwner.name || HeldToolName() != "" {
		t.Fatalf("release did not restore persistent tool: active=%q held=%q", Selected().Name(), HeldToolName())
	}
	SetHeldSelected(heldTool.name)
	processActiveTool()
	if gestureOwner.processes != 1 || heldTool.processes != 0 {
		t.Fatalf("per-frame process owner/held tool = %d/%d", gestureOwner.processes, heldTool.processes)
	}
	RestorePersistentSelection()
	processSelectedToolMove()
	canvas.dragging = false
	processSelectedToolsStop()
	if gestureOwner.moves != 1 || gestureOwner.stops != 1 || heldTool.moves != 0 || heldTool.stops != 0 {
		t.Fatalf("gesture dispatch owner move/stop=%d/%d; held tool=%d/%d", gestureOwner.moves, gestureOwner.stops, heldTool.moves, heldTool.stops)
	}
}

func TestAltPickActionContextNamesTheExactTypeHide(t *testing.T) {
	_, owner := lifecycleFixture(t)
	point := util.Point{X: 1, Y: 1, Z: 1}
	instance := owner.m.GetTile(point).Instances()[0]
	pick := newPick()

	context := pick.ActionContext(ActionInput{
		Modifiers: ToolModifiers{Alt: true},
		Position:  point,
		InBounds:  true,
		Target:    instance,
	})
	if context.Action != "Hide exact type" || context.Scope != "all matching types in the local filter" {
		t.Fatalf("Alt-Pick context action/scope = %q / %q", context.Action, context.Scope)
	}
	if context.Target != instance.Prefab().Path() || context.Cue != CueHideExactType || !context.Available {
		t.Fatalf("Alt-Pick context target/cue/availability = %q / %q / %t", context.Target, context.Cue, context.Available)
	}
}

func TestAllToolContextsDescribeTheirNormalAndSupportedModifierActions(t *testing.T) {
	grab, owner := lifecycleFixture(t)
	point := util.Point{X: 1, Y: 1, Z: 1}
	target := owner.m.GetTile(point).Instances()[0]
	input := ActionInput{Position: point, InBounds: true, Target: target}

	for _, item := range []struct {
		name string
		tool Tool
	}{
		{TNAdd, newAdd()},
		{TNFill, newFill()},
		{TNGrab, grab},
		{TNMove, newMove()},
		{TNPick, newPick()},
		{TNDelete, newDelete()},
		{TNReplace, newReplace()},
	} {
		context := item.tool.ActionContext(input)
		if context.ToolName != item.name || context.Action == "" || context.ModifierHelp == "" || context.ShortcutAction == "" {
			t.Errorf("%s normal action context = %+v", item.name, context)
		}
	}

	add := newAdd().ActionContext(ActionInput{Modifiers: ToolModifiers{Alt: true}, Position: point, InBounds: true})
	if !add.Alternate || add.Action != "Replace object channel" {
		t.Errorf("Add Alt context = %+v", add)
	}
	fill := newFill().ActionContext(ActionInput{Modifiers: ToolModifiers{Ctrl: true}, Position: point, InBounds: true})
	if !fill.Outline || !fill.Modifiers.Ctrl || !strings.Contains(fill.Action, "outline") {
		t.Errorf("Fill Ctrl context = %+v", fill)
	}
	move := newMove().ActionContext(ActionInput{Modifiers: ToolModifiers{Shift: true}, Position: point, InBounds: true, Target: target})
	if move.Action != "Offset instance" || !strings.Contains(move.Scope, "tile position stays fixed") {
		t.Errorf("Move Shift context = %+v", move)
	}
	delete := newDelete().ActionContext(ActionInput{Modifiers: ToolModifiers{Alt: true}, Position: point, InBounds: true, Target: target})
	if !delete.Alternate || delete.Action != "Erase tile" || delete.Cue != CueErase {
		t.Errorf("Delete Alt context = %+v", delete)
	}
	replace := newReplace().ActionContext(ActionInput{Modifiers: ToolModifiers{Alt: true, Ctrl: true, Shift: true}, Position: point, InBounds: true, Target: target})
	if replace.Alternate || replace.Action != "Replace instance" || replace.Available {
		t.Errorf("Replace context should not invent modifier behavior and should explain the missing prefab: %+v", replace)
	}
}

func TestMoveActionContextRecognizesInheritedCompositionRoot(t *testing.T) {
	_, owner := lifecycleFixture(t)
	target := owner.m.GetTile(util.Point{X: 1, Y: 1, Z: 1}).Instances()[0]
	ed = compositionRootActionEditor{lifecycleEditor: owner, root: target}

	context := newMove().ActionContext(ActionInput{
		Position: util.Point{X: 1, Y: 1, Z: 1},
		InBounds: true,
		Target:   target,
	})
	if !strings.Contains(context.Scope, "modular root anchor") {
		t.Fatalf("inherited composition root action context = %+v", context)
	}
}

func TestFillActionContextDescribesConfiguredRandomFillBeforePress(t *testing.T) {
	_, owner := lifecycleFixture(t)
	settings := &editing.MapperSettings{
		RandomFill: true,
		Palette: editing.RandomPalette{
			Version: 1,
			Name:    "Fixture palette",
			Entries: []editing.PaletteEntry{{
				ID: "fixture", Weight: 1,
				Prefab: model.PrefabState{Path: "/obj/test", Vars: map[string]string{}},
			}},
		},
		Density:  0.75,
		Seed:     "31415",
		SeedLock: true,
	}
	ed = randomFillActionEditor{lifecycleEditor: owner, settings: settings}

	context := newFill().ActionContext(ActionInput{
		Modifiers: ToolModifiers{Alt: true},
		Position:  util.Point{X: 1, Y: 1, Z: 1},
		InBounds:  true,
	})
	if !context.Available || context.Alternate || !strings.Contains(context.Action, "Random-fill") {
		t.Fatalf("configured Random Fill action context = %+v", context)
	}
	for _, want := range []string{"Fixture palette", "31415", "75%"} {
		if !strings.Contains(context.Scope, want) {
			t.Errorf("Random Fill scope %q omits %q", context.Scope, want)
		}
	}
	if !strings.Contains(context.ModifierHelp, "Alt does not replace") {
		t.Fatalf("Random Fill Alt semantics not explained: %q", context.ModifierHelp)
	}
}

func TestHeldRotationUsesGestureOwnerDuringHeldToolSwitch(t *testing.T) {
	_, owner := lifecycleFixture(t)
	point := util.Point{X: 1, Y: 1, Z: 1}
	move := newMove()
	move.instance = owner.m.GetTile(point).Instances()[0]
	oldMove, oldPick := tools[TNMove], tools[TNPick]
	oldName, oldPersistent, oldHeld := selectedToolName, persistentToolName, heldToolName
	oldActive, oldStarted := active, startedTool
	tools[TNMove], tools[TNPick] = move, newPick()
	selectedToolName, persistentToolName, heldToolName = TNPick, TNMove, TNPick
	active, startedTool = true, move
	t.Cleanup(func() {
		tools[TNMove], tools[TNPick] = oldMove, oldPick
		selectedToolName, persistentToolName, heldToolName = oldName, oldPersistent, oldHeld
		active, startedTool = oldActive, oldStarted
	})
	if !CanRotateHeld() {
		t.Fatal("held-rotation shortcut ignored the in-flight Move gesture owner")
	}
}

func TestResetTransientModifiersDoesNotQueryDisposedEditor(t *testing.T) {
	_, owner := lifecycleFixture(t)
	ed = teardownQueryGuardEditor{lifecycleEditor: owner}
	previous := make(map[string]ActionContext, len(tools))
	for name, current := range tools {
		previous[name] = actionContextForTest(current)
	}
	t.Cleanup(func() {
		for name, context := range previous {
			tools[name].setActionContext(context)
		}
	})
	ResetTransientModifiers()
	for name, current := range tools {
		context := actionContextForTest(current)
		if context.Available || context.Reason != "Map input is unavailable" {
			t.Fatalf("%s context after editor teardown = %+v", name, context)
		}
	}
}

func TestGrabCtrlAltActionContextMatchesHandlerCombination(t *testing.T) {
	grab, owner := lifecycleFixture(t)
	ed = owner
	grab.SelectionOperation = editing.SelectionIntersect
	grab.AreaMode = true
	point := util.Point{X: 1, Y: 1, Z: 1}
	target := owner.m.GetTile(point).Instances()[0]
	owner.m.GetTile(point).InstancesAdd(dmmprefab.New(0, "/area/test", target.Prefab().Vars()))

	context := grab.ActionContext(ActionInput{
		Modifiers: ToolModifiers{Ctrl: true, Alt: true},
		Position:  point,
		InBounds:  true,
	})
	if !context.AreaQuery || context.SelectionOperation != editing.SelectionAdd || context.ToggleClick {
		t.Fatalf("Ctrl+Alt context disagrees with Grab dispatch: area=%t operation=%v toggle=%t", context.AreaQuery, context.SelectionOperation, context.ToggleClick)
	}
	if context.Badge != "Area · Add" {
		t.Fatalf("Ctrl+Alt context badge = %q, want one effective Area · Add badge", context.Badge)
	}

	grab.setActionContext(context)
	grab.onStart(point)
	if grab.selectionOperation != editing.SelectionAdd || grab.toggleClick || grab.areaQuery == nil {
		t.Fatalf("Grab dispatch did not consume resolved Ctrl+Alt state: op=%v toggle=%t area=%t", grab.selectionOperation, grab.toggleClick, grab.areaQuery != nil)
	}
}
