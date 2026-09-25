package editor

import (
	"sdmm/internal/aphelion/collab/executor"
	"sdmm/internal/aphelion/collab/model"
	// APHELION EDIT ADDITION START - BYTE-BOUNDED EDIT WORK
	"sdmm/internal/aphelion/resources"
	// APHELION EDIT ADDITION END
	// APHELION EDIT ADDITION START - SELECTION LIFECYCLE
	"sdmm/internal/aphelion/editing"
	// APHELION EDIT ADDITION END
	"sdmm/internal/app/command"
	"sdmm/internal/app/prefs"
	"sdmm/internal/app/ui/cpwsarea/wsmap/pmap/canvas"
	"sdmm/internal/app/ui/cpwsarea/wsmap/pmap/overlay"
	"sdmm/internal/dmapi/dm"
	"sdmm/internal/dmapi/dmenv"
	"sdmm/internal/dmapi/dmmap"
	"sdmm/internal/dmapi/dmmap/dmmdata/dmmprefab"
	"sdmm/internal/dmapi/dmmap/dmminstance"
	"sdmm/internal/dmapi/dmmclip"
	"sdmm/internal/dmapi/dmmsnap"
	"sdmm/internal/util"

	"github.com/SpaiR/imgui-go"
)

type Editor struct {
	// APHELION EDIT ADDITION START - COMPOSITION ANCHORS
	movingCompositionRoot bool
	// APHELION EDIT ADDITION END
	// APHELION EDIT ADDITION START - FILTER FEEDBACK
	visibilityError string
	// APHELION EDIT ADDITION END
	app  app
	pMap attachedMap

	dmm *dmmap.Dmm
	// APHELION EDIT ADDITION START - SEARCH VIEW OWNERSHIP
	mapViewGeneration uint64
	mapViewClosed     bool
	// APHELION EDIT ADDITION END

	flickAreas    []overlay.FlickArea
	flickInstance []overlay.FlickInstance

	areasZones []AreaZone
	// APHELION EDIT ADDITION START - AREA DELTAS
	areaIndexes           map[string]*areaIndex
	areaBordersGeneration uint64
	// APHELION EDIT ADDITION END
	// APHELION EDIT ADDITION START - SELECTION LIFECYCLE
	selectionMove           *editing.Move
	workingSelection        editing.WorkingSelection
	selectionMoveGeneration uint64
	selectionMovePreview    *selectionMoveSession
	selectionOutcome        func(bool)
	paste                   *pasteSession
	randomFill              *randomFillDefinition
	heldPresentation        *heldPresentation
	// APHELION EDIT ADDITION START - BYTE-BOUNDED EDIT WORK
	workBudget *resources.Budget
	localWork  *localWork
	// APHELION EDIT ADDITION END
	// APHELION EDIT ADDITION END
	// APHELION EDIT ADDITION START - REPEAT TRANSFORM
	repeatTransforms editing.TransformRepeat
	repeatAccepted   func()
	// APHELION EDIT ADDITION END

	// APHELION EDIT ADDITION START - COLLABORATION
	executor               executor.Executor
	documentID             model.DocumentID
	actorID                model.ActorID
	authoritative          model.Snapshot
	authoritativeTiles     map[model.Coord]model.TileState
	authoritativePositions map[model.Coord]int
	sessionOwned           bool
	pendingChanges         map[model.Coord]model.TileState
	collaborationErr       error
	attachmentGeneration   uint64
	historyGeneration      uint64 // Resumable local history; callback generation never rewinds.
	history                command.Target
	unresolvedSubmissions  map[model.OperationID]struct{}
	// APHELION EDIT ADDITION END
}

func (e *Editor) SetFlickAreas(flickAreas []overlay.FlickArea) {
	e.flickAreas = flickAreas
}

func (e *Editor) FlickAreas() []overlay.FlickArea {
	return e.flickAreas
}

func (e *Editor) SetFlickInstance(flickInstance []overlay.FlickInstance) {
	e.flickInstance = flickInstance
}

func (e *Editor) FlickInstance() []overlay.FlickInstance {
	return e.flickInstance
}

func (e *Editor) AreasZones() []AreaZone {
	return e.areasZones
}

// APHELION EDIT ADDITION START - CACHED AREA BORDERS
func (e *Editor) AreaBordersGeneration() uint64 { return e.areaBordersGeneration }

// APHELION EDIT ADDITION END

func (e *Editor) ActiveLevel() int {
	return e.pMap.ActiveLevel()
}

type app interface {
	DoSelectPrefab(prefab *dmmprefab.Prefab)
	DoEditInstance(*dmminstance.Instance)

	SelectedPrefab() (*dmmprefab.Prefab, bool)

	CommandStorage() *command.Storage
	Clipboard() *dmmclip.Clipboard
	PathsFilter() *dm.PathsFilter

	ShowLayout(name string, focus bool)

	SyncPrefabs()
	SyncVarEditor()
	RunLater(func())

	Prefs() prefs.Prefs
	// APHELION EDIT ADDITION START - COLLABORATION
	LoadedEnvironment() *dmenv.Dme
	// APHELION EDIT ADDITION END
}

type attachedMap interface {
	ActiveLevel() int
	SetActiveLevel(int)

	Snapshot() *dmmsnap.DmmSnap

	Size() imgui.Vec2

	Canvas() *canvas.Canvas
	CanvasState() *canvas.State
	CanvasControl() *canvas.Control
	CanvasOverlay() *canvas.Overlay

	PushAreaHover(bounds util.Bounds, fillColor, borderColor util.Color)

	OnMapSizeChange()
}

func New(app app, attachedMap attachedMap, dmm *dmmap.Dmm) *Editor {
	e := &Editor{
		app:  app,
		pMap: attachedMap,
		dmm:  dmm,
		// APHELION EDIT ADDITION START - BYTE-BOUNDED EDIT WORK
		workBudget: resources.DefaultBudget(),
		// APHELION EDIT ADDITION END
	}
	// APHELION EDIT ADDITION START - COLLABORATION
	e.history = app.CommandStorage().Bind(dmm.Path.Absolute)
	e.initializeCollaboration()
	// APHELION EDIT ADDITION END
	e.updateAreasZones()
	return e
}

// Dmm returns currently edited map.
func (e *Editor) Dmm() *dmmap.Dmm {
	return e.dmm
}

// HoveredInstance returns currently hovered instance.
func (e *Editor) HoveredInstance() *dmminstance.Instance {
	return e.pMap.CanvasState().HoveredInstance()
}

// UpdateCanvasByCoords updates the canvas for the provided coords.
func (e *Editor) UpdateCanvasByCoords(coords []util.Point) {
	e.pMap.Canvas().Render().UpdateBucketV(e.dmm, e.pMap.ActiveLevel(), coords)
}

// UpdateCanvasByTiles updates the canvas for the provided tiles.
func (e *Editor) UpdateCanvasByTiles(tiles []dmmap.Tile) {
	coords := make([]util.Point, 0, len(tiles))
	for _, tile := range tiles {
		coords = append(coords, tile.Coord)
	}
	e.UpdateCanvasByCoords(coords)
}

// SelectedPrefab returns a currently selected prefab.
func (e *Editor) SelectedPrefab() (*dmmprefab.Prefab, bool) {
	return e.app.SelectedPrefab()
}

// ReplacePrefab replaces all old prefabs on the map with the new one. Commits map changes.
func (e *Editor) ReplacePrefab(oldPrefab, newPrefab *dmmprefab.Prefab) {
	// APHELION EDIT ADDITION START - LOCAL BULK PREPARATION
	if e.trySchedulePrefabBatch(oldPrefab, newPrefab, "Replace Prefab") {
		return
	}
	// APHELION EDIT ADDITION END
	// APHELION EDIT ADDITION START - PASTE PLACEMENT
	if e.HasPastePlacement() {
		return
	}
	// APHELION EDIT ADDITION END
	// APHELION EDIT ADDITION START - PROPERTY CAPTURE
	instances := e.InstancesFindByPrefabId(oldPrefab.Id())
	coords := make([]util.Point, 0, len(instances))
	for _, instance := range instances {
		coords = append(coords, instance.Coord())
	}
	if !e.TryBeginTileChange(coords...) {
		return
	}
	for _, instance := range instances {
		instance.SetPrefab(newPrefab)
	}
	// APHELION EDIT ADDITION END
	/* APHELION EDIT REMOVAL START - PROPERTY CAPTURE
	for _, tile := range e.dmm.Tiles {
		for _, instance := range tile.Instances() {
			if instance.Prefab().Id() == oldPrefab.Id() {
				// APHELION EDIT ADDITION START - COLLABORATION
				e.BeginTileChange(tile.Coord)
				// APHELION EDIT ADDITION END
				instance.SetPrefab(newPrefab)
			}
		}
	}
	APHELION EDIT REMOVAL END */
}

// FocusCamera moves the camera in a way, so it will be centered on the instance.
func (e *Editor) FocusCamera(i *dmminstance.Instance) {
	relPos := i.Coord()
	absPos := util.Point{X: (relPos.X - 1) * -dmmap.WorldIconSize, Y: (relPos.Y - 1) * -dmmap.WorldIconSize, Z: relPos.Z}

	camera := e.pMap.Canvas().Render().Camera
	camera.ShiftX = e.pMap.Size().X/2/camera.Scale + float32(absPos.X)
	camera.ShiftY = e.pMap.Size().Y/2/camera.Scale + float32(absPos.Y)

	e.pMap.SetActiveLevel(relPos.Z)
}

// FocusCameraOnPosition centers the camera on given coordinates.
func (e *Editor) FocusCameraOnPosition(coord util.Point) {
	absPos := util.Point{X: (coord.X - 1) * -dmmap.WorldIconSize, Y: (coord.Y - 1) * -dmmap.WorldIconSize, Z: coord.Z}

	camera := e.pMap.Canvas().Render().Camera
	camera.ShiftX = e.pMap.Size().X/2/camera.Scale + float32(absPos.X)
	camera.ShiftY = e.pMap.Size().Y/2/camera.Scale + float32(absPos.Y)

	e.pMap.SetActiveLevel(coord.Z)
	e.OverlaySetTileFlick(coord)
}

func (e *Editor) ZoomLevel() float32 {
	return e.pMap.Canvas().Render().Camera.Scale
}

func (e *Editor) Prefs() prefs.Prefs {
	return e.app.Prefs()
}
