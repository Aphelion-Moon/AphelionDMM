package wsmap

import (
	"testing"

	"sdmm/internal/app/ui/cpwsarea/wsmap/tools"
)

func TestWorkspaceDisposeReleasesSelectedToolState(t *testing.T) {
	ws, _ := newSelectionWorkspace(t)
	grab := activateSelectionWorkspace(t, ws)
	if !grab.HasSelectedArea() {
		t.Fatal("fixture has no selection")
	}
	ws.Dispose()
	if grab.HasSelectedArea() || !grab.Stale() || len(tools.SelectedTiles()) != 0 {
		t.Fatal("disposed workspace retained selectable tool contents")
	}
	// A later mouse event must not access the closed editor or canvas.
	tools.OnMouseMove()
}
