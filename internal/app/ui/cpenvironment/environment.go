package cpenvironment

import (
	// APHELION EDIT ADDITION START - FILTER PROFILES
	"sdmm/internal/aphelion/filterprofiles"
	// APHELION EDIT ADDITION END
	"strings"

	"sdmm/internal/app/config"
	"sdmm/internal/app/prefs"
	"sdmm/internal/app/ui/component"
	"sdmm/internal/app/ui/shortcut"
	"sdmm/internal/dmapi/dm"

	"github.com/SpaiR/imgui-go"
	"github.com/rs/zerolog/log"

	"sdmm/internal/dmapi/dmenv"
)

type App interface {
	LoadedEnvironment() *dmenv.Dme
	DoSelectPrefabByPath(string)
	DoEditPrefabByPath(string)
	DoSearchPrefabByPath(path string)
	HasActiveMap() bool
	ShowLayout(name string, focus bool)
	PathsFilter() *dm.PathsFilter

	ConfigRegister(config.Config)
	ConfigFind(name string) config.Config
	Prefs() prefs.Prefs
}

// Only 25 nodes can be loaded per one process tick.
// This helps to distribute performance load between process calls.
const newTreeNodesLimit = 25

// APHELION EDIT ADDITION START - OBJECT TREE FILTER CLIPPING
// Filtering also has a per-frame traversal budget. Node creation and path
// matching are both work proportional to the type tree, so bounding only node
// creation still leaves a large tree synchronous on every filter update.
const filterObjectsPerTick = 250

var filterRoots = [...]string{"/area", "/turf", "/obj", "/mob"}

type filterFrame struct {
	object    *dmenv.Object
	nextChild int
	entered   bool
}

// APHELION EDIT ADDITION END

type Environment struct {
	component.Component

	app App
	// APHELION EDIT ADDITION START - FILTER PROFILES
	filterProfileMissing                                                                  string
	filterProfiles                                                                        filterprofiles.Session
	filterProfileEnvironment                                                              *dmenv.Dme
	filterProfileProjectKey                                                               string
	filterProfileChoice, filterProfileName, filterProfileStatus, filterProfileConfigError string
	filterCompileGeneration                                                               uint64
	filterCompilePending                                                                  bool
	// APHELION EDIT ADDITION END

	shortcuts shortcut.Shortcuts

	typesFilterEnabled bool

	treeId uint

	treeNodes         map[string]*treeNode
	filteredTreeNodes []*treeNode
	// APHELION EDIT ADDITION START - OBJECT TREE FILTER CLIPPING
	treeEnvironment   *dmenv.Dme
	filterFrames      []filterFrame
	filterEnvironment *dmenv.Dme
	filterText        string
	// APHELION EDIT ADDITION END

	filter       string
	selectedPath string

	tmpNewTreeNodesCount int
	/* APHELION EDIT REMOVAL START - OBJECT TREE FILTER CLIPPING
	tmpDoRepeatFilter bool
	APHELION EDIT REMOVAL END */
	tmpDoCollapseAll bool
	tmpDoSelectPath  bool
}

func (e *Environment) Init(app App) {
	e.app = app
	e.treeNodes = make(map[string]*treeNode)

	e.addShortcuts()
	e.loadConfig()

	e.AddOnFocused(func(focused bool) {
		e.shortcuts.SetVisible(focused)
	})
}

/* APHELION EDIT REMOVAL START - OBJECT TREE FILTER CLIPPING
func (e *Environment) Free() {
	e.treeId++
	e.treeNodes = make(map[string]*treeNode)
	e.filteredTreeNodes = nil
	e.filter = ""
	e.selectedPath = ""
	log.Print("environment panel free")
}
APHELION EDIT REMOVAL END */

// APHELION EDIT ADDITION START - OBJECT TREE FILTER CLIPPING
func (e *Environment) Free() {
	e.invalidateFilterProfiles()
	e.treeId++
	e.treeNodes = make(map[string]*treeNode)
	e.filteredTreeNodes = nil
	e.treeEnvironment = nil
	e.filterFrames = nil
	e.filterEnvironment = nil
	e.filterText = ""
	e.filter = ""
	e.selectedPath = ""
	log.Print("environment panel free")
}

/* APHELION EDIT REMOVAL START - OBJECT TREE FILTER CLIPPING
func (e *Environment) process() {
	e.tmpNewTreeNodesCount = 0

	if e.tmpDoRepeatFilter {
		e.doFilter()
	}
}
APHELION EDIT REMOVAL END */

func (e *Environment) process(environment *dmenv.Dme) {
	e.tmpNewTreeNodesCount = 0
	e.setTreeEnvironment(environment)
	e.prepareFilter()
	e.continueFilter()
}

func (e *Environment) setTreeEnvironment(environment *dmenv.Dme) {
	if e.treeEnvironment == environment {
		return
	}

	e.treeId++
	e.treeEnvironment = environment
	e.treeNodes = make(map[string]*treeNode)
	e.filteredTreeNodes = nil
	e.filterFrames = nil
	e.filterEnvironment = nil
}

// APHELION EDIT ADDITION END

func (e *Environment) postProcess() {
	e.tmpDoCollapseAll = false
}

func (e *Environment) SelectPath(path string) {
	if path != e.selectedPath {
		log.Printf("environment path selected: [%s]", path)
		e.selectedPath = path
		e.tmpDoSelectPath = true
	}
}

/* APHELION EDIT REMOVAL START - OBJECT TREE FILTER CLIPPING
func (e *Environment) doFilter() {
	e.filteredTreeNodes = nil

	if len(e.filter) == 0 {
		return
	}

	initialNewTreeNodesCount := e.tmpNewTreeNodesCount

	e.filterPathBranch("/area")
	e.filterPathBranch("/turf")
	e.filterPathBranch("/obj")
	e.filterPathBranch("/mob")

	e.tmpDoRepeatFilter = initialNewTreeNodesCount != e.tmpNewTreeNodesCount
}

func (e *Environment) filterPathBranch(t string) {
	if atom := e.app.LoadedEnvironment().Objects[t]; atom != nil {
		e.filterBranch0(atom)
	}
}

func (e *Environment) filterBranch0(object *dmenv.Object) {
	if strings.Contains(object.Path, e.filter) {
		if node, ok := e.newTreeNode(object); ok {
			e.filteredTreeNodes = append(e.filteredTreeNodes, node)
		}
	}

	for _, childPath := range object.DirectChildren {
		e.filterBranch0(e.app.LoadedEnvironment().Objects[childPath])
	}
}
APHELION EDIT REMOVAL END */

// APHELION EDIT ADDITION START - OBJECT TREE FILTER CLIPPING
func (e *Environment) doFilter() {
	e.filterEnvironment = e.treeEnvironment
	e.filterText = e.filter
	e.filteredTreeNodes = nil
	e.filterFrames = nil

	if e.filter == "" || e.treeEnvironment == nil {
		return
	}

	// The stack is LIFO, so push the roots in reverse to retain the historical
	// /area, /turf, /obj, /mob search order.
	for i := len(filterRoots) - 1; i >= 0; i-- {
		if object := e.treeEnvironment.Objects[filterRoots[i]]; object != nil {
			e.filterFrames = append(e.filterFrames, filterFrame{object: object})
		}
	}
}

func (e *Environment) prepareFilter() {
	if e.filterEnvironment == e.treeEnvironment && e.filterText == e.filter {
		return
	}
	e.doFilter()
}

func (e *Environment) continueFilter() {
	if e.filter == "" || e.treeEnvironment == nil {
		return
	}

	visited := 0
	for len(e.filterFrames) > 0 && visited < filterObjectsPerTick {
		last := len(e.filterFrames) - 1
		frame := &e.filterFrames[last]
		if !frame.entered {
			if strings.Contains(frame.object.Path, e.filter) {
				node, ok := e.newTreeNode(frame.object)
				if !ok {
					// Leave the match at the top of the stack to retry it when
					// the per-frame node creation budget resets.
					break
				}
				e.filteredTreeNodes = append(e.filteredTreeNodes, node)
			}
			frame.entered = true
			visited++
		}

		if frame.nextChild < len(frame.object.DirectChildren) {
			childPath := frame.object.DirectChildren[frame.nextChild]
			frame.nextChild++
			if child := e.treeEnvironment.Objects[childPath]; child != nil {
				e.filterFrames = append(e.filterFrames, filterFrame{object: child})
			}
			continue
		}
		e.filterFrames = e.filterFrames[:last]
	}

	// process() will continue the current traversal on the next frame. No full
	// tree walk is repeated while the result list is being built.
}

// APHELION EDIT ADDITION END

func (e *Environment) iconSize() float32 {
	return imgui.FrameHeight() * (float32(e.config().NodeScale) / 100)
}

func (e *Environment) doCollapseAll() {
	log.Print("do collapse all")
	e.tmpDoCollapseAll = true
}

func (e *Environment) doToggleTypesFilter() {
	e.typesFilterEnabled = !e.typesFilterEnabled
	log.Print("do toggle types filter:", e.typesFilterEnabled)
}
