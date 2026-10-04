package psettings

import (
	"sdmm/internal/aphelion/editing"
	"sdmm/internal/dmapi/dm"
	"sdmm/internal/dmapi/dmmap/dmmdata/dmmprefab"
	"sdmm/internal/dmapi/dmmap/dmminstance"
	"sdmm/internal/dmapi/dmvars"
	"sdmm/internal/util"
	"testing"
)

func TestScreenshotPolicyUsesSparseMembershipAndCapturedFilter(t *testing.T) {
	p1, p3 := util.Point{X: 1, Y: 1, Z: 2}, util.Point{X: 3, Y: 1, Z: 2}
	selection, err := editing.MaskSelection([]util.Point{p1, p3})
	if err != nil {
		t.Fatal(err)
	}
	filter := dm.NewPathsFilterEmpty()
	filter.TogglePath("/obj/hidden")
	policy := screenshotPolicy{selection: selection, filter: filter.Copy()}
	filter.Clear()
	geometry, ok := any(policy).(interface {
		GeometryInstanceVisible(*dmminstance.Instance) bool
	})
	if !ok {
		t.Fatal("screenshot selection is applied only after whole-map geometry construction")
	}
	for _, point := range []util.Point{p1, p3, {X: 2, Y: 1, Z: 2}, {X: 1, Y: 1, Z: 1}} {
		for _, path := range []string{"/obj/visible", "/obj/hidden"} {
			instance := dmminstance.New(point, dmmprefab.New(0, path, (&dmvars.MutableVariables{}).ToImmutable()))
			if geometry.GeometryInstanceVisible(instance) != policy.includes(point, path) {
				t.Fatal("geometry filtering differs from screenshot membership")
			}
		}
	}
	if !policy.includes(p1, "/obj/visible") || !policy.includes(p3, "/obj/visible") || policy.includes(util.Point{X: 2, Y: 1, Z: 2}, "/obj/visible") || policy.includes(util.Point{X: 1, Y: 1, Z: 1}, "/obj/visible") || policy.includes(p1, "/obj/hidden") {
		t.Fatal("screenshot widened membership or changed its captured policy")
	}
}
