// APHELION EDIT ADDITION START - SELECTION STAMPS
package pmap

import (
	"errors"
	"fmt"
	"strings"

	"github.com/SpaiR/imgui-go"
	native "github.com/sqweek/dialog"
	"sdmm/internal/aphelion/editing/stamps"
	"sdmm/internal/app/ui/cpwsarea/wsmap/tools"
	"sdmm/internal/app/ui/dialog"
	w "sdmm/internal/imguiext/widget"
)

func (p *PaneMap) showStampControls() {
	w.Button("Stamps...", p.OpenStamps).Tooltip("Capture a named selection, save or load a stamp file, then preview before placing").Build()
}

func (p *PaneMap) OpenStamps() {
	generation, _ := p.editor.SaveVersion()
	current := func() bool {
		actual, _ := p.editor.SaveVersion()
		return actual == generation && (activePane == p || (activePane == nil && lastActivePane == p)) && p.editor.CanStartMapEdit()
	}
	canCapture := func() bool {
		if !current() || !p.canRotateSelection() {
			return false
		}
		g := tools.Selected().(*tools.ToolGrab)
		return !g.Placing()
	}
	d := &stampDialog{
		stamp: p.stamp, canCapture: canCapture, canPlace: current,
		capture: func(name string) (*stamps.Stamp, error) {
			if !canCapture() {
				return nil, fmt.Errorf("select tiles on this map and finish pending edits first")
			}
			return p.editor.CaptureStamp(name, tools.SelectedTiles())
		},
		place: func(s *stamps.Stamp, allow bool) error {
			if !current() {
				return fmt.Errorf("map changed or is busy; close this view and reopen Stamps on the target map")
			}
			return p.editor.StartStamp(s, allow)
		},
		matches:  p.editor.StampEnvironmentMatches,
		remember: func(s *stamps.Stamp) { p.stamp = s },
		openPath: func() (string, error) {
			return native.File().Title("Load Selection Stamp").Filter("Selection stamp", "admmstamp").Load()
		},
		savePath: func() (string, error) {
			return native.File().Title("Save Selection Stamp").Filter("Selection stamp", "admmstamp").SetStartFile("selection.admmstamp").Save()
		},
	}
	if d.stamp != nil {
		d.environmentMatches = d.matches(d.stamp)
	}
	dialog.Open(d)
}

type stampDialog struct {
	stamp                              *stamps.Stamp
	name, status                       string
	environmentMatches, allowDifferent bool
	canCapture, canPlace               func() bool
	capture                            func(string) (*stamps.Stamp, error)
	place                              func(*stamps.Stamp, bool) error
	matches                            func(*stamps.Stamp) bool
	remember                           func(*stamps.Stamp)
	openPath, savePath                 func() (string, error)
}

func (*stampDialog) Name() string         { return "Selection stamps" }
func (*stampDialog) HasCloseButton() bool { return true }

func (d *stampDialog) Process() {
	size := imgui.MainViewport().WorkSize()
	width := min(float32(620), max(float32(280), size.X-48))
	imgui.PushTextWrapPosV(imgui.CursorPosX() + width)
	imgui.Text("Capture visible selection contents. Save a stamp file to reuse after closing the map.")
	imgui.PopTextWrapPos()
	w.InputText("Name", &d.name).Width(width - 80).Build()
	w.Disabled(!d.canCapture() || strings.TrimSpace(d.name) == "", w.Button("Capture selection", d.captureSelection)).Build()
	imgui.SameLine()
	w.Button("Load stamp...", d.load).Build()
	if imgui.BeginChildV("stamp-preview", imgui.Vec2{X: width, Y: min(float32(260), max(float32(80), size.Y-300))}, true, imgui.WindowFlagsHorizontalScrollbar) {
		if d.stamp == nil {
			imgui.Text("No stamp loaded.")
		} else {
			imgui.Text(d.stamp.Preview())
		}
	}
	imgui.EndChild()
	imgui.PushTextWrapPosV(imgui.CursorPosX() + width)
	defer imgui.PopTextWrapPos()
	if d.status != "" {
		imgui.Text(d.status)
	}
	if d.stamp != nil && !d.environmentMatches {
		imgui.Text("This stamp was captured in a different environment. Types and defaults may differ; unknown values are retained.")
		imgui.Checkbox("Use with the current environment", &d.allowDifferent)
	}
	imgui.Text("Preview preserves captured and currently hidden layers. Place or cancel it on the map.")
	w.Button("Close", imgui.CloseCurrentPopup).Build()
	imgui.SameLine()
	w.Disabled(d.stamp == nil, w.Button("Save stamp...", d.save)).Build()
	imgui.SameLine()
	w.Disabled(d.stamp == nil || !d.canPlace() || (!d.environmentMatches && !d.allowDifferent), w.Button("Preview stamp", func() {
		if d.preview() {
			imgui.CloseCurrentPopup()
		}
	})).Build()
}

func (d *stampDialog) setStamp(s *stamps.Stamp) {
	if d.stamp != nil && d.stamp != s {
		d.stamp.Close()
	}
	d.stamp, d.allowDifferent = s, false
	d.environmentMatches = d.matches(s)
	d.remember(s)
}

func (d *stampDialog) captureSelection() {
	s, err := d.capture(d.name)
	if err != nil {
		d.status = err.Error()
		return
	}
	d.setStamp(s)
	d.status = "Captured in memory. Save a stamp file to keep it after closing the map."
}

func (d *stampDialog) load() {
	path, err := d.openPath()
	if errors.Is(err, native.ErrCancelled) {
		return
	}
	var s *stamps.Stamp
	if err == nil {
		s, err = stamps.Load(path)
	}
	if err != nil {
		d.status = "Unable to load stamp: " + err.Error()
		return
	}
	d.setStamp(s)
	d.status = "Loaded stamp. Preview it before placing."
}

func (d *stampDialog) save() {
	if d.stamp == nil {
		return
	}
	path, err := d.savePath()
	if errors.Is(err, native.ErrCancelled) {
		return
	}
	if err == nil {
		err = d.stamp.Save(path)
	}
	if err != nil {
		d.status = "Unable to save stamp: " + err.Error()
		return
	}
	d.status = "Stamp saved. Map contents and clipboard are unchanged."
}

func (d *stampDialog) preview() bool {
	if d.stamp == nil || (!d.environmentMatches && !d.allowDifferent) {
		return false
	}
	if err := d.place(d.stamp, d.allowDifferent); err != nil {
		d.status = err.Error()
		return false
	}
	return true
}

// APHELION EDIT ADDITION END
