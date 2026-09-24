package editor

import (
	"context"
	"errors"
	"reflect"
	"testing"
	"time"

	"sdmm/internal/aphelion/resources"
	"sdmm/internal/dmapi/dm"
	"sdmm/internal/dmapi/dmmap"
	"sdmm/internal/util"
)

func TestPasteAdmissionFailureLeavesAuthorityDisplayAndHistoryUntouched(t *testing.T) {
	e := selectionEditor(t)
	e.workBudget = resources.NewFixedBudget(1)
	beforeDisplay := e.dmm.Copy()
	beforeAuthority, err := e.SaveSnapshot(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	beforeHash, err := beforeAuthority.Hash()
	if err != nil {
		t.Fatal(err)
	}

	filter := dm.NewPathsFilterEmpty()
	source := []dmmap.Tile{e.dmm.GetTile(util.Point{X: 1, Y: 1, Z: 1}).Copy()}
	target := util.Point{X: 2, Y: 1, Z: 1}
	if err := e.beginPasteProposal(source, filter.IsVisiblePath, target); err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		e.ProcessPasteWork()
		if p := e.paste; p != nil && p.phase == pasteReady && !p.workerBusy {
			break
		}
		time.Sleep(time.Millisecond)
	}
	p := e.paste
	if p == nil {
		t.Fatalf("admission failure removed the paste session: progress=%q", e.PastePlacementProgress())
		return
	}
	if p.phase != pasteReady || p.workerBusy || p.err == nil {
		t.Fatalf("admission failure did not settle: progress=%q err=%v", e.PastePlacementProgress(), p.err)
	}
	var admission *resources.AdmissionError
	if !errors.As(p.err, &admission) || admission.Needed <= admission.Available {
		t.Fatalf("failure did not report required and available bytes: %v", p.err)
	}
	if _, ready, err := e.UpdatePastePlacement(target); ready || !errors.As(err, &admission) {
		t.Fatalf("rejected proposal became ready or lost its admission error: ready=%t err=%v", ready, err)
	}
	if !reflect.DeepEqual(e.dmm.Copy(), beforeDisplay) || e.app.CommandStorage().HasUndoV(e.dmm.Path.Absolute) {
		t.Fatal("admission failure changed the display or created history")
	}
	// An unplaced, denied source must not block committed saves either.
	afterAuthority, err := e.SaveSnapshot(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	afterHash, err := afterAuthority.Hash()
	if err != nil || afterHash != beforeHash {
		t.Fatalf("admission failure changed authority: hash=%q err=%v", afterHash, err)
	}
	e.CancelPastePlacement()
	if e.HasPastePlacement() || e.editWorkBudget().Used() != 0 {
		t.Fatal("cancelling rejected work retained the session or its reservation")
	}
}
