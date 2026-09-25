package pmap

import (
	"sdmm/internal/aphelion/collab/model"
	"sdmm/internal/aphelion/collab/protocol"
	// APHELION EDIT ADDITION START - SELECTION STAMPS
	"sdmm/internal/aphelion/editing/stamps"
	// APHELION EDIT ADDITION END
	collabui "sdmm/internal/aphelion/collab/ui"
	"sdmm/internal/app/command"
	"sdmm/internal/app/prefs"
	"sdmm/internal/app/render"
	"sdmm/internal/app/ui/cpwsarea/wsmap/pmap/canvas"
	"sdmm/internal/app/ui/cpwsarea/wsmap/pmap/editor"
	"sdmm/internal/app/ui/cpwsarea/wsmap/pmap/pquickedit"
	"sdmm/internal/app/ui/cpwsarea/wsmap/pmap/psettings"
	"sdmm/internal/app/ui/cpwsarea/wsmap/pmap/tilemenu"
	"sdmm/internal/app/ui/cpwsarea/wsmap/tools"
	"sdmm/internal/app/ui/shortcut"
	"sdmm/internal/dmapi/dm"
	"sdmm/internal/dmapi/dmenv"
	"sdmm/internal/dmapi/dmmap"
	"sdmm/internal/dmapi/dmmap/dmmdata/dmmprefab"
	"sdmm/internal/dmapi/dmmap/dmminstance"
	"sdmm/internal/dmapi/dmmclip"
	"sdmm/internal/dmapi/dmmsnap"
	"sdmm/internal/imguiext/style"

	"github.com/SpaiR/imgui-go"
	"github.com/rs/zerolog/log"
)

type App interface {
	tilemenu.App
	pquickedit.App
	psettings.App

	Prefs() prefs.Prefs

	LoadedEnvironment() *dmenv.Dme

	DoSelectPrefab(prefab *dmmprefab.Prefab)
	DoEditInstance(*dmminstance.Instance)

	SelectedPrefab() (*dmmprefab.Prefab, bool)
	SelectedInstance() (*dmminstance.Instance, bool)

	HasSelectedPrefab() bool
	HasSelectedInstance() bool

	AddMouseChangeCallback(cb func(uint, uint)) int
	RemoveMouseChangeCallback(id int)

	CommandStorage() *command.Storage
	Clipboard() *dmmclip.Clipboard
	PathsFilter() *dm.PathsFilter

	ShowLayout(name string, focus bool)

	SyncPrefabs()
	SyncVarEditor()
	// APHELION EDIT ADDITION START - COLLABORATION
	RunLater(func())
	CollaborationPresence() []collabui.ObservedPresence
	PublishCollaborationPresence(model.Coord, *protocol.PresenceSelection)
	// APHELION EDIT ADDITION END
}

var (
	MirrorCanvasCamera   bool
	AreaBordersRendering = true

	// Used to do a camera mirroring.
	activeCamera *render.Camera
	// To persist a previous active pane.
	// Mostly for cases when we switch between panes. At that moment activePane is nil.
	lastActivePane *PaneMap
	// Used to do syncs, which require accessing to the currently active pane.
	activePane *PaneMap
)

func ActiveCamera() *render.Camera {
	return activeCamera
}

type PaneMap struct {
	app App

	dmm *dmmap.Dmm

	shortcuts shortcut.Shortcuts

	snapshot *dmmsnap.DmmSnap
	editor   *editor.Editor

	tileMenu *tilemenu.TileMenu

	pQuickEdit *pquickedit.Panel
	pSettings  *psettings.Panel

	showSettings bool
	// APHELION EDIT ADDITION START - SELECTION STAMPS
	stamp *stamps.Stamp
	// APHELION EDIT ADDITION END

	canvas        *canvas.Canvas
	canvasState   *canvas.State
	canvasControl *canvas.Control
	canvasOverlay *canvas.Overlay
	// APHELION EDIT ADDITION START - LOCKED SOURCE CONTEXT
	contextTexture uint32
	// APHELION EDIT ADDITION END

	// ID is needed to dispose a mouse callback when the pane is closed.
	mouseChangeCbId int
	// APHELION EDIT ADDITION START - CURRENT FRAME INPUT
	pointerSamples  []imgui.Vec2
	lastSample      imgui.Vec2
	lastSampleValid bool
	pendingTileMenu bool
	// APHELION EDIT ADDITION END

	// Properties for the pane.
	pos, size imgui.Vec2
	focused   bool
	active    bool
	centered  bool

	panelTopSize         imgui.Vec2
	panelRightTopSize    imgui.Vec2
	panelRightBottomSize imgui.Vec2
	panelBottomSize      imgui.Vec2
	// APHELION EDIT ADDITION START - EDIT STATUS
	editBubble  editBubbleState
	areaBorders areaBorderCache
	// APHELION EDIT ADDITION END

	// The value of the Z-level with which the user is currently working.
	activeLevel int

	tmpLastHoveredInstance *dmminstance.Instance
}

func (p *PaneMap) Canvas() *canvas.Canvas {
	return p.canvas
}

func (p *PaneMap) CanvasState() *canvas.State {
	return p.canvasState
}

func (p *PaneMap) CanvasControl() *canvas.Control {
	return p.canvasControl
}

func (p *PaneMap) CanvasOverlay() *canvas.Overlay {
	return p.canvasOverlay
}

func (p *PaneMap) Editor() *editor.Editor {
	return p.editor
}

func (p *PaneMap) Dmm() *dmmap.Dmm {
	return p.dmm
}

func (p *PaneMap) Focused() bool {
	return p.focused
}

func (p *PaneMap) ActiveLevel() int {
	return p.activeLevel
}

func (p *PaneMap) SetActiveLevel(activeLevel int) {
	// APHELION EDIT ADDITION START - PERSISTENT SELECTION
	if p.activeLevel == activeLevel {
		return
	}
	tools.DeactivateEditor(p.editor)
	// APHELION EDIT ADDITION END
	p.activeLevel = activeLevel
	// APHELION EDIT ADDITION START - PERSISTENT SELECTION
	tools.BindSelectionLevel(p.editor)
	// APHELION EDIT ADDITION END
}

func (p *PaneMap) Size() imgui.Vec2 {
	return p.size
}

func (p *PaneMap) Snapshot() *dmmsnap.DmmSnap {
	return p.snapshot
}

func (p *PaneMap) SetShortcutsVisible(visible bool) {
	p.shortcuts.SetVisible(visible)
}

func New(app App, dmm *dmmap.Dmm) *PaneMap {
	// APHELION EDIT ADDITION START - OWNED MAP OPEN
	return newPaneMap(app, dmm, nil)
}

func NewPrepared(app App, prepared *editor.PreparedOpen) *PaneMap {
	return newPaneMap(app, prepared.Dmm(), prepared)
}

func newPaneMap(app App, dmm *dmmap.Dmm, prepared *editor.PreparedOpen) *PaneMap {
	// APHELION EDIT ADDITION END
	p := &PaneMap{
		app: app,
		dmm: dmm,
	}

	p.activeLevel = 1 // Every map has at least 1 z-level, so we point to it.

	/* APHELION EDIT REMOVAL START - OWNED MAP OPEN
	p.snapshot = dmmsnap.New(dmm)
	p.editor = editor.New(app, p, dmm)
	APHELION EDIT REMOVAL END */
	// APHELION EDIT ADDITION START - OWNED MAP OPEN
	if prepared == nil {
		p.snapshot = dmmsnap.New(dmm)
		p.editor = editor.New(app, p, dmm)
	} else {
		p.snapshot = prepared.Compatibility
		p.editor = editor.NewPrepared(app, p, prepared)
	}
	// APHELION EDIT ADDITION END

	p.tileMenu = tilemenu.New(app, p.editor)

	p.pQuickEdit = pquickedit.New(app, p.editor)
	p.pSettings = psettings.New(app, p.editor)

	p.canvas = canvas.New()
	p.canvasState = canvas.NewState(dmm.MaxX, dmm.MaxY, dmmap.WorldIconSize)
	p.canvasControl = canvas.NewControl()
	p.canvasOverlay = canvas.NewOverlay()

	// APHELION EDIT CHANGE - CURRENT FRAME INPUT - ORIGINAL: p.canvasControl.SetOnRmbClick(p.openTileMenu)
	p.canvasControl.SetOnRmbClick(func() { p.pendingTileMenu = true })

	p.canvas.Render().SetOverlay(p.canvasOverlay)
	p.canvas.Render().SetUnitProcessor(p)
	// APHELION EDIT ADDITION START - OWNED MAP OPEN
	// Register every Z and build progressively on all open paths.
	p.canvas.Render().BeginLevelBuild(p.dmm, p.activeLevel)
	// APHELION EDIT ADDITION END

	p.mouseChangeCbId = app.AddMouseChangeCallback(p.mouseChangeCallback)
	p.addShortcuts()

	return p
}

func (p *PaneMap) Process() {
	// APHELION EDIT ADDITION START - OWNED MAP OPEN
	// Refresh foreground priority; WsArea advances the shared frame budget.
	p.canvas.Render().SetActiveLevel(p.dmm, p.activeLevel)
	// APHELION EDIT ADDITION END
	// APHELION EDIT ADDITION START - COLLABORATION
	p.editor.ProcessCollaborationUpdates()
	p.editor.ProcessPasteWork()
	// APHELION EDIT ADDITION END

	/* APHELION EDIT REMOVAL START - CURRENT FRAME INPUT
	// Enforce a focus to the current window if the canvas was touched.
	if p.canvasControl.Touched() && !imgui.IsWindowFocusedV(imgui.FocusedFlagsRootAndChildWindows) {
		imgui.SetWindowFocus()
	}
	APHELION EDIT REMOVAL END */

	p.updateShortcutsState()

	// Update properties.
	p.pos = imgui.WindowPos().Plus(imgui.WindowContentRegionMin())
	p.size = imgui.WindowSize()
	p.focused = imgui.IsWindowFocusedV(imgui.FocusedFlagsRootAndChildWindows)

	if !p.centered {
		// On first load, set the camera to the center of the map, taking UI size into account.
		p.canvas.Render().Camera.Translate(float32((int(p.size.X)-p.dmm.MaxX*dmmap.WorldIconSize)/2), float32((int(p.size.Y)-p.dmm.MaxY*dmmap.WorldIconSize)/2))
		p.centered = true
	}

	/* APHELION EDIT REMOVAL START - BOUNDED COLD LEVEL
	p.canvas.Render().SetActiveLevel(p.dmm, p.activeLevel)
	APHELION EDIT REMOVAL END */

	p.canvasControl.Process(p.size)
	// APHELION EDIT ADDITION START - MAP COMPOSITION
	p.compositionCamera()
	// APHELION EDIT ADDITION END
	// APHELION EDIT ADDITION START - CURRENT FRAME INPUT
	if p.canvasControl.Touched() && p.canvasControl.Active() {
		imgui.SetWindowFocus()
		if activePane != p {
			p.OnActivate()
		}
	}
	p.ResolveCanvasInput()
	// APHELION EDIT ADDITION END
	// APHELION EDIT ADDITION START - LOCKED SOURCE CONTEXT
	p.contextTexture = 0
	if context, ok := p.app.(interface {
		CompositionBackdrop(string, render.Camera, imgui.Vec2) (uint32, bool)
	}); ok {
		if texture, visible := context.CompositionBackdrop(p.dmm.Path.Absolute, *p.canvas.Render().Camera, p.size); visible {
			p.contextTexture = texture
		}
	}
	p.canvas.SetTransparent(p.contextTexture != 0)
	// APHELION EDIT ADDITION END
	p.canvas.Process(p.size)

	/* APHELION EDIT REMOVAL START - CURRENT FRAME INPUT
	p.processCanvasCamera()
	p.processCanvasOverlay()
	p.processCanvasHoveredInstance()
	APHELION EDIT REMOVAL END */

	p.tileMenu.Process()

	p.showCanvas()
	p.showPanel("canvasTool_"+p.dmm.Name, pPosTop, p.showToolsPanel)
	p.showPanelV("settings_"+p.dmm.Name, pPosRightTop, p.showSettings, p.pSettings.Process)
	p.showPanelV(
		"quickEdit_"+p.dmm.Name,
		pPosRightBottom,
		p.app.Prefs().Controls.QuickEditMapPane && p.active && p.app.HasSelectedInstance(),
		p.pQuickEdit.Process,
	)
	p.showPanel("canvasStat_"+p.dmm.Name, pPosBottom, p.showStatusPanel)
	// APHELION EDIT ADDITION START - EDIT STATUS
	p.showEditStatus()
	// APHELION EDIT ADDITION END
}

func (p *PaneMap) Dispose() {
	// APHELION EDIT ADDITION START - MAP COMPOSITION
	if host, ok := p.app.(interface{ CloseCompositionSource(string) }); ok {
		host.CloseCompositionSource(p.dmm.Path.Absolute)
	}
	// APHELION EDIT ADDITION END
	// APHELION EDIT ADDITION START - SELECTION STAMPS
	if p.stamp != nil {
		p.stamp.Close()
	}
	p.stamp = nil
	// APHELION EDIT ADDITION END
	// APHELION EDIT ADDITION START - CLOSED MAP TOOL OWNERSHIP
	tools.ReleaseEditor(p.editor)
	// APHELION EDIT ADDITION END
	// APHELION EDIT ADDITION START - COLLABORATION
	p.editor.Close()
	// APHELION EDIT ADDITION END
	if p == lastActivePane {
		lastActivePane = nil
	}

	p.syncActiveCamera()
	p.syncActivePane()
	p.canvas.Dispose()
	p.app.RemoveMouseChangeCallback(p.mouseChangeCbId)
	p.tileMenu.Dispose()
	p.shortcuts.Dispose()

	log.Print("disposed")
}

func (p *PaneMap) prepareTools() {
	log.Print("preparing tools:", p.dmm.Name)
	tools.SetEditor(p.editor)
	tools.SetCanvasState(p.canvasState)
	tools.SetCanvasControl(p.canvasControl)
}

func (p *PaneMap) showCanvas() {
	texture := imgui.TextureID(p.canvas.Texture())
	uvMin := imgui.Vec2{X: 0, Y: 1}
	uvMax := imgui.Vec2{X: 1, Y: 0}
	// APHELION EDIT ADDITION START - LOCKED SOURCE CONTEXT
	if p.contextTexture != 0 {
		imgui.WindowDrawList().AddImageV(imgui.TextureID(p.contextTexture), p.canvasControl.PosMin(), p.canvasControl.PosMax(), uvMin, uvMax, style.ColorWhitePacked)
	}
	// APHELION EDIT ADDITION END

	imgui.WindowDrawList().AddImageV(
		texture,
		p.canvasControl.PosMin(), p.canvasControl.PosMax(),
		uvMin, uvMax,
		style.ColorWhitePacked,
	)
	// APHELION EDIT ADDITION START - MAP COMPOSITION
	p.compositionDraw()
	// APHELION EDIT ADDITION END
	// APHELION EDIT ADDITION START - COLLABORATION
	p.showCollaborationPresence()
	// APHELION EDIT ADDITION END
	// APHELION EDIT ADDITION START - LOCKED SOURCE CONTEXT
	if p.contextTexture != 0 {
		at := p.canvasControl.PosMin()
		at.Y = p.canvasControl.PosMax().Y - 48
		imgui.WindowDrawList().AddText(at, style.ColorWhitePacked, "Locked parent context — tools edit this source only")
	}
	// APHELION EDIT ADDITION END
}

func (p *PaneMap) mouseChangeCallback(x, y uint) {
	// APHELION EDIT ADDITION START - CURRENT FRAME INPUT
	if activePane != p && !tools.OwnsGesture(p.editor) {
		return
	}
	if tools.OwnsGesture(p.editor) {
		p.pointerSamples = append(p.pointerSamples, imgui.Vec2{X: float32(int(x)), Y: float32(int(y))})
	}
	// Pointer callbacks record the admitted stroke, but never mutate the map.
	// Current-frame control/camera resolution consumes these samples.
	// APHELION EDIT ADDITION END
	p.updateCanvasMousePosition(int(x), int(y))
	/* APHELION EDIT REMOVAL START - CURRENT FRAME INPUT
	// APHELION EDIT ADDITION START - COLLABORATION
	if !p.canvasState.HoverOutOfBounds() {
		hovered := p.canvasState.HoveredTile()
		var selection *protocol.PresenceSelection
		if selected, ok := tools.Selected().(*tools.ToolGrab); ok && selected.HasSelectedArea() {
			bounds := selected.Bounds()
			selection = &protocol.PresenceSelection{
				Min: model.Coord{X: int(bounds.X1), Y: int(bounds.Y1), Z: p.activeLevel},
				Max: model.Coord{X: int(bounds.X2), Y: int(bounds.Y2), Z: p.activeLevel},
			}
		}
		p.app.PublishCollaborationPresence(model.Coord{X: hovered.X, Y: hovered.Y, Z: hovered.Z}, selection)
	}
	// APHELION EDIT ADDITION END
	tools.OnMouseMove()
	APHELION EDIT REMOVAL END */
}

func (p *PaneMap) openTileMenu() {
	if !p.canvasState.HoverOutOfBounds() {
		log.Print("open tile menu:", p.canvasState.HoveredTile())
		p.tileMenu.Open(p.canvasState.HoveredTile())
	}
}

func (p *PaneMap) processCanvasHoveredInstance() {
	p.canvasState.SetHoveredInstance(p.tmpLastHoveredInstance)
	p.tmpLastHoveredInstance = nil
}

func (p *PaneMap) updateShortcutsState() {
	if imgui.IsWindowFocusedV(imgui.FocusedFlagsRootAndChildWindows) {
		p.shortcuts.SetVisible(true)
	}
}

func (p *PaneMap) OnActivate() {
	log.Print("pane activated:", p.dmm.Name)
	activeCamera = p.canvas.Render().Camera
	activePane = p
	lastActivePane = p
	p.prepareTools()
	p.active = true
	p.focused = true
}

func (p *PaneMap) OnDeactivate() {
	// APHELION EDIT ADDITION START - COMPOSITION ANCHORS
	if host, ok := p.app.(interface{ CancelCompositionDraft(string) }); ok {
		host.CancelCompositionDraft(p.dmm.Path.Absolute)
	}
	// APHELION EDIT ADDITION END
	p.focused = false
	p.active = false
	// APHELION EDIT CHANGE - TOOL GESTURE OWNERSHIP - ORIGINAL: tools.Selected().OnDeselect()
	tools.DeactivateEditor(p.editor)
	p.syncActiveCamera()
	p.syncActivePane()
	log.Print("pane deactivated:", p.dmm.Name)
}

func (p *PaneMap) syncActiveCamera() {
	if activeCamera == p.canvas.Render().Camera {
		activeCamera = nil
		log.Print("active camera cleared:", p.dmm.Name)
	}
}

func (p *PaneMap) syncActivePane() {
	if activePane == p {
		activePane = nil
		log.Print("active pane cleared:", p.dmm.Name)
	}
}

// Fully reloads a canvas for the current pane. Does a full re-initialization of the renderer.
// Needed when changing global parts of the map, like the map size etc.
func (p *PaneMap) reloadCanvas() {
	oldCamera := p.canvas.Render().Camera // To keep current camera position
	// APHELION EDIT ADDITION START - RESIZED CANVAS LIFETIME
	// Keep already-built draw commands valid until the next frame, then
	// release the replaced framebuffer and texture through the normal queue.
	p.canvas.Dispose()
	// APHELION EDIT ADDITION END
	p.canvas = canvas.New()
	p.canvas.Render().Camera = oldCamera
	p.canvas.Render().SetOverlay(p.canvasOverlay)
	p.canvas.Render().SetUnitProcessor(p)
	// APHELION EDIT CHANGE - OWNED MAP OPEN - ORIGINAL: p.canvas.Render().UpdateBucket(p.dmm, p.activeLevel)
	p.canvas.Render().BeginLevelBuild(p.dmm, p.activeLevel)
	p.canvasState.SetMaxX(p.dmm.MaxX)
	p.canvasState.SetMaxY(p.dmm.MaxY)
}

func (p *PaneMap) OnMapSizeChange() {
	// APHELION EDIT ADDITION START - LOCAL RESIZE
	if activePane == p || (activePane == nil && lastActivePane == p) {
		tools.Selected().OnDeselect()
	}
	// APHELION EDIT ADDITION END
	p.reloadCanvas()
	// APHELION EDIT ADDITION START - PERSISTENT SELECTION
	tools.BindSelectionLevel(p.editor)
	// APHELION EDIT ADDITION END
	p.pSettings.DropSessionMapSize()
}
