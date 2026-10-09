// APHELION EDIT ADDITION START - BRUSH TOOL
package pmap

import (
	"fmt"

	"github.com/SpaiR/imgui-go"

	"sdmm/internal/aphelion/editing"
	"sdmm/internal/dmapi/dmmap"
)

// showUtilityControls edits what the Brush tool lays.
func (p *PaneMap) showUtilityControls() {
	if p.app == nil || p.app.Prefs().Mapper == nil {
		return
	}
	settings := p.app.Prefs().Mapper
	bundle := settings.Bundle()
	imgui.Separator()
	imgui.Text("Brush (tool 8): what each run lays")
	imgui.Checkbox("Disposal pipe (straights, bends, junctions and trunks chosen per tile)", &settings.BrushDisposals)
	remove := -1
	for i := range bundle {
		line := &bundle[i]
		imgui.Checkbox(fmt.Sprintf("##utility-on-%d", i), &line.Enabled)
		imgui.SameLine()
		label := line.Path
		if !dmmap.IsKnownType(line.Path) {
			label += "  (not in this environment)"
		}
		imgui.Text(label)
		imgui.SameLine()
		if prefab, ok := p.app.SelectedPrefab(); ok && imgui.SmallButton(fmt.Sprintf("Use selected##utility-set-%d", i)) {
			line.Path = prefab.Path()
		}
		imgui.SameLine()
		if imgui.SmallButton(fmt.Sprintf("Remove##utility-remove-%d", i)) {
			remove = i
		}
	}
	if remove >= 0 {
		settings.UtilityBundle = append(bundle[:remove:remove], bundle[remove+1:]...)
	}
	if prefab, ok := p.app.SelectedPrefab(); ok && imgui.Button("Add selected prefab") {
		settings.UtilityBundle = append(settings.Bundle(), editing.UtilityLine{Path: prefab.Path(), Enabled: true})
	}
	imgui.SameLine()
	if imgui.Button("Reset to station default") {
		settings.UtilityBundle = editing.DefaultUtilityBundle()
	}
}

// APHELION EDIT ADDITION END
