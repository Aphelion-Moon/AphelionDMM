// APHELION EDIT ADDITION START - BOUNDED DEFERRED WORK
package window

import "time"

// Completion order is durable; only the amount consumed by a frame is bounded.
// Producers may enqueue while a job executes. Their work follows the remaining
// detached queue, rather than overtaking an older accepted edit.
func runLaterJobsBudget(limit int, budget time.Duration) {
	laterJobsMutex.Lock()
	jobs := laterJobs
	laterJobs = nil
	laterJobsMutex.Unlock()
	started := time.Now()
	completed := 0
	for completed < len(jobs) && completed < limit {
		job := jobs[completed]
		jobs[completed] = nil
		completed++
		job()
		if time.Since(started) >= budget {
			break
		}
	}
	if completed < len(jobs) {
		laterJobsMutex.Lock()
		laterJobs = append(jobs[completed:], laterJobs...)
		laterJobsMutex.Unlock()
	}
}

// APHELION EDIT ADDITION END
