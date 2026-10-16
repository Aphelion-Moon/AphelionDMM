package maplint

import "testing"

// Meridian-Rift's cable_varedits.yml and windoor_var_edits.yml shapes.
const (
	editCableRule    = "/obj/structure/cable:\n  banned_variables: true\n"
	editWindoorRule  = "help: \"Use the directional variants when possible.\"\n/obj/machinery/door/window:\n  banned_variables:\n    dir:\n      deny: [1, 2, 4, 8]\n"
	editNeighborRule = "/obj/machinery/disposal:\n  required_neighbors:\n    - /obj/structure/disposalpipe/trunk\n"
)

func TestEditViolationsReportVariableBansTheEditAdds(t *testing.T) {
	rs := mustLoad(t, map[string]string{"cable_varedits.yml": editCableRule, "windoor_var_edits.yml": editWindoorRule, "disposals_without_trunks.yml": editNeighborRule})

	got := rs.EditViolations(atom("/obj/structure/cable"), atom("/obj/structure/cable", "dir", "8"))
	if len(got) != 1 || got[0].Kind != KindBannedVariables || !got[0].AnyValue || got[0].RuleFile != "cable_varedits.yml" {
		t.Fatalf("cable dir edit: %+v", got)
	}

	got = rs.EditViolations(atom("/obj/machinery/door/window"), atom("/obj/machinery/door/window", "dir", "4"))
	if len(got) != 1 || got[0].Kind != KindBannedVariable || got[0].AnyValue || got[0].Variable != "dir" || got[0].Help == "" {
		t.Fatalf("windoor dir edit: %+v", got)
	}

	// A turn between two values the rule already rejects adds nothing new.
	if got := rs.EditViolations(atom("/obj/machinery/door/window", "dir", "8"), atom("/obj/machinery/door/window", "dir", "1")); len(got) != 0 {
		t.Fatalf("existing violation reported as new: %+v", got)
	}
	// Allowed values and unrelated types pass.
	if got := rs.EditViolations(atom("/obj/machinery/door/window"), atom("/obj/machinery/door/window", "dir", "5")); len(got) != 0 {
		t.Fatalf("allowed value: %+v", got)
	}
	// Neighbor rules need the tile; an edit check must not report them.
	if got := rs.EditViolations(atom("/obj/machinery/disposal/bin"), atom("/obj/machinery/disposal/bin", "dir", "4")); len(got) != 0 {
		t.Fatalf("neighbor rule reported for a lone atom: %+v", got)
	}
	var none *RuleSet
	if got := none.EditViolations(atom("/obj/structure/cable"), atom("/obj/structure/cable", "dir", "8")); got != nil {
		t.Fatal("nil rule set must report nothing")
	}
}
