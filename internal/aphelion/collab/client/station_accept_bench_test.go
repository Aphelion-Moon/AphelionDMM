package client

import (
	"encoding/json"
	"fmt"
	"testing"
	"time"

	"sdmm/internal/aphelion/collab/model"
	"sdmm/internal/aphelion/collab/protocol"
)

// stationSnapshot is a station-sized level: 255x255 tiles with an area, a turf
// and two objects each, close to MetaStation's tile and prefab counts.
func stationSnapshot() model.Snapshot {
	snapshot := indexedFixture(1)
	snapshot.MaxX, snapshot.MaxY, snapshot.Tiles = 255, 255, nil
	id := 0
	next := func() model.StableID {
		id++
		return model.StableID(fmt.Sprintf("01890f3e-7b5c-7abc-8def-%012x", id))
	}
	for y := 1; y <= 255; y++ {
		for x := 1; x <= 255; x++ {
			snapshot.Tiles = append(snapshot.Tiles, model.Tile{Coord: model.Coord{X: x, Y: y, Z: 1}, State: model.TileState{Prefabs: []model.PrefabState{
				{StableID: next(), Path: "/area/station/hallway/primary/central", Vars: map[string]string{}},
				{StableID: next(), Path: "/turf/open/floor/iron/dark/smooth_half", Vars: map[string]string{"dir": "4"}},
				{StableID: next(), Path: "/obj/machinery/light/directional/north", Vars: map[string]string{"bulb_colour": `"#d1dfff"`}},
				{StableID: next(), Path: "/obj/effect/turf_decal/tile/blue", Vars: map[string]string{"dir": "8", "color": `"#52B4E9"`}},
			}}})
		}
	}
	return snapshot
}

// stationAccepts prepares count consecutive single-tile remote edits and the
// authoritative hash after each, as a server would send them.
func stationAccepts(b *testing.B, snapshot model.Snapshot, count int) []protocol.ServerEnvelope {
	b.Helper()
	envelopes := make([]protocol.ServerEnvelope, 0, count)
	current := model.CloneSnapshot(snapshot)
	for i := 0; i < count; i++ {
		index := (i * 7919) % len(current.Tiles)
		tile := current.Tiles[index]
		after := model.CloneTileState(tile.State)
		after.Prefabs[1].Vars["dir"] = fmt.Sprint(i%4 + 1)
		baseHash, err := current.Hash()
		if err != nil {
			b.Fatal(err)
		}
		accepted := model.AcceptedOperation{Operation: model.Operation{ProtocolVersion: model.ProtocolVersion, DocumentID: current.DocumentID, EnvironmentHash: current.EnvironmentHash,
			OperationID: model.OperationID(fmt.Sprintf("01890f3e-7b5c-7abc-8def-f%011x", i+1)), ActorID: "01890f3e-7b5c-7abc-8def-0123456789bc",
			BaseRevision: current.Revision, BaseMapHash: baseHash, Kind: model.OperationKindTileChange,
			Changes: []model.TileChange{{Coord: tile.Coord, Before: tile.State, After: after}}}, Revision: current.Revision + 1, AcceptedAt: time.Unix(int64(i+1), 0)}
		current.Tiles[index].State = after
		current.Revision++
		hash, err := current.Hash()
		if err != nil {
			b.Fatal(err)
		}
		payload, err := json.Marshal(protocol.OperationAcceptedPayload{Operation: accepted, MapHash: hash})
		if err != nil {
			b.Fatal(err)
		}
		envelopes = append(envelopes, protocol.ServerEnvelope{ProtocolVersion: model.ProtocolVersion, MessageID: "server", SessionID: "session", Type: protocol.ServerOperationAccepted, Payload: payload})
	}
	return envelopes
}

// BenchmarkStationRemoteSingleTileAccept is the per-edit client cost of another
// user's quick single-tile edit on a station-sized map.
func BenchmarkStationRemoteSingleTileAccept(b *testing.B) {
	snapshot := stationSnapshot()
	const batch = 32
	envelopes := stationAccepts(b, snapshot, batch)
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i += batch {
		b.StopTimer()
		network, err := NewNetworkExecutor(newFakeTransport(), snapshot, "01890f3e-7b5c-7abc-8def-0123456789ee", "session")
		if err != nil {
			b.Fatal(err)
		}
		b.StartTimer()
		for _, envelope := range envelopes[:min(batch, b.N-i)] {
			if err := network.Receive(envelope); err != nil {
				b.Fatal(err)
			}
		}
	}
}
