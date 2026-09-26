package mappingui

import (
	"maps"
	"path/filepath"
	"time"

	"sdmm/internal/aphelion/mapping"
)

// Preview work and source navigation have independent outcomes. The last usable
// result remains the authority for inspection even when a request fails.
type previewOperation struct {
	target string
	err    error
	failed *request
}

func (p *Panel) selectOccurrence(id string) {
	p.focusRoot = id
	p.revealRoot = true
	p.cancelMove()
	p.treeDirty = true
}

func (p *Panel) cancelMove() {
	p.moveArmed, p.moveRoot, p.draft = false, "", nil
	for i, view := range p.layerViews {
		if view != nil {
			view.Dispose()
			p.layerViews[i] = nil
		}
	}
}

func (p *Panel) selectedPlacement() (mapping.Placement, bool) {
	return p.placement(p.focusRoot)
}

func (p *Panel) placement(id string) (mapping.Placement, bool) {
	if p.current != nil && p.current.projection != nil {
		for _, placement := range p.current.projection.Placements {
			if placement.Root.ID == id {
				return placement, true
			}
		}
	}
	return mapping.Placement{}, false
}

func (p *Panel) choiceFulfilled(id string) bool {
	if p.current == nil || p.current.projection == nil {
		return false
	}
	want, requested := p.choices[id]
	got, displayed := p.current.projection.Scenario.Choices[id]
	return (!requested || displayed && want.Slot == got.Slot) && p.scenario.Excluded[id] == p.current.projection.Scenario.Excluded[id]
}

func (p *Panel) chooseAlternative(root mapping.Root, candidate mapping.Candidate) {
	if candidate.Error != "" {
		return
	}
	if p.choices == nil {
		p.choices = map[string]mapping.Choice{}
	}
	p.choices[root.ID] = mapping.Choice{Slot: candidate.Slot}
	p.referencePath = candidate.Path
	p.queue(nil)
	p.operation.target = "preview to " + filepath.Base(candidate.Path)
}

func (p *Panel) setPreviewVisible(visible bool) {
	p.previewVisible = visible
	if !visible {
		p.cancelMove()
		return
	}
	p.open, p.compose = true, true
	if p.current == nil && p.pending == nil && p.results == nil && p.operation.err == nil {
		p.queue(nil)
	}
}

func (p *Panel) hasConsumer() bool {
	if !p.open {
		return false
	}
	if !p.managed {
		return true
	}
	active := viewKey(p.app.ActiveMappingPath())
	return p.comparisonActive || p.previewVisible && active == viewKey(p.parentPath) || p.contextPath != "" && p.contextStyle != 2 && active == viewKey(p.contextPath)
}

func (p *Panel) dismissRequest() {
	p.generation++
	if p.cancel != nil {
		p.cancel()
	}
	p.pending = nil
	p.operation = previewOperation{}
	p.refreshBlocked = false
	if p.current != nil && p.current.projection != nil {
		p.scenario = p.current.projection.Scenario
		p.scenario.Excluded = maps.Clone(p.scenario.Excluded)
		p.choices = maps.Clone(p.current.projection.Scenario.Choices)
		p.fixedChoices = maps.Clone(p.current.request.fixedChoices)
		p.mapConfig = p.current.request.mapConfig
	}
}

func (p *Panel) retryPreview(release bool) {
	failed := p.operation.failed
	if failed != nil {
		p.scenario, p.choices = failed.scenario, maps.Clone(failed.scenario.Choices)
		p.scenario.Excluded = maps.Clone(failed.scenario.Excluded)
		p.mapConfig, p.fixedChoices = failed.mapConfig, maps.Clone(failed.fixedChoices)
	}
	if release {
		p.release()
		if p.reuse != nil {
			p.reuse.Close()
			p.reuse = nil
		}
	}
	p.queue(nil)
}

func (p *Panel) observeSources() {
	provider, ok := p.app.(acceptedProvider)
	if !ok || !p.open || p.current == nil && !p.retryAccepted {
		return
	}
	paths := []string{p.parentPath, p.referencePath}
	if p.current != nil {
		paths = p.current.dependencies
	}
	key := provider.MappingRevisionKey(paths)
	if key != p.observedRevisions {
		p.refreshBlocked = false
		p.observedRevisions = key
		p.stale = true
		p.nextRevisionCheck = time.Now().Add(100 * time.Millisecond)
		p.cancelMove()
	}
	if p.refreshBlocked || !p.hasConsumer() || time.Now().Before(p.nextRevisionCheck) || (!p.stale && !p.retryAccepted) {
		return
	}
	if ready, ok := p.app.(interface{ MappingRefreshReady([]string) bool }); ok && !ready.MappingRefreshReady(paths) {
		return
	}
	if p.pending != nil || p.results != nil {
		return
	}
	// An accepted edit must not silently retry a failed user scenario. Refresh
	// the displayed scenario while keeping that failed request independently.
	op := p.operation
	scenario, choices := p.scenario, p.choices
	if op.failed != nil && p.current != nil && p.current.projection != nil {
		p.scenario = p.current.projection.Scenario
		p.choices = p.current.projection.Scenario.Choices
	}
	p.queue(nil)
	p.pending.refresh = true
	p.operation.target = "accepted source changes"
	if op.failed != nil {
		p.operation = op
		p.scenario, p.choices = scenario, choices
	}
}

func (p *Panel) recoverSelection(previous *result) {
	p.treeDirty = true
	if p.focusRoot == "" {
		return
	}
	if _, ok := p.selectedRoot(); ok {
		return
	}
	id := p.focusRoot
	for previous != nil && id != "" {
		parent := ""
		for _, root := range previous.roots {
			if root.ID == id {
				parent = root.Parent
				break
			}
		}
		id = parent
		p.focusRoot = id
		if _, ok := p.selectedRoot(); ok {
			break
		}
	}
	p.focusRoot = id
	p.status = "The selected occurrence disappeared; selected its nearest surviving ancestor. Independent source documents remain open."
}
