package cpwsarea

import (
	"testing"

	"sdmm/internal/app/command"
	"sdmm/internal/app/ui/cpwsarea/workspace"
)

// Tab-menu closes are queued while the workspace loop runs and applied after
// it, so the list being iterated never changes underneath it.
func TestTabCloseOthersRunsAfterTheWorkspaceLoop(t *testing.T) {
	contents := []*guardTestContent{{}, {}, {}}
	var workspaces []*workspace.Workspace
	for _, c := range contents {
		workspaces = append(workspaces, workspace.New(c))
	}
	area := &WsArea{app: &guardTestApp{commands: command.NewStorage()}, workspaces: workspaces}
	keep := workspaces[1]
	area.queueTabAction(func() { area.closeWorkspacesGently(area.workspacesExcept(keep)) })
	if len(area.workspaces) != 3 {
		t.Fatal("queued action ran immediately")
	}
	area.runTabActions()
	if len(area.workspaces) != 1 || area.workspaces[0] != keep || !contents[0].disposed || !contents[2].disposed || contents[1].disposed {
		t.Fatalf("close others left %d workspaces", len(area.workspaces))
	}
	if len(area.tabActions) != 0 {
		t.Fatal("actions were not cleared")
	}
}
