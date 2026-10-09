// APHELION EDIT ADDITION START - PLAYTEST
package cpplaytest

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/SpaiR/imgui-go"

	"sdmm/internal/aphelion/playtest"
	"sdmm/internal/app/prefs"
	"sdmm/internal/app/ui/component"
	"sdmm/internal/app/ui/uikit"
	"sdmm/internal/dmapi/dmenv"
)

const defaultPort = 1337

// App is what the panel needs from the application.
type App interface {
	LoadedEnvironment() *dmenv.Dme
	// PlaytestMap is the active map's file and whether it has unsaved edits.
	PlaytestMap() (path string, unsaved bool, ok bool)
	RunLater(func())
	// PlaytestSettings are the persisted settings; edits are saved.
	PlaytestSettings() *prefs.Playtest
}

// Panel launches a local server on the open map and shows its progress.
type Panel struct {
	component.Component
	app     App
	runner  *playtest.Runner
	message string
}

func (p *Panel) Init(app App) {
	p.app = app
	p.runner = playtest.NewRunner()
}

// Free stops a running server when the environment closes.
func (p *Panel) Free() { p.runner.Stop() }

func (p *Panel) settings() *prefs.Playtest {
	s := p.app.PlaytestSettings()
	if s.Port <= 0 || s.Port > 65535 {
		s.Port = defaultPort
	}
	return s
}

func (p *Panel) bin() string {
	if s := p.settings(); s.ByondBin != "" {
		return s.ByondBin
	}
	return playtest.DefaultBin()
}

// Launch prepares and starts a playtest of the active map.
func (p *Panel) Launch() {
	p.message = ""
	env := p.app.LoadedEnvironment()
	path, unsaved, ok := p.app.PlaytestMap()
	switch {
	case env == nil:
		p.message = "Open an environment first."
		return
	case !ok:
		p.message = "Open a map to playtest."
		return
	case unsaved:
		p.message = "Save the map first: the game loads the file on disk."
		return
	case p.runner.Busy():
		p.message = "A playtest is already running; stop it first."
		return
	}
	bin := p.bin()
	if _, err := os.Stat(filepath.Join(bin, "dreamdaemon.exe")); err != nil {
		p.message = "BYOND was not found. Set its bin folder below."
		return
	}
	root, dme := env.RootDir, env.RootFile
	port, always := p.settings().Port, p.settings().AlwaysCompile
	p.message = "Preparing..."
	go func() {
		config, name, err := playtest.MapConfig(root, path)
		if err == nil {
			err = playtest.WriteMapConfig(root, config)
		}
		compile, reason := true, "Always recompile is on"
		if err == nil && !always {
			compile, reason, err = playtest.NeedsCompile(root, strings.TrimSuffix(dme, filepath.Ext(dme))+".dmb")
		}
		if err == nil {
			if compile {
				reason = "recompiling: " + reason
			}
			err = p.runner.Start(playtest.Build(bin, dme, port, compile), root, port, nil)
		}
		p.app.RunLater(func() {
			if err != nil {
				p.message = "Playtest not started: " + err.Error()
				return
			}
			p.message = fmt.Sprintf("Next round loads %s (data/next_map.json). %s", name, reason)
		})
	}()
}

func (p *Panel) Process(int32) {
	phase, lines := p.runner.Snapshot()
	path, _, ok := p.app.PlaytestMap()
	if ok {
		imgui.TextWrapped("Map: " + filepath.Base(path))
	} else {
		uikit.EmptyState("Open and save a map to playtest it.")
	}
	imgui.Text("Status: " + phase.String())
	busy := p.runner.Busy()
	if busy {
		imgui.BeginDisabled()
	}
	if imgui.Button("Launch playtest") {
		p.Launch()
	}
	if busy {
		imgui.EndDisabled()
	}
	imgui.SameLine()
	if !busy {
		imgui.BeginDisabled()
	}
	if imgui.Button("Stop") {
		p.runner.Stop()
	}
	if !busy {
		imgui.EndDisabled()
	}
	if p.message != "" {
		imgui.TextWrapped(p.message)
	}
	if imgui.CollapsingHeader("Settings") {
		s := p.settings()
		bin := s.ByondBin
		if imgui.InputTextWithHint("BYOND bin folder", p.bin(), &bin) {
			s.ByondBin = strings.TrimSpace(bin)
		}
		port := int32(s.Port)
		if imgui.InputInt("Port", &port) && port > 0 && port <= 65535 {
			s.Port = int(port)
		}
		imgui.Checkbox("Always recompile", &s.AlwaysCompile)
		imgui.TextDisabled("Otherwise the build is reused unless code, interface or icon files are newer.")
	}
	imgui.Separator()
	if imgui.BeginChild("playtest-log") {
		for _, line := range lines {
			imgui.Text(line)
		}
		if busy && imgui.ScrollY() >= imgui.ScrollMaxY() {
			imgui.SetScrollHereY(1)
		}
	}
	imgui.EndChild()
}

// APHELION EDIT ADDITION END
