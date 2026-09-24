package cpenvironment

import (
	"fmt"
	"testing"

	"sdmm/internal/dmapi/dmenv"
)

func TestEnvironmentFilterResumesWithinPerFrameTraversalBudget(t *testing.T) {
	objects := make(map[string]*dmenv.Object)
	root := &dmenv.Object{Path: "/obj"}
	objects[root.Path] = root
	for i := 0; i < filterObjectsPerTick+10; i++ {
		path := fmt.Sprintf("/obj/match_%03d", i)
		root.DirectChildren = append(root.DirectChildren, path)
		objects[path] = &dmenv.Object{Path: path}
	}

	environment := &dmenv.Dme{Objects: objects}
	panel := &Environment{
		treeEnvironment: environment,
		treeNodes:       make(map[string]*treeNode, len(objects)),
		filter:          "match",
	}
	for _, object := range objects {
		panel.treeNodes[object.Path] = &treeNode{orig: object}
	}
	panel.doFilter()

	panel.continueFilter()
	if got, want := len(panel.filteredTreeNodes), filterObjectsPerTick-1; got != want {
		t.Fatalf("first filter step visited %d matching objects, want %d", got, want)
	}
	if len(panel.filterFrames) == 0 {
		t.Fatal("filter traversal completed despite more objects than its frame budget")
	}

	panel.tmpNewTreeNodesCount = 0
	panel.continueFilter()
	if got, want := len(panel.filteredTreeNodes), filterObjectsPerTick+10; got != want {
		t.Fatalf("resumed filter contains %d objects, want %d", got, want)
	}
	for i, node := range panel.filteredTreeNodes {
		want := fmt.Sprintf("/obj/match_%03d", i)
		if node.orig.Path != want {
			t.Fatalf("filter result %d is %q, want %q", i, node.orig.Path, want)
		}
	}
}

func TestEnvironmentReplacementClearsCachedTreeAndFilterResults(t *testing.T) {
	oldEnvironment := &dmenv.Dme{Objects: map[string]*dmenv.Object{}}
	newEnvironment := &dmenv.Dme{Objects: map[string]*dmenv.Object{}}
	oldNode := &treeNode{}
	panel := &Environment{
		treeId:            4,
		treeEnvironment:   oldEnvironment,
		treeNodes:         map[string]*treeNode{"/obj/old": oldNode},
		filteredTreeNodes: []*treeNode{oldNode},
		filterFrames:      []filterFrame{{object: &dmenv.Object{Path: "/obj/old"}}},
	}

	panel.setTreeEnvironment(newEnvironment)

	if panel.treeId != 5 {
		t.Fatalf("tree id is %d after environment replacement, want 5", panel.treeId)
	}
	if len(panel.treeNodes) != 0 || panel.filteredTreeNodes != nil || panel.filterFrames != nil {
		t.Fatal("environment replacement retained cached tree nodes or filter state")
	}
}
