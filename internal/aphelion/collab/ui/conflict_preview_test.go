package ui

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"testing"

	collabclient "sdmm/internal/aphelion/collab/client"
	"sdmm/internal/aphelion/collab/model"
)

func largeConflictSession(t testing.TB, tiles int) *SessionClient {
	t.Helper()
	snapshot := model.Snapshot{ProtocolVersion: model.ProtocolVersion, SchemaVersion: model.SchemaVersion,
		DocumentID: "01890f3e-7b5c-7abc-8def-0123456789ab", EnvironmentHash: "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
		MaxX: 256, MaxY: 256, MaxZ: 1}
	actor := model.ActorID("01890f3e-7b5c-7abc-8def-0123456789ac")
	hash, err := snapshot.Hash()
	if err != nil {
		t.Fatal(err)
	}
	network, err := collabclient.NewNetworkExecutor(collabclient.NewWebSocketTransport(collabclient.TransportConfig{}), snapshot, actor, "session")
	if err != nil {
		t.Fatal(err)
	}
	operation := model.Operation{ProtocolVersion: model.ProtocolVersion, DocumentID: snapshot.DocumentID, ActorID: actor,
		OperationID: "01890f3e-7b5c-7abc-8def-0123456789ad", EnvironmentHash: snapshot.EnvironmentHash, BaseMapHash: hash, Kind: model.OperationKindTileChange}
	// Reverse coordinate order checks that the preview preserves its sorted view.
	for i := tiles - 1; i >= 0; i-- {
		operation.Changes = append(operation.Changes, model.TileChange{Coord: model.Coord{X: i%256 + 1, Y: i/256 + 1, Z: 1},
			After: model.TileState{Prefabs: []model.PrefabState{{StableID: model.StableID(fmt.Sprintf("01890f3e-7b5c-7abc-8def-%012x", i+1)), Path: "/obj/unknown", Vars: map[string]string{"dir": "2", "opaque": "list(1, /obj/missing)"}}}}})
	}
	if _, err := network.Execute(context.Background(), operation); err == nil {
		t.Fatal("unconnected transport accepted operation")
	}
	if len(network.Conflicts()) != 1 {
		t.Fatal("missing recovery draft")
	}
	session := NewSessionClient(SessionClientConfig{})
	session.network = network
	session.sessionID = "session"
	return session
}

func TestSessionConflictViewBoundsLargeDraftBeforeRendering(t *testing.T) {
	session := largeConflictSession(t, 9216)
	view := BuildViewModel(session.Status())
	if len(view.Conflicts) != 1 || len(view.Conflicts[0].DraftAfter) != maxPanelConflictTiles || len(view.Conflicts[0].DraftBefore) != maxPanelConflictTiles {
		t.Fatal("large draft was fully materialized for a bounded preview")
	}
	if view.Conflicts[0].DraftAfter[0].Coord != (model.Coord{X: 1, Y: 1, Z: 1}) {
		t.Fatal("preview did not keep coordinate order")
	}
	if view.CanLeave || !session.HasRetainedDrafts() {
		t.Fatal("preview lost recovery guards")
	}
	if view.Conflicts[0].DraftTileCount != 9216 {
		t.Fatal("preview omitted hidden tile count")
	}
	if len(session.network.Conflicts()[0].Draft.Changes) != 9216 {
		t.Fatal("preview truncated retained intent")
	}
	full := session.network.Conflicts()[0]
	path := filepath.Join(t.TempDir(), "draft.json")
	if err := session.ExportConflict(full.OperationID, path); err != nil {
		t.Fatal(err)
	}
	encoded, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var exported struct{ Operation model.Operation }
	if err := json.Unmarshal(encoded, &exported); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(exported.Operation, full.Draft) {
		t.Fatal("preview limits truncated export")
	}
}

func BenchmarkSessionConflictView(b *testing.B) {
	for _, tiles := range []int{20, 9216} {
		b.Run(fmt.Sprint(tiles), func(b *testing.B) {
			session := largeConflictSession(b, tiles)
			b.ReportAllocs()
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				view := BuildViewModel(session.Status())
				if len(view.Conflicts) != 1 || view.CanLeave {
					b.Fatal("missing conflict")
				}
			}
		})
	}
}
