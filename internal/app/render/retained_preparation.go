// APHELION EDIT ADDITION START - BOUNDED RETAINED PREPARATION
package render

import (
	"sdmm/internal/aphelion/diagnostics/uistage"
	"sdmm/internal/aphelion/rendercache"
	"sdmm/internal/dmapi/dmicon"
)

type retainedPreparation struct {
	key   rendercache.Key
	layer float32
}

func (r *Render) queueRetainedPreparation(key rendercache.Key, versions rendercache.Versions, layer float32) {
	// Dense individual entries keep the policy-correct stream path. Admission
	// bounds both the queue and indivisible upload work, without hiding geometry.
	if len(key.Chunk.UnitsByLayers[layer]) > rendercache.MaxIndexedUnitsPerEntry {
		return
	}
	if r.retainedQueued == nil {
		r.retainedQueued = make(map[rendercache.Key]rendercache.Versions)
	}
	if _, exists := r.retainedQueued[key]; exists {
		r.retainedQueued[key] = versions
		return
	}
	if len(r.retainedPending) >= 512 {
		return
	}
	r.retainedQueued[key] = versions
	r.retainedPending = append(r.retainedPending, retainedPreparation{key: key, layer: layer})
}

func (r *Render) reprioritizeRetained() {
	view := r.viewportBounds(r.viewportWidth, r.viewportHeight)
	if view == r.retainedView {
		return
	}
	r.retainedView = view
	kept := r.retainedPending[:0]
	for _, job := range r.retainedPending {
		if dmicon.Cache.ExpandPendingBounds(job.key.Chunk.ViewBounds).ContainsV(view) {
			kept = append(kept, job)
		} else {
			delete(r.retainedQueued, job.key)
		}
	}
	clear(r.retainedPending[len(kept):])
	r.retainedPending = kept
}

func (r *Render) processRetainedPreparation(b *LevelBuildBudget) bool {
	if r.retained != nil && r.retained.HasRetired() && (r.retainedRetireNext || len(r.retainedPending) == 0) {
		if !b.consume() {
			return false
		}
		r.retainedRetireNext = false
		return r.retained.DisposeRetiredStep()
	}
	if len(r.retainedPending) == 0 || !b.consume() {
		return false
	}
	job := r.retainedPending[0]
	r.retainedRetireNext = true
	r.retainedPending[0] = retainedPreparation{}
	r.retainedPending = r.retainedPending[1:]
	versions := r.retainedQueued[job.key]
	delete(r.retainedQueued, job.key)
	policy, cacheable := r.retainedPolicyRevision()
	current := rendercache.Versions{Chunk: job.key.Chunk.Revision(), Policy: policy, Appearance: dmicon.Cache.Revision()}
	if !cacheable || versions != current || r.viewportWidth <= 0 || r.viewportHeight <= 0 {
		return true
	}
	if !dmicon.Cache.ExpandPendingBounds(job.key.Chunk.ViewBounds).ContainsV(r.viewportBounds(r.viewportWidth, r.viewportHeight)) {
		return true
	}
	if _, found := r.retained.Get(job.key, current); found {
		return true
	}
	region := uistage.Begin("aphelion.retained.prepare_upload")
	r.prepareRetainedChunkLayer(job.key.Chunk, job.layer, job.key, current)
	region.End()
	return true
}

// APHELION EDIT ADDITION END
