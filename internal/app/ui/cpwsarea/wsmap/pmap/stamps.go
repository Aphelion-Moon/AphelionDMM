// APHELION EDIT ADDITION START - SELECTION STAMPS
package pmap

import (
	"context"
	"errors"
	"fmt"
	"sdmm/internal/aphelion/collab/mapadapter"
	"sdmm/internal/aphelion/collab/model"
	"sdmm/internal/aphelion/editing"
	"sdmm/internal/aphelion/resources"
	"sdmm/internal/util"
	"strings"

	"github.com/SpaiR/imgui-go"
	native "github.com/sqweek/dialog"
	"sdmm/internal/aphelion/editing/stamps"
	"sdmm/internal/app/ui/dialog"
	w "sdmm/internal/imguiext/widget"
)

func (p *PaneMap) showStampControls() {
	w.Button("Stamps...", p.OpenStamps).Tooltip("Capture a named selection, save or load a stamp file, then preview before placing").Build()
	imgui.SameLine()
	w.Disabled(p.editor == nil || p.editor.WorkingSelection().Get(p.activeLevel).Len() == 0, w.Button("Save selection as stamp...", func() { p.openStamps(true) })).Build()
}

func (p *PaneMap) OpenStamps() {
	p.openStamps(false)
}

func (p *PaneMap) openStamps(saveSelection bool) {
	generation, _ := p.editor.SaveVersion()
	selection := p.editor.WorkingSelection().Get(p.activeLevel)
	sourceCurrent := func() bool {
		actual, _ := p.editor.SaveVersion()
		return actual == generation && p.editor.StampCaptureReason() == ""
	}
	current := func() bool {
		actual, _ := p.editor.SaveVersion()
		return actual == generation && (activePane == p || (activePane == nil && lastActivePane == p)) && p.editor.CanStartMapEdit()
	}
	canCapture := func() bool {
		return sourceCurrent() && selection.Len() != 0
	}
	d := &stampDialog{
		captureAttached: func() bool { actual, _ := p.editor.SaveVersion(); return actual == generation },
		name:            "Selection", runLater: p.app.RunLater, source: "Current selection",
		stamp: p.stamp, canCapture: canCapture, canPlace: current,
		captureReason: func() string {
			if selection.Len() == 0 {
				return "Select tiles before opening Stamps, or capture the clipboard."
			}
			if !sourceCurrent() {
				return "Source map changed or has an unresolved edit; reopen Stamps."
			}
			return ""
		},
		prepareCapture: func(name string) (func(context.Context) (*stamps.Stamp, error), error) {
			if !canCapture() {
				return nil, fmt.Errorf("selection source is unavailable")
			}
			return p.editor.PrepareStampCapture(name, selection)
		},
		capture: func(name string) (*stamps.Stamp, error) {
			if !canCapture() {
				return nil, fmt.Errorf("select tiles on this map and finish pending edits first")
			}
			return p.editor.CaptureStamp(name, selection.Coordinates())
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
	d.captureClipboard = func() {
		data := p.app.Clipboard().Buffer()
		if len(data.Buffer) == 0 {
			d.status = "Clipboard is empty."
			return
		}
		name := d.captureName()
		d.beginWork(func(ctx context.Context) (*stamps.Stamp, error) {
			reservation, err := resources.DefaultBudget().Reserve(uint64(len(data.Buffer))*256 + 1024)
			if err != nil {
				return nil, err
			}
			defer reservation.Release()
			points := make([]util.Point, 0, len(data.Buffer))
			indexes := make(map[model.Coord]int, len(data.Buffer))
			for i := range data.Buffer {
				if err := ctx.Err(); err != nil {
					return nil, err
				}
				tile := &data.Buffer[i]
				points = append(points, tile.Coord)
				indexes[model.Coord{X: tile.Coord.X, Y: tile.Coord.Y, Z: tile.Coord.Z}] = i
			}
			mask, err := editing.MaskSelection(points)
			if err != nil {
				return nil, err
			}
			return stamps.CaptureModel(ctx, name, data.EnvironmentHash, mask, &data.Filter, func(c model.Coord) (model.TileState, bool) {
				index, ok := indexes[c]
				if !ok {
					return model.TileState{}, false
				}
				state, err := mapadapter.CaptureTile(&data.Buffer[index])
				return state, err == nil
			}, nil)
		}, "Clipboard", false)
	}
	if d.stamp != nil {
		d.environmentMatches = d.matches(d.stamp)
	}
	dialog.Open(d)
	if saveSelection {
		d.saveAfterCapture = true
		d.captureSelection()
	}
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
	source                             string
	captureReason                      func() string
	prepareCapture                     func(string) (func(context.Context) (*stamps.Stamp, error), error)
	captureClipboard                   func()
	runLater                           func(func())
	busy, closed, saveAfterCapture     bool
	cancel                             context.CancelFunc
	captureAttached                    func() bool
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
	imgui.Text("Source: " + d.source)
	w.Disabled(d.busy || !d.canCapture(), w.Button("Capture selection", d.captureSelection)).Build()
	imgui.SameLine()
	w.Disabled(d.busy, w.Button("Load stamp...", d.load)).Build()
	if d.captureClipboard != nil {
		imgui.SameLine()
		w.Disabled(d.busy, w.Button("Capture clipboard", d.captureClipboard)).Build()
	}
	if d.captureReason != nil {
		if reason := d.captureReason(); reason != "" {
			imgui.Text(reason)
		}
	}
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
		if d.stamp.EnvironmentHash() == "" {
			imgui.Text("Source environment is unknown. Review compatibility before placing.")
		} else {
			imgui.Text("This stamp was captured in a different environment. Types and defaults may differ; unknown values are retained.")
		}
		imgui.Checkbox("Use with the current environment", &d.allowDifferent)
	}
	imgui.Text("Preview preserves captured and currently hidden layers. Place or cancel it on the map.")
	w.Button("Close", imgui.CloseCurrentPopup).Build()
	imgui.SameLine()
	w.Disabled(d.busy || d.stamp == nil, w.Button("Save stamp...", d.save)).Build()
	imgui.SameLine()
	w.Disabled(d.busy || d.stamp == nil || !d.canPlace() || (!d.environmentMatches && !d.allowDifferent), w.Button("Preview stamp", func() {
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
	if d.busy {
		return
	}
	if d.prepareCapture != nil {
		work, err := d.prepareCapture(d.captureName())
		if err != nil {
			d.status = err.Error()
			return
		}
		save := d.saveAfterCapture
		d.saveAfterCapture = false
		d.beginWork(work, "Current selection", save)
		return
	}
	s, err := d.capture(d.captureName())
	if err != nil {
		d.status = err.Error()
		return
	}
	d.setStamp(s)
	d.status = "Captured in memory. Save a stamp file to keep it after closing the map."
}

func (d *stampDialog) captureName() string {
	if name := strings.TrimSpace(d.name); name != "" {
		return name
	}
	return "Selection"
}

func (d *stampDialog) OnClose() {
	d.closed = true
	if d.cancel != nil {
		d.cancel()
	}
}

func (d *stampDialog) beginWork(work func(context.Context) (*stamps.Stamp, error), source string, save bool) {
	if d.busy {
		return
	}
	d.busy = true
	d.status = "Preparing stamp..."
	ctx, cancel := context.WithCancel(context.Background())
	d.cancel = cancel
	go func() {
		s, err := work(ctx)
		d.runLater(func() {
			d.busy = false
			d.cancel = nil
			cancel()
			if d.closed || source == "Current selection" && d.captureAttached != nil && !d.captureAttached() {
				if s != nil {
					s.Close()
				}
				d.status = "Source map was closed or replaced; capture discarded."
				return
			}
			if err != nil {
				d.status = err.Error()
				return
			}
			d.setStamp(s)
			d.source = source
			d.status = "Captured in memory. Save to keep this stamp."
			if save {
				d.save()
			}
		})
	}()
}

func (d *stampDialog) load() {
	path, err := d.openPath()
	if errors.Is(err, native.ErrCancelled) {
		return
	}
	if err == nil && d.runLater != nil {
		d.beginWork(func(ctx context.Context) (*stamps.Stamp, error) {
			if err := ctx.Err(); err != nil {
				return nil, err
			}
			s, err := stamps.Load(path)
			if err == nil && ctx.Err() != nil {
				s.Close()
				return nil, ctx.Err()
			}
			return s, err
		}, "Stamp file", false)
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
	if err == nil && d.runLater != nil {
		s := d.stamp
		lease, leaseErr := s.Acquire()
		if leaseErr != nil {
			d.status = leaseErr.Error()
			return
		}
		d.busy = true
		d.status = "Saving stamp..."
		go func() {
			saveErr := s.Save(path)
			lease.Release()
			d.runLater(func() {
				d.busy = false
				if d.closed {
					return
				}
				if saveErr != nil {
					d.status = "Unable to save stamp: " + saveErr.Error()
				} else {
					d.status = "Stamp saved. Map contents and clipboard are unchanged."
				}
			})
		}()
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
