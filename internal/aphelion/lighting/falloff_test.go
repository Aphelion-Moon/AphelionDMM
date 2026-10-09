package lighting

import (
	"math"
	"testing"
)

func near(t *testing.T, name string, got, want, eps float64) {
	t.Helper()
	if math.Abs(got-want) > eps {
		t.Fatalf("%s: got %.9f want %.9f", name, got, want)
	}
}

// Hand computed from lighting_source.dm:276:
// 1 - clamp01(sqrt(x^2 + y^2 + z^2 + height) / max(1, range)).
func TestFalloffOmniMatchesTG(t *testing.T) {
	// range 5, height 1. NOTE: height is added inside the root unsquared.
	near(t, "(0.5,0.5)", Falloff(0.5, 0.5, 0, 1, 5, DirNorth, 360), 1-math.Sqrt(1.5)/5, 1e-12)
	near(t, "(0.5,0.5) literal", Falloff(0.5, 0.5, 0, 1, 5, DirNorth, 360), 0.755051025721682, 1e-12)
	near(t, "(2.5,0.5)", Falloff(2.5, 0.5, 0, 1, 5, DirNorth, 360), 1-math.Sqrt(7.5)/5, 1e-12)
	near(t, "(2.5,0.5) literal", Falloff(2.5, 0.5, 0, 1, 5, DirNorth, 360), 0.452277442494834, 1e-12)
	near(t, "(4.5,0.5)", Falloff(4.5, 0.5, 0, 1, 5, DirNorth, 360), 1-math.Sqrt(21.5)/5, 1e-12)
	if got := Falloff(5.5, 0.5, 0, 1, 5, DirNorth, 360); got != 0 {
		t.Fatalf("beyond range must clamp to 0, got %v", got)
	}
}

func TestFalloffRangeDivisorFloorsAtOne(t *testing.T) {
	// range 0.5 -> divisor max(1, range) = 1 (lighting_source.dm:271).
	near(t, "range<1", Falloff(0, 0, 0, 0, 0.5, DirNorth, 360), 1, 1e-12)
	near(t, "range<1 edge", Falloff(0.6, 0, 0, 0, 0.5, DirNorth, 360), 0.4, 1e-12)
}

func TestFalloffStarlightHeightNeverNaN(t *testing.T) {
	// LIGHTING_HEIGHT_SPACE = -0.5 (__DEFINES/lighting.dm). The nearest corner
	// offset (0.5,0.5) gives exactly sqrt(0) = full strength.
	near(t, "space nearest", Falloff(0.5, 0.5, 0, -0.5, 2, DirNorth, 360), 1, 1e-12)
	// A negative radicand must not yield NaN (BYOND would runtime).
	if v := Falloff(0.1, 0.1, 0, -0.5, 2, DirNorth, 360); math.IsNaN(v) || v < 0 || v > 1 {
		t.Fatalf("negative radicand: %v", v)
	}
}

func TestFalloffConeMatchesTG(t *testing.T) {
	base := Falloff(1, -math.Sqrt(3), 0, 1, 6, DirEast, 360)
	// Coordinate at angle 150 degrees (delta 60 from East): angle=90, half=45,
	// (1 - (60-45)/30) = 0.5 (lighting_source.dm:294).
	got := Falloff(1, -math.Sqrt(3), 0, 1, 6, DirEast, 90)
	near(t, "cone edge", got, base*0.5, 1e-9)

	// Inside the half angle the multiplier is untouched.
	inside := Falloff(3, 0.5, 0, 1, 6, DirEast, 90)
	near(t, "cone inside", inside, Falloff(3, 0.5, 0, 1, 6, DirEast, 360), 1e-12)

	// Opposite side is fully dark (clamped by max(.., 0)).
	if v := Falloff(-3, 0.5, 0, 1, 6, DirEast, 90); v != 0 {
		t.Fatalf("behind cone must be 0, got %v", v)
	}
}

func TestFalloffConeWrapsAcrossNorth(t *testing.T) {
	// Light pointing North, coordinate at 350 degrees: delta must wrap to 10,
	// not 350 (lighting_source.dm:288-289).
	x, y := math.Sin(350*math.Pi/180)*3, math.Cos(350*math.Pi/180)*3
	got := Falloff(x, y, 0, 1, 6, DirNorth, 90)
	near(t, "wrap", got, Falloff(x, y, 0, 1, 6, DirNorth, 360), 1e-12)
}

func TestFalloffAngleDegenerateIsOmni(t *testing.T) {
	for _, a := range []float64{0, -5, 360, 400} {
		near(t, "omni", Falloff(-3, 0.5, 0, 1, 6, DirEast, a), Falloff(-3, 0.5, 0, 1, 6, DirEast, 360), 1e-12)
	}
}

func TestDirNoneCentersOnNorth(t *testing.T) {
	// dir2angle() returns null for unknown dirs, which BYOND reads as 0.
	near(t, "none", Falloff(0.5, 3, 0, 1, 6, DirNone, 90), Falloff(0.5, 3, 0, 1, 6, DirNorth, 90), 1e-12)
}

func TestDirAngleTable(t *testing.T) {
	cases := map[Dir]float64{
		DirNorth: 0, DirEast: 90, DirSouth: 180, DirWest: 270,
		DirNorth | DirEast: 45, DirSouth | DirEast: 135, DirSouth | DirWest: 225, DirNorth | DirWest: 315,
	}
	for d, want := range cases {
		if got := d.Angle(); got != want {
			t.Fatalf("dir %d: got %v want %v", d, got, want)
		}
	}
}
