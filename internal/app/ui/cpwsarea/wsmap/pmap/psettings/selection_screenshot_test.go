package psettings

import (
	"sdmm/internal/aphelion/editing"
	"sdmm/internal/dmapi/dm"
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
	if !policy.includes(p1, "/obj/visible") || !policy.includes(p3, "/obj/visible") || policy.includes(util.Point{X: 2, Y: 1, Z: 2}, "/obj/visible") || policy.includes(util.Point{X: 1, Y: 1, Z: 1}, "/obj/visible") || policy.includes(p1, "/obj/hidden") {
		t.Fatal("screenshot widened membership or changed its captured policy")
	}
}
