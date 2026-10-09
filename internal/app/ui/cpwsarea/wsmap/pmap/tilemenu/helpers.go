// APHELION EDIT ADDITION START - MAPPING HELPER FINDER
package tilemenu

import (
	"fmt"
	"sort"
	"strings"

	"github.com/SpaiR/imgui-go"
	"github.com/rs/zerolog/log"

	"sdmm/internal/aphelion/helpers"
	"sdmm/internal/dmapi/dmmap"
	"sdmm/internal/dmapi/dmmap/dmminstance"
	"sdmm/internal/util"
)

// helperNode groups helpers by the path segments below their family.
type helperNode struct {
	name     string
	helper   *helpers.Helper
	children []*helperNode
}

func helperTree(list []helpers.Helper) *helperNode {
	root := &helperNode{}
	for i := range list {
		h := &list[i]
		node := root
		family := strings.TrimPrefix(h.Family, helpers.Root+"/")
		segments := append([]string{family}, strings.Split(h.Relative, "/")...)
		for _, segment := range segments {
			if segment == "" {
				continue
			}
			var next *helperNode
			for _, child := range node.children {
				if child.name == segment {
					next = child
					break
				}
			}
			if next == nil {
				next = &helperNode{name: segment}
				node.children = append(node.children, next)
			}
			node = next
		}
		node.helper = h
	}
	return root
}

// showMappingHelpers offers the helpers that act on instance i, nested by
// path, each with its description. A click adds it to i's tile.
func (t *TileMenu) showMappingHelpers(i *dmminstance.Instance, idx int) {
	list := helpers.ForEnvironment(t.app.LoadedEnvironment()).For(i.Prefab().Path())
	if len(list) == 0 {
		return
	}
	present := map[string]bool{}
	for _, other := range t.editor.Dmm().GetTile(i.Coord()).Instances() {
		present[other.Prefab().Path()] = true
	}
	if !imgui.BeginMenu(fmt.Sprintf("Mapping Helpers (%d)##helpers_%d", len(list), idx)) {
		return
	}
	imgui.TextDisabled("Helpers that act on this " + lastSegment(i.Prefab().Path()))
	imgui.Separator()
	t.showHelperNode(helperTree(list), i, present)
	imgui.EndMenu()
}

func (t *TileMenu) showHelperNode(node *helperNode, i *dmminstance.Instance, present map[string]bool) {
	sort.SliceStable(node.children, func(a, b int) bool { return node.children[a].name < node.children[b].name })
	for _, child := range node.children {
		if child.helper != nil {
			label := child.name
			if child.helper.Name != "" && child.helper.Name != child.name {
				label += "  (" + child.helper.Name + ")"
			}
			if imgui.MenuItemV(label+"##"+child.helper.Path, "", present[child.helper.Path], true) {
				t.addHelper(i, child.helper.Path)
			}
			if imgui.IsItemHovered() {
				tip := child.helper.Path
				if child.helper.Desc != "" {
					tip = child.helper.Desc + "\n\n" + tip
				}
				imgui.SetTooltip(tip)
			}
		}
		if len(child.children) != 0 {
			if imgui.BeginMenu(child.name + "##" + fmt.Sprintf("%p", child)) {
				t.showHelperNode(child, i, present)
				imgui.EndMenu()
			}
		}
	}
}

func (t *TileMenu) addHelper(i *dmminstance.Instance, path string) {
	prefab, ok := dmmap.PrefabStorage.InitialV(path)
	if !ok || !t.editor.TryBeginTileChange(i.Coord()) {
		return
	}
	log.Printf("add mapping helper [%s] for [%s] at %v", path, i.Prefab().Path(), i.Coord())
	tile := t.editor.Dmm().GetTile(i.Coord())
	tile.InstancesAdd(prefab)
	tile.InstancesRegenerate()
	t.editor.UpdateCanvasByCoords([]util.Point{i.Coord()})
	t.editor.CommitOperation("Add Mapping Helper")
}

func lastSegment(path string) string { return path[strings.LastIndex(path, "/")+1:] }

// APHELION EDIT ADDITION END
