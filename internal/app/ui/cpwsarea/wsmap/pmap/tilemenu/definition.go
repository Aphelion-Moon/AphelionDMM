// APHELION EDIT ADDITION START - MAP GO TO DEFINITION
package tilemenu

import (
	"fmt"

	"github.com/SpaiR/imgui-go"
	"github.com/rs/zerolog/log"

	"sdmm/internal/app/ui/cpenvironment"
	"sdmm/internal/dmapi/dmmap/dmminstance"
	"sdmm/internal/imguiext/icon"
	w "sdmm/internal/imguiext/widget"
)

// openDefinition is the Environment panel's launcher, shared so both menus
// open a type's source the same way. Tests replace it.
var openDefinition = cpenvironment.OpenDefinition

// hasDefinition reports whether the loaded environment defines i's type.
func (t *TileMenu) hasDefinition(i *dmminstance.Instance) bool {
	return cpenvironment.HasDefinition(t.app.LoadedEnvironment(), i.Prefab().Path())
}

func (t *TileMenu) showGoToDefinition(i *dmminstance.Instance, idx int) {
	known := t.hasDefinition(i)
	w.MenuItem(fmt.Sprint("Go to Definition##go_to_definition_", idx), t.doGoToDefinition(i)).
		Icon(icon.FolderOpen).
		Enabled(known).
		Build()
	if !known && imgui.IsItemHoveredV(imgui.HoveredFlagsAllowWhenDisabled) {
		imgui.SetTooltip(fmt.Sprintf("Unavailable: %s is not defined by the loaded environment, so it has no source definition.", i.Prefab().Path()))
	}
}

func (t *TileMenu) doGoToDefinition(i *dmminstance.Instance) func() {
	return func() {
		path := i.Prefab().Path()
		log.Print("do go to definition:", path)
		if err := openDefinition(t.app.LoadedEnvironment(), t.app.Prefs().Editor.CodeEditor, path); err != nil {
			log.Print("unable to open definition file: ", err)
		}
	}
}

// APHELION EDIT ADDITION END
