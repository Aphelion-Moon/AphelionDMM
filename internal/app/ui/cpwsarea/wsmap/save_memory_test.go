package wsmap

import (
	"testing"

	"sdmm/internal/aphelion/collab/model"
	"sdmm/internal/aphelion/resources"
)

// APHELION EDIT ADDITION START - RESPONSIVE_SAVE
type countedSaveCapture struct {
	snapshot     model.Snapshot
	estimated    uint64
	materialized int
}

func (capture *countedSaveCapture) Snapshot() model.Snapshot {
	capture.materialized++
	return capture.snapshot
}

func (capture *countedSaveCapture) EstimatedBytes() uint64 { return capture.estimated }

func TestSaveAdmissionPrecedesMaterializationAndReleasesOnFailure(t *testing.T) {
	capture := &countedSaveCapture{estimated: 1 << 20}
	request := saveRequest{captured: capture}

	denied := resources.NewFixedBudget(1)
	if result := runSaveWorkerWithBudget(request, denied); result.err == nil || capture.materialized != 0 || denied.Used() != 0 {
		t.Fatal("save materialized before admission or retained a denied reservation")
	}

	accepted := resources.NewFixedBudget(8 << 20)
	if result := runSaveWorkerWithBudget(request, accepted); result.err == nil || capture.materialized != 1 || accepted.Used() != 0 {
		t.Fatal("failed save projection leaked its memory reservation")
	}
}
// APHELION EDIT ADDITION END
