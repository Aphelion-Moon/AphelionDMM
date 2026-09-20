package window

import "github.com/go-gl/glfw/v3.3/glfw"

// External integration tests exercise the real queue without a production API.
var DrainFrameJobsForTest = runLaterJobs
var RunRepeatJobsForTest = runRepeatJobs

// Supply a hidden native handle while retaining the production frame path.
func FrameRunnerForTest(handle *glfw.Window, app application) func() {
	w := &Window{handle: handle, application: app}
	return w.runFrame
}

func PendingFrameJobsForTest() int {
	laterJobsMutex.Lock()
	defer laterJobsMutex.Unlock()
	return len(laterJobs)
}
