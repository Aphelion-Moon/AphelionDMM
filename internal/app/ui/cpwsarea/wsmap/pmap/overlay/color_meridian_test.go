package overlay

import (
	"testing"

	"sdmm/internal/aphelion/theme"
)

func TestOverlayFollowsTheme(t *testing.T) {
	defer theme.Select(theme.NameMeridian)
	theme.Select(theme.NameClassic)
	if ColorToolPickInstance != classicOverlay.pick || ColorToolDeleteInstance != classicOverlay.deleteInstance {
		t.Fatal("Classic did not restore the inherited overlay colours")
	}
	theme.Select(theme.NameMeridian)
	if ColorToolPickInstance == classicOverlay.pick || ColorToolDeleteInstance == classicOverlay.deleteInstance || ColorToolSelectIntersectBorder == classicOverlay.selectIntersect {
		t.Fatal("Meridian left overlays on the inherited colours")
	}
	if ColorToolAddTileBorder != classicOverlay.addAltBorder && ColorToolAddTileBorder.A() != 1 {
		t.Fatal("white borders must stay opaque")
	}
}
