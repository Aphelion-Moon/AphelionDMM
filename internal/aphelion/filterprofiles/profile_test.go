package filterprofiles

import (
	"bytes"
	"reflect"
	"testing"

	"sdmm/internal/dmapi/dm"
	"sdmm/internal/dmapi/dmenv"
)

func testCatalog() *dmenv.Dme {
	return &dmenv.Dme{Objects: map[string]*dmenv.Object{
		"/obj":           {Path: "/obj", DirectChildren: []string{"/obj/foo", "/obj/foobar"}},
		"/obj/foo":       {Path: "/obj/foo", DirectChildren: []string{"/obj/foo/leaf", "/obj/foo/child"}},
		"/obj/foo/leaf":  {Path: "/obj/foo/leaf"},
		"/obj/foo/child": {Path: "/obj/foo/child"},
		"/obj/foobar":    {Path: "/obj/foobar"},
	}}
}

func testFilter(env *dmenv.Dme) *dm.PathsFilter {
	return dm.NewPathsFilter(func(path string) []string {
		if object := env.Objects[path]; object != nil {
			return object.DirectChildren
		}
		return nil
	})
}

func TestBuiltinsContainOnlyApprovedSubtreeIncludes(t *testing.T) {
	got := make(map[string][]string)
	for _, profile := range Builtins() {
		if profile.DefaultVisible {
			t.Errorf("%s must default to isolated visibility", profile.ID)
		}
		for _, rule := range profile.Rules {
			if rule.Scope != ScopeSubtree || !rule.Visible {
				t.Errorf("%s includes unexpected rule %+v", profile.ID, rule)
			}
			got[profile.ID] = append(got[profile.ID], rule.Path)
		}
	}
	want := map[string][]string{
		"builtin:piping-atmospherics": {"/obj/machinery/atmospherics"},
		"builtin:wiring-power":        {"/obj/structure/cable", "/obj/machinery/power"},
		"builtin:disposal":            {"/obj/structure/disposalpipe", "/obj/machinery/disposal", "/obj/structure/disposaloutlet"},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("built-ins = %v, want %v", got, want)
	}
}

func TestCompileHonorsExactRulesBoundariesAndSpecificity(t *testing.T) {
	env := testCatalog()
	profile := Profile{ID: "custom", Name: "test", DefaultVisible: false, Rules: []Rule{
		{Path: "/obj/foo", Scope: ScopeSubtree, Visible: true},
		{Path: "/obj/foo", Scope: ScopeExact, Visible: false},
	}}
	compiled, err := Compile(profile, Overrides{}, env)
	if err != nil {
		t.Fatal(err)
	}
	filter := testFilter(env)
	filter.ApplyHiddenPaths(compiled.HiddenPaths)
	if filter.IsVisiblePath("/obj/foo") {
		t.Fatal("exact rule did not override subtree rule at the same path")
	}
	if !filter.IsVisiblePath("/obj/foo/leaf") || filter.IsVisiblePath("/obj/foobar") {
		t.Fatal("exact/subtree matching crossed the wrong type boundary")
	}

	profile.Rules = append(profile.Rules, Rule{Path: "/obj/foo/leaf", Scope: ScopeExact, Visible: false})
	compiled, err = Compile(profile, Overrides{}, env)
	if err != nil {
		t.Fatal(err)
	}
	filter.ApplyHiddenPaths(compiled.HiddenPaths)
	if filter.IsVisiblePath("/obj/foo/leaf") || !filter.IsVisiblePath("/obj/foo/child") {
		t.Fatal("deeper exact rule did not override only its matching type")
	}
}

func TestCompileTemporaryOverrideWinsOnlyAtEqualSpecificity(t *testing.T) {
	env := testCatalog()
	profile := Profile{ID: "custom", Name: "test", DefaultVisible: false, Rules: []Rule{
		{Path: "/obj", Scope: ScopeSubtree, Visible: true},
		{Path: "/obj/foo", Scope: ScopeSubtree, Visible: false},
	}}
	overrides := Overrides{Rules: []Rule{{Path: "/obj/foo", Scope: ScopeSubtree, Visible: true}}}
	first, err := Compile(profile, overrides, env)
	if err != nil {
		t.Fatal(err)
	}
	profile.Rules[0], profile.Rules[1] = profile.Rules[1], profile.Rules[0]
	second, err := Compile(profile, overrides, env)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(first.HiddenPaths, second.HiddenPaths) {
		t.Fatalf("rule ordering changed output: %v and %v", first.HiddenPaths, second.HiddenPaths)
	}
	filter := testFilter(env)
	filter.ApplyHiddenPaths(first.HiddenPaths)
	if !filter.IsVisiblePath("/obj/foo/leaf") {
		t.Fatal("same-scope temporary subtree override did not win")
	}
	profile.Rules = append(profile.Rules, Rule{Path: "/obj/foo/leaf", Scope: ScopeExact, Visible: false})
	compiled, err := Compile(profile, overrides, env)
	if err != nil {
		t.Fatal(err)
	}
	filter.ApplyHiddenPaths(compiled.HiddenPaths)
	if filter.IsVisiblePath("/obj/foo/leaf") {
		t.Fatal("more-specific exact profile rule lost to broader override")
	}
}

func TestUnresolvedRulesAreWarnedAndRetained(t *testing.T) {
	profile := Profile{ID: "custom", Name: "test", Rules: []Rule{{Path: "/obj/missing", Scope: ScopeSubtree, Visible: true}}}
	compiled, err := Compile(profile, Overrides{}, testCatalog())
	if err != nil {
		t.Fatal(err)
	}
	if len(compiled.Warnings) != 1 || compiled.Warnings[0].Path != "/obj/missing" {
		t.Fatalf("unresolved warnings = %+v", compiled.Warnings)
	}
	if profile.Rules[0].Path != "/obj/missing" {
		t.Fatal("compile discarded the unresolved declaration")
	}
}

func TestProfileDocumentVersionAndSizeValidation(t *testing.T) {
	profile := Profile{ID: "custom", Name: "test", Rules: []Rule{{Path: "/obj/foo", Scope: ScopeExact, Visible: false}}}
	encoded, err := Encode(profile)
	if err != nil {
		t.Fatal(err)
	}
	decoded, err := Decode(encoded)
	if err != nil || !reflect.DeepEqual(decoded, profile) {
		t.Fatalf("round trip = %+v, err %v", decoded, err)
	}
	for _, input := range [][]byte{
		[]byte("{\"format\":\"aphelion-filter-profile\",\"version\":2,\"profile\":{\"id\":\"x\",\"name\":\"x\"}}"),
		[]byte("{\"format\":\"aphelion-filter-profile\",\"version\":1,\"profile\":{\"id\":\"x\",\"name\":\"x\",\"extra\":true}}"),
		append(bytes.Clone(encoded), []byte("{}")...),
		bytes.Repeat([]byte("x"), MaxDocumentSize+1),
	} {
		if _, err := Decode(input); err == nil {
			t.Errorf("invalid document accepted: %.50q", input)
		}
	}
}

func TestStoreKeepsBuiltinsImmutableAndSupportsCustomCRUD(t *testing.T) {
	store := NewStore()
	builtin, _ := Builtin("builtin:piping-atmospherics")
	if err := store.Add(builtin); err == nil {
		t.Fatal("built-in profile was added to the custom store")
	}
	if err := store.Add(Profile{ID: "custom-1", Name: "My profile", DefaultVisible: true}); err != nil {
		t.Fatal(err)
	}
	copy, err := Duplicate(builtin, "custom-2", "Pipes copy")
	if err != nil {
		t.Fatal(err)
	}
	if err := store.Add(copy); err != nil {
		t.Fatal(err)
	}
	if err := store.Rename("custom-2", "my PROFILE"); err == nil {
		t.Fatal("case-insensitive duplicate name was accepted")
	}
	if err := store.Rename("custom-2", "Renamed"); err != nil {
		t.Fatal(err)
	}
	if err := store.Delete("custom-2"); err != nil {
		t.Fatal(err)
	}
	if _, found := store.Find("custom-2"); found {
		t.Fatal("deleted custom profile remains")
	}
	if _, found := store.Find(builtin.ID); !found {
		t.Fatal("custom delete affected the built-in")
	}
}

func TestSessionUnhideLastShowAllAndResetAreBatched(t *testing.T) {
	env := testCatalog()
	filter := testFilter(env)
	session := Session{}
	profile := Profile{ID: "custom", Name: "test", DefaultVisible: false, Rules: []Rule{
		{Path: "/obj/foo", Scope: ScopeSubtree, Visible: true},
	}}
	if err := session.Apply(profile, env, filter); err != nil {
		t.Fatal(err)
	}
	revision := filter.PolicyRevision()
	if err := session.SetVisibility("/obj/foo", ScopeExact, false, env, filter); err != nil {
		t.Fatal(err)
	}
	if filter.PolicyRevision() != revision+1 || !filter.IsHiddenPath("/obj/foo") || !filter.IsVisiblePath("/obj/foo/leaf") {
		t.Fatal("exact hide did not publish one isolated update")
	}
	revision = filter.PolicyRevision()
	if err := session.UnhideLast(env, filter); err != nil {
		t.Fatal(err)
	}
	if filter.PolicyRevision() != revision+1 || !filter.IsVisiblePath("/obj/foo") {
		t.Fatal("Unhide last did not restore the exact type in one update")
	}
	if err := session.ShowAll(env, filter); err != nil {
		t.Fatal(err)
	}
	if len(filter.HiddenPaths()) != 0 || !session.OverridesDirty() {
		t.Fatal("Show All did not install a dirty temporary override")
	}
	if err := session.Reset(env, filter); err != nil {
		t.Fatal(err)
	}
	if session.OverridesDirty() || filter.IsVisiblePath("/obj/foobar") || !filter.IsVisiblePath("/obj/foo/leaf") {
		t.Fatal("Reset did not restore the active profile")
	}
}

func TestRepeatedNoopHideDoesNotReplaceRecoveryTarget(t *testing.T) {
	env := testCatalog()
	filter := testFilter(env)
	s := Session{}
	if err := s.Apply(DefaultProfile(), env, filter); err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{"/obj/foo", "/obj/foobar", "/obj/foo"} {
		if err := s.SetVisibility(path, ScopeExact, false, env, filter); err != nil {
			t.Fatal(err)
		}
	}
	if err := s.UnhideLast(env, filter); err != nil {
		t.Fatal(err)
	}
	if !filter.IsVisiblePath("/obj/foobar") || !filter.IsHiddenPath("/obj/foo") {
		t.Fatal("no-op hide replaced the last effective hide recovery target")
	}
}

func TestVisibilityHistoryNavigatesAndBranchesWithoutChangingSavedProfile(t *testing.T) {
	env := testCatalog()
	filter := testFilter(env)
	s := Session{}
	profile := DefaultProfile()
	if err := s.Apply(profile, env, filter); err != nil {
		t.Fatal(err)
	}
	for _, p := range []string{"/obj/foo", "/obj/foobar"} {
		if err := s.SetVisibility(p, ScopeExact, false, env, filter); err != nil {
			t.Fatal(err)
		}
	}
	if err := s.UndoVisibility(env, filter); err != nil {
		t.Fatal(err)
	}
	if !filter.IsHiddenPath("/obj/foo") || filter.IsHiddenPath("/obj/foobar") {
		t.Fatal("undo changed unrelated visibility")
	}
	if err := s.RedoVisibility(env, filter); err != nil {
		t.Fatal(err)
	}
	if !filter.IsHiddenPath("/obj/foobar") {
		t.Fatal("redo did not restore hide")
	}
	if err := s.UndoVisibility(env, filter); err != nil {
		t.Fatal(err)
	}
	if err := s.SetVisibility("/obj/foo/leaf", ScopeExact, false, env, filter); err != nil {
		t.Fatal(err)
	}
	if err := s.RedoVisibility(env, filter); err == nil {
		t.Fatal("new command retained stale redo branch")
	}
	active, _ := s.Active()
	if !reflect.DeepEqual(active, profile) {
		t.Fatal("visibility history changed saved profile")
	}
}

func TestVisibilityHistoryRemainsBoundedAndRecoversLatestChange(t *testing.T) {
	env := testCatalog()
	filter := testFilter(env)
	s := Session{}
	if err := s.Apply(DefaultProfile(), env, filter); err != nil {
		t.Fatal(err)
	}
	for i := range 300 {
		if err := s.SetVisibility("/obj/foo", ScopeExact, i%2 == 0, env, filter); err != nil {
			t.Fatal(err)
		}
	}
	status := s.HistoryStatus()
	if !status.Trimmed || status.Count > 256 || s.historyBytes > 4<<20 {
		t.Fatalf("unbounded history: %+v", status)
	}
	if err := s.UndoVisibility(env, filter); err != nil {
		t.Fatal(err)
	}
	if filter.IsHiddenPath("/obj/foo") {
		t.Fatal("history expiry lost latest undo")
	}
}
