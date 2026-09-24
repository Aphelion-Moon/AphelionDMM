package client

import (
	"context"
	"fmt"
	"testing"
	"time"

	"sdmm/internal/aphelion/collab/model"
)

func wholeLevelNetwork(t testing.TB) (*NetworkExecutor, model.OperationID) {
	t.Helper()
	snapshot := indexedFixture(1)
	snapshot.MaxX = 256
	snapshot.MaxY = 256
	snapshot.Tiles = nil
	snapshot.Revision = 1
	id := model.OperationID("01890f3e-7b5c-7abc-8def-0123456789bb")
	actor := model.ActorID("01890f3e-7b5c-7abc-8def-0123456789bc")
	accepted := model.AcceptedOperation{Operation: model.Operation{OperationID: id, ActorID: actor, Kind: model.OperationKindTileChange}, Revision: 1, AcceptedAt: time.Unix(1, 0)}
	for i := 0; i < 65536; i++ {
		coord := model.Coord{X: i%256 + 1, Y: i/256 + 1, Z: 1}
		state := model.TileState{Prefabs: []model.PrefabState{{StableID: model.StableID(fmt.Sprintf("01890f3e-7b5c-7abc-8def-%012x", i+1)), Path: "/obj/unknown", Vars: map[string]string{"raw": "list(1, /obj/missing)"}}}}
		snapshot.Tiles = append(snapshot.Tiles, model.Tile{Coord: coord, State: state})
		accepted.Changes = append(accepted.Changes, model.TileChange{Coord: coord, After: state})
	}
	network, err := NewNetworkExecutor(newFakeTransport(), snapshot, actor, "session")
	if err != nil {
		t.Fatal(err)
	}
	network.accepted[id] = accepted
	return network, id
}

func TestWholeLevelInverseOwnershipAndLateConflict(t *testing.T) {
	network, id := wholeLevelNetwork(t)
	inverse, err := network.BuildInverse(context.Background(), id)
	if err != nil {
		t.Fatal(err)
	}
	if len(inverse.Changes) != 65536 {
		t.Fatal("inverse lost tiles")
	}
	inverse.Changes[0].Before.Prefabs[0].Vars["raw"] = "changed caller"
	if network.accepted[id].Changes[0].After.Prefabs[0].Vars["raw"] == "changed caller" {
		t.Fatal("inverse aliases history")
	}
	network.projection.Acknowledged.Tiles[65535].State.Prefabs[0].Vars["raw"] = "conflict"
	if _, err := network.BuildInverse(context.Background(), id); err == nil {
		t.Fatal("late inverse conflict accepted")
	}
}

func BenchmarkWholeLevelNetworkInverse(b *testing.B) {
	network, id := wholeLevelNetwork(b)
	b.ResetTimer()
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		if _, err := network.BuildInverse(context.Background(), id); err != nil {
			b.Fatal(err)
		}
	}
}
