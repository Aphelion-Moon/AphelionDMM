// APHELION EDIT ADDITION START - OPERATION AUTHORITY
package editor

import "fmt"

func (e *Editor) commitWithAuthority(message string) {
	if e.localWork != nil {
		return
	}
	if e.HasPastePlacement() {
		e.reportCollaborationError("Unable to apply map change", fmt.Errorf("confirm or cancel paste placement first"))
		return
	}
	if e.collaborationErr == nil && !e.history.Valid() {
		e.collaborationErr = fmt.Errorf("map command history is unavailable")
	}
	if e.collaborationErr == nil && e.executor != nil {
		e.commitOperation(message)
		return
	}
	if e.collaborationErr == nil {
		e.collaborationErr = fmt.Errorf("map operation executor is unavailable")
	}
	selectionApplied(e.selectionOutcome, false)
	e.reportCollaborationError("Unable to apply map change", e.collaborationErr)
}

// APHELION EDIT ADDITION END
