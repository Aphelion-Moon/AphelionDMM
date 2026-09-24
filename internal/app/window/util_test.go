package window

import (
	"reflect"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestRunLaterAcceptsConcurrentProducersWithoutLosingJobs(t *testing.T) {
	laterJobs = nil
	t.Cleanup(func() { laterJobs = nil })

	const producers = 16
	const jobsPerProducer = 64
	var producersDone sync.WaitGroup
	var completed atomic.Int64
	for range producers {
		producersDone.Add(1)
		go func() {
			defer producersDone.Done()
			for range jobsPerProducer {
				RunLater(func() { completed.Add(1) })
			}
		}()
	}
	producersDone.Wait()
	for PendingFrameJobsForTest() != 0 {
		runLaterJobs()
	}
	if actual := completed.Load(); actual != producers*jobsPerProducer {
		t.Fatalf("completed jobs = %d, want %d", actual, producers*jobsPerProducer)
	}
}

func TestDeferredBudgetPreservesCompletionOrderAcrossFrames(t *testing.T) {
	laterJobs = nil
	t.Cleanup(func() { laterJobs = nil })
	var got []int
	RunLater(func() { got = append(got, 1); RunLater(func() { got = append(got, 4) }) })
	RunLater(func() { got = append(got, 2) })
	RunLater(func() { got = append(got, 3) })
	runLaterJobsBudget(1, time.Hour)
	if !reflect.DeepEqual(got, []int{1}) || PendingFrameJobsForTest() != 3 {
		t.Fatal("frame budget drained too much or lost jobs", got)
	}
	runLaterJobsBudget(2, time.Hour)
	if !reflect.DeepEqual(got, []int{1, 2, 3}) {
		t.Fatal("new producer jumped ordered completions", got)
	}
	runLaterJobsBudget(2, time.Hour)
	if !reflect.DeepEqual(got, []int{1, 2, 3, 4}) {
		t.Fatal("queued completion was dropped", got)
	}
}
