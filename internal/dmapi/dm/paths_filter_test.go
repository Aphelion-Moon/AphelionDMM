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
