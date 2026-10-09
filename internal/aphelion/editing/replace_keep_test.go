package editing

import (
	"reflect"
	"testing"
)

func TestKeptEditsCarriesOverOnlyMeaningfulEdits(t *testing.T) {
	defaults := map[string]string{"dir": "8", "name": `"windoor"`, "req_access": "null", "icon_state": `"left"`}
	lookup := func(name string) (string, bool) { v, ok := defaults[name]; return v, ok }
	old := []VarEdit{{"dir", "8"}, {"name", `"Reception Desk"`}, {"req_access", `list("security")`}, {"legacy_var", "1"}, {"icon_state", `"right"`}}
	selected := []VarEdit{{"icon_state", `"left"`}}
	got, dropped := KeptEdits(old, selected, lookup)
	want := []VarEdit{{"icon_state", `"left"`}, {"name", `"Reception Desk"`}, {"req_access", `list("security")`}}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("kept = %v, want %v", got, want)
	}
	// dir matches the new default; legacy_var is undeclared on the new type;
	// icon_state is overridden by the selected prefab.
	if !reflect.DeepEqual(dropped, []string{"dir", "legacy_var", "icon_state"}) {
		t.Fatalf("dropped = %v", dropped)
	}
}
