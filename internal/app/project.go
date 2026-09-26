package app

import (
	// APHELION EDIT ADDITION START - ENVIRONMENT SNAPSHOT
	"sdmm/internal/aphelion/diagnostics/uistage"
	"sdmm/internal/aphelion/envload"
	"sdmm/internal/aphelion/envsnapshot"
	"sdmm/internal/aphelion/mapindex"
	// APHELION EDIT ADDITION END
	"context"
	"fmt"
	"os"
	"path/filepath"
	// APHELION EDIT REMOVAL START - OWNED MAP OPEN
	// "runtime"
	// APHELION EDIT REMOVAL END
	"sdmm/third_party/sdmmparser"
	"sort"
	"time"

	"sdmm/internal/app/ui/cpwsarea/workspace"
	// APHELION EDIT ADDITION START - LOADING RESPONSIVENESS
	"sdmm/internal/app/ui/cpwsarea/wsmap"
	// APHELION EDIT ADDITION END
	// APHELION EDIT ADDITION START - OWNED MAP OPEN
	"sdmm/internal/app/ui/cpwsarea/wsmap/pmap/editor"
	"sdmm/internal/dmapi/dmmap/dmmdata/dmmprefab"
	// APHELION EDIT ADDITION END
	"sdmm/internal/app/ui/dialog"
	"sdmm/internal/app/window"
	"sdmm/internal/dmapi/dm"
	// APHELION EDIT REMOVAL - LOADING RESPONSIVENESS: "sdmm/internal/imguiext/style"
	w "sdmm/internal/imguiext/widget"

	"sdmm/internal/dmapi/dmenv"
	"sdmm/internal/dmapi/dmicon"
	"sdmm/internal/dmapi/dmmap"
	"sdmm/internal/dmapi/dmmap/dmmdata"
	"sdmm/internal/util"

	"github.com/SpaiR/imgui-go"
	"github.com/rs/zerolog/log"
)

func (a *app) loadResource(path string) {
	a.loadResourceV(path, nil)
}

// Universal method to open any editor resource.
// If it gets a map file, then the code will try to find an environment to open it.
func (a *app) loadResourceV(path string, ws *workspace.Workspace) {
	path, err := filepath.Abs(path)
	if err != nil {
		log.Print("unable to get resource absolute path:", err)
		return
	}

	if filepath.Ext(path) == ".dme" {
		a.loadEnvironment(path)
		return
	}

	// APHELION EDIT CHANGE - SOURCE AUTHORING - ORIGINAL: if filepath.Ext(path) != ".dmm" {
	if filepath.Ext(path) != ".dmm" && filepath.Ext(path) != ".tgm" {
		log.Print("invalid resource to load:", path)
		return
	}

	/* APHELION EDIT REMOVAL START - OWNED MAP OPEN
	environmentPath, err := findEnvironmentFileFromBase(path)
	APHELION EDIT REMOVAL END */

	if a.HasLoadedEnvironment() {
		a.loadMap(path, ws)
		return
	}
	// APHELION EDIT ADDITION START - OWNED MAP OPEN
	environmentPath, err := findEnvironmentFileFromBase(path)
	// APHELION EDIT ADDITION END

	if err == nil {
		a.loadEnvironmentV(environmentPath, func() {
			a.loadMap(path, ws)
		})
	} else {
		log.Print("unable to find environment from file:", path)
		dialog.Open(dialog.TypeInformation{
			Title: "No dme found!",
			Information: "Can't find an environment file.\n" +
				"Please, ensure it can be accessed or open manually.\n" +
				path,
		})
	}
}

// Goes through all parents starting from the current file location and look for a ".dme" file.
func findEnvironmentFileFromBase(path string) (string, error) {
	for {
		dir := filepath.Dir(path)

		if dir == path {
			return "", fmt.Errorf("unable to find environment")
		}

		files, err := os.ReadDir(dir)
		if err != nil {
			log.Print("unable to read dir while looking for environment:", err)
			return "", fmt.Errorf("unable to read dir: "+dir, err)
		}

		for _, file := range files {
			if filepath.Ext(file.Name()) == ".dme" {
				return filepath.Join(dir, file.Name()), nil
			}
		}

		path = filepath.Dir(path)
	}
}

func (a *app) loadEnvironment(path string) {
	a.loadEnvironmentV(path, nil)
}

func (a *app) loadEnvironmentV(path string, callback func()) {
	a.closeEnvironment(func(closed bool) {
		if closed {
			a.forceLoadEnvironment(path, callback)
		}
	})
}

func (a *app) forceLoadEnvironment(path string, callback func()) {
	// APHELION EDIT ADDITION START - ENVIRONMENT SNAPSHOT
	a.forceLoadEnvironmentWithOptions(path, callback, envsnapshot.Options{Enabled: !a.preferencesConfig().Application.BypassEnvironmentCache})
}

func (a *app) forceLoadEnvironmentWithOptions(path string, callback func(), options envsnapshot.Options) {
	// APHELION EDIT ADDITION END
	log.Printf("opening environment [%s]...", path)
	// APHELION EDIT ADDITION START - OWNED ENVIRONMENT LOAD
	a.environmentLoadRequest++
	request := a.environmentLoadRequest
	if a.environmentLoadCancel != nil {
		a.environmentLoadCancel()
	}
	if a.environmentLoadDialog != nil {
		dialog.Close(a.environmentLoadDialog)
	}
	a.environmentLoadFinish = nil
	ctx, cancel := context.WithCancel(context.Background())
	a.environmentLoadCancel = cancel
	progress := &envload.Progress{}
	started := time.Now()
	published := false
	dlg := dialog.TypeCustom{Title: "Loading", Layout: w.Layout{w.Text(path), w.Custom(func() {
		label := progress.Stage()
		if ctx.Err() != nil {
			label = "Cancelling - waiting for the current preparation step"
		}
		imgui.TextWrapped(label)
		imgui.Text(fmt.Sprint(time.Since(started).Round(time.Second)))
		imgui.BeginDisabledV(ctx.Err() != nil)
		cancelLabel := "Cancel load"
		if published {
			cancelLabel = "Cancel remaining load"
		}
		if imgui.Button(cancelLabel) {
			cancel()
		}
		imgui.EndDisabled()
	})}}
	a.environmentLoadDialog = dlg
	dialog.Open(dlg)
	// APHELION EDIT ADDITION END

	afterLoad := func(env *dmenv.Dme) {
		// Cancellation can discard prepared state until this UI-owned publication.
		// Afterwards the ordinary close/save workflow owns the installed project.
		published = true
		progress.Set("Retiring previous environment resources")
		retire := uistage.Begin(uistage.Stage("aphelion.environment.retire"))
		a.freeEnvironmentResources()
		retire.End()
		install := uistage.Begin(uistage.Stage("aphelion.environment.install"))
		defer install.End()
		progress.Set("Installing environment and visibility")

		a.projectConfig().AddProject(path)
		a.loadedEnvironment = env
		a.pathsFilter = newPathsFilter(env)
		// APHELION EDIT ADDITION START - FILTER PROFILES
		a.layout.Environment.BindFilterEnvironment(env)
		// APHELION EDIT ADDITION END

		dmicon.Cache.SetRootDirPath(env.RootDir)
		dmmap.Init(env)

		a.layout.WsArea.AddEmptyWorkspaceIfNone()
		a.UpdateTitle()

		// APHELION EDIT REMOVAL START - OWNED MAP OPEN
		// runtime.GC() - let the Go heap budget schedule collection off the UI callback.
		// APHELION EDIT REMOVAL END

		log.Print("environment opened:", path)

		if callback != nil {
			progress.Set("Opening requested map")
			callback()
		}
		frames := 0
		a.environmentLoadFinish = func() {
			if request != a.environmentLoadRequest || a.loadedEnvironment != env || a.closed {
				dialog.Close(dlg)
				a.environmentLoadFinish = nil
				return
			}
			if ctx.Err() != nil {
				// Installed documents are valid independent owners. Stop pending
				// opens and remove the modal without claiming geometry is ready.
				a.cancelMapOpens()
				dialog.Close(dlg)
				a.environmentLoadDialog, a.environmentLoadFinish, a.environmentLoadCancel = nil, nil, nil
				log.Info().Msg("environment installed; remaining load cancelled")
				return
			}
			frames++
			ready := a.layout.Environment.VisibilityReady() && a.layout.WsArea.ResourceDiscoveryReady() && a.mapOpenActive == nil && len(a.mapOpenQueue) == 0
			for _, workspace := range a.layout.WsArea.MapWorkspaces() {
				if ws, ok := workspace.Content().(*wsmap.WsMap); ok && !ws.Map().Canvas().Render().LevelReady(ws.Map().ActiveLevel()) {
					ready = false
				}
			}
			if !ready || frames < 2 {
				progress.Set("Preparing visibility and current map view")
				if !a.layout.WsArea.ResourceDiscoveryReady() {
					progress.Set("Discovering project maps")
				}
				return
			}
			usable := uistage.Begin(uistage.Stage("aphelion.environment.first_usable_frame"))
			usable.End()
			log.Info().Dur("elapsed", time.Since(started)).Msg("environment usable")
			dialog.Close(dlg)
			a.environmentLoadDialog = nil
			a.environmentLoadFinish = nil
			a.environmentLoadCancel = nil
		}
	}

	go func() {
		/* APHELION EDIT REMOVAL START - OWNED ENVIRONMENT LOAD
		dlg := makeLoadingDialog(path)
		dialog.Open(dlg)
		defer dialog.Close(dlg)
		APHELION EDIT REMOVAL END */

		start := time.Now()
		log.Printf("parsing environment: [%s]...", path)

		// APHELION EDIT CHANGE - ENVIRONMENT SNAPSHOT - ORIGINAL: env, err := dmenv.New(path)
		env, err := dmenv.NewWithProgress(ctx, path, options, progress.Set)
		// APHELION EDIT ADDITION START - OWNED ENVIRONMENT LOAD
		elapsed := time.Since(start)
		window.RunLater(func() {
			if request != a.environmentLoadRequest || a.closed {
				return
			}
			if ctx.Err() != nil {
				dialog.Close(dlg)
				a.environmentLoadDialog = nil
				a.environmentLoadCancel = nil
				return
			}
			// APHELION EDIT ADDITION END

			if err != nil {
				dialog.Close(dlg)
				a.environmentLoadDialog = nil
				a.environmentLoadCancel = nil
				log.Print("unable to open environment by path:", path, err)

				if sdmmparser.IsParserError(err) {
					dialog.Open(dialog.TypeCustom{
						Title:       "Parser Error!",
						CloseButton: true,
						Layout: w.Layout{
							w.Text("Unable to open environment: " + path),
							w.Separator(),
							w.Text(err.Error()),
						},
					})
				} else {
					dialog.Open(dialog.TypeInformation{
						Title:       "Error!",
						Information: "Unable to open environment: " + path,
					})
				}
				return
			}

			// APHELION EDIT CHANGE - OWNED ENVIRONMENT LOAD - ORIGINAL: log.Printf("environment [%s] parsed in [%d] ms", path, time.Since(start).Milliseconds())
			log.Printf("environment [%s] parsed in [%d] ms", path, elapsed.Milliseconds())
			// APHELION EDIT ADDITION START - ENVIRONMENT SNAPSHOT
			log.Info().Str("cache", env.CacheStatus).Msg("environment load completed")
			// APHELION EDIT ADDITION END

			// APHELION EDIT REMOVAL START - OWNED ENVIRONMENT LOAD
			// window.RunLater(func() { moved before all completion UI.
			// APHELION EDIT REMOVAL END
			afterLoad(env)
		})
	}()
}

/* APHELION EDIT REMOVAL START - LOADING RESPONSIVENESS
func makeLoadingDialog(path string) dialog.Type {
	start := time.Now()
	return dialog.TypeCustom{
		Title: "Loading",
		Layout: w.Layout{
			w.Text(path),
			w.Custom(func() {
				passed := fmt.Sprint(time.Since(start).Round(time.Second))

				width := imgui.WindowWidth()
				textW := imgui.CalcTextSize(passed, false, 0).X

				imgui.SetCursorPos(imgui.Vec2{X: (width - textW) * .5, Y: imgui.CursorPosY()})
				imgui.TextColored(style.ColorGold, passed)
			}),
		},
	}
}
APHELION EDIT REMOVAL END */

// Configure paths filter to access a newly opened environment.
func newPathsFilter(env *dmenv.Dme) *dm.PathsFilter {
	return dm.NewPathsFilter(func(path string) []string {
		// APHELION EDIT CHANGE - FILTER PROFILES - ORIGINAL: return env.Objects[path].DirectChildren
		if object := env.Objects[path]; object != nil {
			return object.DirectChildren
		}
		return nil
	})
}

func (a *app) loadMap(path string, workspace *workspace.Workspace) {
	// APHELION EDIT ADDITION START - OWNED MAP OPEN
	a.enqueueMapOpen(path, workspace)
}

func (a *app) installParsedMap(path string, workspace *workspace.Workspace, data *dmmdata.DmmData, backup string) {
	dmm, unknown := dmmap.New(a.loadedEnvironment, data, backup)
	a.installOpenMap(path, workspace, dmm, unknown, nil)
}

func (a *app) installOpenMap(path string, workspace *workspace.Workspace, dmm *dmmap.Dmm, unknownPrefabs map[string]*dmmprefab.Prefab, prepared *editor.PreparedOpen) {
	// APHELION EDIT ADDITION END
	/* APHELION EDIT REMOVAL START - OWNED MAP OPEN
	log.Printf("opening map [%s]...", path)

	start := time.Now()
	log.Printf("parsing map: [%s]...", path)
	data, err := dmmdata.New(path)
	if err != nil {
		log.Printf("unable to open map by path [%s]: %v", path, err)
		dialog.Open(dialog.TypeInformation{
			Title:       "Error: Unable to open map",
			Information: fmt.Sprintf("Error while parsing the map:\n - %s\n - %s", path, err),
		})
		return
	}
	elapsed := time.Since(start).Milliseconds()
	log.Printf("map [%s] parsed in [%d] ms", path, elapsed)
	APHELION EDIT REMOVAL END */
	/* APHELION EDIT REMOVAL START - OWNED MAP OPEN
	// APHELION EDIT ADDITION START - BACKUP FAILURE ISOLATION
	backup, err := a.backupMap(path)
	if err != nil {
		log.Error().Err(err).Msg("map open aborted before installation")
		dialog.Open(dialog.TypeCustom{
			Title: "Unable to back up map",
			Layout: w.Layout{
				w.Text(fmt.Sprintf("The map was not opened because its recovery backup failed:\n%s", err)),
				w.Button("Retry", func() {
					imgui.CloseCurrentPopup()
					window.RunLater(func() { a.loadMap(path, workspace) })
				}),
				w.SameLine(),
				w.Button("Cancel", imgui.CloseCurrentPopup),
			},
		})
		return
	}
	// APHELION EDIT ADDITION END
	APHELION EDIT REMOVAL END */

	// Add map to the recent only if it is a part of the currently opened environment.
	// APHELION EDIT CHANGE - LOADING RESPONSIVENESS - ORIGINAL: if slice.StrContains(a.AvailableMaps(), path) {
	if mapindex.Contains(a.LoadedEnvironment().RootDir, path) {
		log.Print("adding map path to the recent:", path)
		cfg := a.projectConfig()
		cfg.AddMap(path)
	} else {
		log.Print("ignoring map path add to the recent, since it's an outside resource")
	}

	/* APHELION EDIT REMOVAL START - OWNED MAP OPEN
	dmm, unknownPrefabs := dmmap.New(a.loadedEnvironment, data, backup)
	if a.layout.WsArea.OpenMap(dmm, workspace) {
	APHELION EDIT REMOVAL END */
	// APHELION EDIT ADDITION START - OWNED MAP OPEN
	var installed bool
	if prepared != nil {
		installed = a.layout.WsArea.OpenPreparedMap(prepared, workspace)
	} else {
		installed = a.layout.WsArea.OpenMap(dmm, workspace)
	}
	if installed {
		// APHELION EDIT ADDITION END
		a.layout.Prefabs.Sync()

		// TODO: processing for unknown prefabs
		if len(unknownPrefabs) != 0 {
			// Collect keys
			var prefabPaths []string
			for path := range unknownPrefabs {
				prefabPaths = append(prefabPaths, path)
			}

			// Sort them alphabetically
			sort.Strings(prefabPaths)

			// Build the string
			var prefabsNames string
			for _, path := range prefabPaths {
				prefabsNames += " - " + path + "\n"
			}

			dialog.Open(dialog.TypeInformation{
				Title: "Unknown Types [WIP]",
				Information: fmt.Sprintf(
					"There are unknown types on the map: %s\n"+
						// APHELION EDIT CHANGE - UNKNOWN TYPE PRESERVATION - ORIGINAL: "Types below will be discarded on save:\n"+
						"These types are preserved on save; rendering may be limited:\n"+
						"%s", dmm.Name, prefabsNames,
				),
			})
		}
	}
	a.layout.Search.Free()

	// APHELION EDIT REMOVAL START - OWNED MAP OPEN
	// runtime.GC() - collection follows the normal heap budget.
	// APHELION EDIT REMOVAL END

	log.Print("map opened:", path)
}

func (a *app) closeEnvironment(callback func(bool)) {
	// APHELION EDIT ADDITION START - COLLABORATION
	completeReplacement, err := a.collaborationProjectReplacementGuard()
	if err != nil {
		log.Error().Err(err).Msg("unable to prepare environment replacement")
		util.ShowErrorDialog("Unable to close environment: " + err.Error())
		if callback != nil {
			callback(false)
		}
		return
	}
	// APHELION EDIT ADDITION END

	// NewMap workspaces depend on the opened environment, so we close them too.
	a.layout.WsArea.CloseAllCreateMaps()
	// APHELION EDIT CHANGE - COLLABORATION - ORIGINAL: a.layout.WsArea.CloseAllMaps(func(closed bool) {
	a.layout.WsArea.CloseAllMapsGuarded(completeReplacement, func(closed bool) {
		if callback != nil {
			callback(closed)
		}
	})
}

// APHELION EDIT ADDITION START - COLLABORATION

func (a *app) collaborationProjectReplacementGuard() (func() bool, error) {
	replacementPermit, err := a.collaborationController.BeginProjectReplacement()
	if err != nil {
		return nil, err
	}
	return func() bool {
		ctx, cancel := context.WithTimeout(context.Background(), collaborationActionTimeout)
		defer cancel()
		if err := a.collaborationController.CompleteProjectReplacement(ctx, replacementPermit); err != nil {
			log.Error().Err(err).Msg("unable to complete project replacement")
			util.ShowErrorDialog("Unable to close project: " + err.Error())
			return false
		}
		a.collaborationEditor = nil
		return true
	}, nil
}

// APHELION EDIT ADDITION END

// Frees all resources connected with opened environment.
func (a *app) freeEnvironmentResources() {
	// APHELION EDIT ADDITION START - OWNED MAP OPEN
	a.cancelMapOpens()
	// APHELION EDIT ADDITION START - COMPOSITION INSPECTOR
	if a.layout.Composition != nil {
		a.layout.Composition.Invalidate()
	}
	// APHELION EDIT ADDITION END
	// APHELION EDIT ADDITION END
	log.Print("free environment resources...")

	a.pathsFilter = dm.NewPathsFilterEmpty()

	a.layout.Prefabs.Free()
	a.layout.Search.Free()
	a.layout.Environment.Free()
	a.layout.WsArea.Free()
	a.layout.VarEditor.Free()

	a.commandStorage.Free()
	a.clipboard.Free()

	dmicon.Cache.Free()
	dmmap.PrefabStorage.Free()
	dmmap.Free()

	a.loadedEnvironment = nil

	a.UpdateTitle()

	log.Print("environment resources free!")
}

func (a *app) environmentName() string {
	if a.loadedEnvironment != nil {
		return a.loadedEnvironment.Name
	}
	return ""
}

// APHELION EDIT CHANGE - BACKUP FAILURE ISOLATION - ORIGINAL: func (a *app) backupMap(path string) string {
func (a *app) backupMap(path string) (string, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		/* APHELION EDIT REMOVAL START - BACKUP FAILURE ISOLATION
		log.Print("unable to read map to backup:", path)
		util.ShowErrorDialog("Unable to read map to backup: " + path)
		os.Exit(1)
		APHELION EDIT REMOVAL END */
		// APHELION EDIT ADDITION START - BACKUP FAILURE ISOLATION
		return "", fmt.Errorf("read map for backup: %w", err)
		// APHELION EDIT ADDITION END
	}

	// format: backup/environment.dme/map.dmm/time.dmm
	dst := filepath.FromSlash(a.backupDir + "/" +
		a.environmentName() + "/" +
		filepath.Base(path) + "/" +
		time.Now().Format(util.TimeFormat) + ".dmm",
	)

	// APHELION EDIT ADDITION START - BACKUP FAILURE ISOLATION
	// ORIGINAL: _ = os.MkdirAll(filepath.Dir(dst), os.ModePerm)
	if err := os.MkdirAll(filepath.Dir(dst), os.ModePerm); err != nil {
		return "", fmt.Errorf("create map backup directory: %w", err)
	}
	// APHELION EDIT ADDITION END

	err = os.WriteFile(dst, data, os.ModePerm)
	if err != nil {
		/* APHELION EDIT REMOVAL START - BACKUP FAILURE ISOLATION
		log.Print("unable to write map backup to a file:", dst)
		util.ShowErrorDialog("Unable to write map backup to a file: " + path)
		os.Exit(1)
		APHELION EDIT REMOVAL END */
		// APHELION EDIT ADDITION START - BACKUP FAILURE ISOLATION
		return "", fmt.Errorf("write map backup: %w", err)
		// APHELION EDIT ADDITION END
	}
	log.Print("map backup created:", dst)

	// APHELION EDIT CHANGE - BACKUP FAILURE ISOLATION - ORIGINAL: return dst
	return dst, nil
}
