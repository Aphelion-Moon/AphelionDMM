package tools

// APHELION EDIT ADDITION START - SHARED TOOL FEEDBACK

import (
	"fmt"
	"strconv"

	"github.com/SpaiR/imgui-go"

	"sdmm/internal/aphelion/editing"
	"sdmm/internal/app/prefs"
	"sdmm/internal/app/ui/shortcut"
	"sdmm/internal/dmapi/dm"
	"sdmm/internal/dmapi/dmmap/dmmdata/dmmprefab"
	"sdmm/internal/dmapi/dmmap/dmminstance"
	"sdmm/internal/imguiext"
	"sdmm/internal/util"
)

// ToolModifiers contains only modifiers that change the resolved tool action.
type ToolModifiers struct {
	Alt, Ctrl, Shift bool
}

type ActionInput struct {
	Modifiers ToolModifiers
	Position  util.Point
	InBounds  bool
	Target    *dmminstance.Instance
}

type ActionCue string

const (
	CueDefault       ActionCue = "default"
	CueHideExactType ActionCue = "hide-exact-type"
	CueMembership    ActionCue = "selection-membership"
	CueErase         ActionCue = "erase"
)

// ActionContext is the resolved description used by both tool dispatch and UI
// feedback. Dispatch fields preserve each existing tool's modifier contract.
type ActionContext struct {
	ToolName           string
	ShortcutAction     string
	PersistentTool     string
	ActiveTool         string
	HeldTool           string
	GestureTool        string
	Action             string
	Badge              string
	Scope              string
	Target             string
	Reason             string
	ModifierHelp       string
	Available          bool
	Modifiers          ToolModifiers
	Captured           bool
	IsHeld             bool
	Cue                ActionCue
	Footprint          util.Bounds
	HasFootprint       bool
	Alternate          bool
	Outline            bool
	SelectionGesture   bool
	SelectionOperation editing.SelectionOperation
	AreaQuery          bool
	ToggleClick        bool
	targetInstance     *dmminstance.Instance
	prefab             *dmmprefab.Prefab
}

func currentActionInput(alt bool) ActionInput {
	input := ActionInput{}
	if cs != nil {
		input.Position = cs.HoveredTile()
		input.InBounds = !cs.HoverOutOfBounds()
	}
	if input.InBounds && ed != nil {
		input.Target = ed.HoveredInstance()
	}
	if shortcut.BackgroundInputBlocked() || imgui.IsAnyItemActive() || imgui.CurrentIO().WantTextInput() ||
		!imgui.IsWindowFocusedV(imgui.FocusedFlagsAnyWindow) {
		return input
	}
	input.Modifiers = ToolModifiers{Alt: alt, Ctrl: imguiext.IsCtrlDown(), Shift: imguiext.IsShiftDown()}
	return input
}

// CurrentActionContext describes the gesture owner while a press is in flight;
// otherwise it describes the currently active tool, including a held tool.
func CurrentActionContext() ActionContext {
	tool := Selected()
	gestureTool := ""
	if active && startedTool != nil {
		tool = startedTool
		gestureTool = startedTool.Name()
	}
	context := tool.ActionContext(currentActionInput(imguiext.IsAltDown()))
	context.PersistentTool = persistentToolName
	context.ActiveTool = selectedToolName
	context.HeldTool = heldToolName
	context.GestureTool = gestureTool
	context.IsHeld = heldToolName != "" && tool.Name() == heldToolName
	return context
}

func ActionContextForTool(toolName string) ActionContext {
	tool := tools[toolName]
	if tool == nil {
		return ActionContext{ToolName: toolName, Action: "Unavailable", Reason: "Tool is not registered"}
	}
	return tool.ActionContext(currentActionInput(imguiext.IsAltDown()))
}

func actionContext(name, action, scope string, input ActionInput) ActionContext {
	shortcutAction := ""
	switch name {
	case TNAdd:
		shortcutAction = "pmap#selectAddTool"
	case TNFill:
		shortcutAction = "pmap#selectFillTool"
	case TNGrab:
		shortcutAction = "pmap#selectSelectTool"
	case TNMove:
		shortcutAction = "pmap#selectMoveTool"
	case TNPick:
		shortcutAction = "pmap#selectPickTool"
	case TNDelete:
		shortcutAction = "pmap#selectDeleteTool"
	case TNReplace:
		shortcutAction = "pmap#selectReplaceTool"
	}
	return ActionContext{
		ToolName:       name,
		ShortcutAction: shortcutAction,
		Action:         action,
		Badge:          action,
		Scope:          scope,
		Available:      true,
		Cue:            CueDefault,
	}
}

func setUnavailable(context *ActionContext, reason string) {
	context.Available = false
	context.Reason = reason
}

func targetPath(instance *dmminstance.Instance) string {
	if instance == nil || instance.Prefab() == nil {
		return ""
	}
	return instance.Prefab().Path()
}

func pointFootprint(point util.Point) (util.Bounds, bool) {
	if point == (util.Point{}) {
		return util.Bounds{}, false
	}
	bounds := util.Bounds{X1: float32(point.X), Y1: float32(point.Y), X2: float32(point.X), Y2: float32(point.Y)}
	return bounds, true
}

func selectionOperationName(operation editing.SelectionOperation) string {
	switch operation {
	case editing.SelectionAdd:
		return "Add"
	case editing.SelectionSubtract:
		return "Subtract"
	case editing.SelectionIntersect:
		return "Intersect"
	default:
		return "Replace"
	}
}

func shapeName(shape editing.ShapeDescriptor) string {
	switch shape.Kind {
	case editing.ShapeEllipse:
		return "ellipse"
	case editing.ShapeCircle:
		return "circle"
	default:
		return "rectangle"
	}
}

func (t *ToolAdd) ActionContext(input ActionInput) ActionContext {
	context := actionContext(TNAdd, "Place selected prefab", "selected prefab channel", input)
	context.ModifierHelp = "Alt replaces the object channel; area and turf placement retain the existing channel behavior."
	context.Modifiers.Alt = input.Modifiers.Alt
	context.Alternate = input.Modifiers.Alt
	context.Scope = "objects place on top; area/turf channels replace by default"
	if context.Alternate {
		context.Action = "Replace object channel"
		context.Badge = "Replace objects"
		context.Scope = "object channel; area and turf values are preserved"
	}
	if ed == nil {
		setUnavailable(&context, "No map is active")
	} else if prefab, ok := ed.SelectedPrefab(); ok && prefab != nil {
		context.Target = prefab.Path()
		context.prefab = prefab
	} else {
		setUnavailable(&context, "Select a prefab before placing")
	}
	shape := currentShape()
	context.Outline = shape.Outline
	if shapeBrushEnabled() {
		context.Action = "Draw " + shapeName(shape) + " footprint"
		if shape.Outline {
			context.Action += " outline"
		}
		if context.Alternate {
			context.Action = "Replace object channel with " + shapeName(shape)
			if shape.Outline {
				context.Action += " outline"
			}
		}
		context.Badge = shapeName(shape)
		context.Scope = "selected object channel; dragged shape"
		if shape.Outline {
			context.Scope += "; border only"
		}
	}
	if shapeRestricted() {
		context.Scope += "; restricted to the current selection"
	}
	if t.shapeStroke != nil {
		context = t.shapeContext
		context.Captured = true
		context.Alternate = t.shapeReplace
		context.Modifiers.Alt = t.shapeReplace
		context.Action = "Adding shape"
		context.Badge = "Captured add"
		if t.shapeReplace {
			context.Badge = "Captured replace"
		}
		context.Footprint = t.shapeStroke.Selection().Bounds()
		context.HasFootprint = t.shapeStroke.Selection().Len() != 0
	}
	if bounds, ok := pointFootprint(input.Position); ok && !context.HasFootprint {
		context.Footprint, context.HasFootprint = bounds, true
	}
	return context
}

func (t *ToolFill) ActionContext(input ActionInput) ActionContext {
	context := actionContext(TNFill, "Fill shape", "selected visible tile channels", input)
	context.ModifierHelp = "Alt replaces eligible values; Ctrl fills the outline."
	context.Modifiers = ToolModifiers{Alt: input.Modifiers.Alt, Ctrl: input.Modifiers.Ctrl}
	randomEnabled := t.random
	var randomPalette editing.RandomPalette
	var randomSettings *editing.MapperSettings
	density := t.density
	seedDescription := ""
	if t.random {
		randomPalette = t.palette
		seedDescription = strconv.FormatUint(t.seed, 10)
	} else if ed != nil {
		if owner, ok := ed.(randomFillOwner); ok {
			randomSettings = owner.RandomFillSettings()
			if randomSettings != nil && randomSettings.RandomFill {
				randomEnabled = true
				randomPalette = randomSettings.Palette
				density = randomSettings.Density
				if randomSettings.SeedLock {
					parsedSeed, err := strconv.ParseUint(randomSettings.Seed, 10, 64)
					if err != nil {
						setUnavailable(&context, "Locked random-fill seed must be an unsigned integer")
					} else {
						seedDescription = strconv.FormatUint(parsedSeed, 10)
					}
				} else {
					seedDescription = "generated per gesture"
				}
			}
		}
	}
	context.Alternate = context.Modifiers.Alt && !randomEnabled
	shape := currentShape()
	context.Outline = shape.Outline || context.Modifiers.Ctrl
	if randomEnabled {
		context.ModifierHelp = "Ctrl fills only the outline; Alt does not replace values in Random Fill."
	} else if context.Alternate {
		context.Badge = "Replace"
		context.Scope = "replace eligible values in the selected channels"
	}
	if context.Outline {
		context.Badge += " · Outline"
		context.Scope += "; border only"
	}
	if shapeRestricted() {
		context.Scope += "; intersected with the current selection"
	}
	context.Action = fmt.Sprintf("Fill %s", shapeName(shape))
	if shape.Outline || context.Modifiers.Ctrl {
		context.Action = fmt.Sprintf("Fill %s outline", shapeName(shape))
	}
	if context.Alternate {
		context.Action = fmt.Sprintf("Replace eligible values in %s", shapeName(shape))
	}
	if randomEnabled {
		context.Action = fmt.Sprintf("Random-fill %s", shapeName(shape))
		paletteName := randomPalette.Name
		if paletteName == "" {
			paletteName = "unnamed palette"
		}
		context.Badge = "Random Fill"
		context.Scope = fmt.Sprintf("%s; seed %s; density %.0f%%", paletteName, seedDescription, density*100)
		if t.random {
			context.Scope += "; captured palette and seed"
		}
		if _, err := randomPalette.Compile(); err != nil {
			setUnavailable(&context, "Random-fill palette is invalid: "+err.Error())
		}
	}
	if ed != nil {
		if prefab, ok := ed.SelectedPrefab(); ok && prefab != nil {
			context.Target = prefab.Path()
			context.prefab = prefab
		} else if !randomEnabled {
			if randomSettings == nil || !randomSettings.RandomFill {
				setUnavailable(&context, "Select a prefab or enable Random Fill")
			}
		}
	} else {
		setUnavailable(&context, "No map is active")
	}
	if t.gestureCaptured {
		context = t.gestureContext
		context.Captured = true
		if t.selection.Len() != 0 {
			context.Footprint, context.HasFootprint = t.selection.Bounds(), true
		} else if t.dragging {
			context.Footprint, context.HasFootprint = t.fillArea, true
		}
	} else if bounds, ok := pointFootprint(input.Position); ok {
		context.Footprint, context.HasFootprint = bounds, true
	}
	return context
}

func (t *ToolGrab) ActionContext(input ActionInput) ActionContext {
	context := actionContext(TNGrab, "Select tiles", "drag over the map to build a selection", input)
	context.ModifierHelp = "Ctrl adds membership (click toggles, drag adds); Alt selects matching areas. Ctrl+Alt selects an area and adds membership."
	context.Cue = CueMembership
	context.targetInstance = input.Target
	selection := t.Selection()
	if selection.Len() != 0 {
		context.Footprint, context.HasFootprint = selection.Bounds(), true
	}
	if t.Placing() {
		context.Action = "Place selected contents"
		context.Badge = "Place"
		context.Scope = "selected contents at the destination"
		if bounds, ok := pointFootprint(input.Position); ok {
			context.Footprint, context.HasFootprint = bounds, true
		}
		if err := t.PlacementError(); err != nil {
			setUnavailable(&context, err.Error())
		}
		return context
	}
	if t.SelectingArea() {
		context.Action = "Finding matching area"
		context.Badge = "Area query"
		context.Scope = "area selection is being prepared"
		if t.Selection().Len() != 0 {
			context.Footprint, context.HasFootprint = t.Selection().Bounds(), true
		}
		return context
	}
	if t.gestureCaptured {
		context = t.gestureContext
		context.Captured = true
		if t.Selection().Len() != 0 {
			context.Footprint, context.HasFootprint = t.Selection().Bounds(), true
		}
		return context
	}

	modifiers := ToolModifiers{Alt: input.Modifiers.Alt, Ctrl: input.Modifiers.Ctrl}
	context.Modifiers = modifiers
	insideSelection := selection.Len() != 0 && selection.Contains(input.Position)
	if t.mode == tSelectModeMoveArea && insideSelection && !modifiers.Alt && !modifiers.Ctrl && t.SelectionOperation == editing.SelectionReplace {
		context.Action = "Move selected contents"
		context.Badge = "Move"
		context.Scope = "visible contents in the selected cells"
		return context
	}
	operation := t.SelectionOperation
	if modifiers.Ctrl {
		operation = editing.SelectionAdd
	}
	area := modifiers.Alt || t.AreaMode && !modifiers.Ctrl
	toggle := modifiers.Ctrl && !modifiers.Alt
	context.SelectionOperation = operation
	context.AreaQuery = area
	context.ToggleClick = toggle
	context.SelectionGesture = modifiers.Ctrl || modifiers.Alt || operation != editing.SelectionReplace
	context.Badge = selectionOperationName(operation)
	context.Action = "Select tiles"
	context.Scope = "candidate membership is separate from the retained selection"
	if toggle {
		if selection.Contains(input.Position) {
			context.Action = "Subtract candidate on click; add tiles on drag"
			context.Badge = "Subtract click · Add drag"
		} else {
			context.Action = "Add candidate on click or drag"
			context.Badge = "Add"
		}
	} else if operation != editing.SelectionReplace {
		context.Action = selectionOperationName(operation) + " candidate selection"
	}
	if area {
		context.Action = "Select matching area"
		context.Badge = "Area · " + selectionOperationName(operation)
		context.Scope = "all matching areas on this level"
		if !t.AllMatchingAreas {
			context.Scope = "the matching area under the pointer"
		}
	}
	if !input.InBounds {
		setUnavailable(&context, "Pointer is outside the map")
	} else if bounds, ok := pointFootprint(input.Position); ok {
		context.Footprint, context.HasFootprint = bounds, true
	}
	if context.SelectionGesture && modifiers.Ctrl && modifiers.Alt {
		// Ctrl overrides the stored selection operation; Alt changes the query
		// candidate to an area. This is the handler's admitted combination.
		context.SelectionOperation = editing.SelectionAdd
		context.Badge = "Area · Add"
		context.Action = "Select area and add membership"
		context.AreaQuery = true
		context.ToggleClick = false
	}
	return context
}

func (t *ToolMove) ActionContext(input ActionInput) ActionContext {
	context := actionContext(TNMove, "Move instance", "tile position", input)
	context.ModifierHelp = "Shift-drag offsets pixel or step variables while leaving the tile position fixed."
	context.Modifiers.Shift = input.Modifiers.Shift
	instance := input.Target
	if t.instance != nil {
		instance = t.instance
	}
	context.Target = targetPath(instance)
	context.targetInstance = instance
	if context.Modifiers.Shift {
		mode := "pixel"
		if ed != nil {
			switch ed.Prefs().Editor.NudgeMode {
			case prefs.SaveNudgeModeStep:
				mode = "step"
			case prefs.SaveNudgeModePixelAlt:
				mode = "alternate pixel"
			}
		}
		context.Action = "Offset instance"
		context.Badge = "Shift · " + mode
		context.Scope = mode + " offsets; tile position stays fixed"
	} else {
		context.Action = "Move instance to another tile"
		context.Badge = "Move tile"
		context.Scope = "instance identity and prefab stay attached"
	}
	isCompositionRoot := context.Target == "/obj/modular_map_root"
	if instance != nil {
		if owner, ok := ed.(interface {
			IsCompositionRoot(*dmminstance.Instance) bool
		}); ok {
			isCompositionRoot = owner.IsCompositionRoot(instance)
		}
	}
	if isCompositionRoot {
		context.Scope = "move the modular root anchor by tile; Shift offsets only pixel/step variables"
		if context.Modifiers.Shift {
			context.Action = "Offset root sprite"
		}
	}
	if instance == nil {
		setUnavailable(&context, "No movable instance under the pointer")
	}
	if bounds, ok := pointFootprint(input.Position); ok {
		context.Footprint, context.HasFootprint = bounds, true
	}
	return context
}

func (t *ToolPick) ActionContext(input ActionInput) ActionContext {
	context := actionContext(TNPick, "Pick instance", "selected instance and its prefab", input)
	context.ModifierHelp = "Alt hides the exact target type throughout the local filter without deleting map contents."
	context.Target = targetPath(input.Target)
	context.targetInstance = input.Target
	context.Modifiers.Alt = input.Modifiers.Alt
	context.Alternate = input.Modifiers.Alt
	if context.Alternate {
		context.Action = "Hide exact type"
		context.Badge = "⊘ Hide type"
		context.Scope = "all matching types in the local filter"
		context.Cue = CueHideExactType
	} else {
		context.Scope = "inspect/select this instance and its prefab"
	}
	if context.Target == "" {
		setUnavailable(&context, "No visible instance under the pointer")
	}
	if bounds, ok := pointFootprint(input.Position); ok {
		context.Footprint, context.HasFootprint = bounds, true
	}
	return context
}

func (t *ToolDelete) ActionContext(input ActionInput) ActionContext {
	context := actionContext(TNDelete, "Delete instance", "one eligible visible instance", input)
	context.ModifierHelp = "Alt erases eligible visible instances from the tile; filtered types and required defaults stay protected."
	context.Cue = CueErase
	context.Modifiers.Alt = input.Modifiers.Alt
	context.Alternate = input.Modifiers.Alt
	if t.shapeStroke != nil {
		context.Captured = true
		context.Alternate = t.shapeAll
		context.Modifiers.Alt = t.shapeAll
		context.Action = "Erase shape"
		context.Badge = "Shape erase"
		context.Scope = "eligible visible types; current filter and selection restriction apply"
		context.Footprint = t.shapeStroke.Selection().Bounds()
		context.HasFootprint = t.shapeStroke.Selection().Len() != 0
		return context
	}
	if t.stroke != nil {
		context.Captured = true
		context.Alternate = t.stroke.All()
		context.Modifiers.Alt = t.stroke.All()
	}
	if context.Alternate {
		context.Action = "Erase tile"
		context.Badge = "Erase tile"
		context.Scope = "eligible visible instances on this tile; filtered types and required defaults stay protected"
	} else {
		context.Target = targetPath(input.Target)
		context.Scope = "exact visible instance; held erase does not repeat-hide types"
		if context.Target == "" {
			setUnavailable(&context, "No eligible visible instance under the pointer")
		} else if filterOwner, ok := ed.(interface{ BrushFilter() dm.PathsFilter }); ok {
			filter := filterOwner.BrushFilter()
			if !filter.IsVisiblePath(context.Target) {
				setUnavailable(&context, "The target type is hidden by the local filter")
			}
		}
	}
	if bounds, ok := pointFootprint(input.Position); ok {
		context.Footprint, context.HasFootprint = bounds, true
	}
	return context
}

func (t *ToolReplace) ActionContext(input ActionInput) ActionContext {
	context := actionContext(TNReplace, "Replace instance", "same base type as the target", input)
	context.ModifierHelp = "No mouse modifier changes this action."
	context.Target = targetPath(input.Target)
	context.targetInstance = input.Target
	if context.Target == "" {
		setUnavailable(&context, "No visible instance under the pointer")
	} else if ed == nil {
		setUnavailable(&context, "No map is active")
	} else if prefab, ok := ed.SelectedPrefab(); !ok || prefab == nil {
		setUnavailable(&context, "Select a replacement prefab")
	} else {
		context.prefab = prefab
		context.Badge = "Replace with " + prefab.Path()
		if !dm.IsPathBaseSame(context.Target, prefab.Path()) {
			setUnavailable(&context, "Replacement must have the same DreamMaker base type")
		} else {
			context.Scope = "target instance only; same-base-type replacement"
			context.Reason = "Replacement changes the target's prefab while preserving its instance identity"
		}
	}
	if bounds, ok := pointFootprint(input.Position); ok {
		context.Footprint, context.HasFootprint = bounds, true
	}
	return context
}

// APHELION EDIT ADDITION END
