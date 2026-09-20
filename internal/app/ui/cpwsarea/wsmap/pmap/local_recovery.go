// APHELION EDIT ADDITION START - LOCAL EDIT RECOVERY
package pmap

import (
	"errors"
	"fmt"

	"github.com/SpaiR/imgui-go"
	native "github.com/sqweek/dialog"
	"sdmm/internal/aphelion/editing/recovery"
	"sdmm/internal/app/ui/cpwsarea/wsmap/tools"
	"sdmm/internal/app/ui/dialog"
	w "sdmm/internal/imguiext/widget"
	"sdmm/internal/util"
)

func (p *PaneMap) showLocalRecoveryControls() {
	if !p.editor.HasLocalRecovery() {
		return
	}
	w.TextWrapped("An unsubmitted edit or capture error is blocking Save.").Build()
	w.Button("Inspect retained edit", p.OpenLocalRecovery).Build()
}

// OpenLocalRecovery captures exactly what the user can export or explicitly
// discard. Opening or closing this modal never finishes a gesture or submits it.
func (p *PaneMap) OpenLocalRecovery() {
	draft, err := p.editor.InspectLocalRecovery()
	if err != nil {
		util.ShowErrorDialog("Unable to inspect local edit: " + err.Error())
		return
	}
	dialog.Open(&localRecoveryDialog{
		record: draft.Record,
		exportPath: func() (string, error) {
			return native.File().Title("Export Local Edit Recovery").Filter("JSON recovery reference", "json").SetStartFile("local-edit-recovery.json").Save()
		},
		discard: func() error {
			if err := p.editor.DiscardLocalRecovery(draft); err != nil {
				return err
			}
			// Clear stale tool instance pointers without calling legacy onStop,
			// which could otherwise submit an edit after its explicit discard.
			tools.ReleaseEditor(p.editor)
			if activePane == p {
				p.prepareTools()
			}
			return nil
		},
	})
}

type localRecoveryDialog struct {
	record     *recovery.Record
	discard    func() error
	exportPath func() (string, error)
	confirm    bool
	status     string
}

func (*localRecoveryDialog) Name() string         { return "Local edit recovery" }
func (*localRecoveryDialog) HasCloseButton() bool { return true }

func (d *localRecoveryDialog) Process() {
	size := imgui.MainViewport().WorkSize()
	width := min(float32(640), max(float32(280), size.X-48))
	height := min(float32(320), max(float32(80), size.Y-260))
	imgui.PushTextWrapPosV(imgui.CursorPosX() + width)
	imgui.Text("Inspect retained contents before discarding. Closing this view keeps the edit.")
	imgui.Text("Export is a recovery reference, not a saved map or an edit ready to submit.")
	imgui.PopTextWrapPos()
	if imgui.BeginChildV("local-recovery-values", imgui.Vec2{X: width, Y: height}, true, imgui.WindowFlagsHorizontalScrollbar) {
		imgui.Text(d.record.Preview())
	}
	imgui.EndChild()
	imgui.PushTextWrapPosV(imgui.CursorPosX() + width)
	defer imgui.PopTextWrapPos()
	if d.status != "" {
		imgui.Text(d.status)
	}
	w.Button("Keep edit", imgui.CloseCurrentPopup).Build()
	if width >= 420 {
		imgui.SameLine()
	}
	w.Button("Export JSON", d.export).Build()
	if width >= 420 {
		imgui.SameLine()
	}
	w.Button("Discard local edit...", func() { d.confirm = true }).Build()
	if d.confirm {
		imgui.Text("Discard the inspected display changes and restore committed map state?")
		imgui.Text("This cannot be undone. Export first to keep a reference. Accepted edits keep their history.")
		w.Button("Keep retained edit", func() { d.confirm = false }).Build()
		imgui.SameLine()
		w.Button("Confirm discard", func() {
			if err := d.discard(); err != nil {
				d.status = err.Error()
				d.confirm = false
				return
			}
			imgui.CloseCurrentPopup()
		}).Build()
	}
}

func (d *localRecoveryDialog) export() {
	path, err := d.exportPath()
	if errors.Is(err, native.ErrCancelled) {
		return
	}
	if err == nil {
		err = d.record.Export(path)
	}
	if err != nil {
		d.status = fmt.Sprintf("Export failed: %s", err)
		return
	}
	d.status = "Exported the inspected record. The local edit is still retained."
}

// APHELION EDIT ADDITION END
