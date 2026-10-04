package editor

import (
	"context"
	"strings"
	"testing"

	"sdmm/internal/aphelion/editing"
	"sdmm/internal/app/render"
	"sdmm/internal/util"
)

func TestMoveSourceDefaultsRespectHiddenFamiliesAndOverlap(t *testing.T) {
	for _, hiddenArea := range []bool{false, true} {
		t.Run(map[bool]string{false: "visible", true: "hidden_area"}[hiddenArea], func(t *testing.T) {
			e := selectionEditor(t)
			selection := editing.RectangleSelection(util.Bounds{X1: 1, Y1: 1, X2: 1, Y2: 1}, 1)
			visible := func(path string) bool {
				return !hiddenArea || !strings.HasPrefix(path, "/area") || path == "/area/default"
			}
			payload, err := editing.CompileMovePayload(context.Background(), selection, visible, e.authorityTile)
			if err != nil {
				t.Fatal(err)
			}
			pose, err := editing.NewSelectionMove(selection)
			if err != nil {
				t.Fatal(err)
			}
			session := &selectionMoveSession{pose: pose, selection: selection, visible: visible, payload: payload}
			session.defaults.Area.Path = "/area/default"
			session.defaults.Turf.Path = "/turf/default"
			e.prepareSelectionMovePresentation(session)
			build := session.presentationBuild
			wantDefaults := 2
			if hiddenArea {
				wantDefaults = 1
			}
			if got := build.instanceCount(payload.TileCount()); got != wantDefaults {
				t.Fatalf("source default count=%d want=%d", got, wantDefaults)
			}
			appearance := render.Appearance{Coord: util.Point{}, Path: "/turf/default", WorldSpace: true}
			if build.presentation.Visible(appearance) {
				t.Fatal("default covers unmoved payload")
			}
			if _, _, err := pose.Update(util.Point{X: 1}, 2, 1, 1); err != nil {
				t.Fatal(err)
			}
			if !build.presentation.Visible(appearance) {
				t.Fatal("vacated source default is hidden")
			}
			appearance.Coord.X = 1
			if build.presentation.Visible(appearance) {
				t.Fatal("source default follows destination")
			}
		})
	}
}

func TestPresentationBuildUsesBudgetAndResumesAtExactSprite(t *testing.T) {
	counts := []int{0, 700, 3}
	prepared := 0
	build := &presentationBuild{
		presentation: &render.Presentation{}, tileCount: len(counts),
		instanceCount: func(tile int) int { return counts[tile] },
		appearance: func(tile, instance int) render.Appearance {
			if tile == 1 && instance != prepared || tile == 2 && instance != prepared-700 {
				t.Fatalf("lost preparation position: tile=%d instance=%d prepared=%d", tile, instance, prepared)
			}
			prepared++
			return render.Appearance{Layer: float32(tile), Coord: util.Point{X: instance, Y: tile}}
		},
	}
	if build.advance(func() bool { return prepared >= 513 }) {
		t.Fatal("unfinished presentation reported ready")
	}
	if prepared != 513 || build.presentation.Ready {
		t.Fatalf("unused preparation budget: prepared=%d ready=%t", prepared, build.presentation.Ready)
	}
	if !build.advance(func() bool { return false }) || !build.presentation.Ready || prepared != 703 {
		t.Fatalf("resumed preparation lost sprites or readiness: prepared=%d ready=%t", prepared, build.presentation.Ready)
	}
}
