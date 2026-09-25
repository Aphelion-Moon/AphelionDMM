package resources

import "time"

// Frame preparation shares one allowance across deferred publication, icon
// upload and map geometry. Only the graphics owner reads or writes this state.
// Individual indivisible steps can exceed the allowance; later owners then yield.
var frameWork struct {
	active    bool
	remaining time.Duration
}

func BeginFrameWork() { frameWork.active = true; frameWork.remaining = 4 * time.Millisecond }
func EndFrameWork()   { frameWork.active = false }
func FrameWorkRemaining(limit time.Duration) time.Duration {
	if !frameWork.active {
		return limit
	}
	return max(0, min(limit, frameWork.remaining))
}
func ChargeFrameWork(started time.Time) {
	if frameWork.active {
		frameWork.remaining -= time.Since(started)
	}
}
