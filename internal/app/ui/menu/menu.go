package menu

import (
	"sdmm/internal/app/command"
	"sdmm/internal/app/ui/shortcut"
	"sdmm/internal/dmapi/dm"
	"sdmm/internal/dmapi/dmenv"
	"sdmm/internal/dmapi/dmmclip"
	"sdmm/internal/imguiext/icon"
	"sdmm/internal/imguiext/style"
	w "sdmm/internal/imguiext/widget"
	/* APHELION EDIT REMOVAL START - EDITABLE SHORTCUTS
	"sdmm/internal/platform"
	APHELION EDIT REMOVAL END */
	"sdmm/internal/rsc"

	"github.com/SpaiR/imgui-go"
)

//goland:noinspection GoCommentStart
type app interface {
	// File
	DoNewWorkspace()
	DoNewMap()
	DoOpen()
	DoLoadResource(path string)
	DoClearRecentMaps()
	DoCloseEnvironment()
	DoClose()
	DoCloseAll()
	DoSave()
	DoSaveAll()
	DoOpenPreferences()
	DoExit()
	// APHELION EDIT ADDITION START - COLLABORATION
	DoCreateLocalCollaborationSession()
	DoSignInHostedCollaboration()
	DoCreateHostedCollaborationSession()
	DoSignOutHostedCollaboration()
	DoJoinCollaborationSession()
	DoLeaveCollaborationSession()
	DoOpenCollaborationPanel()
	// APHELION EDIT ADDITION END

	// Edit
	DoUndo()
	DoRedo()
	DoCopy()
	DoPaste()
	DoCut()
	DoDelete()
	DoSearch()
	DoDeselect()
	DoOpenJumpWindow()

	// View
	DoAreaBorders()
	DoMultiZRendering()
	DoMirrorCanvasCamera()

	// Window
	DoResetLayout()

	// Help
	DoOpenChangelog()
	DoOpenAbout()
	DoOpenLogs()
	DoOpenSourceCode()
	DoCheckForUpdates()
	DoOpenSupport()

	// Other
	DoSelfUpdate()
	DoRestart()
	DoIgnoreUpdate()

	// Helpers

	RecentMapsByLoadedEnvironment() []string
	RecentMaps() []string

	LoadedEnvironment() *dmenv.Dme
	HasLoadedEnvironment() bool

	HasActiveMap() bool
	// APHELION EDIT ADDITION START - COLLABORATION
	HasActiveCollaboration() bool
	HasHostedCollaborationSignIn() bool
	// APHELION EDIT ADDITION END

	PathsFilter() *dm.PathsFilter
	CommandStorage() *command.Storage
	Clipboard() *dmmclip.Clipboard

	AreaBordersRendering() bool
	MultiZRendering() bool
	MirrorCanvasCamera() bool
}

type upStatus int

const (
	upStatusNone upStatus = iota
	upStatusAvailable
	upStatusUpdating
	upStatusUpdated
	upStatusError
)

type Menu struct {
	app app

	shortcuts shortcut.Shortcuts
	// APHELION EDIT ADDITION START - SHORTCUT REFERENCE
	showHotkeys       bool
	hotkeyFilter      string
	hotkeyAction      string
	hotkeyDraft       string
	hotkeyError       string
	hotkeyAllowShared bool
	// APHELION EDIT ADDITION END

	updateStatus      upStatus
	updateVersion     string
	updateDescription string
}

func New(app app) *Menu {
	m := &Menu{app: app}
	m.addShortcuts()
	return m
}

func (m *Menu) Process() {
	w.MainMenuBar(w.Layout{
		w.Menu("File", w.Layout{
			w.MenuItem("New Workspace", m.app.DoNewWorkspace).
				Icon(icon.File).
				// APHELION EDIT CHANGE - EDITABLE SHORTCUTS - ORIGINAL: Shortcut(platform.KeyModName(), "N")
				Shortcut(shortcut.Label("menu#DoNewWorkspace")),
			w.MenuItem("New Map", m.app.DoNewMap).
				IconEmpty().
				Enabled(m.app.HasLoadedEnvironment()),
			w.Separator(),
			w.MenuItem("Open...", m.app.DoOpen).
				Icon(icon.FolderOpen).
				// APHELION EDIT CHANGE - EDITABLE SHORTCUTS - ORIGINAL: Shortcut(platform.KeyModName(), "O")
				Shortcut(shortcut.Label("menu#DoOpen")),
			w.Menu("Recent Maps", w.Layout{
				w.Custom(func() {
					for _, recentMap := range m.app.RecentMaps() {
						w.MenuItem(recentMap, func() {
							m.app.DoLoadResource(recentMap)
						}).IconEmpty().Build()
					}
					w.Layout{
						w.Separator(),
						w.MenuItem("Clear Recent Maps", m.app.DoClearRecentMaps).
							Icon(icon.Delete),
					}.Build()
				}),
			}).Icon(icon.AccessTime).Enabled(len(m.app.RecentMaps()) != 0),
			w.MenuItem("Close Environment", m.app.DoCloseEnvironment).
				IconEmpty().
				Enabled(m.app.HasLoadedEnvironment()),
			w.Separator(),
			w.MenuItem("Close", m.app.DoClose).
				IconEmpty().
				// APHELION EDIT CHANGE - EDITABLE SHORTCUTS - ORIGINAL: Shortcut(platform.KeyModName(), "W")
				Shortcut(shortcut.Label("menu#DoClose")),
			w.MenuItem("Close All", m.app.DoCloseAll).
				IconEmpty().
				// APHELION EDIT CHANGE - EDITABLE SHORTCUTS - ORIGINAL: Shortcut(platform.KeyModName(), "Shift", "W")
				Shortcut(shortcut.Label("menu#DoCloseAll")),
			w.Separator(),
			w.MenuItem("Save", m.app.DoSave).
				Icon(icon.Save).
				Enabled(m.app.HasActiveMap()).
				// APHELION EDIT CHANGE - EDITABLE SHORTCUTS - ORIGINAL: Shortcut(platform.KeyModName(), "S")
				Shortcut(shortcut.Label("menu#DoSave")),
			w.MenuItem("Save All", m.app.DoSaveAll).
				Icon(icon.Save).
				Enabled(m.app.HasActiveMap()).
				// APHELION EDIT CHANGE - EDITABLE SHORTCUTS - ORIGINAL: Shortcut(platform.KeyModName(), "Shift", "S")
				Shortcut(shortcut.Label("menu#DoSaveAll")),
			w.Separator(),
			w.MenuItem("Preferences", m.app.DoOpenPreferences).
				Icon(icon.Wrench),
			w.Separator(),
			w.MenuItem("Exit", m.app.DoExit).
				IconEmpty().
				// APHELION EDIT CHANGE - EDITABLE SHORTCUTS - ORIGINAL: Shortcut(shortcut.Combine(platform.KeyModName(), "Q"))
				Shortcut(shortcut.Label("menu#DoExit")),
			// APHELION EDIT ADDITION START - SHORTCUT FOCUS
			w.Custom(func() {
				shortcut.ProcessPopup("menu#DoNewWorkspace", "menu#DoOpen", "menu#DoClose", "menu#DoCloseAll", "menu#DoSave", "menu#DoSaveAll", "menu#DoExit")
			}),
			// APHELION EDIT ADDITION END
		}),

		w.Menu("Edit", w.Layout{
			w.MenuItem("Undo", m.app.DoUndo).
				Icon(icon.Undo).
				Enabled(m.app.CommandStorage().HasUndo()).
				// APHELION EDIT CHANGE - EDITABLE SHORTCUTS - ORIGINAL: Shortcut(platform.KeyModName(), "Z")
				Shortcut(shortcut.Label("menu#DoUndo")),
			w.MenuItem("Redo", m.app.DoRedo).
				Icon(icon.Redo).
				Enabled(m.app.CommandStorage().HasRedo()).
				// APHELION EDIT CHANGE - EDITABLE SHORTCUTS - ORIGINAL: Shortcut(platform.KeyModName(), "Shift", "Z")
				Shortcut(shortcut.Label("menu#DoRedo")),
			w.Separator(),
			w.MenuItem("Copy", m.app.DoCopy).
				Icon(icon.ContentCopy).
				// APHELION EDIT CHANGE - EDITABLE SHORTCUTS - ORIGINAL: Shortcut(platform.KeyModName(), "C")
				Shortcut(shortcut.Label("menu#DoCopy")),
			w.MenuItem("Paste", m.app.DoPaste).
				Icon(icon.ContentPaste).
				Enabled(m.app.Clipboard().HasData()).
				// APHELION EDIT CHANGE - EDITABLE SHORTCUTS - ORIGINAL: Shortcut(platform.KeyModName(), "V")
				Shortcut(shortcut.Label("menu#DoPaste")),
			w.MenuItem("Cut", m.app.DoCut).
				Icon(icon.ContentCut).
				// APHELION EDIT CHANGE - EDITABLE SHORTCUTS - ORIGINAL: Shortcut(platform.KeyModName(), "X")
				Shortcut(shortcut.Label("menu#DoCut")),
			w.MenuItem("Delete", m.app.DoDelete).
				Icon(icon.Eraser).
				// APHELION EDIT CHANGE - EDITABLE SHORTCUTS - ORIGINAL: Shortcut("Delete")
				Shortcut(shortcut.Label("menu#DoDelete")),
			w.MenuItem("Deselect", m.app.DoDeselect).
				IconEmpty().
				// APHELION EDIT CHANGE - EDITABLE SHORTCUTS - ORIGINAL: Shortcut(platform.KeyModName(), "D")
				Shortcut(shortcut.Label("pmap#doDeselectAll")),
			w.Separator(),
			w.MenuItem("Search", m.app.DoSearch).
				Icon(icon.Search).
				Enabled(m.app.HasActiveMap()).
				// APHELION EDIT CHANGE - EDITABLE SHORTCUTS - ORIGINAL: Shortcut(platform.KeyModName(), "F")
				Shortcut(shortcut.Label("menu#DoSearch")),
			w.MenuItem("Go to Coords", m.app.DoOpenJumpWindow).
				Icon(icon.Shrink).
				Enabled(m.app.HasActiveMap()).
				// APHELION EDIT CHANGE - EDITABLE SHORTCUTS - ORIGINAL: Shortcut(platform.KeyModName(), "G")
				Shortcut(shortcut.Label("menu#DoOpenJumpWindow")),
			// APHELION EDIT ADDITION START - SHORTCUT FOCUS
			w.Custom(func() {
				shortcut.ProcessPopup("menu#DoUndo", "menu#DoRedo", "menu#DoCopy", "menu#DoPaste", "menu#DoCut", "menu#DoDelete", "pmap#doDeselectAll", "menu#DoSearch", "menu#DoOpenJumpWindow")
			}),
			// APHELION EDIT ADDITION END
		}),

		// APHELION EDIT ADDITION START - COLLABORATION
		w.Menu("Collaboration", w.Layout{
			w.MenuItem("Show Session Panel", m.app.DoOpenCollaborationPanel).
				IconEmpty(),
			w.MenuItem("Start Local Session", m.app.DoCreateLocalCollaborationSession).
				IconEmpty().
				Enabled(m.app.HasActiveMap() && !m.app.HasActiveCollaboration()),
			w.MenuItem("Sign In to Hosted Service", m.app.DoSignInHostedCollaboration).
				IconEmpty().
				Enabled(!m.app.HasHostedCollaborationSignIn() && !m.app.HasActiveCollaboration()),
			w.MenuItem("Start Hosted Session", m.app.DoCreateHostedCollaborationSession).
				IconEmpty().
				Enabled(m.app.HasActiveMap() && m.app.HasHostedCollaborationSignIn() && !m.app.HasActiveCollaboration()),
			w.MenuItem("Join Session", m.app.DoJoinCollaborationSession).
				IconEmpty().
				Enabled(m.app.HasActiveMap() && !m.app.HasActiveCollaboration()),
			w.MenuItem("Leave Session", m.app.DoLeaveCollaborationSession).
				IconEmpty().
				Enabled(m.app.HasActiveCollaboration()),
			w.MenuItem("Sign Out of Hosted Service", m.app.DoSignOutHostedCollaboration).
				IconEmpty().
				Enabled(m.app.HasHostedCollaborationSignIn() && !m.app.HasActiveCollaboration()),
		}),
		// APHELION EDIT ADDITION END

		w.Menu("View", w.Layout{
			w.MenuItem("Show Area", m.doToggleArea).
				IconEmpty().
				Enabled(m.app.HasLoadedEnvironment()).
				Selected(m.isAreaToggled()).
				// APHELION EDIT CHANGE - EDITABLE SHORTCUTS - ORIGINAL: Shortcut(platform.KeyModName(), "1")
				Shortcut(shortcut.Label("pmap#doToggleArea")),
			w.MenuItem("Show Turf", m.doToggleTurf).
				IconEmpty().
				Enabled(m.app.HasLoadedEnvironment()).
				Selected(m.isTurfToggled()).
				// APHELION EDIT CHANGE - EDITABLE SHORTCUTS - ORIGINAL: Shortcut(platform.KeyModName(), "2")
				Shortcut(shortcut.Label("pmap#doToggleTurf")),
			w.MenuItem("Show Object", m.doToggleObject).
				IconEmpty().
				Enabled(m.app.HasLoadedEnvironment()).
				Selected(m.isObjectToggled()).
				// APHELION EDIT CHANGE - EDITABLE SHORTCUTS - ORIGINAL: Shortcut(platform.KeyModName(), "3")
				Shortcut(shortcut.Label("pmap#doToggleObject")),
			w.MenuItem("Show Mob", m.doToggleMob).
				IconEmpty().
				Enabled(m.app.HasLoadedEnvironment()).
				Selected(m.isMobToggled()).
				// APHELION EDIT CHANGE - EDITABLE SHORTCUTS - ORIGINAL: Shortcut(platform.KeyModName(), "4")
				Shortcut(shortcut.Label("pmap#doToggleMob")),
			w.MenuItem("Show All", m.doShowAll).
				IconEmpty().
				Enabled(m.app.HasLoadedEnvironment()),
			w.Separator(),
			w.MenuItem("Area Borders", m.app.DoAreaBorders).
				IconEmpty().
				Selected(m.app.AreaBordersRendering()),
			w.MenuItem("Multi-Z Rendering", m.app.DoMultiZRendering).
				IconEmpty().
				Selected(m.app.MultiZRendering()).
				// APHELION EDIT CHANGE - EDITABLE SHORTCUTS - ORIGINAL: Shortcut(platform.KeyModName(), "0")
				Shortcut(shortcut.Label("menu#DoMultiZRendering")),
			w.MenuItem("Mirror Canvas Camera", m.app.DoMirrorCanvasCamera).
				IconEmpty().
				Selected(m.app.MirrorCanvasCamera()),
			// APHELION EDIT ADDITION START - SHORTCUT FOCUS
			w.Custom(func() {
				shortcut.ProcessPopup("pmap#doToggleArea", "pmap#doToggleTurf", "pmap#doToggleObject", "pmap#doToggleMob", "menu#DoMultiZRendering")
			}),
			// APHELION EDIT ADDITION END
		}),

		w.Menu("Window", w.Layout{
			// APHELION EDIT CHANGE - EDITABLE SHORTCUTS - ORIGINAL: w.MenuItem("Reset Layout", m.app.DoResetLayout).Shortcut("F5").
			w.MenuItem("Reset Layout", m.app.DoResetLayout).Shortcut(shortcut.Label("menu#DoResetLayout")).
				Icon(icon.WindowRestore),
			// APHELION EDIT ADDITION START - SHORTCUT FOCUS
			w.Custom(func() { shortcut.ProcessPopup("menu#DoResetLayout") }),
			// APHELION EDIT ADDITION END
		}),

		w.Menu("Help", w.Layout{
			// APHELION EDIT ADDITION START - SHORTCUT REFERENCE
			w.MenuItem("Keyboard Shortcuts", m.openShortcutReference).Shortcut(shortcut.Label("menu#showHotkeys")).IconEmpty(),
			w.Separator(),
			// APHELION EDIT ADDITION END
			w.MenuItem("Changelog", m.app.DoOpenChangelog).
				Icon(icon.ClipboardMultiple),
			w.MenuItem("Source Code", m.app.DoOpenSourceCode).
				Icon(icon.GitHub),
			w.MenuItem("Check for Updates", m.app.DoCheckForUpdates).
				Icon(icon.SystemUpdate),
			w.Separator(),
			w.MenuItem("Open Logs Folder", m.app.DoOpenLogs).
				IconEmpty(),
			w.MenuItem("About", m.app.DoOpenAbout).
				IconEmpty(),
			w.Separator(),
			// APHELION EDIT CHANGE - BRANDING - ORIGINAL: w.Button("Support", m.app.DoOpenSupport).
			w.Button("Support StrongDMM", m.app.DoOpenSupport).
				Size(imgui.Vec2{X: -1}).
				Style(style.ButtonFireCoral{}).
				Tooltip(rsc.SupportTxt).
				Icon(icon.KoFi),
			// APHELION EDIT ADDITION START - SHORTCUT FOCUS
			w.Custom(func() { shortcut.ProcessPopup("menu#showHotkeys") }),
			// APHELION EDIT ADDITION END
		}),

		w.Custom(func() {
			if m.updateStatus != upStatusNone {
				m.showUpdateMenu()
			}
		}),
	}).Build()
	// APHELION EDIT ADDITION START - SHORTCUT REFERENCE
	if m.showHotkeys {
		m.showShortcutReference()
	}
	// APHELION EDIT ADDITION END
}

func (m *Menu) SetUpdateAvailable(version, description string) {
	m.updateStatus = upStatusAvailable
	m.updateVersion = version
	m.updateDescription = description
}

func (m *Menu) SetUpdating() {
	m.updateStatus = upStatusUpdating
}

func (m *Menu) SetUpdated() {
	m.updateStatus = upStatusUpdated
}

func (m *Menu) SetUpdateError() {
	m.updateStatus = upStatusError
}

func (m *Menu) doToggleArea() {
	m.app.PathsFilter().TogglePath("/area")
}

func (m *Menu) doToggleTurf() {
	m.app.PathsFilter().TogglePath("/turf")
}

func (m *Menu) doToggleObject() {
	m.app.PathsFilter().TogglePath("/obj")
}

func (m *Menu) doToggleMob() {
	m.app.PathsFilter().TogglePath("/mob")
}

func (m *Menu) doShowAll() {
	m.app.PathsFilter().Clear()
}

func (m *Menu) isAreaToggled() bool {
	return m.app.PathsFilter().IsVisiblePath("/area")
}

func (m *Menu) isTurfToggled() bool {
	return m.app.PathsFilter().IsVisiblePath("/turf")
}

func (m *Menu) isObjectToggled() bool {
	return m.app.PathsFilter().IsVisiblePath("/obj")
}

func (m *Menu) isMobToggled() bool {
	return m.app.PathsFilter().IsVisiblePath("/mob")
}
