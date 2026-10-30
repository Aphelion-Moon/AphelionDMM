package cpenvironment

import (
	"testing"

	"sdmm/internal/dmapi/dmenv"
)

func selectPathFixture() *Environment {
	e, a := profileFixture()
	a.env = &dmenv.Dme{Objects: map[string]*dmenv.Object{
		"/area":                 {Path: "/area"},
		"/area/hallway":         {Path: "/area/hallway"},
		"/area/hallway/primary": {Path: "/area/hallway/primary"},
		"/area/hall":            {Path: "/area/hall"},
	}}
	return e
}

func revealNode(path string) *treeNode {
	return &treeNode{orig: &dmenv.Object{Path: path}}
}

// A path the environment does not define (an unknown type preserved in an old
// map) has no tree node, so the reveal request could never complete and kept
// forcing its ancestors open on every frame.
func TestSelectingUndefinedPathDoesNotForceTreeOpen(t *testing.T) {
	e := selectPathFixture()
	e.SelectPath("/area/hallway/primary/aft")
	if e.selectedPath != "/area/hallway/primary/aft" {
		t.Fatal("undefined path was not recorded as the selection")
	}
	for _, ancestor := range []string{"/area", "/area/hallway", "/area/hallway/primary"} {
		if e.isPartOfSelectedPath(revealNode(ancestor)) {
			t.Fatalf("undefined selection forces %s open", ancestor)
		}
	}
}

func TestSelectingDefinedPathRevealsOnlyPathAncestors(t *testing.T) {
	e := selectPathFixture()
	e.SelectPath("/area/hallway/primary")
	for path, want := range map[string]bool{
		"/area":                 true,
		"/area/hallway":         true,
		"/area/hallway/primary": true,
		// A string prefix that is not a path ancestor must stay closed.
		"/area/hall":         false,
		"/area/hallway/prim": false,
	} {
		if got := e.isPartOfSelectedPath(revealNode(path)); got != want {
			t.Fatalf("%s reveal = %v, want %v", path, got, want)
		}
	}
}

func TestRevealStopsWhenEnvironmentNoLongerDefinesSelection(t *testing.T) {
	e := selectPathFixture()
	e.SelectPath("/area/hallway/primary")
	replacement := &dmenv.Dme{Objects: map[string]*dmenv.Object{"/area": {Path: "/area"}}}
	e.app.(*profileApp).env = replacement
	e.process(replacement)
	if e.isPartOfSelectedPath(revealNode("/area")) {
		t.Fatal("selection absent from the current tree still forces nodes open")
	}
}
