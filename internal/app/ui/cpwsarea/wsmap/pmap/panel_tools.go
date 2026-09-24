package pmap

import (
	// APHELION EDIT ADDITION START - STABLE TOOLBAR
	"github.com/SpaiR/imgui-go"
	// APHELION EDIT ADDITION END
	"sdmm/internal/app/ui/cpwsarea/wsmap/tools"
	// APHELION EDIT ADDITION START - SELECTION MEMBERSHIP
	"sdmm/internal/util"
	// APHELION EDIT ADDITION END
	"sdmm/internal/imguiext/icon"
	"sdmm/internal/imguiext/style"
	w "sdmm/internal/imguiext/widget"

	"github.com/rs/zerolog/log"
)

type toolDesc struct {
	btnIcon string
	tooltip w.Layout
}

const tSeparator = "toolsSeparator"

var (
	toolsOrder = []string{
		tools.TNAdd,
		tools.TNFill,
		tools.TNGrab,
		tools.TNMove,
		tSeparator,
		tools.TNPick,
		tools.TNDelete,
		tools.TNReplace,
	}

	toolsDesc = map[string]toolDesc{
		tools.TNAdd: {
			btnIcon: icon.Add,
			tooltip: w.Layout{
				w.AlignTextToFramePadding(),
				w.Text(tools.TNAdd),
				w.SameLine(),
				w.TextFrame("1"),
				w.Separator(),
				w.Text("Place the selected object"),
				w.Line(w.TextFrame("Hold Alt"), w.Text("Place the selected object with replace")),
			},
		},
		tools.TNFill: {
			btnIcon: icon.BorderAll,
			tooltip: w.Layout{
				w.AlignTextToFramePadding(),
				w.Text(tools.TNFill),
				w.SameLine(),
				w.TextFrame("2"),
				w.Separator(),
				w.Text("Fill the area with the selected object"),
				w.Line(w.TextFrame("Hold Alt"), w.Text("Fill the selected area with the selected object with replace")),
				w.Line(w.TextFrame("Hold Ctrl"), w.Text("Fill the area with the selected object, borders only")),
			},
		},
		tools.TNGrab: {
			btnIcon: icon.BorderStyle,
			tooltip: w.Layout{
				w.AlignTextToFramePadding(),
				w.Text(tools.TNGrab),
				w.SameLine(),
				w.TextFrame("3"),
				w.Separator(),
				w.Text("Select the area / Move the selection with visible objects inside"),
				// APHELION EDIT ADDITION START - SELECTION ROTATION
				w.Text("[ / ]: rotate. H / V: mirror. Alt+Arrow: move one tile. Esc: deselect. F1: all shortcuts."),
				// APHELION EDIT ADDITION END
			},
		},
		tools.TNMove: {
			btnIcon: icon.Shrink,
			tooltip: w.Layout{
				w.AlignTextToFramePadding(),
				w.Text(tools.TNMove),
				w.SameLine(),
				w.TextFrame("4"),
				w.Separator(),
				w.Text("Move a singular object"),
				w.Line(w.TextFrame("Hold Shift"), w.Text("Pixel/Step offset the selected object via dragging")),
			},
		},
		tools.TNPick: {
			btnIcon: icon.EyeDropper,
			tooltip: w.Layout{
				w.AlignTextToFramePadding(),
				w.Text(tools.TNPick),
				w.SameLine(),
				// APHELION EDIT CHANGE - EDITOR SHORTCUTS - ORIGINAL: w.TextFrame("Hold S"),
				w.TextFrame("5 / Hold S"),
				w.Separator(),
				w.Text("Pick the hovered instance"),
			},
		},
		tools.TNDelete: {
			btnIcon: icon.Eraser,
			tooltip: w.Layout{
				w.AlignTextToFramePadding(),
				w.Text(tools.TNDelete),
				w.SameLine(),
				// APHELION EDIT CHANGE - EDITOR SHORTCUTS - ORIGINAL: w.TextFrame("Hold D"),
				w.TextFrame("6 / Hold D"),
				w.Separator(),
				w.Text("Delete the hovered instance"),
				w.Line(w.TextFrame("+Alt"), w.Text("Delete the whole tile")),
			},
		},
		tools.TNReplace: {
			btnIcon: icon.Repeat,
			tooltip: w.Layout{
				w.AlignTextToFramePadding(),
				w.Text(tools.TNReplace),
				w.SameLine(),
				// APHELION EDIT CHANGE - EDITOR SHORTCUTS - ORIGINAL: w.TextFrame("Hold R"),
				w.TextFrame("7 / Hold R"),
				w.Separator(),
				w.Text("Replace the hovered instance with the selected object"),
			},
		},
	}
)

func (p *PaneMap) showToolsPanel() {
	/* APHELION EDIT REMOVAL START - STABLE TOOLBAR
	w.Layout{
		p.panelToolsLayoutTools(),
		w.SameLine(),
		w.Layout{
			w.AlignRight,
			p.panelToolsLayoutSettings(),
		},
	}.Build()
	APHELION EDIT REMOVAL END */
	// APHELION EDIT ADDITION START - STABLE TOOLBAR
	settings := p.panelToolsLayoutSettings()
	if imgui.BeginTableV("tool-row", 2, imgui.TableFlagsSizingStretchProp|imgui.TableFlagsNoPadOuterX, imgui.Vec2{}, 0) {
		imgui.TableSetupColumnV("tools", imgui.TableColumnFlagsWidthStretch, 1, 0)
		imgui.TableSetupColumnV("settings", imgui.TableColumnFlagsWidthFixed, settings.CalcSize().X, 0)
		imgui.TableNextColumn()
		p.panelToolsLayoutTools().Build()
		imgui.TableNextColumn()
		settings.Build()
		imgui.EndTable()
	}
	// Paste controls follow the stable first row; progress belongs to the bubble.
	p.showPastePlacementControls()
	if tools.IsSelected(tools.TNGrab) {
		grab := tools.Selected().(*tools.ToolGrab)
		imgui.Checkbox("Area selection", &grab.AreaMode)
		if grab.AreaMode {
			imgui.Checkbox("All matching areas on this level", &grab.AllMatchingAreas)
		}
	}
	// APHELION EDIT ADDITION END
	// APHELION EDIT ADDITION START - SELECTION STAMPS
	p.showStampControls()
	// APHELION EDIT ADDITION END
	// APHELION EDIT ADDITION START - SELECTION ROTATION
	if tools.IsSelected(tools.TNGrab) && p.canTransformSelection() {
		w.Layout{
			w.Button("Rotate Left [", func() { p.rotateSelection(false) }).Tooltip("Rotate visible contents 90 degrees left around the bottom-left corner"),
			w.SameLine(),
			w.Button("Rotate Right ]", func() { p.rotateSelection(true) }).Tooltip("Rotate visible contents 90 degrees right around the bottom-left corner"),
		}.Build()
		p.showSelectionMirrorButtons()
		if !tools.Selected().(*tools.ToolGrab).Placing() {
			p.showSelectionNudgeButtons()
		}
		p.showRepeatTransformButton()
		// APHELION EDIT ADDITION START - SELECTION MEMBERSHIP
		if prefab, ok := p.Editor().SelectedPrefab(); ok {
			selection := tools.Selected().(*tools.ToolGrab).Selection()
			w.Layout{
				w.Button("Fill selection", func() {
					if err := p.Editor().FillSelection(selection, prefab, false); err != nil {
						util.ShowErrorDialog(err.Error())
					}
				}),
				w.SameLine(),
				w.Button("Replace selected channel", func() {
					if err := p.Editor().FillSelection(selection, prefab, true); err != nil {
						util.ShowErrorDialog(err.Error())
					}
				}).Tooltip("Replace the selected prefab's channel only in the selected cells; protect excluded types"),
			}.Build()
		}
		// APHELION EDIT ADDITION END
	}
	// APHELION EDIT ADDITION END
}

func (p *PaneMap) panelToolsLayoutTools() (layout w.Layout) {
	// APHELION EDIT ADDITION START - STABLE TOOLBAR
	used, available := float32(0), imgui.ContentRegionAvail().X
	spacing := imgui.CurrentStyle().ItemSpacing().X
	// APHELION EDIT ADDITION END
	// APHELION EDIT CHANGE - STABLE TOOLBAR - ORIGINAL: for idx, toolName := range toolsOrder {
	for _, toolName := range toolsOrder {
		var toolName = toolName // Closure (hello, js)

		// APHELION EDIT REMOVAL - STABLE TOOLBAR - ORIGINAL: if idx > 0 || idx < len(toolsOrder)-1 { layout = append(layout, w.SameLine()) }

		if toolName == tSeparator {
			// APHELION EDIT CHANGE - STABLE TOOLBAR - ORIGINAL: layout = append(layout, w.TextDisabled("|"))
			// The reserved settings column replaces separator-based spacing.
			continue
		}

		tool := tools.Tools()[toolName]
		desc := toolsDesc[toolName]

		btn := w.Button(desc.btnIcon, func() {
			tools.SetSelected(toolName)
		}).Round(true)

		if tools.Selected() == tool {
			if tool.AltBehaviour() {
				btn.Style(style.ButtonGold{}).TextColor(style.ColorBlack)
			} else {
				btn.Style(style.ButtonGreen{})
			}
		}

		// APHELION EDIT ADDITION START - STABLE TOOLBAR
		width := btn.CalcSize().X
		if used > 0 && used+spacing+width <= available {
			layout = append(layout, w.SameLine())
			used += spacing
		} else {
			used = 0
		}
		used += width
		// APHELION EDIT ADDITION END
		layout = append(layout, btn, w.Tooltip(desc.tooltip))
	}
	return layout
}

func (p *PaneMap) panelToolsLayoutSettings() w.Layout {
	var bntStyle w.ButtonStyle
	if p.showSettings {
		bntStyle = style.ButtonGreen{}
	} else {
		bntStyle = style.ButtonDefault{}
	}
	return w.Layout{
		w.Button(icon.Cog, p.doToggleSettings).
			Tooltip("Settings").
			Style(bntStyle).
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
