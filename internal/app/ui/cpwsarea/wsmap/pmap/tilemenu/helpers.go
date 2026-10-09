// APHELION EDIT ADDITION START - MAPPING HELPER FINDER
package tilemenu

import (
	"fmt"

	"sdmm/internal/aphelion/helpers"
	"sdmm/internal/app/ui/layout/lnode"
	"sdmm/internal/dmapi/dmmap/dmminstance"
	w "sdmm/internal/imguiext/widget"
)

// helperCount is the number of mapping helpers offered for instance i.
func (t *TileMenu) helperCount(i *dmminstance.Instance) int {
	return len(helpers.Offerable(helpers.ForEnvironment(t.app.LoadedEnvironment()).For(i.Prefab().Path())))
}

// showMappingHelpers links to the Mapping Helpers tab, which can search and
// group long lists (a public airlock has 236 helpers); a nested menu cannot.
func (t *TileMenu) showMappingHelpers(i *dmminstance.Instance, idx int) {
	n := t.helperCount(i)
	if n == 0 {
		return
	}
	w.MenuItem(fmt.Sprintf("Mapping Helpers (%d)...##helpers_%d", n, idx), t.doShowHelpers(i)).IconEmpty().Build()
}

func (t *TileMenu) doShowHelpers(i *dmminstance.Instance) func() {
	return func() {
		t.editor.InstanceSelect(i)
		t.app.ShowLayout(lnode.NameHelpers, true)
	}
}

// APHELION EDIT ADDITION END
