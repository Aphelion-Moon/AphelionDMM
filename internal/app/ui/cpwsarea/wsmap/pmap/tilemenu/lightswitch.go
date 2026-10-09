// APHELION EDIT ADDITION START - LIGHT SWITCH
package tilemenu

import (
	"fmt"

	"github.com/SpaiR/imgui-go"
	"github.com/rs/zerolog/log"

	"sdmm/internal/aphelion/lighting"
	"sdmm/internal/dmapi/dmmap/dmminstance"
)

// lightSwitchLabel is the menu label for instance i, or "" when it is not a
// light.
func (t *TileMenu) lightSwitchLabel(i *dmminstance.Instance) (string, string) {
	env := t.app.LoadedEnvironment()
	if env == nil {
		return "", ""
	}
	object := env.Objects[i.Prefab().Path()]
	if object == nil {
		return "", ""
	}
	sw, ok := lighting.LightSwitch(i.Prefab().Path(), i.Prefab().Vars().Value, object.Vars.Value, lighting.DefaultProfiles())
	if !ok {
		return "", ""
	}
	fixture := lighting.IsFixture(i.Prefab().Path(), lighting.DefaultProfiles())
	switch {
	case sw.On && fixture:
		return "Turn Light Off", "Removes the bulb (status = LIGHT_EMPTY), like the /empty mapping subtypes.\nWall fixtures ignore light_on in game."
	case sw.On:
		return "Turn Light Off", "Sets light_on = FALSE."
	case fixture:
		return "Turn Light On", "Fits a working bulb (status = LIGHT_OK)."
	default:
		return "Turn Light On", "Clears light_on = FALSE."
	}
}

// isLight reports whether instance i offers the light switch.
func (t *TileMenu) isLight(i *dmminstance.Instance) bool {
	label, _ := t.lightSwitchLabel(i)
	return label != ""
}

func (t *TileMenu) showLightSwitch(i *dmminstance.Instance, idx int) {
	label, tip := t.lightSwitchLabel(i)
	if label == "" {
		return
	}
	if imgui.MenuItem(fmt.Sprintf("%s##light_switch_%d", label, idx)) {
		t.switchLight(i, label)
	}
	if imgui.IsItemHovered() {
		imgui.SetTooltip(tip)
	}
}

func (t *TileMenu) switchLight(i *dmminstance.Instance, label string) {
	if !t.editor.TryBeginTileChange(i.Coord()) {
		return
	}
	if _, err := t.editor.InstanceSwitchLight(i); err != nil {
		log.Print("light switch refused:", err)
		return
	}
	t.editor.CommitOperation(label)
}

// APHELION EDIT ADDITION END
