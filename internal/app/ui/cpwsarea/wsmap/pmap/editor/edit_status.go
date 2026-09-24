// APHELION EDIT ADDITION START - EDIT STATUS
package editor

// EditStatus is presentation only. It never clears retained captures or performs
// a snapshot/recovery scan. gestureOwned refers to this document's input owner.
func (e *Editor) EditStatus(gestureOwned bool) (message string, recovery, busy bool) {
	if e.mapViewClosed {
		return "", false, false
	}
	if e.collaborationErr != nil {
		return e.collaborationErr.Error(), true, false
	}
	if e.localWork != nil || len(e.unresolvedSubmissions) != 0 {
		return "Applying edit…", false, true
	}
	if pending, ok := e.executor.(pendingExecutor); ok && pending.HasUnacknowledgedOperations() {
		return "Applying edit…", false, true
	}
	if e.paste != nil {
		if e.paste.err != nil {
			return e.paste.err.Error(), false, false
		}
		if progress := e.PastePlacementProgress(); progress != "" {
			return progress, false, true
		}
		return "Unplaced preview is not included in saves.", false, false
	}
	if e.selectionMovePreview != nil {
		if e.selectionMovePreview.err != nil {
			return e.selectionMovePreview.err.Error(), false, false
		}
		if e.selectionMovePreview.preparing {
			return "Preparing move preview…", false, true
		}
		if e.selectionMovePreview.phase == selectionMoveResolving {
			return "Applying edit…", false, true
		}
		return "Unplaced preview is not included in saves.", false, false
	}
	if len(e.pendingChanges) != 0 && !gestureOwned && e.selectionMove == nil {
		return "A retained edit is blocking Save.", true, false
	}
	return "", false, false
}

// APHELION EDIT ADDITION END
