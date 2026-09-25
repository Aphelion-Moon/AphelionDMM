package mappingui

import (
	"crypto/rand"
	"fmt"
	"math/big"
	"path/filepath"
	"strings"

	"github.com/SpaiR/imgui-go"
	"sdmm/internal/aphelion/mapping"
	"sdmm/internal/app/render"
	"sdmm/internal/dmapi/dmmap"
)

func (p *Panel) browserControls() {
	if !imgui.TreeNode("Reference browser / lazy preview") {
		return
	}
	defer imgui.TreePop()
	if imgui.Button("Scan project map sources") {
		p.scanNext = true
		p.queue(nil)
	}
	imgui.SameLine()
	if imgui.Button("Preview reference thumbnail") {
		p.thumbnailRequested = true
		p.queue(nil)
	}
	imgui.InputText("Source search", &p.browserFilter)
	if len(p.discovered) > 0 {
		imgui.BeginChildV("source-paths", imgui.Vec2{Y: 100}, true, imgui.WindowFlagsNone)
		shown := 0
		for _, path := range p.discovered {
			if !strings.Contains(strings.ToLower(path), strings.ToLower(p.browserFilter)) {
				continue
			}
			if shown >= 100 {
				imgui.Text("Refine search to view additional sources.")
				break
			}
			shown++
			relative, _ := filepath.Rel(p.environment.RootDir, path)
			if imgui.Selectable(relative) {
				p.referencePath = path
				p.thumbnailRequested = true
				p.queue(nil)
			}
		}
		imgui.EndChild()
	}
	if p.current == nil || p.current.thumbnailSource == nil {
		imgui.TextWrapped("Dimensions and sprites are loaded only for the requested source. Reachable configured alternatives remain listed under their roots.")
		return
	}
	source := p.current.thumbnailSource
	imgui.TextWrapped(fmt.Sprintf("%s: %d×%d×%d; %d placed connectors", source.Identity.Path, source.Size.X, source.Size.Y, source.Size.Z, p.current.thumbnailConnectors))
	if p.thumbnail == nil {
		return
	}
	size := imgui.Vec2{X: 192, Y: 144}
	scale := min(size.X/float32(source.Size.X*dmmap.WorldIconSize), size.Y/float32(source.Size.Y*dmmap.WorldIconSize))
	*p.thumbnail.Render().Camera = render.Camera{Scale: scale, Level: 1}
	p.thumbnail.Process(size)
	imgui.ImageV(imgui.TextureID(p.thumbnail.Texture()), size, imgui.Vec2{X: 0, Y: 1}, imgui.Vec2{X: 1, Y: 0}, imgui.Vec4{X: 1, Y: 1, Z: 1, W: 1}, imgui.Vec4{})
	if p.thumbnail.Render().LevelLoading() {
		imgui.Text("Preparing sprite preview…")
	}
}

func (p *Panel) alternativeControls(root mapping.Root) {
	if len(root.Candidates) == 0 {
		return
	}
	choose := func(slot int, compare bool) {
		candidate := root.Candidates[slot]
		if candidate.Error != "" {
			p.status = candidate.Error
			return
		}
		if p.choices == nil {
			p.choices = map[string]mapping.Choice{}
		}
		p.choices[root.ID] = mapping.Choice{Slot: slot}
		p.referencePath = candidate.Path
		p.focusRoot = root.ID
		if compare {
			if host, ok := p.app.(mapHost); ok {
				host.OpenMappingComparison(p)
			}
		}
		anchor := root.Destination
		p.queue(&anchor)
	}
	slot := p.choices[root.ID].Slot
	if imgui.SmallButton("Cycle next") {
		choose((slot+1)%len(root.Candidates), false)
	}
	imgui.SameLine()
	if imgui.SmallButton("Reroll this root") {
		// Local scenario choice only; duplicate configured slots retain weight.
		value, err := rand.Int(rand.Reader, big.NewInt(int64(len(root.Candidates))))
		if err != nil {
			p.status = err.Error()
		} else {
			choose(int(value.Int64()), false)
		}
	}
	imgui.SameLine()
	if imgui.SmallButton("Compare selected in context") {
		choose(max(0, min(slot, len(root.Candidates)-1)), true)
	}
}
