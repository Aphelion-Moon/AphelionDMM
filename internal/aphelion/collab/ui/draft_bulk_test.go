package ui

import (
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	collabclient "sdmm/internal/aphelion/collab/client"
	"sdmm/internal/aphelion/collab/model"
)

type resolveCall struct {
	id     model.OperationID
	action ConflictAction
}

func draftIDs(count int) []model.OperationID {
	ids := make([]model.OperationID, count)
	for index := range ids {
		ids[index] = model.OperationID(fmt.Sprintf("draft-%02d", index))
	}
	return ids
}

func TestBulkDiscardExportsEveryDraftBeforeDiscardingAnyAcrossPages(t *testing.T) {
	t.Parallel()

	ids := draftIDs(maxVisibleConflicts*2 + 3)
	var calls []resolveCall
	outcome := RunBulkDraftAction(BulkDiscardAll, true, ids, func(id model.OperationID, action ConflictAction) error {
		calls = append(calls, resolveCall{id, action})
		return nil
	})
	if outcome.Err != nil || outcome.Completed != len(ids) || outcome.Total != len(ids) {
		t.Fatalf("outcome = %#v", outcome)
	}
	if len(calls) != 2*len(ids) {
		t.Fatalf("calls = %d, want %d", len(calls), 2*len(ids))
	}
	for index, id := range ids {
		if calls[index] != (resolveCall{id, ConflictActionExport}) {
			t.Fatalf("call %d = %#v, want export of %s", index, calls[index], id)
		}
		if calls[len(ids)+index] != (resolveCall{id, ConflictActionDiscard}) {
			t.Fatalf("call %d = %#v, want discard of %s", len(ids)+index, calls[len(ids)+index], id)
		}
	}
}

func TestBulkDiscardWithoutExportOnlyDiscards(t *testing.T) {
	t.Parallel()

	var calls []resolveCall
	outcome := RunBulkDraftAction(BulkDiscardAll, false, draftIDs(3), func(id model.OperationID, action ConflictAction) error {
		calls = append(calls, resolveCall{id, action})
		return nil
	})
	if outcome.Err != nil || len(calls) != 3 {
		t.Fatalf("outcome = %#v calls = %#v", outcome, calls)
	}
	for _, call := range calls {
		if call.action != ConflictActionDiscard {
			t.Fatalf("unexpected action %#v", call)
		}
	}
}

func TestBulkDiscardNeverDiscardsWhenAnExportFails(t *testing.T) {
	t.Parallel()

	ids := draftIDs(4)
	boom := errors.New("disk full")
	var calls []resolveCall
	outcome := RunBulkDraftAction(BulkDiscardAll, true, ids, func(id model.OperationID, action ConflictAction) error {
		calls = append(calls, resolveCall{id, action})
		if id == ids[2] {
			return boom
		}
		return nil
	})
	if !errors.Is(outcome.Err, boom) || outcome.FailedID != ids[2] || outcome.FailedAction != ConflictActionExport {
		t.Fatalf("outcome = %#v", outcome)
	}
	if outcome.Completed != 0 || outcome.Remaining != 4 {
		t.Fatalf("completed/remaining = %d/%d, want 0/4: nothing may be discarded", outcome.Completed, outcome.Remaining)
	}
	for _, call := range calls {
		if call.action == ConflictActionDiscard {
			t.Fatalf("discard attempted after export failure: %#v", calls)
		}
	}
	if len(calls) != 3 {
		t.Fatalf("calls after failure continued: %#v", calls)
	}
}

func TestBulkRefreshStopsAtFirstFailureAndReportsRest(t *testing.T) {
	t.Parallel()

	ids := draftIDs(5)
	boom := errors.New("conflict is gone")
	var calls []resolveCall
	outcome := RunBulkDraftAction(BulkRefreshAll, true, ids, func(id model.OperationID, action ConflictAction) error {
		calls = append(calls, resolveCall{id, action})
		if id == ids[3] {
			return boom
		}
		return nil
	})
	if len(calls) != 4 || outcome.Completed != 3 || outcome.Remaining != 2 || outcome.FailedID != ids[3] || !errors.Is(outcome.Err, boom) {
		t.Fatalf("outcome = %#v calls = %d", outcome, len(calls))
	}
	for _, call := range calls {
		if call.action != ConflictActionRefresh {
			t.Fatalf("refresh-all used %q", call.action)
		}
	}
	message := outcome.Message()
	for _, want := range []string{string(ids[3]), "3 of 5", "2", "conflict is gone"} {
		if !strings.Contains(message, want) {
			t.Fatalf("message %q lacks %q", message, want)
		}
	}
}

func TestBulkExportAllNeverResolvesDrafts(t *testing.T) {
	t.Parallel()

	var calls []resolveCall
	outcome := RunBulkDraftAction(BulkExportAll, true, draftIDs(3), func(id model.OperationID, action ConflictAction) error {
		calls = append(calls, resolveCall{id, action})
		return nil
	})
	if outcome.Err != nil || outcome.Completed != 3 || len(calls) != 3 {
		t.Fatalf("outcome = %#v", outcome)
	}
	for _, call := range calls {
		if call.action != ConflictActionExport {
			t.Fatalf("export-all used %q", call.action)
		}
	}
}

func TestBulkWithNoDraftsIsANoOp(t *testing.T) {
	t.Parallel()

	outcome := RunBulkDraftAction(BulkDiscardAll, true, nil, func(model.OperationID, ConflictAction) error {
		t.Fatal("resolver called")
		return nil
	})
	if outcome.Err != nil || outcome.Total != 0 || outcome.Message() != "" {
		t.Fatalf("outcome = %#v", outcome)
	}
}

func TestBulkCancelledExportIsNotAFailureButStopsRun(t *testing.T) {
	t.Parallel()

	ids := draftIDs(3)
	var calls int
	outcome := RunBulkDraftAction(BulkDiscardAll, true, ids, func(id model.OperationID, action ConflictAction) error {
		calls++
		return ErrBulkCancelled
	})
	if calls != 1 || !outcome.Cancelled || outcome.Completed != 0 || outcome.Remaining != 3 {
		t.Fatalf("outcome = %#v calls = %d", outcome, calls)
	}
}

func TestDraftExportFileNameIsFlatAndUnique(t *testing.T) {
	t.Parallel()

	name := DraftExportFileName(2, model.OperationID(`../x\y:z`))
	if strings.ContainsAny(name, `/\:`) || !strings.HasSuffix(name, ".json") || !strings.HasPrefix(name, "collaboration-draft-003-") {
		t.Fatalf("name = %q", name)
	}
	if DraftExportFileName(0, "a") == DraftExportFileName(1, "a") {
		t.Fatal("names collide")
	}
}

func TestViewModelBannerAndPromptStates(t *testing.T) {
	t.Parallel()

	limited := fmt.Errorf("%w (last attempt: dial refused)", collabclient.ErrRateLimited)
	cases := []struct {
		name          string
		status        SessionStatus
		banner        string
		gaveUp        bool
		interrupted   bool
		showReconnect bool
	}{
		{"rate limited retrying", SessionStatus{SessionID: "s", State: collabclient.StateReconnecting, Err: limited, ConflictCount: 2}, "Reconnecting (rate limited by server)", false, true, true},
		{"rate limited no drafts", SessionStatus{SessionID: "s", State: collabclient.StateReconnecting, Err: limited}, "Reconnecting (rate limited by server)", false, false, true},
		{"gave up", SessionStatus{SessionID: "s", State: collabclient.StateReconnecting, ReconnectReady: true, Err: limited, ConflictCount: 1}, "Reconnecting (rate limited by server)", true, true, true},
		{"gave up generic", SessionStatus{SessionID: "s", State: collabclient.StateReconnecting, ReconnectReady: true, Err: errors.New("dial refused"), ConflictCount: 1}, "Reconnect attempts exhausted. Use Retry Reconnect.", true, true, true},
		{"terminal", SessionStatus{SessionID: "s", State: collabclient.StateClosed, ConflictCount: 3}, "", false, true, false},
		{"disconnected resumable", SessionStatus{SessionID: "s", State: collabclient.StateDisconnected, ReconnectReady: true, ConflictCount: 1}, "Reconnect attempts exhausted. Use Retry Reconnect.", true, true, true},
		{"reconnecting normally", SessionStatus{SessionID: "s", State: collabclient.StateReconnecting, ConflictCount: 1, Err: errors.New("dial refused")}, "", false, false, true},
		{"connected with drafts", SessionStatus{SessionID: "s", State: collabclient.StateConflict, ConflictCount: 1}, "", false, false, false},
		{"idle", SessionStatus{State: collabclient.StateDisconnected}, "", false, false, false},
	}
	for _, test := range cases {
		view := BuildViewModel(test.status)
		if view.StatusBanner != test.banner || view.ReconnectGaveUp != test.gaveUp || view.DraftsInterrupted != test.interrupted || view.ShowReconnect != test.showReconnect {
			t.Errorf("%s: banner=%q gaveUp=%v interrupted=%v showReconnect=%v", test.name, view.StatusBanner, view.ReconnectGaveUp, view.DraftsInterrupted, view.ShowReconnect)
		}
		if view.DraftCount != test.status.ConflictCount {
			t.Errorf("%s: draft count = %d", test.name, view.DraftCount)
		}
	}
	_ = time.Second
}

func TestDraftPromptGateShowsOncePerDisconnectEpisode(t *testing.T) {
	t.Parallel()

	interrupted := ViewModel{SessionLabel: "s", DraftsInterrupted: true, DraftCount: 2}
	healthy := ViewModel{SessionLabel: "s", DraftCount: 2}
	var gate DraftPromptGate
	if gate.Observe(healthy) {
		t.Fatal("opened while healthy")
	}
	if !gate.Observe(interrupted) {
		t.Fatal("did not open on interruption")
	}
	gate.Dismiss()
	for frame := 0; frame < 5; frame++ {
		if gate.Observe(interrupted) {
			t.Fatal("reopened in the same episode after dismissal")
		}
	}
	// Even if the prompt is still open (never dismissed) it must not request a second one.
	var other DraftPromptGate
	other.Observe(interrupted)
	if other.Observe(interrupted) {
		t.Fatal("requested a second prompt while one is open")
	}
	// Recovering ends the episode; the next interruption prompts again.
	if gate.Observe(healthy) {
		t.Fatal("opened on recovery")
	}
	if !gate.Observe(interrupted) {
		t.Fatal("new episode did not prompt")
	}
	gate.Dismiss()
	// A different session is a new episode.
	if !gate.Observe(ViewModel{SessionLabel: "t", DraftsInterrupted: true, DraftCount: 1}) {
		t.Fatal("new session did not prompt")
	}
	// Draining the drafts ends the episode.
	gate.Observe(ViewModel{SessionLabel: "t", DraftsInterrupted: false})
	if !gate.Observe(ViewModel{SessionLabel: "t", DraftsInterrupted: true, DraftCount: 1}) {
		t.Fatal("episode after drain did not prompt")
	}
}
