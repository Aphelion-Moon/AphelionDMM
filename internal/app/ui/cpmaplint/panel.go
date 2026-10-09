// APHELION EDIT ADDITION START - PLACEMENT LINT
package cpmaplint

import (
	"context"
	"fmt"

	"github.com/SpaiR/imgui-go"

	"sdmm/internal/aphelion/maplint"
	"sdmm/internal/app/ui/component"
	"sdmm/internal/app/ui/cpwsarea/wsmap/pmap/editor"
)

const findingsLimit = 2000

// App is what the panel needs from the application.
type App interface {
	CurrentEditor() *editor.Editor
	RunLater(func())
}

// Panel lists the loaded repository lint rules and scans the open map for
// violations. Scans run on a worker over an immutable committed revision.
type Panel struct {
	component.Component

	app App

	scan     *scanState
	scanID   uint64
	owner    *editor.Editor
	result   *editor.LintScanResult
	scanErr  string
	selected int
}

type scanState struct {
	id     uint64
	cancel context.CancelFunc
}

func (p *Panel) Init(app App) { p.app = app }

// Free cancels any scan and drops results (environment closed or switched).
func (p *Panel) Free() {
	if p.scan != nil {
		p.scan.cancel()
	}
	p.scan, p.owner, p.result, p.scanErr, p.selected = nil, nil, nil, "", -1
	p.scanID++
}

// StartScan begins a scan of the current map. Completion is fenced by scan id
// and by the editor that was current when it started.
func (p *Panel) StartScan() {
	ed := p.app.CurrentEditor()
	p.cancelScan()
	p.result, p.scanErr, p.selected = nil, "", -1
	if ed == nil {
		p.scanErr = "No map opened"
		return
	}
	job, err := ed.PrepareLintScan(findingsLimit)
	if err != nil {
		p.scanErr = err.Error()
		return
	}
	p.scanID++
	ctx, cancel := context.WithCancel(context.Background())
	state := &scanState{id: p.scanID, cancel: cancel}
	p.scan, p.owner = state, ed
	go func() {
		result, err := job(ctx)
		p.app.RunLater(func() {
			if p.scan != state {
				return
			}
			p.scan = nil
			cancel()
			if err != nil {
				if ctx.Err() == nil {
					p.scanErr = err.Error()
				}
				return
			}
			p.result = &result
		})
	}()
}

func (p *Panel) cancelScan() {
	if p.scan != nil {
		p.scan.cancel()
		p.scan = nil
	}
}

func (p *Panel) Process(int32) {
	status := maplint.Active().Status()
	p.showStatus(status)
	imgui.Separator()
	if !status.Active() || status.Loading || status.RuleCount == 0 {
		return
	}
	ed := p.app.CurrentEditor()
	if ed == nil {
		imgui.TextDisabled("No map opened")
		return
	}
	if p.scan != nil {
		imgui.TextDisabled("Scanning...")
		imgui.SameLine()
		if imgui.Button("Cancel scan") {
			p.cancelScan()
		}
	} else if imgui.Button("Scan open map") {
		p.StartScan()
	}
	if p.scanErr != "" {
		imgui.TextWrapped(p.scanErr)
	}
	if p.result == nil {
		return
	}
	if p.owner != ed {
		imgui.TextDisabled("Results are for a different map; scan again.")
		return
	}
	summary := fmt.Sprintf("%d violations in %d tiles", len(p.result.Findings), p.result.Tiles)
	if p.result.Truncated {
		summary += fmt.Sprintf(" (stopped at %d)", findingsLimit)
	}
	imgui.Text(summary)
	if imgui.BeginChild("maplint-results") {
		for i, f := range p.result.Findings {
			label := fmt.Sprintf("%d,%d,%d  %s: %s##lint%d", f.Coord.X, f.Coord.Y, f.Coord.Z, f.Rule, f.Message, i)
			if imgui.SelectableV(label, p.selected == i, 0, imgui.Vec2{}) {
				p.selected = i
				ed.FocusCameraOnPosition(f.Coord)
			}
			if f.Help != "" && imgui.IsItemHovered() {
				imgui.SetTooltip(f.Help)
			}
		}
	}
	imgui.EndChild()
}

func (p *Panel) showStatus(status maplint.Status) {
	switch {
	case !status.Active():
		imgui.TextWrapped("This environment ships no tools/maplint/lints directory; placement checks are inactive.")
		return
	case status.Loading:
		imgui.TextDisabled("Loading rules...")
		return
	}
	imgui.TextWrapped(fmt.Sprintf("%d rules from %d files (%s)", status.RuleCount, len(status.Files), status.Dir))
	if len(status.Unsupported) != 0 && imgui.TreeNodeV(fmt.Sprintf("%d unsupported rules (ignored)##unsupported", len(status.Unsupported)), 0) {
		for _, line := range status.Unsupported {
			imgui.TextWrapped(line)
		}
		imgui.TreePop()
	}
	if len(status.Errors) != 0 && imgui.TreeNodeV(fmt.Sprintf("%d files failed to load##loaderrors", len(status.Errors)), 0) {
		for _, line := range status.Errors {
			imgui.TextWrapped(line)
		}
		imgui.TreePop()
	}
}

// APHELION EDIT ADDITION END
