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

	// Automatic fixes. fixDisabled holds the kinds switched off; it starts as
	// the opt-in kinds.
	fixDisabled maplint.FixKinds
	fixing      bool
	fixMessage  string
}

func (p *Panel) fixKinds() maplint.FixKinds { return maplint.AllFixes &^ p.fixDisabled }

type scanState struct {
	id     uint64
	cancel context.CancelFunc
}

func (p *Panel) Init(app App) {
	p.app = app
	p.fixDisabled = maplint.AllFixes &^ maplint.DefaultFixes
}

// Free cancels any scan and drops results (environment closed or switched).
func (p *Panel) Free() {
	if p.scan != nil {
		p.scan.cancel()
	}
	p.scan, p.owner, p.result, p.scanErr, p.selected = nil, nil, nil, "", -1
	p.fixing, p.fixMessage = false, ""
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
		p.fixMessage = ""
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
	summary := fmt.Sprintf("%d violations in %d tiles", p.result.Violations, p.result.Tiles)
	if p.result.Truncated {
		summary += fmt.Sprintf(" (first %d listed)", findingsLimit)
	}
	imgui.Text(summary)
	p.showFixes(ed)
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

// showFixes offers the automatic fixes the last scan previewed. Applying
// replans from the current map, runs as one undoable edit and rescans.
func (p *Panel) showFixes(ed *editor.Editor) {
	if p.fixMessage != "" {
		imgui.TextWrapped(p.fixMessage)
	}
	if p.result.FixTiles == 0 {
		if p.result.Violations != 0 {
			imgui.TextDisabled("No violation can be fixed automatically.")
		}
		return
	}
	perKind := map[maplint.FixKind]int{}
	for _, c := range p.result.Fixes {
		perKind[c.Kind] += c.Count
	}
	if !imgui.CollapsingHeaderV(fmt.Sprintf("Automatic fixes: up to %d violations on %d tiles##maplint-fixes", p.result.Fixable, p.result.FixTiles), imgui.TreeNodeFlagsDefaultOpen) {
		return
	}
	for _, kind := range maplint.FixKindList {
		if perKind[kind] == 0 {
			continue
		}
		enabled := p.fixKinds().Has(kind)
		if imgui.Checkbox(fmt.Sprintf("%s (%d)##fixkind%d", kind, perKind[kind], kind), &enabled) {
			p.fixDisabled = p.fixDisabled.Toggle(kind)
		}
		if imgui.IsItemHovered() {
			imgui.SetTooltip(kind.Description())
		}
	}
	if imgui.TreeNodeV("By rule##maplint-fix-rules", 0) {
		for _, c := range p.result.Fixes {
			imgui.Text(fmt.Sprintf("%s: %s x%d", c.RuleFile, c.Kind, c.Count))
		}
		imgui.TreePop()
	}
	busy := p.fixing || p.scan != nil
	if busy {
		imgui.BeginDisabled()
	}
	if imgui.Button("Apply fixes") {
		p.applyFixes(ed)
	}
	if busy {
		imgui.EndDisabled()
	}
	imgui.SameLine()
	imgui.TextDisabled("One undoable edit; the map is rescanned afterwards.")
}

func (p *Panel) applyFixes(ed *editor.Editor) {
	p.fixing, p.fixMessage = true, ""
	err := ed.ApplyLintFixes(p.fixKinds(), func(r editor.LintFixResult) {
		p.fixing = false
		switch {
		case r.Err != nil:
			p.fixMessage = "Fixes were not applied: " + r.Err.Error()
		case r.Tiles == 0:
			p.fixMessage = "Nothing to fix with the selected kinds."
		default:
			p.fixMessage = fmt.Sprintf("Applied %d fixes on %d tiles.", r.Fixes, r.Tiles)
		}
		if p.app.CurrentEditor() == ed {
			p.StartScan()
		}
	})
	if err != nil {
		p.fixing, p.fixMessage = false, "Fixes were not applied: "+err.Error()
	}
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
