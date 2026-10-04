package client

import (
	"encoding/json"
	"errors"
	"fmt"
	"math/rand"
	"strings"
	"testing"

	"sdmm/internal/aphelion/collab/model"
)

func admissionSizeFixture(tiles int) model.Operation {
	snapshot := indexedFixture(tiles)
	operation := model.Operation{ProtocolVersion: model.ProtocolVersion, DocumentID: snapshot.DocumentID,
		ActorID: "01890f3e-7b5c-7abc-8def-0123456789ac", OperationID: "01890f3e-7b5c-7abc-8def-0123456789ad",
		EnvironmentHash: snapshot.EnvironmentHash, BaseMapHash: strings.Repeat("b", 64), Kind: model.OperationKindTileChange}
	for _, tile := range snapshot.Tiles {
		after := model.CloneTileState(tile.State)
		after.Prefabs[0].Vars["dir"] = "4"
		operation.Changes = append(operation.Changes, model.TileChange{Coord: tile.Coord, Before: tile.State, After: after})
	}
	return operation
}

func TestAdmissionSizeMatchesJSONEncoding(t *testing.T) {
	rng := rand.New(rand.NewSource(260928))
	texts := []string{"", "plain", "quote\"backslash\\", "<script>&\u2028\u2029雪😀", "\x00\x01\b\t\n\f\r\x1f", "\xff\xfe\xc0\xaf", "�"}
	allBytes := make([]byte, 256)
	for i := range allBytes {
		allBytes[i] = byte(i)
	}
	texts = append(texts, string(allBytes))
	for sample := 0; sample < 300; sample++ {
		operation := admissionSizeFixture(sample % 4)
		if sample%11 == 0 {
			operation.Changes = []model.TileChange{}
		}
		if sample%13 == 0 {
			operation.Changes = nil
		}
		operation.ProtocolVersion = uint16(rng.Uint32())
		operation.BaseRevision = model.Revision(rng.Uint64())
		if sample%2 == 0 {
			operation.BaseRevision = model.Revision(^uint64(0))
		}
		operation.Kind = model.OperationKind(texts[sample%len(texts)])
		if sample%2 == 0 {
			inverse := model.OperationID(texts[(sample+1)%len(texts)])
			operation.InverseOf = &inverse
		}
		for i := range operation.Changes {
			change := &operation.Changes[i]
			change.Coord = model.Coord{X: int(rng.Uint64()), Y: -int(^uint(0)>>1) - 1, Z: int(^uint(0) >> 1)}
			change.Before = model.TileState{}
			if sample%2 == 0 {
				change.Before.Prefabs = []model.PrefabState{}
			}
			prefab := &change.After.Prefabs[0]
			prefab.Path = texts[rng.Intn(len(texts))]
			prefab.StableID = model.StableID(texts[rng.Intn(len(texts))])
			switch sample % 3 {
			case 0:
				prefab.Vars = nil
			case 1:
				prefab.Vars = map[string]string{}
			case 2:
				prefab.Vars = map[string]string{}
				for _, key := range texts {
					prefab.Vars[key] = texts[rng.Intn(len(texts))]
				}
			}
		}
		encoded, err := json.Marshal(operation)
		if err != nil {
			t.Fatal(err)
		}
		size := operationAdmissionBytes(operation)
		if size != uint64(len(encoded)) {
			t.Fatalf("sample %d: counted %d, encoded %d", sample, size, len(encoded))
		}
	}
}

func TestAdmissionSizeDoesNotAllocateAnEncodedOperation(t *testing.T) {
	operation := admissionSizeFixture(128)
	allocations := testing.AllocsPerRun(10, func() { _ = operationAdmissionBytes(operation) })
	if allocations != 0 {
		t.Fatalf("admission accounting allocated %.0f objects", allocations)
	}
}

func BenchmarkAdmissionSize(b *testing.B) {
	for _, tiles := range []int{1, 9216, 65536} {
		b.Run(fmt.Sprint(tiles), func(b *testing.B) {
			operation := admissionSizeFixture(tiles)
			b.ReportAllocs()
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				_ = operationAdmissionBytes(operation)
			}
		})
	}
}

func TestAdmissionByteBoundaryMatchesEncodedSize(t *testing.T) {
	network, err := NewNetworkExecutor(newFakeTransport(), projectionSnapshot(t), mustActorID(t), "session")
	if err != nil {
		t.Fatal(err)
	}
	operation, err := network.prepareOperation(admissionSizeFixture(1))
	if err != nil {
		t.Fatal(err)
	}
	encoded, err := json.Marshal(operation)
	if err != nil {
		t.Fatal(err)
	}
	network.publication.pendingAdmissionBytes = maxLocalAdmissionBytes - uint64(len(encoded))
	ticket, charged, err := network.reserveAdmission(operation)
	if err != nil || charged != uint64(len(encoded)) {
		t.Fatal("exact byte boundary was rejected or charged incorrectly", err)
	}
	network.finishAdmission(operation.OperationID, ticket, charged, false)
	network.publication.pendingAdmissionBytes++
	if _, _, err := network.reserveAdmission(operation); !errors.Is(err, errLocalAdmissionFull) {
		t.Fatal("over-budget operation admitted", err)
	}
}

func BenchmarkSubmissionPreparation(b *testing.B) {
	operation := admissionSizeFixture(9216)
	network := &NetworkExecutor{actor: operation.ActorID}
	for _, encode := range []bool{true, false} {
		name := "count_size"
		if encode {
			name = "encode_size"
		}
		b.Run(name, func(b *testing.B) {
			b.ReportAllocs()
			for i := 0; i < b.N; i++ {
				prepared, err := network.prepareOperation(operation)
				if err != nil {
					b.Fatal(err)
				}
				if encode {
					if _, err := json.Marshal(prepared); err != nil {
						b.Fatal(err)
					}
				} else {
					_ = operationAdmissionBytes(prepared)
				}
			}
		})
	}
}
