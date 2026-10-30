package dm

import "testing"

func TestPathsFilterHiddenChildSummaryUsesTypeBoundaries(t *testing.T) {
	filter := NewPathsFilterEmpty()
	if !filter.ApplyHiddenPaths([]string{"/obj/foobar"}) {
		t.Fatal("expected the hidden-path set to change")
	}
	if !filter.HasHiddenChildPath("/obj") {
		t.Fatal("expected /obj to summarize its hidden descendant")
	}
	if filter.HasHiddenChildPath("/obj/foo") {
		t.Fatal("/obj/foobar is a sibling, not a descendant of /obj/foo")
	}
}

func TestPathsFilterSubtreeToggleUpdatesSummaryAndRevisionOnce(t *testing.T) {
	children := map[string][]string{
		"/obj":        {"/obj/foo", "/obj/foobar"},
		"/obj/foo":    {"/obj/foo/leaf"},
		"/obj/foobar": nil,
	}
	filter := NewPathsFilter(func(path string) []string { return children[path] })

	filter.TogglePath("/obj")
	if got := filter.PolicyRevision(); got != 1 {
		t.Fatalf("one subtree toggle should publish one revision; got %d", got)
	}
	if !filter.HasHiddenChildPath("/obj/foo") {
		t.Fatal("expected a hidden leaf below /obj/foo")
	}

	filter.TogglePath("/obj/foo")
	if got := filter.PolicyRevision(); got != 2 {
		t.Fatalf("second subtree toggle should publish one additional revision; got %d", got)
	}
	if !filter.IsHiddenPath("/obj") || !filter.IsHiddenPath("/obj/foobar") {
		t.Fatal("the parent and sibling should remain hidden")
	}
	if filter.IsHiddenPath("/obj/foo") || filter.IsHiddenPath("/obj/foo/leaf") {
		t.Fatal("re-showing the child should show its subtree")
	}
	if !filter.HasHiddenChildPath("/obj") {
		t.Fatal("the parent summary should still include the hidden sibling")
	}
	if filter.HasHiddenChildPath("/obj/foo") {
		t.Fatal("a shown child subtree should have no hidden descendants")
	}
}

func TestPathsFilterApplyHiddenPathsBatchesRevisionAndCopiesSummary(t *testing.T) {
	paths := []string{"/obj/foo", "/obj/foo/leaf"}
	filter := NewPathsFilterEmpty()
	if !filter.ApplyHiddenPaths(paths) {
		t.Fatal("expected the hidden-path set to change")
	}
	paths[0] = "/obj/reused"
	if !filter.IsHiddenPath("/obj/foo") {
		t.Fatal("filter must not retain the caller's input slice")
	}
	if got := filter.PolicyRevision(); got != 1 {
		t.Fatalf("one batch should publish one revision; got %d", got)
	}
	if filter.ApplyHiddenPaths([]string{"/obj/foo/leaf", "/obj/foo"}) {
		t.Fatal("an equivalent policy should not publish a new revision")
	}

	copy := filter.Copy()
	filter.Clear()
	if got := filter.PolicyRevision(); got != 2 {
		t.Fatalf("clear should publish one revision; got %d", got)
	}
	if !copy.HasHiddenChildPath("/obj/foo") || copy.PolicyRevision() != 1 {
		t.Fatal("copy should retain its independent summary and revision")
	}
}

// APHELION EDIT ADDITION START - UNKNOWN TYPE VISIBILITY
func unknownTypeFilter() *PathsFilter {
	children := map[string][]string{
		"/area":        {"/area/engine", "/area/hallway"},
		"/area/engine": {"/area/engine/core"},
		"/obj":         {"/obj/item"},
	}
	known := map[string]bool{"/area": true, "/area/engine": true, "/area/engine/core": true, "/area/hallway": true, "/obj": true, "/obj/item": true}
	filter := NewPathsFilter(func(path string) []string { return children[path] })
	filter.SetKnownPath(func(path string) bool { return known[path] })
	return filter
}

func TestPathsFilterUnknownTypeFollowsNearestKnownAncestor(t *testing.T) {
	filter := unknownTypeFilter()
	if filter.IsHiddenPath("/area/engine/break_room") {
		t.Fatal("nothing is hidden yet")
	}

	filter.TogglePath("/area")
	for _, path := range []string{"/area/engine/break_room", "/area/removed", "/area/removed/deeper"} {
		if !filter.IsHiddenPath(path) || filter.IsVisiblePath(path) {
			t.Fatalf("unknown %s should be hidden with /area", path)
		}
	}
	if filter.IsHiddenPath("/obj/item/removed") {
		t.Fatal("an unknown /obj type must follow /obj, not /area")
	}

	filter.TogglePath("/area/engine")
	if filter.IsHiddenPath("/area/engine/break_room") {
		t.Fatal("the nearest known ancestor /area/engine is shown, so its unknown child is shown")
	}
	if !filter.IsHiddenPath("/area/removed") {
		t.Fatal("/area/removed still follows the hidden /area")
	}

	copied := filter.Copy()
	if copied.IsHiddenPath("/area/engine/break_room") || !copied.IsHiddenPath("/area/removed") {
		t.Fatal("a copy must keep the unknown-type rule")
	}
}

func TestPathsFilterUnknownTypeFollowsDeepestHiddenKnownPrefix(t *testing.T) {
	filter := unknownTypeFilter()
	filter.TogglePath("/area/engine")
	if !filter.IsHiddenPath("/area/engine/break_room") {
		t.Fatal("an unknown type below hidden /area/engine should be hidden")
	}
	if filter.IsHiddenPath("/area/removed") || filter.IsHiddenPath("/area/hallway") {
		t.Fatal("hiding /area/engine must not hide its siblings")
	}
}

func TestPathsFilterKnownTypesKeepExactVisibility(t *testing.T) {
	filter := unknownTypeFilter()
	filter.TogglePath("/area")
	filter.TogglePath("/area/engine/core")
	if filter.IsHiddenPath("/area/engine/core") {
		t.Fatal("a known type shown explicitly must not inherit its hidden ancestor")
	}

	plain := NewPathsFilterEmpty()
	plain.ApplyHiddenPaths([]string{"/area"})
	if plain.IsHiddenPath("/area/engine/break_room") {
		t.Fatal("without a known-type predicate the filter keeps exact-path semantics")
	}
	var zero PathsFilter
	if zero.IsHiddenPath("/area/x") {
		t.Fatal("a zero filter hides nothing")
	}
}

// APHELION EDIT ADDITION END
