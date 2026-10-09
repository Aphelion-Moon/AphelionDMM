// APHELION EDIT ADDITION START - MAPPING HELPER PANEL
// Package cphelpers is the Helpers tab beside Prefabs: the mapping helpers
// that act on the selected object, searchable and grouped, with the ones on
// its tile checked.
package cphelpers

import (
	"fmt"
	"strings"

	"github.com/SpaiR/imgui-go"
	"github.com/rs/zerolog/log"

	"sdmm/internal/aphelion/helpers"
	"sdmm/internal/app/ui/component"
	"sdmm/internal/app/ui/uikit"
	"sdmm/internal/dmapi/dmenv"
	"sdmm/internal/dmapi/dmicon"
	"sdmm/internal/dmapi/dmmap"
	"sdmm/internal/dmapi/dmmap/dmminstance"
	"sdmm/internal/util"
)

// probe, when set by a test, observes each group header as it is drawn.
var probe func(key string, open bool, min, max imgui.Vec2)

// largeBranch is the branch size that earns its own category chip; on an
// airlock only access (222 of 236) qualifies.
const largeBranch = 8

// App is what the panel needs from the application.
type App interface {
	LoadedEnvironment() *dmenv.Dme
	SelectedInstance() (*dmminstance.Instance, bool)
}

// Editor is the active map editor; nil when no map is open.
type Editor interface {
	Dmm() *dmmap.Dmm
	TryBeginTileChange(...util.Point) bool
	UpdateCanvasByCoords([]util.Point)
	InstanceDelete(i *dmminstance.Instance)
	CommitOperation(string)
}

type Panel struct {
	component.Component
	app    App
	editor func() Editor

	query    string
	category string
	// filterChanged is true for the frame after the query or category
	// changed; lastFilter is what was drawn before.
	filterChanged bool
	lastFilter    [2]string

	env   *dmenv.Dme
	cache map[string][]helpers.Helper // target path -> offerable helpers
}

// Init wires the panel; editor returns the active map editor or nil.
func (p *Panel) Init(app App, editor func() Editor) {
	p.app, p.editor = app, editor
}

// Free drops cached helper lists when the environment closes.
func (p *Panel) Free() { p.env, p.cache = nil, nil }

// helpersFor returns the offerable helpers for path, cached per environment.
func (p *Panel) helpersFor(path string) []helpers.Helper {
	env := p.app.LoadedEnvironment()
	if env == nil {
		return nil
	}
	if env != p.env || p.cache == nil {
		p.env, p.cache = env, map[string][]helpers.Helper{}
	}
	list, ok := p.cache[path]
	if !ok {
		list = helpers.Offerable(helpers.ForEnvironment(env).For(path))
		p.cache[path] = list
	}
	return list
}

func (p *Panel) target() (*dmminstance.Instance, []helpers.Helper) {
	i, ok := p.app.SelectedInstance()
	if !ok || i == nil || i.Prefab() == nil {
		return nil, nil
	}
	return i, p.helpersFor(i.Prefab().Path())
}

// TabHighlight reports whether the selected object has helpers, so the tab
// can draw attention to itself.
func (p *Panel) TabHighlight() bool {
	_, list := p.target()
	return len(list) != 0
}

// HelperCount is the number of helpers for path, for menus that link here.
func (p *Panel) HelperCount(path string) int { return len(p.helpersFor(path)) }

func (p *Panel) Process(int32) {
	i, list := p.target()
	switch {
	case p.app.LoadedEnvironment() == nil:
		uikit.EmptyState("Open an environment to find mapping helpers.")
		return
	case i == nil:
		uikit.EmptyState("Select an object on the map to see the mapping helpers that act on it: use the Select tool, or right-click an object and choose Select.")
		return
	}
	name := i.Prefab().Vars().TextV("name", lastSegment(i.Prefab().Path()))
	imgui.Text(name)
	imgui.SameLine()
	imgui.TextDisabled(i.Prefab().Path())
	if len(list) == 0 {
		uikit.EmptyState("No mapping helpers act on this object.")
		return
	}

	present := p.presentOnTile(i)
	p.showPresent(list, present)
	p.showFilters(list)

	imgui.BeginChild("helpers_tree")
	current := [2]string{p.query, p.category}
	p.filterChanged, p.lastFilter = current != p.lastFilter, current
	root := helpers.Browse(list, helpers.Filter{Query: p.query, Category: p.category, Large: largeBranch})
	if root.Count == 0 {
		imgui.TextDisabled("No helpers match.")
	}
	nodes := root.Children
	if len(nodes) == 1 && nodes[0].Helper == nil {
		nodes = nodes[0].Children // one family: skip its level
	}
	for _, n := range nodes {
		p.showNode(n, i, present, 0)
	}
	imgui.EndChild()
}

// presentOnTile maps helper paths to their instances on the target's tile.
func (p *Panel) presentOnTile(i *dmminstance.Instance) map[string]*dmminstance.Instance {
	present := map[string]*dmminstance.Instance{}
	ed := p.editor()
	if ed == nil || ed.Dmm() == nil || !ed.Dmm().HasTile(i.Coord()) {
		return present
	}
	for _, other := range ed.Dmm().GetTile(i.Coord()).Instances() {
		if path := other.Prefab().Path(); strings.HasPrefix(path, helpers.Root+"/") {
			present[path] = other
		}
	}
	return present
}

// showPresent lists the helpers already on the tile as removable chips.
func (p *Panel) showPresent(list []helpers.Helper, present map[string]*dmminstance.Instance) {
	var on []helpers.Helper
	for _, h := range list {
		if present[h.Path] != nil {
			on = append(on, h)
		}
	}
	imgui.TextDisabled(fmt.Sprintf("%d helpers · %d on this tile", len(list), len(on)))
	for n, h := range on {
		if n != 0 {
			imgui.SameLine()
			if imgui.ContentRegionAvail().X < imgui.CalcTextSize(chipLabel(h), false, 0).X+imgui.FontSize()*3 {
				imgui.NewLine()
			}
		}
		if imgui.SmallButton(chipLabel(h) + "  x##present_" + h.Path) {
			p.remove(present[h.Path], h.Path)
		}
		if imgui.IsItemHovered() {
			imgui.SetTooltip("Remove " + h.Path)
		}
	}
}

func chipLabel(h helpers.Helper) string {
	if h.Relative == "" {
		return lastSegment(h.Path)
	}
	return h.Relative
}

// showFilters draws the search box and category chips.
func (p *Panel) showFilters(list []helpers.Helper) {
	imgui.SetNextItemWidth(-1)
	imgui.InputTextWithHint("##helpers_query", "Search helpers (e.g. engineering any)", &p.query)
	cats := helpers.Categories(list, largeBranch)
	if len(cats) < 2 {
		p.category = ""
		return
	}
	if chip("All", len(list), p.category == "") {
		p.category = ""
	}
	for _, c := range cats {
		imgui.SameLine()
		if chip(c.Name, c.Count, p.category == c.Name) {
			p.category = c.Name
		}
	}
}

func chip(name string, count int, active bool) bool {
	if active {
		imgui.PushStyleColor(imgui.StyleColorButton, imgui.CurrentStyle().Color(imgui.StyleColorButtonActive))
		defer imgui.PopStyleColor()
	}
	return imgui.SmallButton(fmt.Sprintf("%s %d##chip_%s", name, count, name))
}

func (p *Panel) showNode(n *helpers.Node, target *dmminstance.Instance, present map[string]*dmminstance.Instance, depth int) {
	if n.Helper != nil {
		p.showHelper(n, target, present)
		return
	}
	// A new search opens every matching group, and a new category its first
	// level, once; afterwards the user's own open and close clicks stand.
	if p.filterChanged && (p.query != "" || (p.category != "" && depth == 0)) {
		imgui.SetNextItemOpen(true, imgui.ConditionAlways)
	}
	// The tree is rebuilt every frame, so the ID is the group's path; "###"
	// keeps it fixed while the count in the label changes.
	open := imgui.TreeNodeV(fmt.Sprintf("%s (%d)###group_%s", n.Name, n.Count, n.Key), imgui.TreeNodeFlagsSpanAvailWidth)
	if probe != nil {
		probe(n.Key, open, imgui.ItemRectMin(), imgui.ItemRectMax())
	}
	if open {
		for _, c := range n.Children {
			p.showNode(c, target, present, depth+1)
		}
		imgui.TreePop()
	}
}

func (p *Panel) showHelper(n *helpers.Node, target *dmminstance.Instance, present map[string]*dmminstance.Instance) {
	h := n.Helper
	if s := helperSprite(p.app.LoadedEnvironment(), h.Path); s != nil {
		size := imgui.FrameHeight()
		imgui.ImageV(imgui.TextureID(s.Texture()), imgui.Vec2{X: size, Y: size}, imgui.Vec2{X: s.U1, Y: s.V1}, imgui.Vec2{X: s.U2, Y: s.V2}, imgui.Vec4{X: 1, Y: 1, Z: 1, W: 1}, imgui.Vec4{})
		imgui.SameLine()
	}
	on := present[h.Path] != nil
	checked := on
	if imgui.Checkbox(n.Name+"##helper_"+h.Path, &checked) {
		if on {
			p.remove(present[h.Path], h.Path)
		} else {
			p.add(target, h.Path)
		}
	}
	if imgui.IsItemHovered() {
		tip := h.Path
		if h.Desc != "" {
			tip = h.Desc + "\n\n" + tip
		}
		imgui.SetTooltip(tip)
	}
	if h.Name != "" && h.Name != n.Name {
		imgui.SameLine()
		imgui.TextDisabled(h.Name)
	}
}

func helperSprite(env *dmenv.Dme, path string) *dmicon.Sprite {
	if env == nil || dmicon.Cache == nil {
		return nil
	}
	object := env.Objects[path]
	if object == nil || object.Vars == nil {
		return nil
	}
	icon := object.Vars.TextV("icon", "")
	if icon == "" {
		return nil
	}
	return dmicon.Cache.GetSpriteOrPlaceholderV(icon, object.Vars.TextV("icon_state", ""), object.Vars.IntV("dir", 2))
}

func (p *Panel) add(target *dmminstance.Instance, path string) {
	ed := p.editor()
	if ed == nil {
		return
	}
	prefab, ok := dmmap.PrefabStorage.InitialV(path)
	if !ok || !ed.TryBeginTileChange(target.Coord()) {
		return
	}
	log.Printf("add mapping helper [%s] for [%s] at %v", path, target.Prefab().Path(), target.Coord())
	tile := ed.Dmm().GetTile(target.Coord())
	tile.InstancesAdd(prefab)
	tile.InstancesRegenerate()
	ed.UpdateCanvasByCoords([]util.Point{target.Coord()})
	ed.CommitOperation("Add Mapping Helper")
}

func (p *Panel) remove(helper *dmminstance.Instance, path string) {
	ed := p.editor()
	if ed == nil || helper == nil {
		return
	}
	log.Printf("remove mapping helper [%s] at %v", path, helper.Coord())
	ed.InstanceDelete(helper)
	ed.CommitOperation("Remove Mapping Helper")
}

func lastSegment(path string) string { return path[strings.LastIndex(path, "/")+1:] }

// APHELION EDIT ADDITION END
