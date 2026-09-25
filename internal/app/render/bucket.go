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

func (r *Render) batchBucketUnits(viewBounds util.Bounds) {
	if MultiZRendering && r.Camera.Level > 1 {
		for level := 1; level < r.Camera.Level; level++ {
			r.batchLevel(level, viewBounds, false) // Draw everything below.
		}

		// Draw a "shadow" overlay to visually separate different levels.
		brush.RectFilled(viewBounds.X1, viewBounds.Y1, viewBounds.X2, viewBounds.Y2, multiZShadow)
	}

	r.batchLevel(r.Camera.Level, viewBounds, true) // Draw currently visible level.

	if r.overlay != nil {
		r.overlay.FlushUnits()
	}
}

func (r *Render) batchLevel(level int, viewBounds util.Bounds, withUnitHighlight bool) {
	visibleLevel := r.bucket.Level(level)
	if visibleLevel == nil {
		return
	}

	// APHELION EDIT ADDITION START - RETAINED SUBMISSIONS
	// Retained map-space submissions remain valid across camera movement; the
	// current viewport culls chunks and the current camera matrix transforms them.
	policyRevision, cacheable := r.retainedPolicyRevision()
	var highlightedUnitIDs map[uint64]struct{}
	if withUnitHighlight && r.overlay != nil {
		units := r.overlay.Units()
		if len(units) > 0 {
			highlightedUnitIDs = make(map[uint64]struct{}, len(units))
			for id := range units {
				highlightedUnitIDs[id] = struct{}{}
			}
		}
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
			// Ghost suppression changes base membership. A selected unit changes
			// painter order only in its own chunk-layer, so keep other layers retained.
			if cacheable && ghost == nil && !r.retainedChunkLayerHasHighlight(chunk, layer, policyRevision, highlightedUnitIDs, viewBounds) {
				r.drawRetainedChunkLayer(chunk, layer, policyRevision)
				continue
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

				brush.RectTexturedV(
					u.ViewBounds().X1, u.ViewBounds().Y1, u.ViewBounds().X2, u.ViewBounds().Y2,
					u.R(), u.G(), u.B(), u.A(),
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
func (r *Render) retainedPolicyRevision() (uint64, bool) {
	if r.unitProcessor == nil {
		return 0, true
	}
	versioned, ok := r.unitProcessor.(interface{ RenderPolicyRevision() uint64 })
	if !ok {
		return 0, false
	}
	return versioned.RenderPolicyRevision(), true
}
func (r *Render) retainedChunkLayerHasHighlight(c *chunk.Chunk, layer float32, policyRevision uint64, ids map[uint64]struct{}, viewBounds util.Bounds) bool {
	if len(ids) == 0 {
		return false
	}
	if r.retained != nil {
		key := rendercache.Key{Chunk: c, Layer: rendercache.LayerKey(layer)}
		versions := rendercache.Versions{Chunk: c.Revision(), Policy: policyRevision, Appearance: dmicon.Cache.Revision()}
		if entry, found := r.retained.Get(key, versions); found {
			return entry.IntersectsUnitIDs(ids)
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

func (r *Render) drawRetainedChunkLayer(c *chunk.Chunk, layer float32, policyRevision uint64) {
	if r.retained == nil {
		r.retained = rendercache.New()
	}
	key := rendercache.Key{Chunk: c, Layer: rendercache.LayerKey(layer)}
	versions := rendercache.Versions{Chunk: c.Revision(), Policy: policyRevision, Appearance: dmicon.Cache.Revision()}
	entry, found := r.retained.Get(key, versions)
	if !found {
		unitIDs := make([]uint64, 0, min(len(c.UnitsByLayers[layer]), rendercache.MaxIndexedUnitsPerEntry))
		indexComplete := true
		submission := brush.CaptureSubmission(func() {
			for _, u := range c.UnitsByLayers[layer] {
				if r.unitProcessor != nil && !r.unitProcessor.ProcessUnit(u) {
					continue
				}
				if indexComplete {
					if len(unitIDs) == rendercache.MaxIndexedUnitsPerEntry {
						unitIDs = nil
						indexComplete = false
					} else {
						unitIDs = append(unitIDs, u.Instance().Id())
					}
				}
				bounds := u.ViewBounds()
				brush.RectTexturedV(bounds.X1, bounds.Y1, bounds.X2, bounds.Y2, u.R(), u.G(), u.B(), u.A(), u.Sprite().Texture(), u.Sprite().U1, u.Sprite().V1, u.Sprite().U2, u.Sprite().V2)
			}
		})
		r.retained.RecordBuild(submission)
		if !r.retained.PutWithUnitIDs(key, versions, submission, unitIDs, indexComplete) {
			if submission != nil {
				submission.Draw(r.viewportWidth, r.viewportHeight, r.Camera.ShiftX, r.Camera.ShiftY, r.Camera.Scale)
				submission.Dispose()
			}
			return
		}
		entry, _ = r.retained.Get(key, versions)
	}
	if entry != nil && entry.Submission != nil {
		entry.Submission.Draw(r.viewportWidth, r.viewportHeight, r.Camera.ShiftX, r.Camera.ShiftY, r.Camera.Scale)
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
