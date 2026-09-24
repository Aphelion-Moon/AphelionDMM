package store

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"sdmm/internal/aphelion/collab/engine"
	"sdmm/internal/aphelion/collab/model"
)

func TestMemoryReplayPagesAreContiguousAndPinCheckpointRevision(t *testing.T) {
	ctx := context.Background()
	fixture, err := NewConformanceFixture()
	if err != nil {
		t.Fatal(err)
	}
	value := NewMemoryStore()
	t.Cleanup(func() { _ = value.Close() })
	if err := value.Create(ctx, fixture.Initial); err != nil {
		t.Fatal(err)
	}
	if err := value.Append(ctx, fixture.First); err != nil {
		t.Fatal(err)
	}
	if err := value.Append(ctx, fixture.Second); err != nil {
		t.Fatal(err)
	}

	page, err := value.LoadReplayPage(ctx, fixture.Initial.DocumentID, fixture.Initial.Revision, fixture.Latest.Revision, nil, 1)
	if err != nil {
		t.Fatal(err)
	}
	if page.SnapshotRevision != fixture.Initial.Revision || page.HeadRevision != fixture.Latest.Revision || page.ThroughRevision != fixture.Latest.Revision || !page.HasMore {
		t.Fatalf("first replay page metadata = %#v", page)
	}
	if len(page.Entries) != 1 || page.Entries[0].Accepted.OperationID != fixture.First.OperationID || page.Entries[0].StorageVersion != LegacyTransactionVersion || len(page.Entries[0].Accepted.Changes) != 1 {
		t.Fatalf("first replay page entries = %#v", page.Entries)
	}

	checkpoint := page.SnapshotRevision
	page, err = value.LoadReplayPage(ctx, fixture.Initial.DocumentID, fixture.First.Revision, fixture.Latest.Revision, &checkpoint, 1)
	if err != nil {
		t.Fatal(err)
	}
	if page.SnapshotRevision != checkpoint || page.HasMore || len(page.Entries) != 1 || page.Entries[0].Accepted.OperationID != fixture.Second.OperationID {
		t.Fatalf("second replay page = %#v", page)
	}

	if err := value.SaveSnapshot(ctx, fixture.FirstSnapshot); err != nil {
		t.Fatal(err)
	}
	if _, err := value.LoadReplayPage(ctx, fixture.Initial.DocumentID, fixture.First.Revision, fixture.Latest.Revision, &checkpoint, 1); !errors.Is(err, ErrReplayCheckpointChanged) {
		t.Fatalf("page after concurrent checkpoint = %v, want %v", err, ErrReplayCheckpointChanged)
	}
	page, err = value.LoadReplayPage(ctx, fixture.Initial.DocumentID, fixture.Initial.Revision, fixture.Latest.Revision, nil, 1)
	if err != nil {
		t.Fatal(err)
	}
	if page.SnapshotRevision != fixture.First.Revision || len(page.Entries) != 0 {
		t.Fatalf("stale client did not receive the retained snapshot floor: %#v", page)
	}
}

func TestMemoryReplayPageRejectsMissingRevisionAndUnboundedPageSize(t *testing.T) {
	ctx := context.Background()
	fixture, err := NewConformanceFixture()
	if err != nil {
		t.Fatal(err)
	}
	value := NewMemoryStore()
	t.Cleanup(func() { _ = value.Close() })
	if err := value.Create(ctx, fixture.Initial); err != nil {
		t.Fatal(err)
	}
	if err := value.Append(ctx, fixture.First); err != nil {
		t.Fatal(err)
	}
	if err := value.Append(ctx, fixture.Second); err != nil {
		t.Fatal(err)
	}
	value.mutex.Lock()
	value.operations[fixture.Initial.DocumentID] = append(value.operations[fixture.Initial.DocumentID][:1], value.operations[fixture.Initial.DocumentID][2:]...)
	value.mutex.Unlock()
	if _, err := value.LoadReplayPage(ctx, fixture.Initial.DocumentID, fixture.Initial.Revision, fixture.Latest.Revision, nil, 1); !errors.Is(err, ErrReplayGap) {
		t.Fatalf("page with a missing revision = %v, want %v", err, ErrReplayGap)
	}
	if _, err := value.LoadReplayPage(ctx, fixture.Initial.DocumentID, fixture.Initial.Revision, fixture.Latest.Revision, nil, MaxReplayPageEntries+1); !errors.Is(err, ErrReplayPageLimit) {
		t.Fatalf("unbounded page size = %v, want %v", err, ErrReplayPageLimit)
	}
}

func TestMemoryReplayPagesAdmitInlineBytesBeforeCloning(t *testing.T) {
	ctx := context.Background()
	initial, err := newConformanceSnapshot(3)
	if err != nil {
		t.Fatal(err)
	}
	document, err := engine.NewDocument(initial)
	if err != nil {
		t.Fatal(err)
	}
	value := NewMemoryStore()
	t.Cleanup(func() { _ = value.Close() })
	if err := value.Create(ctx, initial); err != nil {
		t.Fatal(err)
	}
	if err := value.ConfigureTransactions(ctx, initial.DocumentID, BulkTransactionVersion); err != nil {
		t.Fatal(err)
	}
	payloadSizes := []int{MaxReplayPageInlineBytes * 3 / 5, MaxReplayPageInlineBytes * 3 / 5, MaxReplayPageInlineBytes + 4096}
	for index, coordinate := range []int{1, 2, 3} {
		operation, err := newConformanceOperation(document.Snapshot(), coordinate)
		if err != nil {
			t.Fatal(err)
		}
		payload := strings.Repeat(string(rune('a'+index)), payloadSizes[index])
		operation.Changes[0].After.Prefabs[0].Vars = map[string]string{"payload": payload}
		accepted, err := document.Apply(operation, time.Unix(int64(index+1), 0).UTC())
		if err != nil {
			t.Fatal(err)
		}
		if err := value.Append(ctx, accepted); err != nil {
			t.Fatal(err)
		}
	}

	page, err := value.LoadReplayPage(ctx, initial.DocumentID, initial.Revision, 3, nil, 32)
	if err != nil {
		t.Fatal(err)
	}
	if len(page.Entries) != 1 || !page.HasMore || page.Entries[0].Accepted.Revision != 1 {
		t.Fatalf("large legacy entries were not byte-admitted: count=%d more=%t revisions=%v", len(page.Entries), page.HasMore, replayPageRevisions(page.Entries))
	}
	page.Entries[0].Accepted.Changes[0].After.Prefabs[0].Vars["payload"] = "caller mutation"
	firstAgain, err := value.LoadReplayPage(ctx, initial.DocumentID, initial.Revision, 3, nil, 32)
	if err != nil {
		t.Fatal(err)
	}
	if firstAgain.Entries[0].Accepted.Changes[0].After.Prefabs[0].Vars["payload"] != strings.Repeat("a", payloadSizes[0]) {
		t.Fatal("replay page caller mutation changed retained operations")
	}
	next, err := value.LoadReplayPage(ctx, initial.DocumentID, 1, 3, nil, 32)
	if err != nil {
		t.Fatal(err)
	}
	if len(next.Entries) != 1 || !next.HasMore || next.Entries[0].Accepted.Revision != 2 || next.Entries[0].Accepted.Changes[0].After.Prefabs[0].Vars["payload"] != strings.Repeat("b", payloadSizes[1]) {
		t.Fatal("bounded second replay page is incorrect")
	}
	last, err := value.LoadReplayPage(ctx, initial.DocumentID, 2, 3, nil, 32)
	if err != nil {
		t.Fatal(err)
	}
	if len(last.Entries) != 1 || last.HasMore || last.Entries[0].Accepted.Revision != 3 || len(last.Entries[0].Accepted.Changes[0].After.Prefabs[0].Vars["payload"]) <= MaxReplayPageInlineBytes {
		t.Fatal("single oversized bulk operation did not remain an isolated replay page entry")
	}
}

func TestReplayAcceptedJSONSizeMatchesEncoderWithoutEncodingPayload(t *testing.T) {
	fixture, err := NewConformanceFixture()
	if err != nil {
		t.Fatal(err)
	}
	accepted := model.CloneAcceptedOperation(fixture.First)
	accepted.Changes[0].Before.Prefabs = nil
	accepted.Changes[0].After.Prefabs[0].Path = "/obj/<tag>&\"line\n\u2028🙂"
	accepted.Changes[0].After.Prefabs[0].Vars = map[string]string{"raw\tkey": "invalid-\xff-utf8"}
	encoded, err := json.Marshal(accepted)
	if err != nil {
		t.Fatal(err)
	}
	got, err := replayAcceptedJSONSize(accepted)
	if err != nil {
		t.Fatal(err)
	}
	if got != int64(len(encoded)) {
		t.Fatalf("structural JSON size = %d; encoding/json emits %d", got, len(encoded))
	}
}

func replayPageRevisions(entries []ReplayEntry) []model.Revision {
	revisions := make([]model.Revision, len(entries))
	for index, entry := range entries {
		revisions[index] = entry.Accepted.Revision
	}
	return revisions
}
