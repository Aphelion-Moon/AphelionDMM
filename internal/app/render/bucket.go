package render

import (
	// APHELION EDIT ADDITION START - ASYNC ICONS
	"sdmm/internal/dmapi/dmicon"
	// APHELION EDIT ADDITION END
	"sdmm/internal/app/render/brush"
	// APHELION EDIT ADDITION START - RETAINED SUBMISSIONS
	"sdmm/internal/aphelion/rendercache"
	"sdmm/internal/app/render/bucket/level/chunk"
	// APHELION EDIT ADDITION END
	"sdmm/internal/app/render/bucket/level/chunk/unit"
	"sdmm/internal/util"
)

var (
	MultiZRendering = true

	multiZShadow = util.MakeColor(0, 0, 0, .35)
)

type unitProcessor interface {
	ProcessUnit(unit.Unit) (visible bool)
}

// APHELION EDIT CHANGE - SHARED BRUSH PASS - ORIGINAL: func (r *Render) batchBucketUnits(viewBounds util.Bounds) {
func (r *Render) batchBucketUnits(pass *brush.DrawPass, viewBounds util.Bounds) {
	if MultiZRendering && r.Camera.Level > 1 {
		for level := 1; level < r.Camera.Level; level++ {
			// APHELION EDIT CHANGE - SHARED BRUSH PASS - ORIGINAL: r.batchLevel(level, viewBounds, false) // Draw everything below.
			r.batchLevel(pass, level, viewBounds, false) // Draw everything below.
		}

		// Draw a "shadow" overlay to visually separate different levels.
		brush.RectFilled(viewBounds.X1, viewBounds.Y1, viewBounds.X2, viewBounds.Y2, multiZShadow)
	}

	// APHELION EDIT CHANGE - SHARED BRUSH PASS - ORIGINAL: r.batchLevel(r.Camera.Level, viewBounds, true) // Draw currently visible level.
	r.batchLevel(pass, r.Camera.Level, viewBounds, true) // Draw currently visible level.

	if r.overlay != nil {
		r.overlay.FlushUnits()
	}
}

// APHELION EDIT CHANGE - SHARED BRUSH PASS - ORIGINAL: func (r *Render) batchLevel(level int, viewBounds util.Bounds, withUnitHighlight bool) {
func (r *Render) batchLevel(pass *brush.DrawPass, level int, viewBounds util.Bounds, withUnitHighlight bool) {
	visibleLevel := r.bucket.Level(level)
	if visibleLevel == nil {
		return
	}

	// APHELION EDIT ADDITION START - RETAINED SUBMISSIONS
	// Retained map-space submissions remain valid across camera movement; the
	// current viewport culls chunks and the current camera matrix transforms them.
	policyRevision, cacheable := r.retainedPolicyRevision()
	// APHELION EDIT ADDITION START - AREA PRESENTATION
	areaPolicy := CurrentAreaPolicy()
	// APHELION EDIT ADDITION END
	var highlightedUnits map[uint64]HighlightUnit
	if withUnitHighlight && r.overlay != nil {
		// The UI owns this map and flushes it after all levels finish drawing.
		highlightedUnits = r.overlay.Units()
	}
	// APHELION EDIT ADDITION END
	// Iterate through every layer to render.
	// APHELION EDIT ADDITION START - PLACEMENT PRESENTATION
	var ghost *Presentation
	var ghostLayers []float32
	if p := r.presentation; p != nil && p.Ready && p.Anchor.Z == level {
		ghost = p
		ghostLayers = p.Layers
	}
	// APHELION EDIT CHANGE - PLACEMENT PRESENTATION - ORIGINAL: for _, layer := range visibleLevel.Layers {
	eachPresentationLayer(visibleLevel.Layers, ghostLayers, func(layer float32) {
		// APHELION EDIT ADDITION END
		// Iterate through chunks with units on the rendered layer.
		for _, chunk := range visibleLevel.ChunksByLayers[layer] {
			// Out of bounds = skip.
			// APHELION EDIT CHANGE - ASYNC ICONS - ORIGINAL: if !chunk.ViewBounds.ContainsV(viewBounds) {
			if !dmicon.Cache.ExpandPendingBounds(chunk.ViewBounds).ContainsV(viewBounds) {
				continue
			}

			// APHELION EDIT ADDITION START - RETAINED SUBMISSIONS
			// Suppressed chunks and highlighted chunk-layers need the stream path.
			// A conservative preview bound keeps unaffected committed chunks retained.
			if cacheable && (ghost == nil || ghost.MaySuppress != nil && !ghost.MaySuppress(chunk.MapBounds)) && !r.retainedChunkLayerHasHighlight(chunk, layer, policyRevision, highlightedUnits, viewBounds) {
				if r.drawRetainedChunkLayer(pass, chunk, layer, policyRevision) {
					continue
				}
			}
			// APHELION EDIT ADDITION END
			// Get all units in the chunk for the specific layer.
			for _, u := range chunk.UnitsByLayers[layer] {
				// Out of bounds = skip
				if !u.ViewBounds().ContainsV(viewBounds) {
					continue
				}
				// Process unit
				// APHELION EDIT ADDITION START - PLACEMENT PRESENTATION
				if ghost != nil && ghost.Suppress != nil && ghost.Suppress(u) {
					continue
				}
				// APHELION EDIT ADDITION END
				if r.unitProcessor != nil && !r.unitProcessor.ProcessUnit(u) {
					continue
				}

				// APHELION EDIT ADDITION START - AREA PRESENTATION
				unitAlpha, unitVisible := areaPolicy.ApplyUnit(u)
				if !unitVisible {
					continue
				}
				// APHELION EDIT ADDITION END
				brush.RectTexturedV(
					u.ViewBounds().X1, u.ViewBounds().Y1, u.ViewBounds().X2, u.ViewBounds().Y2,
					// APHELION EDIT CHANGE - AREA PRESENTATION - ORIGINAL: u.R(), u.G(), u.B(), u.A(),
					u.R(), u.G(), u.B(), unitAlpha,
					u.Sprite().Texture(),
					u.Sprite().U1, u.Sprite().V1, u.Sprite().U2, u.Sprite().V2,
				)

				if withUnitHighlight {
					r.batchUnitHighlight(u)
				}
			}
		}
		// APHELION EDIT ADDITION START - PLACEMENT PRESENTATION
		if ghost != nil {
			ghost.drawLayer(layer, viewBounds)
		}
	})
	// APHELION EDIT ADDITION END
}

// APHELION EDIT ADDITION START - RETAINED SUBMISSIONS
func (r *Render) RetainedCacheStats() rendercache.Stats {
	if r == nil || r.retained == nil {
		return rendercache.Stats{}
	}
	return r.retained.Stats()
}

func (r *Render) clearRetainedScene() {
	r.retainedPending = nil
	r.retainedQueued = nil
	if r.retained != nil {
		r.retained.Clear()
	}
}
func (r *Render) ReleaseRetainedSubmissions() {
	r.clearRetainedScene()
	if r.retained != nil {
		r.retained.DisposeRetired()
	}
}

// ReleaseRetainedSubmissionsStep permits disposed canvases to drain charged GL
// allocations through the existing deferred-frame queue after losing a pane.
func (r *Render) ReleaseRetainedSubmissionsStep() bool {
	r.clearRetainedScene()
	return r.retained != nil && r.retained.DisposeRetiredStep()
}
func (r *Render) retainedPolicyRevision() (uint64, bool) {
	// APHELION EDIT ADDITION START - AREA PRESENTATION
	// The area presentation policy is part of every retained submission's version.
	areaRevision := AreaPolicyRevision() * 0x9E3779B97F4A7C15
	// APHELION EDIT ADDITION END
	if r.unitProcessor == nil {
		// APHELION EDIT CHANGE - AREA PRESENTATION - ORIGINAL: return 0, true
		return areaRevision, true
	}
	versioned, ok := r.unitProcessor.(interface{ RenderPolicyRevision() uint64 })
	if !ok {
		return 0, false
	}
	// APHELION EDIT CHANGE - AREA PRESENTATION - ORIGINAL: return versioned.RenderPolicyRevision(), true
	return versioned.RenderPolicyRevision() ^ areaRevision, true
}
func (r *Render) retainedEntry(key rendercache.Key, versions rendercache.Versions) (*rendercache.Entry, bool) {
	var visible func(unit.Unit) bool
	if r.unitProcessor != nil {
		visible = r.unitProcessor.ProcessUnit
	}
	return r.retained.GetWithDependencies(key, versions, visible, dmicon.Cache)
}
func (r *Render) retainedChunkLayerHasHighlight(c *chunk.Chunk, layer float32, policyRevision uint64, ids map[uint64]HighlightUnit, viewBounds util.Bounds) bool {
	if len(ids) == 0 {
		return false
	}
	if r.retained != nil {
		key := rendercache.Key{Chunk: c, Layer: rendercache.LayerKey(layer)}
		versions := rendercache.Versions{Chunk: c.Revision(), Policy: policyRevision, Appearance: dmicon.Cache.Revision()}
		if entry, found := r.retainedEntry(key, versions); found {
			return rendercache.IntersectsUnitIDs(entry, ids)
		}
	}
	// A first highlighted frame has no retained index yet. Check only this
	// chunk-layer before deciding whether its original per-unit order is needed.
	for _, u := range c.UnitsByLayers[layer] {
		if !u.ViewBounds().ContainsV(viewBounds) {
			continue
		}
		if _, selected := ids[u.Instance().Id()]; selected {
			return true
		}
	}
	return false
}

func (r *Render) drawRetainedChunkLayer(pass *brush.DrawPass, c *chunk.Chunk, layer float32, policyRevision uint64) bool {
	if r.retained == nil {
		r.retained = rendercache.New()
	}
	key := rendercache.Key{Chunk: c, Layer: rendercache.LayerKey(layer)}
	versions := rendercache.Versions{Chunk: c.Revision(), Policy: policyRevision, Appearance: dmicon.Cache.Revision()}
	entry, found := r.retainedEntry(key, versions)
	if !found {
		r.queueRetainedPreparation(key, versions, layer)
		return false
	}
	if entry != nil && entry.Submission != nil {
		pass.DrawSubmission(entry.Submission)
	}
	return true
}

func (r *Render) prepareRetainedChunkLayer(c *chunk.Chunk, layer float32, key rendercache.Key, versions rendercache.Versions) {
	unitIDs := make([]uint64, 0, min(len(c.UnitsByLayers[layer]), rendercache.MaxIndexedUnitsPerEntry))
	indexComplete := true
	// APHELION EDIT ADDITION START - AREA PRESENTATION
	areaPolicy := CurrentAreaPolicy()
	// APHELION EDIT ADDITION END
	dependencies := rendercache.Dependencies{IconLifetime: dmicon.Cache.Lifetime(), Icons: make(map[string]uint64)}
	// One rectangle uses 152 GPU bytes. This upper estimate also admits
	// transient staging, worst-case draw calls, and retained unit IDs.
	submission, err := r.retained.CaptureAdmittedSubmission(uint64(len(c.UnitsByLayers[layer]))*512+4096, func() {
		for _, u := range c.UnitsByLayers[layer] {
			if r.unitProcessor != nil && !r.unitProcessor.ProcessUnit(u) {
				continue
			}
			// APHELION EDIT ADDITION START - AREA PRESENTATION
			unitAlpha, unitVisible := areaPolicy.ApplyUnit(u)
			if !unitVisible {
				continue
			}
			// APHELION EDIT ADDITION END
			if indexComplete {
				if len(unitIDs) == rendercache.MaxIndexedUnitsPerEntry {
					unitIDs = nil
					indexComplete = false
				} else {
					unitIDs = append(unitIDs, u.Instance().Id())
				}
			}
			// The drawn icon: an in-game part (a spawned window) loads its own DMI.
			icon := u.Icon()
			dependencies.Icons[icon] = dmicon.Cache.IconRevision(icon)
			bounds := u.ViewBounds()
			// APHELION EDIT CHANGE - AREA PRESENTATION - ORIGINAL: u.A()
			brush.RectTexturedV(bounds.X1, bounds.Y1, bounds.X2, bounds.Y2, u.R(), u.G(), u.B(), unitAlpha, u.Sprite().Texture(), u.Sprite().U1, u.Sprite().V1, u.Sprite().U2, u.Sprite().V2)
		}
	})
	if err != nil {
		// Failed pre-admission must make progress even when the oldest charged
		// submission is now offscreen and would otherwise never be revisited.
		r.retained.EvictOldest()
		return
	}
	r.retained.RecordBuild(submission)
	if !r.retained.PutWithDependencies(key, versions, submission, unitIDs, indexComplete, dependencies) {
		if submission != nil {
			submission.Dispose()
		}
		return
	}
}

// APHELION EDIT ADDITION END - RETAINED SUBMISSIONS

func (r *Render) batchUnitHighlight(u unit.Unit) {
	if r.overlay == nil {
		return
	}
	if highlight := r.overlay.Units()[u.Instance().Id()]; highlight != nil {
		r, g, b, a := highlight.Color().RGBA()
		brush.RectTexturedV(
			u.ViewBounds().X1, u.ViewBounds().Y1, u.ViewBounds().X2, u.ViewBounds().Y2,
			r, g, b, a,
			u.Sprite().Texture(),
			u.Sprite().U1, u.Sprite().V1, u.Sprite().U2, u.Sprite().V2,
		)
	}
}
