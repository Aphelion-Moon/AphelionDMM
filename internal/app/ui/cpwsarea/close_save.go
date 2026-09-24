package cpwsarea

import "sdmm/internal/app/ui/cpwsarea/workspace"

// APHELION EDIT ADDITION START - RESPONSIVE_SAVE
type asyncWorkspaceSaver interface {
	SaveAsync(func(bool))
}

// Earlier saves and initially clean documents can change while another save
// runs. Recheck the whole close set at the final UI boundary.
func (w *WsArea) savedWorkspacesStillClosable(workspaces []*workspace.Workspace) bool {
	for _, current := range workspaces {
		if w.findWorkspaceIdx(current) < 0 || w.isWorkspaceUnsaved(current) {
			return false
		}
	}
	return true
}

func saveWorkspacesBeforeClose(workspaces []*workspace.Workspace, callback func(bool)) {
	index := 0
	var saveNext func(bool)
	saveNext = func(saved bool) {
		if !saved {
			if callback != nil {
				callback(false)
			}
			return
		}
		if index == len(workspaces) {
			if callback != nil {
				callback(true)
			}
			return
		}
		current := workspaces[index]
		index++
		if saver, ok := current.Content().(asyncWorkspaceSaver); ok {
			saver.SaveAsync(saveNext)
			return
		}
		saveNext(current.Save())
	}
	saveNext(true)
}

// APHELION EDIT ADDITION END
