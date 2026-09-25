package cpenvironment

import (
	"testing"

	"sdmm/internal/dmapi/dmenv"
	"sdmm/internal/dmapi/dmicon"
	"sdmm/internal/dmapi/dmvars"
)

func TestTreeNodeDefersIconDemandUntilRowIsVisible(t *testing.T) {
	vars := &dmvars.MutableVariables{}
	vars.Put("icon", `"absent-from-open-maps.dmi"`)
	vars.Put("icon_state", `"idle"`)
	object := &dmenv.Object{Path: "/obj/test", Vars: vars.ToImmutable()}
	panel := &Environment{treeNodes: make(map[string]*treeNode)}

	node, ok := panel.newTreeNode(object)
	if !ok {
		t.Fatal("tree node was not created")
	}
	if node.icon != "absent-from-open-maps.dmi" || node.state != "idle" {
		t.Fatalf("tree node lost its icon request key: %q state %q", node.icon, node.state)
	}
	if node.sprite != nil || node.load.State != dmicon.SpriteUnassigned {
		t.Fatalf("offscreen tree-node creation started icon demand: sprite %p state %d", node.sprite, node.load.State)
	}
}
