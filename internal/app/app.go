package app

import (
	"context"
	"os"
	"path/filepath"
	"runtime"
	"time"

	"github.com/rs/zerolog/log"

	"sdmm/internal/aphelion/collab/model"
	// APHELION EDIT ADDITION START - UI STAGE TRACE
	"sdmm/internal/aphelion/diagnostics/uistage"
	// APHELION EDIT ADDITION END
	collabserver "sdmm/internal/aphelion/collab/server"
	collabui "sdmm/internal/aphelion/collab/ui"
	"sdmm/internal/app/command"
	"sdmm/internal/app/config"
	"sdmm/internal/app/render/brush"
	"sdmm/internal/app/ui/cpwsarea/wsmap/pmap/editor"
	"sdmm/internal/app/ui/dialog"
	"sdmm/internal/app/ui/layout"
	"sdmm/internal/app/ui/menu"
	"sdmm/internal/app/ui/shortcut"
	"sdmm/internal/app/window"
	"sdmm/internal/dmapi/dm"
	"sdmm/internal/dmapi/dmenv"
	"sdmm/internal/dmapi/dmmclip"
	"sdmm/internal/env"
	"sdmm/internal/util"

	"github.com/SpaiR/imgui-go"
	"github.com/matishsiao/goInfo"
)

const (
	ttlDaysLogs    = 14
	ttlDaysBackups = 3
)

func Start() {
	internalDir := getOrCreateInternalDir()
	logDir := initializeLogs(internalDir)
	// APHELION EDIT ADDITION START - UI STAGE TRACE
	if path := os.Getenv("APHELIONDMM_UI_TRACE"); path != "" {
		if recording, err := uistage.StartFile(path, 30*time.Second); err != nil {
			log.Error().Err(err).Msg("Unable to start UI execution trace")
		} else {
			defer func() {
				if err := recording.Close(); err != nil {
					log.Error().Err(err).Msg("Unable to finish UI execution trace")
				}
			}()
			log.Info().Msg("UI execution trace enabled for up to 30 seconds")
		}
	}
	// APHELION EDIT ADDITION END

	log.Info().Msgf("%s, %s", env.Title, env.Version)
	log.Info().Msgf("internal dir: %s", internalDir)
	log.Info().Msgf("log dir: %s", logDir)

	if osInfo, err := goInfo.GetInfo(); err == nil {
		log.Info().Msg("System info:")
		log.Info().Msgf("  Kernel: %s", osInfo.Kernel)
		log.Info().Msgf("  Core: %s", osInfo.Core)
		log.Info().Msgf("  Platform: %s", osInfo.Platform)
		log.Info().Msgf("  OS: %s", osInfo.OS)
		log.Info().Msgf("  CPUs: %d", osInfo.CPUs)
		log.Info().Msgf("  Runtime: %s", runtime.Version())
	}

	a := app{
		internalDir: internalDir,
		logDir:      logDir,
		backupDir:   filepath.FromSlash(internalDir + "/backup"),
		configDir:   filepath.FromSlash(internalDir + "/config"),
	}

	a.masterWindow = window.New(&a)

	log.Info().Msg("start phase: [initialize]")
	a.initialize()
	log.Info().Msg("end phase: [initialize]")

	log.Info().Msg("start phase: [process]")
	a.masterWindow.Process()
	log.Info().Msg("end phase: [process]")

	log.Info().Msg("start phase: [dispose]")
	a.dispose()
	log.Info().Msg("end phase: [dispose]")
}

type app struct {
	masterWindow *window.Window

	internalDir string
	logDir      string
	backupDir   string
	configDir   string

	tmpShouldClose bool
	tmpWindowCond  imgui.Condition
	tmpUpdateScale bool

	// ...omae wa mou shindeiru...
	// Should be modified only in the CloseCheck method. Ensures we made everything before the closing.
	closed bool

	shortcutsEnabled bool

	loadedEnvironment *dmenv.Dme
	pathsFilter       *dm.PathsFilter

	configs map[string]config.Config

	commandStorage *command.Storage
	clipboard      *dmmclip.Clipboard
	// APHELION EDIT ADDITION START - COLLABORATION
	collaborationClient     *collabui.SessionClient
	collaborationController *collabui.Controller
	collaborationEditor     *editor.Editor
	// APHELION EDIT ADDITION END

	menu   *menu.Menu
	layout *layout.Layout
}

func (a *app) initialize() {
	a.deleteOldLogs()
	a.deleteOldBackups()

	a.loadConfig()
	a.loadProjectConfig()
	a.loadPreferencesConfig()

	a.runBackgroundConfigSave()

	a.shortcutsEnabled = true

	a.commandStorage = command.NewStorage()
	a.pathsFilter = dm.NewPathsFilterEmpty()
	a.clipboard = dmmclip.New()
	// APHELION EDIT ADDITION START - COLLABORATION
	a.collaborationClient = collabui.NewSessionClient(collabui.SessionClientConfig{})
	a.collaborationController = collabui.NewController(func(ctx context.Context, snapshot model.Snapshot) (collabui.EmbeddedService, error) {
		return collabserver.StartEmbedded(ctx, snapshot)
	}, a.collaborationClient)
	// APHELION EDIT ADDITION END

	a.menu = menu.New(a)
	a.layout = layout.New(a)

	a.updateScale()
	a.updateLayoutState()

	a.UpdateTitle()

	a.checkProgramArgs()

	if a.preferencesConfig().Application.CheckForUpdates {
		go a.checkForUpdates()
	}
}

func (a *app) Process() {
	if a.shortcutsEnabled {
		shortcut.Process()
	}

	a.menu.Process()
	a.layout.Process()

	dialog.Process()
}

func (a *app) PostProcess() {
	a.checkShouldClose()
	a.checkUpdateScale()
	a.dropTmpState()
}

func (a *app) CloseCheck() {
	log.Print("run close check")
	// APHELION EDIT ADDITION START - COLLABORATION
	if a.HasActiveCollaboration() {
		guard, err := a.collaborationProjectReplacementGuard()
		if err != nil {
			log.Error().Err(err).Msg("unable to prepare application close")
			util.ShowErrorDialog("Unable to close application: " + err.Error())
			a.closed = false
			return
		}
		a.layout.WsArea.CloseAllMapsGuarded(guard, func(closed bool) {
			a.closed = closed
		})
		return
	}
	// APHELION EDIT ADDITION END
	a.layout.WsArea.CloseAllMaps(func(closed bool) {
		a.closed = closed
	})
}

func (a *app) IsClosed() bool {
	return a.closed
}

func (a *app) LayoutIniPath() string {
	return filepath.FromSlash(a.internalDir + "/layout.ini")
}

func (a *app) dispose() {
	// APHELION EDIT ADDITION START - COLLABORATION
	if a.collaborationController != nil {
		shutdownContext, cancelShutdown := context.WithTimeout(context.Background(), 5*time.Second)
		if err := a.collaborationController.Leave(shutdownContext); err != nil {
			log.Error().Err(err).Msg("leave collaboration session during shutdown")
		}
		cancelShutdown()
	}
	// APHELION EDIT ADDITION END
	brush.Dispose()
	a.configSave()
	a.masterWindow.Dispose()
}

func getOrCreateInternalDir() string {
	var internalDir string

	userHomeDir, err := os.UserHomeDir()
	if err != nil {
		panic("unable to find user home dir")
	}

	if runtime.GOOS == "windows" {
		internalDir = userHomeDir + "/AppData/Roaming/StrongDMM"
	} else {
		internalDir = userHomeDir + "/.strongdmm"
	}
	_ = os.MkdirAll(internalDir, os.ModePerm)

	return filepath.FromSlash(internalDir)
}

func (a *app) checkShouldClose() {
	if a.tmpShouldClose {
		a.masterWindow.Handle().SetShouldClose(true)
	}
}

func (a *app) checkUpdateScale() {
	if a.tmpUpdateScale {
		a.updateScale()
	}
}

func (a *app) dropTmpState() {
	a.tmpShouldClose = false
	a.tmpWindowCond = imgui.ConditionFirstUseEver
	a.tmpUpdateScale = false
}

func (a *app) updateScale() {
	// APHELION EDIT CHANGE - STATIC_ANALYSIS - ORIGINAL: window.SetPointSize(float32(a.preferencesConfig().Prefs.Interface.Scale) / 100)
	window.SetPointSize(float32(a.preferencesConfig().Interface.Scale) / 100)
}

// Checks the version of the layout in the user config data and the app itself.
// When different, the user layout will be reset.
// Otherwise, the layout will persist its state between the app sessions.
func (a *app) updateLayoutState() {
	if a.layout.CheckLayoutState() {
		a.layout.SyncLayoutState()
		log.Print("reset layout state")
		a.resetLayout()
	} else if _, err := os.Stat(a.LayoutIniPath()); os.IsNotExist(err) {
		log.Print("no layout was found, resetting...")
		a.resetLayout()
	} else {
		log.Print("layout state is not changed")
	}
}

func (a *app) resetLayout() {
	_ = os.Remove(a.LayoutIniPath())
	log.Print("layout data deleted:", a.LayoutIniPath())
	a.tmpWindowCond = imgui.ConditionAlways
	log.Print("layout reset")
}

func (a *app) deleteOldLogs() {
	logsCount := deleteOldFiles(a.logDir, ttlDaysLogs)
	if logsCount > 0 {
		log.Print("old logs deleted:", logsCount)
	} else {
		log.Print("no old logs to delete")
	}
}

func (a *app) deleteOldBackups() {
	backupsCount := deleteOldFiles(a.backupDir, ttlDaysBackups)
	if backupsCount > 0 {
		log.Print("old backups deleted:", backupsCount)
	} else {
		log.Print("no old backups to delete")
	}
}

func deleteOldFiles(dir string, ttlDays float64) (deletedFiles int) {
	_ = filepath.Walk(dir, func(path string, info os.FileInfo, _ error) error {
		if info != nil && !info.IsDir() && time.Since(info.ModTime()).Hours()/24 > ttlDays {
			if err := os.Remove(path); err == nil {
				log.Print("old file deleted:", path)
				deletedFiles++
			} else {
				log.Printf("unable to delete old file [%s]: %v", path, err)
			}
		}
		return nil
	})
	return
}
