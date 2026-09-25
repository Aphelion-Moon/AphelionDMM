package pmap

import (
	// APHELION EDIT ADDITION START - COMPACT U2 TOOLBAR
	"fmt"
	// APHELION EDIT ADDITION END

	"github.com/SpaiR/imgui-go"

	"sdmm/internal/aphelion/editing"
	"sdmm/internal/app/ui/cpwsarea/wsmap/tools"
	// APHELION EDIT ADDITION START - COMPACT U2 TOOLBAR
	"sdmm/internal/app/ui/shortcut"
	// APHELION EDIT ADDITION END
	"sdmm/internal/imguiext/icon"
	"sdmm/internal/imguiext/style"
	w "sdmm/internal/imguiext/widget"
	"sdmm/internal/util"

	"github.com/rs/zerolog/log"
)

// APHELION EDIT ADDITION START - COMPACT U2 TOOLBAR
type toolDesc struct {
	btnIcon string
}

var toolsOrder = []string{
	tools.TNAdd,
	tools.TNFill,
	tools.TNGrab,
	tools.TNMove,
	tools.TNPick,
	tools.TNDelete,
	tools.TNReplace,
}

var toolsDesc = map[string]toolDesc{
	tools.TNAdd:     {btnIcon: icon.Add},
	tools.TNFill:    {btnIcon: icon.BorderAll},
	tools.TNGrab:    {btnIcon: icon.BorderStyle},
	tools.TNMove:    {btnIcon: icon.Shrink},
	tools.TNPick:    {btnIcon: icon.EyeDropper},
	tools.TNDelete:  {btnIcon: icon.Eraser},
	tools.TNReplace: {btnIcon: icon.Repeat},
}

func (p *PaneMap) showToolsPanel() {
	/* APHELION EDIT REMOVAL START - COMPACT U2 TOOLBAR
	w.Layout{
		p.panelToolsLayoutTools(),
		w.SameLine(),
		w.Layout{
			w.AlignRight,
			p.panelToolsLayoutSettings(),
		},
	}.Build()
	APHELION EDIT REMOVAL END */
	compact := p.useCompactToolButtons()
	p.panelToolsLayoutTools().Build()
	imgui.SameLine()
	context := tools.CurrentActionContext()
	summary := compactActionSummary(context)
	reserved := 8 * imgui.FontSize()
	if compact {
		reserved = 6 * imgui.FontSize()
	}
	reserved += 3 * imgui.CurrentStyle().ItemSpacing().X
	maxSummaryWidth := max(float32(28), imgui.ContentRegionAvail().X-reserved)
	imgui.Text(truncateToWidth(summary, maxSummaryWidth))
	if imgui.IsItemHovered() {
		imgui.BeginTooltip()
		drawActionDetails(context)
		imgui.EndTooltip()
	}
	imgui.SameLine()
	if !compact {
		p.panelToolsLayoutSettings().Build()
		imgui.SameLine()
	}
	moreLabel := "Options"
	if compact {
		moreLabel = "More"
	}
	if imgui.Button(moreLabel) {
		imgui.OpenPopup("tool-options")
	}
	p.drawToolOptionsPopup()

	imgui.Separator()
	p.drawSelectionContextRow()
	p.drawSelectionPopup()
}

func compactActionSummary(context tools.ActionContext) string {
	if context.Action == "" {
		return "No action"
	}
	badge := context.Badge
	if badge == "" || badge == context.Action {
		badge = context.ToolName
	}
	if context.Cue == tools.CueHideExactType {
		badge = "Hide exact type"
	}
	if context.Captured {
		badge = "Captured · " + badge
	}
	if context.GestureTool != "" {
		badge = "Gesture · " + context.ToolName
	} else if context.IsHeld {
		badge = "Held " + context.ToolName
	}
	return badge + ": " + context.Action
}

func drawActionDetails(context tools.ActionContext) {
	if context.PersistentTool != "" {
		imgui.Text("Persistent tool: " + context.PersistentTool)
	}
	if context.HeldTool != "" {
		imgui.Text("Held tool: " + context.HeldTool)
	}
	if context.GestureTool != "" {
		imgui.Text("Gesture owner: " + context.GestureTool)
	}
	imgui.Text(compactActionSummary(context))
	if context.ModifierHelp != "" {
		imgui.Text("Modifiers: " + context.ModifierHelp)
	}
	if context.Scope != "" {
		imgui.Text("Scope: " + context.Scope)
	}
	if context.Target != "" {
		imgui.Text("Target: " + context.Target)
	}
	if heading, message := actionContextNotice(context); heading != "" {
		imgui.Text(heading + ": " + message)
	}
	if context.ToolName == tools.TNAdd || context.ToolName == tools.TNMove {
		left, right := shortcut.Label("pmap#rotateHeldLeft"), shortcut.Label("pmap#rotateHeldRight")
		if left != "" || right != "" {
			imgui.Text("Rotate held item: " + left + " / " + right)
		}
	}
	if context.ToolName == tools.TNGrab {
		imgui.Text("Rotate: " + shortcut.Label("pmap#rotateLeft") + " / " + shortcut.Label("pmap#rotateRight") +
			" · Mirror: " + shortcut.Label("pmap#mirrorSelectionHorizontal") + " / " + shortcut.Label("pmap#mirrorSelectionVertical"))
		imgui.Text("Move selection: " + shortcut.Label("pmap#nudgeSelectionLeft") + " … " + shortcut.Label("pmap#nudgeSelectionDown"))
	}
	if context.ShortcutAction != "" {
		if keys := shortcut.Label(context.ShortcutAction); keys != "" {
			imgui.Text("Select tool: " + keys)
		}
	}
}

func truncateToWidth(value string, width float32) string {
	if width <= 0 || imgui.CalcTextSize(value, false, 0).X <= width {
		return value
	}
	const suffix = "…"
	runes := []rune(value)
	for len(runes) > 0 {
		runes = runes[:len(runes)-1]
		candidate := string(runes) + suffix
		if imgui.CalcTextSize(candidate, false, 0).X <= width {
			return candidate
		}
	}
	return suffix
}

func (p *PaneMap) panelToolsLayoutTools() (layout w.Layout) {
	if p.useCompactToolButtons() {
		layout = append(layout, w.Button("Tools", func() { imgui.OpenPopup("tool-selector") }).Tooltip("Choose a tool"))
		layout = append(layout, w.Custom(func() {
			if imgui.BeginPopup("tool-selector") {
				for _, name := range toolsOrder {
					label := name
					if keys := shortcut.Label(tools.ActionContextForTool(name).ShortcutAction); keys != "" {
						label += "  " + keys
					}
					if imgui.MenuItem(label) {
						tools.SetSelected(name)
					}
				}
				imgui.EndPopup()
			}
		}))
		return layout
	}

	/* APHELION EDIT REMOVAL START - COMPACT U2 TOOLBAR
	for _, toolName := range toolsOrder {
		tool := tools.Tools()[toolName]
		desc := toolsDesc[toolName]
		btn := w.Button(desc.btnIcon, func() { tools.SetSelected(toolName) }).Round(true)
		if tools.Selected() == tool {
			if tool.AltBehaviour() {
				btn.Style(style.ButtonGold{}).TextColor(style.ColorBlack)
			} else {
				btn.Style(style.ButtonGreen{})
			}
		}
		layout = append(layout, btn, w.Tooltip(desc.tooltip))
	}
	APHELION EDIT REMOVAL END */
	for index, toolName := range toolsOrder {
		name := toolName
		if index > 0 {
			layout = append(layout, w.SameLine())
		}
		button := w.Button(toolsDesc[name].btnIcon, func() { tools.SetSelected(name) }).Round(true)
		if tools.PersistentToolName() == name {
			button.Style(style.ButtonGreen{})
		}
		if tools.HeldToolName() == name {
			button.Style(style.ButtonGold{}).TextColor(style.ColorBlack)
		}
		layout = append(layout, button, w.Tooltip(toolActionTooltip(name)))
	}
	return layout
}

func (p *PaneMap) useCompactToolButtons() bool {
	spacing := imgui.CurrentStyle().ItemSpacing().X
	available := imgui.ContentRegionAvail().X
	toolWidth := float32(0)
	for _, name := range toolsOrder {
		toolWidth += w.Button(toolsDesc[name].btnIcon, nil).Round(true).CalcSize().X
	}
	toolWidth += float32(len(toolsOrder)-1) * spacing
	reserved := 13*imgui.FontSize() + 8*spacing
	return toolWidth+reserved > available
}

func toolActionTooltip(name string) w.Layout {
	context := tools.ActionContextForTool(name)
	return w.Layout{w.Custom(func() { drawActionDetails(context) })}
}

func (p *PaneMap) drawSelectionContextRow() {
	badge := "Selection unavailable"
	var restricted bool
	if p.editor != nil {
		selection := p.editor.WorkingSelection().Get(p.activeLevel)
		badge = fmt.Sprintf("Selection %d", selection.Len())
		if selection.Len() > 0 {
			bounds := selection.Bounds()
			badge += fmt.Sprintf(" · %dx%d", int(bounds.X2-bounds.X1)+1, int(bounds.Y2-bounds.Y1)+1)
		}
		restricted = p.editor.WorkingSelection().Restrict
	}
	spacing := imgui.CurrentStyle().ItemSpacing().X
	limitWidth := imgui.CalcTextSize("Limit", false, 0).X + imgui.CurrentStyle().FramePadding().X*2
	selectionWidth := w.Button("Selection…", nil).CalcSize().X
	badgeWidth := max(float32(40), imgui.ContentRegionAvail().X-limitWidth-selectionWidth-4*spacing)
	imgui.Text(truncateToWidth(badge, badgeWidth))
	imgui.SameLine()
	limitLabel := "Limit"
	if restricted {
		limitLabel += " ●"
	}
	if p.editor == nil {
		w.Disabled(true, w.Button(limitLabel, nil)).Build()
	} else if imgui.SmallButton(limitLabel) {
		p.editor.WorkingSelection().Restrict = !restricted
	}
	if imgui.IsItemHovered() {
		imgui.BeginTooltip()
		imgui.Text("Restrict supported shape operations to the current selection")
		imgui.EndTooltip()
	}
	imgui.SameLine()
	if imgui.Button("Selection…") {
		imgui.OpenPopup("selection-actions")
	}
}

func (p *PaneMap) popupSize() imgui.Vec2 {
	viewport := imgui.MainViewport().WorkSize()
	return imgui.Vec2{
		X: max(float32(160), min(float32(620), viewport.X-24)),
		Y: max(float32(120), min(float32(560), viewport.Y-40)),
	}
}

func (p *PaneMap) drawToolOptionsPopup() {
	size := p.popupSize()
	imgui.SetNextWindowSizeV(size, imgui.ConditionAlways)
	if !imgui.BeginPopup("tool-options") {
		return
	}
	if imgui.BeginChildV("tool-options-scroll", imgui.Vec2{}, true, imgui.WindowFlagsNone) {
		imgui.Text("Tool and brush settings")
		if imgui.MenuItem("Toggle settings panel") {
			p.doToggleSettings()
		}
		p.showPastePlacementControls()
		p.showShapeControls()
		if tools.PreparingShape() {
			imgui.Text("Preparing shape… Escape cancels")
		}
		p.showRandomFillControls()
		p.showAreaSelectionControls()
		if p.editor != nil {
			p.showStampControls()
		}
	}
	imgui.EndChild()
	imgui.EndPopup()
}

func (p *PaneMap) showAreaSelectionControls() {
	if p.app == nil {
		return
	}
	grab := tools.Tools()[tools.TNGrab].(*tools.ToolGrab)
	settings := p.app.Prefs().Mapper
	if settings != nil {
		grab.AreaMode, grab.AllMatchingAreas = settings.AreaMode, settings.AllMatchingAreas
	}
	imgui.Separator()
	imgui.Text("Grab")
	imgui.Checkbox("Area selection", &grab.AreaMode)
	if grab.AreaMode {
		imgui.Checkbox("All matching areas on this level", &grab.AllMatchingAreas)
	}
	if settings != nil {
		settings.AreaMode, settings.AllMatchingAreas = grab.AreaMode, grab.AllMatchingAreas
	}
	for operation, label := range []string{"Replace", "Add", "Subtract", "Intersect"} {
		if operation > 0 {
			imgui.SameLine()
		}
		if imgui.RadioButton(label+"##selection-operation", grab.SelectionOperation == editing.SelectionOperation(operation)) {
			grab.SelectionOperation = editing.SelectionOperation(operation)
		}
	}
}

func (p *PaneMap) drawSelectionPopup() {
	imgui.SetNextWindowSizeV(p.popupSize(), imgui.ConditionAlways)
	if !imgui.BeginPopup("selection-actions") {
		return
	}
	if imgui.BeginChildV("selection-actions-scroll", imgui.Vec2{}, true, imgui.WindowFlagsNone) {
		if p.editor != nil {
			selection := p.editor.WorkingSelection().Get(p.activeLevel)
			imgui.Text(fmt.Sprintf("%d selected tile(s)", selection.Len()))
			if selection.Len() > 0 {
				imgui.Text("Bounds: " + selection.Bounds().String())
			}
		}
		if p.editor != nil {
			if imgui.Button("Clear selection") {
				p.DoDeselect()
			}
		}
		imgui.Separator()
		if p.editor != nil && tools.IsSelected(tools.TNGrab) && tools.Selected().(*tools.ToolGrab).HasSelectedArea() {
			if p.canTransformSelection() {
				w.Button("Rotate left "+shortcut.Label("pmap#rotateLeft"), func() { p.rotateSelection(false) }).Build()
				w.Button("Rotate right "+shortcut.Label("pmap#rotateRight"), func() { p.rotateSelection(true) }).Build()
				p.showSelectionMirrorButtons()
				if !tools.Selected().(*tools.ToolGrab).Placing() {
					p.showSelectionNudgeButtons()
				}
				p.showRepeatTransformButton()
			}
			if prefab, ok := p.Editor().SelectedPrefab(); ok {
				selection := tools.Selected().(*tools.ToolGrab).Selection()
				w.Button("Fill selection", func() {
					if err := p.Editor().FillSelection(selection, prefab, false); err != nil {
						util.ShowErrorDialog(err.Error())
					}
				}).Build()
				w.Button("Replace selected channel", func() {
					if err := p.Editor().FillSelection(selection, prefab, true); err != nil {
						util.ShowErrorDialog(err.Error())
					}
				}).Tooltip("Replace the selected prefab's channel only in selected cells; protect excluded types").Build()
			}
		}
		if p.editor != nil {
			p.showStampControls()
		}
	}
	imgui.EndChild()
	imgui.EndPopup()
}

func (p *PaneMap) panelToolsLayoutSettings() w.Layout {
	var buttonStyle w.ButtonStyle
	if p.showSettings {
		buttonStyle = style.ButtonGreen{}
	} else {
		buttonStyle = style.ButtonDefault{}
	}
	return w.Layout{
		w.Button(icon.Cog, p.doToggleSettings).
			Tooltip("Settings").
			Style(buttonStyle).
			Round(true),
	}
}

func (p *PaneMap) doToggleSettings() {
	log.Print("toggle settings:")
	p.showSettings = !p.showSettings
}

func (p *PaneMap) doPreviousLevel() {
	if p.hasPreviousLevel() {
		p.activeLevel--
		log.Print("active level switched to previous:", p.activeLevel)
	}
}

func (p *PaneMap) doNextLevel() {
	if p.hasNextLevel() {
		p.activeLevel++
		log.Print("active level switched to next:", p.activeLevel)
	}
}

func (p *PaneMap) hasPreviousLevel() bool {
	return p.activeLevel > 1
}

func (p *PaneMap) hasNextLevel() bool {
	return p.activeLevel < p.dmm.MaxZ
}

// APHELION EDIT ADDITION END
