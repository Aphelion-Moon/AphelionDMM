package window

// External integration tests exercise the real queue without a production API.
var DrainFrameJobsForTest = runLaterJobs
var RunRepeatJobsForTest = runRepeatJobs

func PendingFrameJobsForTest() int {
	laterJobsMutex.Lock()
	defer laterJobsMutex.Unlock()
	return len(laterJobs)
}
