package lighting

import "math"

// Falloff mirrors /datum/light_source/proc/falloff_at_coord
// (lighting_source.dm:270-294). dx, dy, dz are the offset from the emitter to
// the corner; the result is in 0..1.
//
// Note the faithful quirk: height is added inside the root unsquared
// (lighting_source.dm:276).
func Falloff(dx, dy, dz, height, rng float64, dir Dir, angle float64) float64 {
	divisor := math.Max(1, rng)                            // :271
	radicand := math.Max(0, dx*dx+dy*dy+dz*dz+height)      // :276 (guard: BYOND would runtime on <0)
	multiplier := 1 - clamp01(math.Sqrt(radicand)/divisor) // :276
	if angle >= 360 || angle <= 0 {                        // :277
		return multiplier
	}
	coord := deltaToAngle(dx, dy)          // :281
	delta := math.Abs(dir.Angle() - coord) // :283-284
	if delta > 180 {                       // :288
		delta = 180 - (delta - 180) // :289
	}
	return math.Max(multiplier*(1-math.Max(delta-angle/2, 0)/30), 0) // :294
}

// deltaToAngle mirrors delta_to_angle (__HELPERS/maths.dm:10-17): north-zero,
// clockwise degrees.
func deltaToAngle(x, y float64) float64 {
	if y == 0 {
		if x >= 0 {
			return 90
		}
		return 270
	}
	a := math.Atan(x/y) * 180 / math.Pi
	if y < 0 {
		a += 180
	} else if x < 0 {
		a += 360
	}
	return a
}

func clamp01(v float64) float64 {
	if v < 0 {
		return 0
	}
	if v > 1 {
		return 1
	}
	return v
}

// lightingRoundValue is LIGHTING_ROUND_VALUE (__DEFINES/lighting.dm:35): 1/64.
const lightingRoundValue = 1.0 / 64.0

func roundLight(v float64) float64 { return math.Round(v/lightingRoundValue) * lightingRoundValue }
