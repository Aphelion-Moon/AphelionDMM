package mappingui

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/SpaiR/imgui-go"

	"sdmm/internal/aphelion/collab/model"
	"sdmm/internal/aphelion/editing"
	"sdmm/internal/aphelion/mapping"
	"sdmm/internal/aphelion/resources"
	"sdmm/internal/util"
)

func authoringLifecycleProposal(t *testing.T) *mapping.AuthoringProposal {
	t.Helper()
	snapshot := model.Snapshot{
		ProtocolVersion: model.ProtocolVersion, SchemaVersion: model.SchemaVersion,
		DocumentID:      "01890f3e-7b5c-7abc-8def-0123456789ab",
		EnvironmentHash: "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
		MaxX:            1, MaxY: 1, MaxZ: 1,
		Tiles: []model.Tile{{Coord: model.Coord{X: 1, Y: 1, Z: 1}, State: model.TileState{Prefabs: []model.PrefabState{{
			StableID: "01890f3e-7b5c-7abc-8def-000000000001", Path: "/turf/floor",
		}}}}},
	}
	proposal, err := mapping.PrepareTemplateExport(context.Background(), filepath.Join(t.TempDir(), "export.dmm"), snapshot,
		editing.RectangleSelection(util.Bounds{X1: 1, Y1: 1, X2: 1, Y2: 1}, 1), nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(proposal.Close)
	return proposal
}

func TestAuthoringCloseReleasesCompletedAndLateStaging(t *testing.T) {
	for _, project := range []bool{false, true} {
		for _, late := range []bool{false, true} {
			name := "map"
			if project {
				name = "project"
			}
			if late {
				name += "/late"
			} else {
				name += "/buffered"
			}
			t.Run(name, func(t *testing.T) {
				before := resources.DefaultBudget().Used()
				proposal := authoringLifecycleProposal(t)
				if resources.DefaultBudget().Used() <= before {
					t.Fatal("fixture did not reserve memory")
				}
				hub := NewHub(&fixtureApp{})
				panel := hub.session("source.dmm")
				results := make(chan authoringResult, 1)
				panel.author.results = results
				ctx, cancel := context.WithCancel(context.Background())
				defer cancel()
				panel.author.cancel = cancel
				out := authoringResult{generation: panel.author.generation, proposal: proposal}
				if !late {
					results <- out
				}
				if project {
					hub.Invalidate()
				} else {
					hub.CloseSource("source.dmm")
				}
				if ctx.Err() == nil {
					t.Fatal("closing source did not cancel staging")
				}
				if late {
					sent := make(chan struct{})
					go func() { results <- out; close(sent) }()
					<-sent
				}
				hub.Advance()
				if got := resources.DefaultBudget().Used(); got != before {
					t.Fatalf("closed authoring retained %d bytes; baseline %d", got, before)
				}
			})
		}
	}
}

type authoringInterruptedWrite struct {
	context.Context
	checks int
}

func (c *authoringInterruptedWrite) Err() error {
	c.checks++
	if c.checks >= 2 {
		return context.Canceled
	}
	return nil
}

func finishDetachedAuthoring(t *testing.T, hub *Hub, author *authoringUI) {
	t.Helper()
	select {
	case result := <-author.results:
		author.results <- result
		hub.Advance()
	case <-time.After(3 * time.Second):
		t.Fatal("authoring worker did not finish")
	}
}

func TestAuthoringCloseRetainsPartialWriteRecovery(t *testing.T) {
	for _, test := range []struct {
		name                              string
		installed, late, project, discard bool
	}{
		{name: "installed/map/discard", installed: true, discard: true},
		{name: "buffered/project/restage", project: true},
		{name: "late/map/restage", late: true},
		{name: "late/project/discard", late: true, project: true, discard: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			before := resources.DefaultBudget().Used()
			proposal := authoringLifecycleProposal(t)
			root := filepath.Dir(proposal.SourcePath)
			config := filepath.Join(root, "overlays.toml")
			if err := os.WriteFile(config, []byte("# original\n"), 0600); err != nil {
				t.Fatal(err)
			}
			if err := proposal.AddOverlayRecipe(root, "overlays.toml", "test", "base.dmm", "Station", util.Point{X: 1, Y: 1, Z: 1}); err != nil {
				t.Fatal(err)
			}
			result := proposal.Apply(&authoringInterruptedWrite{Context: context.Background()})
			if !result.SourceWritten || result.ConfigWritten || !errors.Is(result.Err, context.Canceled) {
				t.Fatalf("not a partial write: %+v", result)
			}
			if err := os.WriteFile(config, []byte("# original\n# external addition\n"), 0600); err != nil {
				t.Fatal(err)
			}
			hub := NewHub(&fixtureApp{})
			panel := hub.session("source.dmm")
			results := make(chan authoringResult, 1)
			out := authoringResult{proposal: proposal, applied: &result}
			if test.installed {
				panel.author.proposal = proposal
			} else {
				panel.author.results = results
				if !test.late {
					results <- out
				}
			}
			if test.project {
				hub.Invalidate()
			} else {
				hub.CloseSource("source.dmm")
			}
			if test.late {
				results <- out
			}
			hub.Advance()
			if len(hub.detachedAuthoring) != 1 || hub.detachedAuthoring[0].proposal != proposal {
				t.Fatal("closed source lost partial-write recovery")
			}
			author := hub.detachedAuthoring[0]
			if panel.author.results != nil || panel.author.proposal != nil {
				t.Fatal("closed panel still owns authoring")
			}
			hub.Invalidate()
			hub.Advance()
			if len(hub.detachedAuthoring) != 1 || hub.detachedAuthoring[0] != author {
				t.Fatal("next project invalidation lost recovery")
			}
			if test.discard {
				author.discardRecovery()
				hub.Advance()
			} else {
				author.restageProposal()
				finishDetachedAuthoring(t, hub, author)
				if author.proposal != proposal || !strings.Contains(proposal.ConfigurationPreview(), "# external addition") {
					t.Fatal("restage lost original recovery or external change")
				}
				author.writeProposal()
				finishDetachedAuthoring(t, hub, author)
			}
			if len(hub.detachedAuthoring) != 0 || resources.DefaultBudget().Used() != before {
				t.Fatal("resolved recovery retained its reservation or holder")
			}
			if _, err := os.Stat(proposal.SourcePath); err != nil {
				t.Fatalf("recovery removed the created source: %v", err)
			}
			content, err := os.ReadFile(config)
			if err != nil {
				t.Fatal(err)
			}
			if !strings.Contains(string(content), "# external addition") || strings.Contains(string(content), "[templates.") == test.discard {
				t.Fatalf("wrong recovered configuration: %s", content)
			}
		})
	}
}

func TestAuthoringRecoveryRendersWithoutActiveMap(t *testing.T) {
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	ctx := imgui.CreateContext(nil)
	defer ctx.Destroy()
	io := imgui.CurrentIO()
	io.SetIniFilename("")
	io.SetDisplaySize(imgui.Vec2{X: 800, Y: 600})
	io.SetDeltaTime(1.0 / 60)
	io.Fonts().TextureDataRGBA32()
	proposal := authoringLifecycleProposal(t)
	if result := proposal.Apply(context.Background()); result.Err != nil {
		t.Fatal(result.Err)
	}
	hub := NewHub(&fixtureApp{})
	hub.session("source.dmm").author.proposal = proposal
	hub.CloseSource("source.dmm")
	imgui.NewFrame()
	imgui.Begin("Composition")
	hub.Process(0)
	imgui.End()
	imgui.Render()
	if len(hub.detachedAuthoring) != 1 {
		t.Fatal("rendering discarded recovery without an active map")
	}
}
