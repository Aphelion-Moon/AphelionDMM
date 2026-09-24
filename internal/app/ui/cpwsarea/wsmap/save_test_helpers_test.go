package wsmap

import (
	"testing"
	"time"
)

func awaitSaveResult(t *testing.T, jobs <-chan func(), start func(func(bool))) bool {
	t.Helper()
	result := make(chan bool, 1)
	start(func(saved bool) { result <- saved })
	timer := time.NewTimer(20 * time.Second)
	defer timer.Stop()
	for {
		select {
		case saved := <-result:
			return saved
		default:
		}
		select {
		case saved := <-result:
			return saved
		case job := <-jobs:
			job()
		case <-timer.C:
			t.Fatal("save did not complete")
			return false
		}
	}
}

func saveForTest(t *testing.T, ws *WsMap, jobs <-chan func()) bool {
	return awaitSaveResult(t, jobs, ws.SaveAsync)
}
