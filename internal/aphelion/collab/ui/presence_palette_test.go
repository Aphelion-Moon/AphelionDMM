package ui

import (
	"image/color"
	"math"
	"testing"

	"sdmm/internal/aphelion/collab/protocol"
)

func relativeLuminance(c color.RGBA) float64 {
	channel := func(v uint8) float64 {
		s := float64(v) / 255
		if s <= 0.03928 {
			return s / 12.92
		}
		return math.Pow((s+0.055)/1.055, 2.4)
	}
	return 0.2126*channel(c.R) + 0.7152*channel(c.G) + 0.0722*channel(c.B)
}

func contrastRatio(a, b color.RGBA) float64 {
	la, lb := relativeLuminance(a), relativeLuminance(b)
	if la < lb {
		la, lb = lb, la
	}
	return (la + 0.05) / (lb + 0.05)
}

func TestPresencePaletteMatchesProtocolAndIsAccessible(t *testing.T) {
	t.Parallel()
	if len(PresencePalette) != protocol.CursorColorPaletteSize {
		t.Fatalf("palette has %d entries, protocol declares %d", len(PresencePalette), protocol.CursorColorPaletteSize)
	}
	black := color.RGBA{A: 255}
	for index, entry := range PresencePalette {
		if entry.Name == "" || entry.Color.A != 255 {
			t.Fatalf("palette[%d] = %#v", index, entry)
		}
		// The marker is drawn with a black outline, so the fill must stand out
		// from it, and badge text (black or white) must be readable on the fill.
		if ratio := contrastRatio(entry.Color, black); ratio < 3 {
			t.Errorf("palette[%d] %s contrast against the outline is %.2f, want >= 3", index, entry.Name, ratio)
		}
		if ratio := contrastRatio(entry.Color, PresenceTextColor(entry.Color)); ratio < 4.5 {
			t.Errorf("palette[%d] %s badge text contrast is %.2f, want >= 4.5", index, entry.Name, ratio)
		}
		for other := index + 1; other < len(PresencePalette); other++ {
			a, b := entry.Color, PresencePalette[other].Color
			distance := math.Sqrt(math.Pow(float64(a.R)-float64(b.R), 2) + math.Pow(float64(a.G)-float64(b.G), 2) + math.Pow(float64(a.B)-float64(b.B), 2))
			if distance < 60 {
				t.Errorf("palette[%d] %s and [%d] %s are too similar (%.0f)", index, entry.Name, other, PresencePalette[other].Name, distance)
			}
		}
	}
}

func TestPresenceSlotColorWrapsDefensively(t *testing.T) {
	t.Parallel()
	if PresenceSlotColor(uint32(len(PresencePalette))) != PresencePalette[0].Color {
		t.Fatal("out-of-range slot must wrap instead of panicking")
	}
}
