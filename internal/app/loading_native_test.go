package app

import (
	"crypto/sha256"
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"sdmm/internal/aphelion/diagnostics/uistage"
	"sdmm/internal/aphelion/envsnapshot"
	"sdmm/internal/app/ui/cpwsarea/wsmap"
	"sdmm/internal/app/window"
)

type loadMeasurement struct {
	Scenario          string
	Cache             string
	ElapsedMS         float64
	LargestFrameGapMS float64
	Frames            int
}

type loadingProbe struct {
	*app
	afterFrame func()
}

func (p *loadingProbe) PostProcess() {
	p.app.PostProcess()
	p.afterFrame()
}

// Exercises the shipped window loop and application open/close entry points.
// The window stays hidden: these timings are not physical desktop acceptance.
// Run each mode in a fresh process; reuse APHELION_LOAD_CACHE_HOME only for the
// cold -> warm pair. Fixture maps are read and backed up but never edited/saved.
func TestNativeEnvironmentLoading(t *testing.T) {
	dme := os.Getenv("APHELION_LOAD_DME")
	mode := os.Getenv("APHELION_LOAD_MODE")
	if os.Getenv("APHELIONDMM_GL_TEST") != "1" || dme == "" || mode == "" {
		t.Skip("set native load fixture, mode and GL opt-in")
	}
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	if cache := os.Getenv("APHELION_LOAD_CACHE_HOME"); cache != "" {
		t.Setenv("LOCALAPPDATA", cache)
	}
	inputs := []string{dme}
	mapPath, extra := os.Getenv("APHELION_LOAD_MAP"), os.Getenv("APHELION_LOAD_EXTRA_MAP")
	if mapPath != "" {
		inputs = append(inputs, mapPath)
	}
	if extra != "" {
		inputs = append(inputs, extra)
	}
	for _, path := range inputs {
		data, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		before := sha256.Sum256(data)
		t.Cleanup(func() {
			data, err := os.ReadFile(path)
			if err != nil || sha256.Sum256(data) != before {
				t.Errorf("fixture changed: %s: %v", filepath.Base(path), err)
			}
		})
	}
	dir := t.TempDir()
	a := &app{internalDir: dir, logDir: filepath.Join(dir, "logs"), backupDir: filepath.Join(dir, "backup"), configDir: filepath.Join(dir, "config")}
	if err := os.MkdirAll(a.configDir, 0700); err != nil {
		t.Fatal(err)
	}
	// Prevent network update checks and choose only the requested cache path.
	preferences := `{"Version":3,"Application":{"CheckForUpdates":false,"AutoUpdate":false,"BypassEnvironmentCache":` + map[bool]string{true: "true", false: "false"}[mode == "bypass"] + `}}`
	if err := os.WriteFile(filepath.Join(a.configDir, "preferences.json"), []byte(preferences), 0600); err != nil {
		t.Fatal(err)
	}
	args := os.Args
	os.Args = os.Args[:1]
	defer func() { os.Args = args }()
	probe := &loadingProbe{app: a}
	a.masterWindow = window.New(probe)
	a.masterWindow.Handle().Hide()
	a.initialize()
	window.RunLater(a.masterWindow.Handle().Hide)
	defer a.dispose()
	var measurements []loadMeasurement
	var started, previous, settled time.Time
	var cacheWait time.Time
	var largest time.Duration
	frames, phase := 0, 0
	scenario := mode
	finish := func() {
		measurements = append(measurements, loadMeasurement{scenario, a.loadedEnvironment.CacheStatus, float64(time.Since(started).Microseconds()) / 1000, float64(largest.Microseconds()) / 1000, frames})
		t.Logf("%+v", measurements[len(measurements)-1])
	}
	begin := func(name string, action func()) {
		scenario = name
		started = time.Now()
		previous = started
		settled = time.Time{}
		largest = 0
		frames = 0
		action()
	}
	probe.afterFrame = func() {
		now := time.Now()
		if !cacheWait.IsZero() {
			cache, _ := envsnapshot.DefaultDirectory()
			entries, _ := filepath.Glob(filepath.Join(cache, "*.json"))
			if len(entries) > 0 || now.Sub(cacheWait) > 30*time.Second {
				t.Logf("cache persistence wait: %s; entries=%d", now.Sub(cacheWait), len(entries))
				a.closed = true
			}
			return
		}
		if started.IsZero() {
			begin(mode, func() {
				if mode == "map-trigger" {
					a.DoLoadResource(mapPath)
				} else {
					a.DoLoadResource(dme)
				}
			})
			return
		}
		frames++
		if mode == "cancel-installed" && a.loadedEnvironment != nil {
			if a.environmentLoadCancel != nil {
				a.environmentLoadCancel()
			}
			if a.environmentLoadDialog == nil && a.environmentLoadFinish == nil {
				if a.mapOpenActive != nil || len(a.mapOpenQueue) != 0 {
					t.Error("installed cancellation retained a pending map open")
				}
				measurements = append(measurements, loadMeasurement{Scenario: mode, Cache: "cancelled after publication; environment retained", ElapsedMS: float64(now.Sub(started).Microseconds()) / 1000, Frames: frames})
				a.closed = true
			}
			return
		}
		if mode == "cancel" && now.Sub(started) > 200*time.Millisecond {
			if a.environmentLoadCancel != nil {
				a.environmentLoadCancel()
			}
			if a.loadedEnvironment != nil {
				t.Error("cancelled environment was published")
				a.closed = true
				return
			}
			if a.environmentLoadDialog == nil {
				measurements = append(measurements, loadMeasurement{Scenario: mode, Cache: "cancelled before publication", ElapsedMS: float64(now.Sub(started).Microseconds()) / 1000, Frames: frames})
				a.closed = true
				return
			}
		}
		if gap := now.Sub(previous); gap > largest {
			largest = gap
		}
		previous = now
		if now.Sub(started) > 150*time.Second {
			t.Error("load did not reach a usable state", scenario)
			a.closed = true
			return
		}
		ready := a.loadedEnvironment != nil && a.environmentLoadDialog == nil && a.environmentLoadFinish == nil && a.mapOpenActive == nil && len(a.mapOpenQueue) == 0
		for _, workspace := range a.layout.WsArea.MapWorkspaces() {
			if ws, ok := workspace.Content().(*wsmap.WsMap); ok && !ws.Map().Canvas().Render().LevelReady(ws.Map().ActiveLevel()) {
				ready = false
			}
		}
		if !ready {
			settled = time.Time{}
			return
		}
		if settled.IsZero() {
			settled = now
			return
		}
		if now.Sub(settled) < 250*time.Millisecond {
			return
		}
		finish()
		if mode == "repeat" && phase == 0 {
			phase++
			old := a.loadedEnvironment
			begin("repeat-in-process", func() { a.DoLoadResource(dme) })
			if a.loadedEnvironment != old {
				t.Error("environment changed before asynchronous publication")
			}
			return
		}
		if mode == "map-trigger" && phase == 0 && extra != "" {
			phase++
			begin("additional-map", func() { a.DoLoadResource(extra) })
			return
		}
		if mode == "cold" {
			cacheWait = now
		} else {
			a.closed = true
		}
	}
	if output := os.Getenv("APHELION_LOAD_TRACE"); output != "" {
		recording, err := uistage.StartFile(output, 5*time.Minute)
		if err != nil {
			t.Fatal(err)
		}
		defer recording.Close()
	}
	a.masterWindow.Process()
	if len(measurements) == 0 {
		t.Fatal("no completed load")
	}
	if mode == "warm" && !strings.Contains(measurements[0].Cache, "validated cache hit") {
		t.Error("warm run was not a validated cache hit")
	}
	if output := os.Getenv("APHELION_LOAD_OUTPUT"); output != "" {
		data, err := json.MarshalIndent(measurements, "", "  ")
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(output, data, 0600); err != nil {
			t.Fatal(err)
		}
	}
}
