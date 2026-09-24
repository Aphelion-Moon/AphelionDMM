// APHELION EDIT ADDITION START - HELD ROTATION
package editor

import (
	"sdmm/internal/aphelion/editing"
	"sdmm/internal/app/render"
	"sdmm/internal/app/render/bucket/level/chunk/unit"
	"sdmm/internal/dmapi/dm"
	"sdmm/internal/dmapi/dmmap"
	"sdmm/internal/dmapi/dmmap/dmmdata/dmmprefab"
	"sdmm/internal/dmapi/dmmap/dmminstance"
	"sdmm/internal/util"
)

type heldPresentation struct {
	prefab      *dmmprefab.Prefab
	visual      *render.Presentation
	alternative bool
}

// PreviewHeldPrefab owns only presentation. A pointer sample updates Anchor;
// prefab changes prepare one sprite without touching map instances or history.
func (e *Editor) PreviewHeldPrefab(prefab *dmmprefab.Prefab, target util.Point, alternative bool) {
	if e.heldPresentation == nil && (prefab == nil || e.mapViewClosed) {
		return
	}
	r := e.pMap.Canvas().Render()
	if prefab == nil || e.mapViewClosed {
		if e.heldPresentation != nil {
			e.heldPresentation = nil
			if r != nil && e.paste == nil && e.selectionMove == nil {
				r.SetPresentation(nil)
			}
		}
		return
	}
	if r == nil || e.paste != nil || e.selectionMove != nil || e.localWork != nil {
		return
	}
	held := e.heldPresentation
	if held == nil || held.prefab != prefab {
		held = &heldPresentation{prefab: prefab, visual: &render.Presentation{IconSize: dmmap.WorldIconSize}}
		held.visual.Add(render.PrepareAppearance(util.Point{X: 1, Y: 1, Z: 1}, dmminstance.New(util.Point{X: 1, Y: 1, Z: 1}, prefab), dmmap.WorldIconSize))
		held.visual.Visible = func(a render.Appearance) bool {
			return e.dmm.HasTile(held.visual.Anchor) && held.visual.Anchor.Z == e.pMap.ActiveLevel() && e.app.PathsFilter().IsVisiblePath(a.Path)
		}
		held.visual.Suppress = func(u unit.Unit) bool {
			if !e.dmm.HasTile(held.visual.Anchor) || u.Instance().Coord() != held.visual.Anchor {
				return false
			}
			sourceChannel := editing.ChannelForPath(held.prefab.Path())
			channel := editing.ChannelForPath(u.Instance().Prefab().Path())
			replaces := !held.alternative && sourceChannel < editing.Objects || held.alternative && dm.IsPath(held.prefab.Path(), "/obj")
			return channel == sourceChannel && replaces && e.app.PathsFilter().IsVisiblePath(u.Instance().Prefab().Path())
		}
		held.visual.Finish()
		e.heldPresentation = held
	}
	held.visual.Anchor = target
	held.alternative = alternative
	r.SetPresentation(held.visual)
}

// APHELION EDIT ADDITION END
