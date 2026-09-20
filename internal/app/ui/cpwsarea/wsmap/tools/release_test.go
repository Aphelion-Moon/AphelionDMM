package tools

import (
	"reflect"
	"testing"

	"sdmm/internal/util"
)

type releaseCanvas struct{ point util.Point }

func (*releaseCanvas) Dragging() bool                { return true }
func (*releaseCanvas) HoverOutOfBounds() bool        { return false }
func (c *releaseCanvas) HoveredTile() util.Point     { return c.point }
func (c *releaseCanvas) LastHoveredTile() util.Point { return c.point }

func TestReleaseEditorPreservesOtherOwnerAndCancelsItsOwnPreview(t *testing.T) {
	grab, owner := lifecycleFixture(t)
	previousTools, previousName := tools, selectedToolName
	previousControl, previousState := cc, cs
	previousActive, previousStarted, previousCoord := active, startedTool, oldCoord
	previousAwaitRelease := awaitMouseRelease
	t.Cleanup(func() {
		tools, selectedToolName = previousTools, previousName
		cc, cs = previousControl, previousState
		active, startedTool, oldCoord = previousActive, previousStarted, previousCoord
		awaitMouseRelease = previousAwaitRelease
	})
	tools = map[string]Tool{TNGrab: grab, TNAdd: newAdd(), TNFill: newFill(), TNMove: newMove(), TNPick: newPick(), TNDelete: newDelete(), TNReplace: newReplace()}
	selectedToolName = TNGrab
	state := &releaseCanvas{point: util.Point{X: 2, Y: 1, Z: 1}}
	cc, cs = state, state
	before := owner.m.Copy()
	grab.onStart(util.Point{X: 1, Y: 1, Z: 1})
	grab.onMove(state.point)
	active, startedTool, oldCoord = true, grab, state.point
	preview := owner.m.Copy()
	ReleaseEditor(&lifecycleEditor{})
	if ed != owner || cc != state || cs != state || !active || grab.Stale() || !reflect.DeepEqual(owner.m.Copy(), preview) {
		t.Fatal("closing another editor disturbed current tools")
	}
	move := tools[TNMove].(*ToolMove)
	move.instance, move.lastTile = owner.m.Tiles[1].Instances()[0], owner.m.Tiles[1]
	ReleaseEditor(owner)
	if ed != nil || cc != nil || cs != nil || active || startedTool != nil || oldCoord != (util.Point{}) {
		t.Fatal("closed editor retained global bindings")
	}
	if grab.HasSelectedArea() || !reflect.DeepEqual(owner.m.Copy(), before) || owner.commits != 0 {
		t.Fatal("release committed or failed to cancel owned preview")
	}
	if move.instance != nil || move.lastTile != nil {
		t.Fatal("inactive move tool retained closed map instances")
	}
	if len(SelectedTiles()) != 0 || selectedToolName != TNGrab {
		t.Fatal("release lost tool preference or returned closed tiles")
	}
	// Idle frame/mouse processing needs no ImGui or map after release.
	process(false)
	OnMouseMove()
	ReleaseEditor(owner)
}
