package ui

import (
	"context"
	"encoding/json"
	"fmt"
	"path/filepath"
	"testing"
	"time"

	collabclient "sdmm/internal/aphelion/collab/client"
	loadscenario "sdmm/internal/aphelion/collab/load"
	"sdmm/internal/aphelion/collab/model"
	"sdmm/internal/aphelion/collab/server"
	"sdmm/internal/aphelion/collab/store/sqlite"
)

func TestSessionClientRecoversDuringIndependentLoad(t *testing.T) {
	scenario, err := loadscenario.GenerateConcurrent(loadscenario.ConcurrentConfig{
		Config: loadscenario.Config{Seed: 20260920, Clients: 4, Operations: 64, MaxX: 8, MaxY: 7,
			TargetOperationsPerSecond: 40, PresencePerClient: 32, TargetPresencePerSecondPerEditor: 20},
		ConflictPairs: 8,
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Run("memory", func(t *testing.T) { verifySlowConsumerRecovery(t, server.NewMemoryStore(), &scenario) })
	t.Run("sqlite", func(t *testing.T) {
		store, err := sqlite.Open(filepath.Join(t.TempDir(), "independent-recovery.sqlite"))
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { _ = store.Close() })
		verifySlowConsumerRecovery(t, store, &scenario)
	})
}

func verifyIndependentRecoveryOffers(t *testing.T, ctx context.Context, owner, slow *SessionClient, store server.SessionStore, gate *slowWriteGate, scenario loadscenario.ConcurrentScenario) []model.OperationID {
	t.Helper()
	owner.mutex.Lock()
	config := loadscenario.RunConfig{Endpoint: owner.baseURL, Origin: owner.origin, SessionID: owner.sessionID, OwnerToken: owner.administrationToken}
	owner.mutex.Unlock()
	type observation struct {
		revision model.Revision
		err      error
	}
	recovered := make(chan observation, 1)
	go func() {
		select {
		case <-gate.blocked:
		case <-ctx.Done():
			recovered <- observation{err: fmt.Errorf("socket never blocked: %w", ctx.Err())}
			return
		}
		owner.mutex.Lock()
		invitation := Invitation{BaseURL: owner.baseURL, Origin: owner.origin, SessionID: owner.sessionID, Token: owner.administrationToken}
		owner.mutex.Unlock()
		blockedSnapshot, err := owner.fetchSnapshot(ctx, invitation)
		if err != nil {
			recovered <- observation{err: fmt.Errorf("read authoritative revision after socket blocked: %w", err)}
			return
		}
		if blockedSnapshot.Revision+12 > model.Revision(scenario.ExpectedAccepted) {
			recovered <- observation{err: fmt.Errorf("slow socket blocked too late to exercise durable queue overflow at revision %d", blockedSnapshot.Revision)}
			return
		}
		ticker := time.NewTicker(5 * time.Millisecond)
		defer ticker.Stop()
		// Twelve accepted events exceed the eight-entry queue even if the
		// blocked write already removed one event. Count only authoritative
		// edits published after the write actually blocked; Status may lag the
		// server head while a SQLite workload is running. This controls only
		// failure injection; the producer's absolute schedules never wait here.
		for owner.Status().Revision < blockedSnapshot.Revision+12 {
			select {
			case <-ticker.C:
			case <-ctx.Done():
				recovered <- observation{err: ctx.Err()}
				return
			}
		}
		gate.unblock()
		for {
			status := slow.Status()
			if status.State == collabclient.StateCaughtUp && status.Revision >= 12 {
				if status.Revision >= model.Revision(scenario.ExpectedAccepted) {
					recovered <- observation{err: fmt.Errorf("recovery was not observed until workload completion")}
				} else {
					recovered <- observation{revision: status.Revision}
				}
				return
			}
			select {
			case <-ticker.C:
			case <-ctx.Done():
				recovered <- observation{err: ctx.Err()}
				return
			}
		}
	}()
	result, err := loadscenario.RunConcurrent(ctx, config, scenario)
	encoded, encodeErr := json.Marshal(result)
	if encodeErr != nil {
		t.Fatal(encodeErr)
	}
	t.Logf("independently scheduled load result: %s", encoded)
	// Presence is intentionally coalesced under backpressure. Every offered
	// update must be accounted for, while durable operations must all be sent.
	if err != nil || !result.GatePassed || result.ScheduledOperations != scenario.Config.Operations || result.StartedOperations != scenario.Config.Operations || result.SentOperations != scenario.Config.Operations || result.AcceptedOperations != scenario.ExpectedAccepted || result.RejectedOperations != scenario.ExpectedRejected || result.AppliedDeliveries != scenario.ExpectedAccepted*scenario.Config.Clients || result.UnresolvedOperations != 0 || result.UnsentBacklog != 0 || result.PresenceSent+result.PresenceCoalesced != result.PresencePlanned {
		t.Fatalf("independent load did not account for all offers: %v", err)
	}
	select {
	case observed := <-recovered:
		if observed.err != nil {
			t.Fatal(observed.err)
		}
		t.Logf("slow observer recovered at revision %d before final workload revision %d", observed.revision, scenario.ExpectedAccepted)
	case <-ctx.Done():
		t.Fatal(ctx.Err())
	}
	state, err := store.LoadRecovery(ctx, scenario.Initial.DocumentID)
	if err != nil {
		t.Fatal(err)
	}
	document, err := state.Restore()
	if err != nil || len(state.Operations) != scenario.ExpectedAccepted {
		t.Fatalf("durable workload recovery differs: %v", err)
	}
	if snapshot := document.Snapshot(); snapshot.Revision != model.Revision(scenario.ExpectedAccepted) || mustSnapshotHash(t, snapshot) != scenario.ExpectedMapHash {
		t.Fatal("durable workload does not match the independent manifest")
	}
	known := make(map[model.OperationID]bool, len(scenario.Intents))
	for _, intent := range scenario.Intents {
		known[intent.Operation.OperationID] = true
	}
	offered := make([]model.OperationID, 0, len(state.Operations)+2)
	for _, accepted := range state.Operations {
		if !known[accepted.OperationID] {
			t.Fatal("unknown or repeated operation in durable history")
		}
		delete(known, accepted.OperationID)
		offered = append(offered, accepted.OperationID)
	}
	return offered
}
