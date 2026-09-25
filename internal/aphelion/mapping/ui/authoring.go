package mappingui

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"

	"github.com/SpaiR/imgui-go"
	"sdmm/internal/aphelion/mapping"
	"sdmm/internal/util"
)

type authoringProvider interface {
	MappingSourceNoop(string, string) error
	PrepareMappingExport(string, *util.Point) (func(context.Context) (*mapping.AuthoringProposal, error), func() bool, error)
}
type authoringResult struct {
	current    func() bool
	generation uint64
	proposal   *mapping.AuthoringProposal
	applied    *mapping.AuthoringResult
	err        error
}
type authoringUI struct {
	target, config, key, name, requiredMap, trait, status string
	recipe                                                int32
	connector                                             bool
	anchor                                                [3]int32
	destination                                           [3]int32
	proposal                                              *mapping.AuthoringProposal
	current                                               func() bool
	results                                               chan authoringResult
	cancel                                                context.CancelFunc
	generation                                            uint64
}

func (a *authoringUI) invalidate() {
	a.generation++
	if a.cancel != nil {
		a.cancel()
	}
	// A completed source write survives window/project changes. Discarding its
	// remaining configuration recovery is an explicit user action.
	if a.proposal == nil || !a.proposal.SourceWritten() {
		a.proposal.Close()
		a.proposal = nil
	}
	a.current = nil
}
func (a *authoringUI) advance() {
	if a.results == nil {
		return
	}
	select {
	case out := <-a.results:
		a.results = nil
		a.cancel = nil
		if out.applied != nil {
			a.current = nil
			a.proposal.Close()
			a.proposal = out.proposal
			r := out.applied
			a.status = fmt.Sprintf("Source written: %t; configuration written: %t.", r.SourceWritten, r.ConfigWritten)
			if r.Err != nil {
				a.status += " " + r.Err.Error()
				if r.SourceWritten {
					a.status += " Partial completion: the separate source exists; inspect it and re-stage only the missing configuration change."
				}
			}
		} else if out.generation != a.generation {
			if out.proposal != nil && out.proposal.SourceWritten() {
				a.proposal.Close()
				a.proposal = out.proposal
				a.status = "Partial-write recovery retained across project/window change; review its original file paths."
			} else {
				out.proposal.Close()
			}
		} else if out.err != nil {
			a.status = out.err.Error()
			if out.proposal != nil {
				a.proposal.Close()
				a.proposal = out.proposal
			}
		} else {
			a.proposal.Close()
			a.proposal = out.proposal
			if out.current != nil {
				a.current = out.current
			}
			a.status = "Prepared only. Review the exact paths, placement and configuration change before writing."
		}
	default:
	}
}

func (p *Panel) authoringControls() {
	provider, ok := p.app.(authoringProvider)
	if !ok || !imgui.TreeNode("Source authoring / separate template export") {
		return
	}
	defer imgui.TreePop()
	a := &p.author
	imgui.BeginChildV("source-authoring", imgui.Vec2{Y: 240}, true, imgui.WindowFlagsNone)
	defer imgui.EndChild()
	active := p.app.ActiveMappingPath()
	imgui.TextWrapped("Active editable source: " + active)
	for _, channel := range []string{"turf", "area"} {
		if imgui.Button("Set selected "+channel+" to noop") && a.results == nil {
			if err := provider.MappingSourceNoop(active, channel); err != nil {
				a.status = err.Error()
			} else {
				a.status = "Source-only edit submitted through normal document history."
			}
		}
	}
	if p.current != nil && p.current.projection != nil {
		uses := 0
		for _, placed := range p.current.projection.Placements {
			if strings.EqualFold(filepath.Clean(placed.Source.Path), filepath.Clean(active)) {
				uses++
			}
		}
		imgui.Text(fmt.Sprintf("Impact: %d selected placements use this source; all refresh from accepted edits.", uses))
	}
	imgui.InputText("Separate output DMM path", &a.target)
	if imgui.BeginCombo("Recipe", []string{"Export selection only", "New modular variant", "New coordinate overlay"}[a.recipe]) {
		for i, label := range []string{"Export selection only", "New modular variant", "New coordinate overlay"} {
			if imgui.Selectable(label) {
				a.recipe = int32(i)
			}
		}
		imgui.EndCombo()
	}
	imgui.Checkbox("Add one connector at selected source coordinate", &a.connector)
	if a.connector {
		imgui.InputInt("Connector source X", &a.anchor[0])
		imgui.InputInt("Connector source Y", &a.anchor[1])
		imgui.InputInt("Connector source Z", &a.anchor[2])
	}
	if a.recipe > 0 {
		imgui.InputText("Project-relative TOML", &a.config)
	}
	if a.recipe == 1 {
		imgui.InputText("Existing room key", &a.key)
	}
	if a.recipe == 2 {
		imgui.InputText("New overlay identifier", &a.name)
		imgui.InputText("Required map filename", &a.requiredMap)
		imgui.InputText("Trait name", &a.trait)
		imgui.InputInt("Destination X", &a.destination[0])
		imgui.InputInt("Destination Y", &a.destination[1])
		imgui.InputInt("Trait-relative Z", &a.destination[2])
	}
	canStage := a.results == nil && (a.proposal == nil || !a.proposal.SourceWritten())
	if !canStage && a.proposal != nil && a.proposal.SourceWritten() {
		imgui.TextWrapped("Complete or explicitly discard the pending configuration recovery before staging another export.")
		if imgui.Button("Discard configuration recovery; keep created source") {
			a.proposal.Close()
			a.proposal = nil
		}
	}
	if imgui.Button("Stage proposal") && canStage {
		path := a.target
		if !filepath.IsAbs(path) {
			path = filepath.Join(p.environment.RootDir, path)
		}
		var anchor *util.Point
		if a.connector {
			anchor = &util.Point{X: int(a.anchor[0]), Y: int(a.anchor[1]), Z: int(a.anchor[2])}
		}
		prepare, current, err := provider.PrepareMappingExport(path, anchor)
		if a.target == "" {
			err = fmt.Errorf("choose a separate output path")
		}
		if err != nil {
			a.status = err.Error()
		} else {
			a.generation++
			generation := a.generation
			ctx, cancel := context.WithCancel(context.Background())
			a.cancel = cancel
			results := make(chan authoringResult, 1)
			a.results = results
			root, config, key, name, requiredMap, trait, recipe, destination := p.environment.RootDir, a.config, a.key, a.name, a.requiredMap, a.trait, a.recipe, a.destination
			environment := p.environment
			a.status = "Preparing a validated separate source and minimal configuration change…"
			go func() {
				proposal, err := prepare(ctx)
				if err == nil && recipe == 1 {
					err = proposal.AddModuleRecipe(root, config, key, environment)
				}
				if err == nil && recipe == 2 {
					err = proposal.AddOverlayRecipe(root, config, name, requiredMap, trait, util.Point{X: int(destination[0]), Y: int(destination[1]), Z: int(destination[2])})
				}
				if err != nil {
					proposal.Close()
					proposal = nil
				}
				results <- authoringResult{generation: generation, proposal: proposal, err: err, current: current}
			}()
		}
	}
	if a.proposal != nil {
		proposal := a.proposal
		imgui.TextWrapped(fmt.Sprintf("%s\n%d×%d; source origin %d,%d,%d\n%s\n%s", proposal.SourcePath, proposal.Width, proposal.Height, proposal.Origin.X, proposal.Origin.Y, proposal.Origin.Z, proposal.ConfigPath, proposal.Summary))
		if proposal.Connector != nil {
			imgui.Text(fmt.Sprintf("Proposed connector local coordinate: %d,%d,1", proposal.Connector.X, proposal.Connector.Y))
		}
		if proposal.ConfigPath != "" && imgui.TreeNode("Configuration insertion") {
			imgui.TextWrapped(proposal.ChangePreview())
			imgui.TreePop()
		}
		if proposal.SourceWritten() && imgui.Button("Restage remaining configuration for review") && a.results == nil {
			a.proposal = nil
			results := make(chan authoringResult, 1)
			a.results = results
			generation := a.generation
			go func() {
				err := proposal.RestageConfiguration()
				if err != nil {
					results <- authoringResult{generation: generation, err: err, proposal: proposal}
					return
				}
				results <- authoringResult{generation: generation, proposal: proposal}
			}()
		}
		if imgui.Button("Write reviewed proposal") && a.results == nil {
			if !proposal.SourceWritten() && (a.current == nil || !a.current()) {
				a.status = "Source changed or has a pending edit; stage a new proposal."
			} else {
				a.proposal = nil
				ctx, cancel := context.WithCancel(context.Background())
				a.cancel = cancel
				results := make(chan authoringResult, 1)
				a.results = results
				go func() {
					result := proposal.Apply(ctx)
					var recovery *mapping.AuthoringProposal
					if result.Err != nil && result.SourceWritten && !result.ConfigWritten {
						recovery = proposal
					} else {
						proposal.Close()
					}
					results <- authoringResult{applied: &result, proposal: recovery}
				}()
			}
		}
	}
	imgui.TextWrapped(a.status)
}
