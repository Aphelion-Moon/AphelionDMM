package window_test

import (
	"runtime"
	"testing"
	"time"

	"sdmm/internal/app/ui/cpwsarea/wsmap"
	"sdmm/internal/app/window"
)

func saveWorkspaceAsync(t *testing.T, workspace *wsmap.WsMap) bool {
	t.Helper()
	result := make(chan bool, 1)
	workspace.SaveAsync(func(saved bool) { result <- saved })
	timer := time.NewTimer(20 * time.Second)
	defer timer.Stop()
	for {
		window.DrainFrameJobsForTest()
		select {
		case saved := <-result:
			return saved
		case <-timer.C:
			t.Fatal("workspace save did not complete")
			return false
		default:
			runtime.Gosched()
			time.Sleep(time.Millisecond)
		}
	}
}
