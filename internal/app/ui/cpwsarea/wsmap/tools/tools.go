package tools

import (
	// APHELION EDIT ADDITION START - TOOL GESTURE OWNERSHIP
	"github.com/SpaiR/imgui-go"
	// APHELION EDIT ADDITION END
	// APHELION EDIT ADDITION START - SELECTION LIFECYCLE
	"sdmm/internal/aphelion/editing"
	// APHELION EDIT ADDITION END
	"sdmm/internal/app/prefs"
	// APHELION EDIT REMOVAL START - CURRENT FRAME INPUT
	// "sdmm/internal/app/window"
	// APHELION EDIT REMOVAL END
	"sdmm/internal/imguiext"

	"sdmm/internal/dmapi/dmmap"
	"sdmm/internal/dmapi/dmmap/dmmdata"
	"sdmm/internal/dmapi/dmmap/dmmdata/dmmprefab"
	"sdmm/internal/dmapi/dmmap/dmminstance"
	"sdmm/internal/util"

	"github.com/rs/zerolog/log"
)

const (
	TNAdd     = "Add"
	TNFill    = "Fill"
	TNGrab    = "Grab"
	TNMove    = "Move"
	TNPick    = "Pick"
	TNDelete  = "Delete"
	TNReplace = "Replace"
)

/* APHELION EDIT REMOVAL START - CURRENT FRAME INPUT
func init() {
	window.RunRepeat(func() {
		process(imguiext.IsAltDown()) // Enable tools alt-behaviour when Alt button is down.
	})
}
APHELION EDIT REMOVAL END */

type canvasControl interface {
	Dragging() bool
}

type canvasState interface {
	HoverOutOfBounds() bool
	HoveredTile() util.Point
	LastHoveredTile() util.Point
}

type editor interface {
	Dmm() *dmmap.Dmm
	// APHELION EDIT ADDITION START - SELECTION LIFECYCLE
	BeginSelectionMove(util.Bounds, int) (*editing.Move, error)
	PreviewSelectionMove(*editing.Move, util.Point) (util.Bounds, error)
	FinishSelectionMove(*editing.Move, bool)
	// APHELION EDIT ADDITION END

	CommitOperation(commitMsg string)
	// APHELION EDIT ADDITION START - COLLABORATION
	BeginTileChange(util.Point)
	// APHELION EDIT ADDITION END
	// APHELION EDIT ADDITION START - INSTANCE MOVE CAPTURE
	CanStartMapEdit() bool
	TryBeginTileChange(...util.Point) bool
	// APHELION EDIT ADDITION END

	UpdateCanvasByCoords([]util.Point)
	UpdateCanvasByTiles([]dmmap.Tile)

	SelectedPrefab() (*dmmprefab.Prefab, bool)

	OverlayPushTile(coord util.Point, colFill, colBorder util.Color)
	OverlayPushArea(area util.Bounds, colFill, colBorder util.Color)

	InstanceSelect(i *dmminstance.Instance)
	InstanceDelete(i *dmminstance.Instance)
	// APHELION EDIT ADDITION START - COLLABORATION
	InstanceReplace(i *dmminstance.Instance, prefab *dmmprefab.Prefab)
	// APHELION EDIT ADDITION END

	TileReplace(coord util.Point, prefabs dmmdata.Prefabs)

	TileDeleteSelected()
	TileDelete(util.Point)
	HoveredInstance() *dmminstance.Instance
	ZoomLevel() float32
	Prefs() prefs.Prefs
}

var (
	cc canvasControl
	cs canvasState
	ed editor

	active   bool
	oldCoord util.Point

	tools = map[string]Tool{
		TNAdd:     newAdd(),
		TNFill:    newFill(),
		TNGrab:    newGrab(),
		TNMove:    newMove(),
		TNPick:    newPick(),
		TNDelete:  newDelete(),
		TNReplace: newReplace(),
	}

	selectedToolName = TNAdd

	startedTool Tool
	// APHELION EDIT ADDITION START - TOOL GESTURE OWNERSHIP
	awaitMouseRelease bool
	// APHELION EDIT ADDITION END
)

func SetSelected(toolName string) Tool {
	if selectedToolName != toolName {
		log.Print("selecting:", toolName)
		tools[selectedToolName].OnDeselect()
		selectedToolName = toolName
	}
	return Selected()
}

func IsSelected(toolName string) bool {
	return selectedToolName == toolName
}

// APHELION EDIT ADDITION START - EDIT STATUS
func OwnsGesture(owner editor) bool { return ed == owner && active }

// APHELION EDIT ADDITION END

func SetEditor(editor editor) {
	// APHELION EDIT ADDITION START - TOOL GESTURE OWNERSHIP
	changed := ed != editor
	if changed {
		DeactivateEditor(ed)
	}
	// APHELION EDIT ADDITION END
	ed = editor
	// APHELION EDIT ADDITION START - PERSISTENT SELECTION
	if changed {
		BindSelectionLevel(editor)
	}
	// APHELION EDIT ADDITION END
}

// APHELION EDIT ADDITION START - TOOL GESTURE OWNERSHIP
// DeactivateEditor ends input ownership before another pane can bind its editor.
// Grab cancels its preview on deselection; legacy tools finish their existing
// edit on the source map. Keep bindings for commands targeting the last pane.
func DeactivateEditor(owner editor) {
	if ed == nil || ed != owner {
		return
	}
	Selected().OnDeselect()
	if active {
		if startedTool != nil {
			startedTool.onStop(oldCoord)
		}
		awaitMouseRelease = true
	}
	active, startedTool, oldCoord = false, nil, util.Point{}
}

// APHELION EDIT ADDITION END

// APHELION EDIT ADDITION START - CLOSED MAP TOOL OWNERSHIP
// ReleaseEditor runs on the UI thread before the owning pane is disposed.
// Closing an inactive pane must not reset tools attached to a different editor.
func ReleaseEditor(owner editor) {
	if ed != owner || ed == nil {
		return
	}
	// Cancel Grab/placement while its editor is still usable. Other tool state
	// is discarded without onStop: disposal must not submit another operation.
	tools[TNGrab].OnDeselect()
	tools[TNGrab].(*ToolGrab).Reset()
	tools[TNDelete].(*ToolDelete).cancelShape()
	*tools[TNAdd].(*ToolAdd) = *newAdd()
	*tools[TNFill].(*ToolFill) = *newFill()
	*tools[TNMove].(*ToolMove) = *newMove()
	*tools[TNPick].(*ToolPick) = *newPick()
	*tools[TNDelete].(*ToolDelete) = *newDelete()
	*tools[TNReplace].(*ToolReplace) = *newReplace()
	ed, cc, cs = nil, nil, nil
	active, startedTool, oldCoord = false, nil, util.Point{}
	awaitMouseRelease = false
}

// APHELION EDIT ADDITION END

func SetCanvasControl(canvasControl canvasControl) {
	cc = canvasControl
}

func SetCanvasState(canvasState canvasState) {
	cs = canvasState
}

func Selected() Tool {
	return tools[selectedToolName]
}

func Tools() map[string]Tool {
	return tools
}

func process(altBehaviour bool) {
	// APHELION EDIT ADDITION START - CURRENT FRAME INPUT
	processFrame(altBehaviour, false)
	// APHELION EDIT ADDITION END
}

// APHELION EDIT ADDITION START - CURRENT FRAME INPUT
// ProcessForEditor runs after the owning canvas has resolved current input.
// A release waits for the queued stroke samples to be consumed in order.
func ProcessForEditor(owner editor, pendingSamples bool) {
	if ed == owner {
		processFrame(imguiext.IsAltDown(), pendingSamples)
	}
}

func processFrame(altBehaviour bool, pendingSamples bool) {
	// APHELION EDIT ADDITION START - CLOSED MAP TOOL OWNERSHIP
	if ed == nil {
		return
	}
	// APHELION EDIT ADDITION END
	// APHELION EDIT ADDITION START - SELECTION LIFECYCLE
	// Escape must remain available while the canvas owns an active mouse item.
	cancelGrabOnEscape()
	// APHELION EDIT ADDITION END
	if active && startedTool != Selected() {
		startedTool.onStop(oldCoord)
	}

	Selected().setAltBehaviour(altBehaviour)
	// APHELION EDIT ADDITION START - MAPPER GESTURES
	grab := tools[TNGrab].(*ToolGrab)
	grab.ctrlSelection, grab.altSelection = imguiext.IsCtrlDown(), altBehaviour
	tools[TNFill].(*ToolFill).border = grab.ctrlSelection
	// APHELION EDIT ADDITION END
	Selected().process()
	processSelectedToolStart()
	if !pendingSamples {
		processSelectedToolsStop()
	}
}

// APHELION EDIT ADDITION END

func OnMouseMove() {
	processSelectedToolMove()
}

func SelectedTiles() []util.Point {
	// APHELION EDIT ADDITION START - PERSISTENT SELECTION
	if s := SelectionForEditor(ed); s.Len() > 0 {
		return s.Coordinates()
	}
	// APHELION EDIT ADDITION END
	if selectTool, ok := Selected().(*ToolGrab); ok {
		// APHELION EDIT ADDITION START - SELECTION MEMBERSHIP
		if selectTool.HasSelectedArea() {
			return selectTool.selectedCoordinates()
		}
		// APHELION EDIT ADDITION END
		if len(selectTool.initTiles) > 0 {
			tiles := make([]util.Point, 0, len(selectTool.initTiles))
			for _, tile := range selectTool.initTiles {
				tiles = append(tiles, tile.Coord)
			}
			return tiles
		}
	}
	// APHELION EDIT ADDITION START - CLOSED MAP TOOL OWNERSHIP
	if cs == nil {
		return nil
	}
	// APHELION EDIT ADDITION END
	return []util.Point{cs.LastHoveredTile()}
}

func processSelectedToolStart() {
	// APHELION EDIT ADDITION START - TOOL GESTURE OWNERSHIP
	if awaitMouseRelease {
		// A newly bound canvas may still have last frame's button state. Use
		// current input so transferring focus cannot turn a held press into a
		// new gesture, even before that canvas processes its first frame.
		if !imgui.IsMouseDown(imgui.MouseButtonLeft) {
			awaitMouseRelease = false
		}
		return
	}
	// APHELION EDIT ADDITION END
	if cs == nil || cc == nil || cs.HoverOutOfBounds() && !Selected().IgnoreBounds() {
		return
	}
	if cc.Dragging() && !active {
		startedTool = Selected()
		Selected().onStart(cs.HoveredTile())
		active = true
	}
}

func processSelectedToolMove() {
	if cs == nil || cs.HoverOutOfBounds() && !Selected().IgnoreBounds() {
		return
	}
	coord := cs.HoveredTile()
	// APHELION EDIT CHANGE - DETERMINISTIC ERASER - ORIGINAL: if coord != oldCoord && active {
	if active && (coord != oldCoord || Selected().Name() == TNDelete) {
		Selected().onMove(coord)
	}
	oldCoord = coord
}

func processSelectedToolsStop() {
	if cc != nil && !cc.Dragging() && active {
		Selected().onStop(oldCoord)
		active = false
	}
}
