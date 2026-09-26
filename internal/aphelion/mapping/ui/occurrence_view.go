package mappingui

import (
	"sdmm/internal/app/render/bucket/level/chunk/unit"
	"sdmm/internal/dmapi/dmmap/dmminstance"
)

// A mask consumes the one immutable displayed map. It never controls source
// picking or mutation; those continue to use the matching projection provenance.
type occurrenceView struct {
	panel   *Panel
	index   map[uint64]string
	ids     map[string]bool
	include bool
}

func (v *occurrenceView) ProcessUnit(u unit.Unit) bool {
	if !v.panel.ProcessUnit(u) {
		return false
	}
	if u.Instance() == nil {
		return !v.include
	}
	return v.ids[v.index[u.Instance().Id()]] == v.include
}
func (v *occurrenceView) RenderPolicyRevision() uint64 { return v.panel.RenderPolicyRevision() }

func (v *occurrenceView) GeometryInstanceVisible(instance *dmminstance.Instance) bool {
	return instance != nil && v.ids[v.index[instance.Id()]] == v.include
}

func (p *Panel) occurrenceView(r *result, id string, include bool) *occurrenceView {
	return &occurrenceView{p, r.occurrences, r.projection.OccurrenceIDs(id), include}
}
