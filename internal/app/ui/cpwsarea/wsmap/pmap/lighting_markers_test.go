package pmap

import (
	"math"
	"strings"
	"testing"

	"sdmm/internal/aphelion/lighting"
	"sdmm/internal/aphelion/lighting/maplight"
)

func near(a, b float64) bool { return math.Abs(a-b) < 1e-6 }

func TestLightingMarkerForOmnidirectionalSource(t *testing.T) {
	m := lightingMarkerFor(lighting.Source{X: 2, Y: 1, Range: 5, Angle: 360, System: lighting.SystemComplex}, 32)
	if !near(m.CX, 80) || !near(m.CY, 48) || !near(m.Ring, 160) || m.Cone {
		t.Fatalf("marker %+v", m)
	}
}

func TestLightingMarkerForConeUsesShiftAndNorthZeroClockwiseEdges(t *testing.T) {
	s := lighting.Source{X: 6, Y: 1, ShiftX: 0.5, Range: 8, Dir: lighting.DirWest, Angle: 170, System: lighting.SystemComplex}
	m := lightingMarkerFor(s, 32)
	if !near(m.CX, 224) || !near(m.CY, 48) || !near(m.Ring, 256) || !m.Cone {
		t.Fatalf("marker %+v", m)
	}
	// West is 270 degrees; the edges are 85 degrees either side.
	for i, deg := range []float64{185, 355} {
		rad := deg * math.Pi / 180
		want := [2]float64{224 + math.Sin(rad)*256, 48 + math.Cos(rad)*256}
		got := m.EdgeA
		if i == 1 {
			got = m.EdgeB
		}
		if !near(got[0], want[0]) || !near(got[1], want[1]) {
			t.Errorf("edge %d got %v want %v", i, got, want)
		}
	}
}

func TestLightingMarkerForOverlaySystemsMatchesApproximation(t *testing.T) {
	round := lightingMarkerFor(lighting.Source{X: 0, Y: 0, Range: 3, Angle: 90, Dir: lighting.DirNorth, System: lighting.SystemOverlayDirectional}, 32)
	if round.Cone {
		t.Fatal("overlay (non-beam) lights are modelled as circles")
	}
	beam := lightingMarkerFor(lighting.Source{X: 0, Y: 0, Range: 3, Angle: 360, Dir: lighting.DirNorth, System: lighting.SystemOverlayBeam}, 32)
	if !beam.Cone {
		t.Fatal("overlay beams are modelled as 45 degree cones")
	}
}

func TestLightingStatusText(t *testing.T) {
	got := lightingStatusText(maplight.Report{Sources: 12, Starlight: 30, Skipped: 3}, false)
	if got != "Lighting (approximate): 12 sources, 3 skipped" {
		t.Fatalf("got %q", got)
	}
	if got := lightingStatusText(maplight.Report{Sources: 1}, true); !strings.HasSuffix(got, "updating...") {
		t.Fatalf("busy suffix missing: %q", got)
	}
	if got := lightingStatusText(maplight.Report{Sources: 5000, Capped: true}, false); !strings.Contains(got, "capped") {
		t.Fatalf("cap warning missing: %q", got)
	}
}
