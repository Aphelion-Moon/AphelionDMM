// APHELION EDIT ADDITION START - DETERMINISTIC RANDOM FILL
package pmap

import (
	"crypto/rand"
	"encoding/binary"
	"fmt"
	"github.com/SpaiR/imgui-go"
	"sdmm/internal/aphelion/collab/model"
	"sdmm/internal/aphelion/editing"
	"sdmm/internal/app/ui/cpwsarea/wsmap/tools"
	"sdmm/internal/util"
	"strconv"
)

func (p *PaneMap) showRandomFillControls() {
	if p.app == nil || p.editor == nil {
		return
	}
	s := p.app.Prefs().Mapper
	if s == nil || (!tools.IsSelected(tools.TNFill) && !p.editor.RandomFillPreview()) {
		return
	}
	preview := p.editor.RandomFillPreview()
	if preview {
		imgui.Text("Palette settings are fixed for this preview. Cancel it to change them.")
	}
	imgui.BeginDisabledV(preview)
	imgui.Checkbox("Random palette Fill", &s.RandomFill)
	if !s.RandomFill {
		imgui.EndDisabled()
		return
	}
	if s.Palette.Version == 0 {
		s.Palette.Version = 1
	}
	imgui.InputText("Palette name", &s.Palette.Name)
	if imgui.Button("Add selected prefab") {
		if prefab, ok := p.editor.SelectedPrefab(); ok {
			id, err := model.NewStableID()
			if err != nil {
				util.ShowErrorDialog(err.Error())
				imgui.EndDisabled()
				return
			}
			entry := editing.PaletteEntry{ID: string(id), Weight: 1, Prefab: model.PrefabState{Path: prefab.Path(), Vars: map[string]string{}}}
			for _, key := range prefab.Vars().Iterate() {
				if value, ok := prefab.Vars().ExplicitValue(key); ok {
					entry.Prefab.Vars[key] = value
				}
			}
			s.Palette.Entries = append(s.Palette.Entries, entry)
		}
	}
	total := float64(0)
	for _, entry := range s.Palette.Entries {
		total += entry.Weight
	}
	for i := 0; i < len(s.Palette.Entries); i++ {
		entry := &s.Palette.Entries[i]
		imgui.PushID(entry.ID)
		imgui.Text(entry.Prefab.Path)
		weight := float32(entry.Weight)
		imgui.SetNextItemWidth(100)
		if imgui.DragFloat("Weight", &weight) {
			entry.Weight = float64(weight)
		}
		if total > 0 {
			imgui.SameLine()
			imgui.Text(fmt.Sprintf("%.1f%%", entry.Weight/total*100))
		}
		if imgui.Button("Remove") {
			s.Palette.Entries = append(s.Palette.Entries[:i], s.Palette.Entries[i+1:]...)
			imgui.PopID()
			break
		}
		if i > 0 {
			imgui.SameLine()
			if imgui.Button("Move up") {
				s.Palette.Entries[i-1], s.Palette.Entries[i] = s.Palette.Entries[i], s.Palette.Entries[i-1]
			}
		}
		imgui.PopID()
	}
	if _, err := s.Palette.Compile(); err != nil {
		imgui.Text(err.Error())
	}
	density := float32(s.Density)
	if imgui.SliderFloat("Density", &density, 0, 1) {
		s.Density = float64(density)
	}
	imgui.InputText("Seed", &s.Seed)
	imgui.Checkbox("Lock seed", &s.SeedLock)
	imgui.EndDisabled()
	if imgui.Button("Reroll") {
		var bytes [8]byte
		if _, err := rand.Read(bytes[:]); err == nil {
			seed := binary.LittleEndian.Uint64(bytes[:])
			if err = p.editor.RerollRandomFill(seed); err != nil {
				util.ShowErrorDialog(err.Error())
			} else {
				s.Seed = strconv.FormatUint(seed, 10)
			}
		}
	}
	imgui.SameLine()
	imgui.BeginDisabledV(preview)
	if imgui.Button("Save palette") {
		if _, err := s.Palette.Compile(); err != nil {
			util.ShowErrorDialog(err.Error())
		} else {
			replaced := false
			for i := range s.SavedPalettes {
				if s.SavedPalettes[i].Name == s.Palette.Name {
					s.SavedPalettes[i] = s.Palette.Clone()
					replaced = true
					break
				}
			}
			if !replaced {
				s.SavedPalettes = append(s.SavedPalettes, s.Palette.Clone())
			}
		}
	}
	for i, saved := range s.SavedPalettes {
		if imgui.Button(fmt.Sprintf("Load %s##palette-%d", saved.Name, i)) {
			s.Palette = saved.Clone()
		}
	}
	imgui.EndDisabled()
	imgui.Text("Drag a shape, review the prepared choices, then click to confirm. Skipped cells stay unchanged.")
}

// APHELION EDIT ADDITION END
