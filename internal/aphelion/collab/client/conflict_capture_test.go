package client

import (
	"context"
	"errors"
	"fmt"
	"testing"

	"sdmm/internal/aphelion/collab/model"
)

func TestConflictRecoveryCapturesPinAuthority(t *testing.T) {
	network, id := recoveryCaptureFixture(t, 256)
	ctx := context.Background()
	refreshed, err := network.RefreshConflict(ctx, id)
	if err != nil {
		t.Fatal(err)
	}
	canceled, cancel := context.WithCancel(ctx)
	cancel()
	if _, err := network.DiscardConflict(canceled, id); !errors.Is(err, context.Canceled) || len(network.conflicts) != 1 {
		t.Fatal("canceled discard changed the retained draft", err)
	}
	discarded, err := network.DiscardConflict(ctx, id)
	if err != nil || len(network.conflicts) != 0 {
		t.Fatal("discard did not remove the draft", err)
	}
	if _, err := network.RefreshConflict(ctx, id); err == nil {
		t.Fatal("refresh accepted an absent draft")
	}
	replacement := refreshed.AcceptedSnapshot()
	replacement.Revision++
	replacement.Tiles[0].State.Prefabs[0].Vars["dir"] = "4"
	if err := network.ReplaceAcknowledgedSnapshot(ctx, replacement); err != nil {
		t.Fatal(err)
	}
	for _, capture := range []ProjectionCapture{refreshed, discarded} {
		snapshot := capture.AcceptedSnapshot()
		if snapshot.Revision != 0 || snapshot.Tiles[0].State.Prefabs[0].Vars["dir"] != "2" {
			t.Fatal("recovery capture changed after authority replacement")
		}
		snapshot.Tiles[0].State.Prefabs[0].Vars["dir"] = "8"
		if capture.AcceptedSnapshot().Tiles[0].State.Prefabs[0].Vars["dir"] != "2" {
			t.Fatal("materialized recovery snapshot shares mutable values")
		}
	}
}

func recoveryCaptureFixture(t testing.TB, tiles int) (*NetworkExecutor, model.OperationID) {
	t.Helper()
	snapshot := indexedFixture(tiles)
	snapshot.MaxX, snapshot.MaxY = 256, (tiles+255)/256
	for i := range snapshot.Tiles {
		snapshot.Tiles[i].Coord = model.Coord{X: i%256 + 1, Y: i/256 + 1, Z: 1}
	}
	network, err := NewNetworkExecutor(newFakeTransport(), snapshot, "01890f3e-7b5c-7abc-8def-0123456789ac", "session")
	if err != nil {
		t.Fatal(err)
	}
	id := model.OperationID("01890f3e-7b5c-7abc-8def-0123456789ad")
	network.conflicts = []Conflict{{OperationID: id}}
	return network, id
}

func TestConflictRecoveryDoesNotCopyWholeMap(t *testing.T) {
	for _, discard := range []bool{false, true} {
		t.Run(fmt.Sprint(discard), func(t *testing.T) {
			network, id := recoveryCaptureFixture(t, 4096)
			allocations := testing.AllocsPerRun(10, func() {
				var err error
				if discard {
					network.conflicts = []Conflict{{OperationID: id}}
					_, err = network.DiscardConflict(context.Background(), id)
				} else {
					_, err = network.RefreshConflict(context.Background(), id)
				}
				if err != nil {
					t.Fatal(err)
				}
			})
			// Constant publication bookkeeping is allowed; per-tile copying is not.
			if allocations > 16 {
				t.Fatalf("recovery copied map-sized data: %.0f allocations", allocations)
			}
		})
	}
}

func BenchmarkConflictRecovery(b *testing.B) {
	for _, discard := range []bool{false, true} {
		b.Run(fmt.Sprintf("discard=%t", discard), func(b *testing.B) {
			network, id := recoveryCaptureFixture(b, 65536)
			b.ReportAllocs()
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				var err error
				if discard {
					network.conflicts = []Conflict{{OperationID: id}}
					_, err = network.DiscardConflict(context.Background(), id)
				} else {
					_, err = network.RefreshConflict(context.Background(), id)
				}
				if err != nil {
					b.Fatal(err)
				}
			}
		})
	}
}
