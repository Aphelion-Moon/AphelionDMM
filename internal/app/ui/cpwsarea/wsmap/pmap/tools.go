package pmap

import (
	// APHELION EDIT ADDITION START - HELD TOOL OWNERSHIP
	"sdmm/internal/aphelion/hotkeys"
	// APHELION EDIT ADDITION END
	"sdmm/internal/app/ui/cpwsarea/wsmap/tools"
	// APHELION EDIT ADDITION START - SHORTCUT FOCUS
	"sdmm/internal/app/ui/shortcut"
	// APHELION EDIT ADDITION END
	// APHELION EDIT REMOVAL START - CURRENT FRAME INPUT
	// "sdmm/internal/app/window"
	// APHELION EDIT REMOVAL END

	"github.com/SpaiR/imgui-go"
	"github.com/go-gl/glfw/v3.3/glfw"
	"github.com/rs/zerolog/log"
)

/* APHELION EDIT REMOVAL START - CURRENT FRAME INPUT
func init() {
	window.RunRepeat(func() {
		processTempToolsMode()
	})
}
APHELION EDIT REMOVAL END */

/* APHELION EDIT REMOVAL START - HELD TOOL OWNERSHIP
var (
	tmpToolIsInTemporalMode bool
	tmpToolLastSelectedName string
	tmpToolPrevSelectedName string
)

func processTempToolsMode() {
	// APHELION EDIT ADDITION START - SHORTCUT MATCHING
	// Holding a letter while typing or using a command must not deselect Grab.
	if shortcut.BackgroundInputBlocked() || imgui.IsAnyItemActive() || imgui.CurrentIO().WantTextInput() ||
		imgui.IsKeyDown(int(glfw.KeyLeftControl)) || imgui.IsKeyDown(int(glfw.KeyRightControl)) ||
		imgui.IsKeyDown(int(glfw.KeyLeftSuper)) || imgui.IsKeyDown(int(glfw.KeyRightSuper)) {
		return
	}
	// APHELION EDIT ADDITION END
	if !tmpToolIsInTemporalMode {
		tmpToolLastSelectedName = tools.Selected().Name()
	}

	var inMode bool
	inMode = inMode || processTempToolMode(int(glfw.KeyS), -1, tools.TNPick)
	inMode = inMode || processTempToolMode(int(glfw.KeyD), -1, tools.TNDelete)
	inMode = inMode || processTempToolMode(int(glfw.KeyR), -1, tools.TNReplace)

	if tmpToolIsInTemporalMode && !inMode {
		log.Print("select before-tmp tool:", tmpToolLastSelectedName)
		tools.SetSelected(tmpToolLastSelectedName)
		tmpToolLastSelectedName = ""
		tmpToolIsInTemporalMode = false
	}
}

func processTempToolMode(key, altKey int, modeName string) bool {
	// Ignore presses when Dear ImGui inputs are in charge or actual shortcuts are invisible.
	{
		var p *PaneMap
		if activePane != nil {
			p = activePane
		} else if lastActivePane != nil {
			p = lastActivePane
		}
		// APHELION EDIT CHANGE - STATIC_ANALYSIS - ORIGINAL: if p != nil && !(p.canvasControl.Active() || p.shortcuts.Visible()) {
		if p != nil && !p.canvasControl.Active() && !p.shortcuts.Visible() {
			return false
		}
	}

	isKeyPressed := imgui.IsKeyPressedV(key, false) || imgui.IsKeyPressedV(altKey, false)
	isKeyReleased := imgui.IsKeyReleased(key) || imgui.IsKeyReleased(altKey)
	isKeyDown := imgui.IsKeyDown(key) || imgui.IsKeyDown(altKey)
	isSelected := tools.IsSelected(modeName)

	if isKeyPressed && !isSelected {
		log.Print("selecting tmp tool:", modeName)
		tmpToolPrevSelectedName = tools.Selected().Name()
		tmpToolIsInTemporalMode = true
		tools.SetSelected(modeName)
	} else if isKeyReleased && len(tmpToolPrevSelectedName) != 0 {
		if isSelected {
			log.Print("selecting prev-tmp tool:", tmpToolPrevSelectedName)
			tools.SetSelected(tmpToolPrevSelectedName)
		}
		tmpToolPrevSelectedName = modeName
	}

	return isKeyDown
}
APHELION EDIT REMOVAL END */

// APHELION EDIT ADDITION START - HELD TOOL OWNERSHIP
var temporaryTools hotkeys.HeldTools

func processTempToolsMode() {
	io := imgui.CurrentIO()
	blocked := shortcut.BackgroundInputBlocked() || imgui.IsAnyItemActive() || io.WantTextInput() ||
		imgui.IsKeyDown(int(glfw.KeyLeftControl)) || imgui.IsKeyDown(int(glfw.KeyRightControl)) ||
		imgui.IsKeyDown(int(glfw.KeyLeftSuper)) || imgui.IsKeyDown(int(glfw.KeyRightSuper))
	p := activePane
	if p == nil {
		p = lastActivePane
	}
	visible := p == nil || p.canvasControl.Active() || p.shortcuts.Visible()
	// Preserve the inherited S, D, R priority, but observe all three keys on
	// every frame so releasing one can reveal another admitted held key.
	inputs := []hotkeys.HeldToolInput{
		{Name: tools.TNPick, Down: visible && imgui.IsKeyDown(int(glfw.KeyS)), Pressed: visible && imgui.IsKeyPressedV(int(glfw.KeyS), false)},
		{Name: tools.TNDelete, Down: visible && imgui.IsKeyDown(int(glfw.KeyD)), Pressed: visible && imgui.IsKeyPressedV(int(glfw.KeyD), false)},
		{Name: tools.TNReplace, Down: visible && imgui.IsKeyDown(int(glfw.KeyR)), Pressed: visible && imgui.IsKeyPressedV(int(glfw.KeyR), false)},
	}
	selected := tools.Selected().Name()
	if next := temporaryTools.Update(selected, blocked, inputs); next != selected {
		if temporaryTools.Active() {
			log.Print("selecting held tool:", next)
			tools.SetHeldSelected(next)
		} else {
			tools.RestorePersistentSelection()
		}
	}
}

// APHELION EDIT ADDITION END

func selectAddTool() {
	tools.SetSelected(tools.TNAdd)
}

func selectFillTool() {
	tools.SetSelected(tools.TNFill)
}

func selectSelectTool() {
	tools.SetSelected(tools.TNGrab)
}

func selectMoveTool() {
	tools.SetSelected(tools.TNMove)
}
