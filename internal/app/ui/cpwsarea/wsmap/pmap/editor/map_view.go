// APHELION EDIT ADDITION START - SEARCH VIEW OWNERSHIP
package editor

// MapViewVersion identifies the UI-owned display for derived instance queries.
// It is not an authoritative document revision. Call only on the UI thread.
// Queries wait for unfinished gestures so previews cannot become action targets
// or cause a full query rebuild on every drag frame.
func (e *Editor) MapViewVersion() (generation uint64, ready bool) {
	return e.mapViewGeneration, !e.mapViewClosed && e.selectionMove == nil && !e.pasteBlocksCommittedView() && len(e.pendingChanges) == 0
}

// CanStartMapEdit rejects new independent actions while another gesture or a
// failed capture owns the display. Already submitted network operations still
// permit ordinary speculative editing; the executor validates their outcomes.
func (e *Editor) CanStartMapEdit() bool {
	_, ready := e.MapViewVersion()
	return ready && e.paste == nil && e.executor != nil && e.collaborationErr == nil && e.history.Valid()
}

// APHELION EDIT ADDITION END
