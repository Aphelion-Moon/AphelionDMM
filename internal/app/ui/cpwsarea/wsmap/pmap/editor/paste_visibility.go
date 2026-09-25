// APHELION EDIT ADDITION START - FILTER PROFILE PRESENTATION
package editor

// Captured source eligibility never broadens. A floating presentation also
// honors current destination visibility and requires a new confirmation when
// that policy changes; already submitted operations keep their accepted intent.
func (p *pasteSession) effectiveVisibility() func(string) bool {
	source, filter := p.visible, p.viewFilter
	return func(path string) bool { return source != nil && source(path) && filter.IsVisiblePath(path) }
}
func (e *Editor) refreshPasteVisibility(p *pasteSession) bool {
	if p == nil || p.phase == pasteResolving || p.viewFilter.PolicyRevision() == e.app.PathsFilter().PolicyRevision() {
		return false
	}
	p.viewFilter = e.app.PathsFilter().Copy()
	p.request++
	p.intent = nil
	p.err = nil
	p.presentation = nil
	p.preparingPresentation = nil
	p.presentationBuild = nil
	e.pMap.Canvas().Render().SetPresentation(nil)
	if !p.workerBusy {
		e.startPasteWorker(p)
	}
	return true
}

// APHELION EDIT ADDITION END
