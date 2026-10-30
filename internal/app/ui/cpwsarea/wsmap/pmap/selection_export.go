// APHELION EDIT ADDITION START - EXPORT SELECTION
package pmap

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"

	"sdmm/internal/aphelion/collab/model"
	"sdmm/internal/aphelion/diskversion"
	"sdmm/internal/aphelion/mapexport"
	"sdmm/internal/aphelion/resources"
	"sdmm/internal/app/ui/cpwsarea/wsmap/pmap/editor"
	"sdmm/internal/app/ui/dialog"
	"sdmm/internal/dmapi/dmenv"
	"sdmm/internal/dmapi/dmmap"
	"sdmm/internal/dmapi/dmmap/dmmdata/dmmprefab"
	w "sdmm/internal/imguiext/widget"
	"sdmm/internal/util"

	"github.com/SpaiR/imgui-go"
	"github.com/rs/zerolog/log"
	native "github.com/sqweek/dialog"
)

// selectionExportBusy serializes exports; it is read and written on the UI thread.
var selectionExportBusy bool

func (p *PaneMap) showExportSelectionButton() {
	empty := p.editor == nil || p.editor.WorkingSelection().Get(p.activeLevel).Len() == 0
	w.Disabled(empty || selectionExportBusy, w.Button("Export selection to new map...", p.ExportSelection).
		Tooltip("Write the selected tiles to a new map sized to the selection (this level only)")).Build()
}

// ExportSelection asks for a destination and writes the active level's
// selection there as a new map. The open map is never written.
func (p *PaneMap) ExportSelection() {
	if p.editor == nil || selectionExportBusy {
		return
	}
	region, err := mapexport.SelectionRegion(p.editor.WorkingSelection().Get(p.activeLevel))
	if err != nil {
		util.ShowErrorDialog("Unable to export the selection: " + err.Error())
		return
	}
	path, err := native.File().Title("Export Selection to New Map").Filter(".dmm").SetStartDir(filepath.Dir(p.dmm.Path.Absolute)).Save()
	if err != nil || path == "" {
		if !errors.Is(err, native.ErrCancelled) {
			log.Print("Export selection canceled or unavailable:", err)
		}
		return
	}
	if filepath.Ext(path) == "" {
		path += ".dmm"
	}
	absolutePath, err := filepath.Abs(path)
	if err != nil {
		util.ShowErrorDialog("Unable to export the selection: " + err.Error())
		return
	}
	if mapexport.SamePath(absolutePath, p.dmm.Path.Absolute) || mapexport.SamePath(absolutePath, p.dmm.Backup) {
		util.ShowErrorDialog("Unable to export the selection: choose a new file; the export never overwrites the currently open map.")
		return
	}
	state, err := diskversion.Capture(absolutePath)
	if err != nil {
		util.ShowErrorDialog(fmt.Sprintf("Unable to export the selection: inspect %q: %v", absolutePath, err))
		return
	}
	if !state.Exists() {
		p.startSelectionExport(region, absolutePath, state)
		return
	}
	dialog.Open(dialog.TypeCustom{
		Title: "Replace existing map?##export-" + absolutePath,
		Layout: w.Layout{
			w.Text("This file already exists. Replace it with the exported selection?"),
			w.Text(absolutePath),
			w.Button("Replace", func() {
				imgui.CloseCurrentPopup()
				p.app.RunLater(func() { p.startSelectionExport(region, absolutePath, state) })
			}),
			w.SameLine(),
			w.Button("Cancel", imgui.CloseCurrentPopup),
		},
		CloseButton: false,
	})
}

func (p *PaneMap) startSelectionExport(region mapexport.Region, path string, expected diskversion.State) {
	if selectionExportBusy || p.editor == nil {
		return
	}
	environment := p.app.LoadedEnvironment()
	if environment == nil {
		util.ShowErrorDialog("Unable to export the selection: the environment is not loaded.")
		return
	}
	filler, err := exportFiller()
	if err != nil {
		util.ShowErrorDialog("Unable to export the selection: " + err.Error())
		return
	}
	// Capturing pins one committed revision; an in-progress edit is refused.
	captured, _, err := p.editor.CaptureSaveSnapshot(context.Background())
	if err != nil {
		util.ShowErrorDialog("Unable to export the selection: " + err.Error())
		return
	}
	target := mapexport.Target{
		Path:              path,
		SourcePath:        p.dmm.Path.Absolute,
		SourceBackup:      p.dmm.Backup,
		SanitizeVariables: p.app.Prefs().Editor.SanitizeVariables,
		Expected:          expected,
	}
	selectionExportBusy = true
	go func() {
		err := runSelectionExport(captured, region, filler, environment, target, resources.DefaultBudget())
		p.app.RunLater(func() {
			selectionExportBusy = false
			p.finishSelectionExport(region, path, err)
		})
	}()
}

// runSelectionExport runs off the UI thread on an owned capture.
func runSelectionExport(captured editor.SaveSnapshotCapture, region mapexport.Region, filler []model.PrefabState,
	environment *dmenv.Dme, target mapexport.Target, budget *resources.Budget) error {
	if captured == nil {
		return fmt.Errorf("map snapshot is unavailable")
	}
	estimate := captured.EstimatedBytes()
	if estimate > ^uint64(0)/2 {
		estimate = ^uint64(0) / 2
	}
	reservation, err := budget.Reserve(estimate * 2) // full snapshot plus the cropped copy and its staging
	if err != nil {
		return fmt.Errorf("admit export memory: %w", err)
	}
	defer reservation.Release()
	cropped, err := mapexport.Crop(captured.Snapshot(), region, filler)
	if err != nil {
		return err
	}
	_, err = mapexport.Write(environment, cropped, target)
	return err
}

func (p *PaneMap) finishSelectionExport(region mapexport.Region, path string, err error) {
	if err != nil {
		log.Error().Err(err).Msg("unable to export selection")
		util.ShowErrorDialog("Unable to export the selection: " + err.Error())
		return
	}
	log.Printf("exported %dx%d selection to [%s]", region.Width(), region.Height(), path)
	layout := w.Layout{
		w.Text(fmt.Sprintf("Exported a %dx%d selection to:", region.Width(), region.Height())),
		w.Text(path),
	}
	if opener, ok := p.app.(interface{ DoLoadResource(string) }); ok {
		layout = append(layout, w.Button("Open exported map", func() {
			imgui.CloseCurrentPopup()
			p.app.RunLater(func() { opener.DoLoadResource(path) })
		}), w.SameLine())
	}
	layout = append(layout, w.Button("Close", imgui.CloseCurrentPopup))
	dialog.Open(dialog.TypeCustom{Title: "Selection exported##" + path, Layout: layout, CloseButton: false})
}

// exportFiller is the content of unselected cells inside a sparse selection's
// bounds: the environment's base area and turf, as on a newly created map.
func exportFiller() ([]model.PrefabState, error) {
	var filler []model.PrefabState
	for _, prefab := range []*dmmprefab.Prefab{dmmap.BaseArea, dmmap.BaseTurf} {
		if prefab == nil {
			continue
		}
		state := model.PrefabState{Path: prefab.Path(), Vars: map[string]string{}}
		if prefab.Vars() != nil {
			for _, name := range prefab.Vars().Iterate() {
				value, ok := prefab.Vars().ExplicitValue(name)
				if !ok {
					return nil, fmt.Errorf("base prefab variable %q is unavailable", name)
				}
				state.Vars[name] = value
			}
		}
		filler = append(filler, state)
	}
	return filler, nil
}

// APHELION EDIT ADDITION END
