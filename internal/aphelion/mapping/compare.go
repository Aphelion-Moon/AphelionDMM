package mapping

import (
	"maps"
	"strings"

	"sdmm/internal/util"
)

type ChannelDiff struct{ Turf, Area, Objects bool }
type Transform struct{ Offset util.Point }

func (t Transform) Apply(p util.Point) util.Point {
	return util.Point{X: p.X + t.Offset.X, Y: p.Y + t.Offset.Y, Z: p.Z + t.Offset.Z}
}
func (t Transform) Inverse(p util.Point) util.Point {
	return util.Point{X: p.X - t.Offset.X, Y: p.Y - t.Offset.Y, Z: p.Z - t.Offset.Z}
}
func channel(a Atom) string {
	if a.Path == "/turf" || strings.HasPrefix(a.Path, "/turf/") {
		return "turf"
	}
	if a.Path == "/area" || strings.HasPrefix(a.Path, "/area/") {
		return "area"
	}
	return "objects"
}

// CompareCell compares immutable source content without copying or exposing it.
func (s *Source) CompareCell(point util.Point, other *Source, otherPoint util.Point) ChannelDiff {
	return CompareCell(s.atomsAt(point), other.atomsAt(otherPoint))
}

func CompareCell(a, b []Atom) ChannelDiff {
	return ChannelDiff{
		Turf:    !equalChannel(a, b, "turf"),
		Area:    !equalChannel(a, b, "area"),
		Objects: !equalChannel(a, b, "objects"),
	}
}

func equalChannel(a, b []Atom, c string) bool {
	// Walk each channel in its authored order without allocating filtered slices.
	for i, j := 0, 0; ; i, j = i+1, j+1 {
		for i < len(a) && channel(a[i]) != c {
			i++
		}
		for j < len(b) && channel(b[j]) != c {
			j++
		}
		if i == len(a) || j == len(b) {
			return i == len(a) && j == len(b)
		}
		if a[i].Path != b[j].Path || !maps.Equal(a[i].Vars, b[j].Vars) {
			return false
		}
	}
}
