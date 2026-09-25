package pmap

import (
	"strings"
	"testing"

	"sdmm/internal/app/ui/cpwsarea/wsmap/tools"
	"sdmm/internal/util"
)

func TestStatusToolSummaryUsesResolvedActionAndBlockedReason(t *testing.T) {
	hide := statusToolSummary(tools.ActionContext{
		Action:    "Hide exact type",
		Scope:     "all matching types in the local filter",
		Target:    "/obj/fixture/door",
		Available: true,
		Modifiers: tools.ToolModifiers{Alt: true},
		Cue:       tools.CueHideExactType,
	})
	for _, want := range []string{"Hide exact type", "Alt", "/obj/fixture/door", "local filter"} {
		if !strings.Contains(hide, want) {
			t.Errorf("hide status %q omits %q", hide, want)
		}
	}
	if strings.Contains(hide, "Delete") {
		t.Fatalf("non-destructive type hide is mislabeled as deletion: %q", hide)
	}

	blocked := statusToolSummary(tools.ActionContext{
		Action:    "Replace instance",
		Reason:    "Replacement must have the same DreamMaker base type",
		Available: false,
	})
	if !strings.Contains(blocked, "Unavailable") || !strings.Contains(blocked, "same DreamMaker base type") {
		t.Fatalf("blocked action summary hides its reason: %q", blocked)
	}
}

func TestStatusToolSummaryShowsCapturedMembershipAndFootprint(t *testing.T) {
	got := statusToolSummary(tools.ActionContext{
		Action:       "Select area and add membership",
		Badge:        "Area · Add",
		Scope:        "all matching areas on this level",
		Available:    true,
		Modifiers:    tools.ToolModifiers{Alt: true, Ctrl: true},
		Captured:     true,
		Footprint:    util.Bounds{X1: 1, Y1: 1, X2: 8, Y2: 4},
		HasFootprint: true,
	})
	for _, want := range []string{"Gesture", "Area · Add", "Ctrl+Alt", "8x4", "all matching areas"} {
		if !strings.Contains(got, want) {
			t.Errorf("membership status %q omits %q", got, want)
		}
	}
}

func TestActionContextNoticeDistinguishesUnavailableFromInformation(t *testing.T) {
	heading, message := actionContextNotice(tools.ActionContext{
		Available: true,
		Reason:    "Replacement changes the target prefab while preserving its instance identity",
	})
	if heading != "Details" || !strings.Contains(message, "preserving its instance identity") {
		t.Fatalf("available action reason presented as %q: %q", heading, message)
	}
	heading, message = actionContextNotice(tools.ActionContext{Available: false, Reason: "No target"})
	if heading != "Unavailable" || message != "No target" {
		t.Fatalf("unavailable action notice = %q: %q", heading, message)
	}
}
