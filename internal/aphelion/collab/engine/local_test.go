package engine

import (
	"context"
	"reflect"
	"testing"
	"time"

	"sdmm/internal/aphelion/collab/model"
)

func TestLocalEditMovesIdentityAtomicallyWithoutDigestOrReplay(t *testing.T) {
	source := initialSnapshot()
	doc, err := NewUnsharedDocument(source)
	if err != nil {
		t.Fatal(err)
	}
	first, second := source.Tiles[0], source.Tiles[1]
	request := LocalRequest{Version: doc.LocalVersion(), Changes: []model.TileChange{
		{Coord: second.Coord, Before: second.State, After: first.State},
		{Coord: first.Coord, Before: first.State, After: model.TileState{}},
	}}
	accepted, err := doc.ApplyLocal(context.Background(), request)
	if err != nil {
		t.Fatal(err)
	}
	if accepted.Revision != source.Revision+1 || len(doc.accepted) != 0 {
		t.Fatal("local edit did not advance once without replay history")
	}
	if _, available := doc.CachedHash(); available {
		t.Fatal("local edit retained a stale digest")
	}
	if !doc.Snapshot().Tiles[1].State.Equal(first.State) || len(doc.Snapshot().Tiles[0].State.Prefabs) != 0 {
		t.Fatal("identity move was not applied as a whole")
	}
	accepted.Changes[1].After.Prefabs[0].Path = "/obj/result-mutation"
	request.Changes[0].After.Prefabs[0].Path = "/obj/request-mutation"
	if doc.Snapshot().Tiles[1].State.Prefabs[0].Path != "/obj/foo1" {
		t.Fatal("local result/input aliases authority")
	}
	// An actual wire consumer must obtain the exact current base, never the
	// previous revision's cached digest. The network path remains unchanged.
	current := doc.Snapshot()
	change := model.TileChange{Coord: second.Coord, Before: current.Tiles[1].State, After: model.TileState{}}
	op := operationFor(t, current, "01890f3e-7b5c-7abc-8def-0123456789cc", []model.TileChange{change})
	if _, err := doc.Apply(op, time.Now()); err != nil {
		t.Fatal(err)
	}
}

func TestLocalEditFailureLeavesIndexesAndBranchesUnchanged(t *testing.T) {
	for _, failure := range []string{"duplicate identity", "stale before", "wrong document", "stale revision", "cancelled"} {
		t.Run(failure, func(t *testing.T) {
			source := initialSnapshot()
			doc, err := NewUnsharedDocument(source)
			if err != nil {
				t.Fatal(err)
			}
			branch := doc.Clone()
			staleOwner := LocalRequest{Version: doc.LocalVersion(), Changes: []model.TileChange{{Coord: source.Tiles[0].Coord, Before: source.Tiles[0].State}}}
			if _, err := branch.ApplyLocal(context.Background(), staleOwner); err == nil {
				t.Fatal("branch accepted another owner's version token")
			}
			request := LocalRequest{Version: doc.LocalVersion(), Changes: []model.TileChange{{Coord: source.Tiles[1].Coord, After: source.Tiles[0].State}}}
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			switch failure {
			case "stale before":
				request.Changes[0].Before = source.Tiles[0].State
			case "wrong document":
				request.Version.DocumentID = "01890f3e-7b5c-7abc-8def-0123456789bc"
			case "stale revision":
				request.Version.Revision--
			case "cancelled":
				cancel()
			}
			if _, err := doc.ApplyLocal(ctx, request); err == nil {
				t.Fatal("invalid edit accepted")
			}
			if !reflect.DeepEqual(doc.Snapshot(), source) || !reflect.DeepEqual(branch.Snapshot(), source) {
				t.Fatal("failed edit changed state")
			}
			// A failed ID assignment must not poison the next valid move.
			request.Version = doc.LocalVersion()
			request.Changes = []model.TileChange{{Coord: source.Tiles[0].Coord, Before: source.Tiles[0].State}, {Coord: source.Tiles[1].Coord, After: source.Tiles[0].State}}
			if _, err := doc.ApplyLocal(context.Background(), request); err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(branch.Snapshot(), source) {
				t.Fatal("local edit changed cloned branch")
			}
			request.Version = branch.LocalVersion()
			if _, err := branch.ApplyLocal(context.Background(), request); err != nil {
				t.Fatal("branch index mutated", err)
			}
		})
	}
}

func TestLocalNoopKeepsRevisionAndDigest(t *testing.T) {
	source := initialSnapshot()
	doc, err := NewUnsharedDocument(source)
	if err != nil {
		t.Fatal(err)
	}
	accepted, err := doc.ApplyLocal(context.Background(), LocalRequest{Version: doc.LocalVersion(), Changes: []model.TileChange{{Coord: source.Tiles[0].Coord, Before: source.Tiles[0].State, After: source.Tiles[0].State}}})
	if err != nil || accepted.Revision != source.Revision || len(accepted.Changes) != 0 {
		t.Fatal("no-op created a revision", accepted, err)
	}
	if _, available := doc.CachedHash(); !available {
		t.Fatal("no-op invalidated digest")
	}
}

func TestSessionDocumentCannotIssueLocalCapability(t *testing.T) {
	source := initialSnapshot()
	doc, err := NewDocument(source)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := doc.ApplyLocal(context.Background(), LocalRequest{Version: doc.LocalVersion(), Changes: []model.TileChange{{Coord: source.Tiles[0].Coord, Before: source.Tiles[0].State}}}); err == nil {
		t.Fatal("session document accepted a local request")
	}
}
