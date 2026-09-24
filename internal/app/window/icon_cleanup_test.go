package window_test

import (
	"testing"

	"sdmm/internal/app/window"
	"sdmm/internal/dmapi/dmicon"
)

func TestFailedIconCleanupDoesNotQueueInvalidResource(t *testing.T) {
	cache := &dmicon.IconsCache{}
	cache.Free()
	cache.SetRootDirPath(t.TempDir())
	if icon, err := cache.Get("missing.dmi"); err == nil || icon != nil {
		t.Fatal("missing icon should be negatively cached")
	}
	cache.Free()
	cache.Free()
	if pending := window.PendingFrameJobsForTest(); pending != 0 {
		t.Fatalf("failed icon disposal queued %d invalid GL callbacks", pending)
	}
	window.DrainFrameJobsForTest()
}
