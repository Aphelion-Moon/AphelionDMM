package load

import (
	"context"
	"fmt"
	"reflect"
	"sync"
	"time"

	"github.com/coder/websocket"
	"sdmm/internal/aphelion/collab/client"
	"sdmm/internal/aphelion/collab/model"
	"sdmm/internal/aphelion/collab/protocol"
)

type concurrentPeer struct {
	writeMu          sync.Mutex
	connection       *websocket.Conn
	actor            model.ActorID
	presenceInterval time.Duration
}

func readConcurrentPeer(ctx context.Context, peerIndex int, peers []*concurrentPeer, metrics *concurrentMeasurements, notify chan<- struct{}) error {
	projection := client.NewProjection(metrics.scenario.Initial)
	for {
		kind, data, err := peers[peerIndex].connection.Read(ctx)
		received := time.Now() // Network receipt precedes decoding and client application.
		if err != nil {
			return err
		}
		message, err := decodeLoadEnvelope(ctx, peers[peerIndex].connection, kind, data)
		if err != nil {
			return err
		}
		if message.Envelope.SessionID != metrics.sessionID {
			return fmt.Errorf("message belongs to another load session")
		}
		switch message.Envelope.Type {
		case protocol.ServerOperationAccepted:
			payload := message.Payload.(*protocol.OperationAcceptedPayload)
			index, exists := metrics.indexes[payload.Operation.OperationID]
			if !exists {
				return fmt.Errorf("client %d received an operation outside the controlled workload", peerIndex)
			}
			intent := metrics.scenario.Intents[index]
			expected := model.CloneOperation(intent.Operation)
			expected.ActorID = peers[intent.Client].actor
			if !reflect.DeepEqual(expected, payload.Operation.Operation) {
				return fmt.Errorf("accepted operation differs from submitted intent")
			}
			projection, err = projection.Accept(payload.Operation, payload.MapHash)
			if err != nil {
				return fmt.Errorf("client %d apply accepted stream: %w", peerIndex, err)
			}
			applied := time.Now()
			metrics.mu.Lock()
			record := &metrics.records[index]
			if record.start.IsZero() || !record.rejected.IsZero() || !record.applied.add(peerIndex) {
				metrics.mu.Unlock()
				return fmt.Errorf("unexpected or duplicate accepted delivery")
			}
			record.accepted = true
			if record.latestApplied.Before(applied) {
				record.latestApplied = applied
			}
			if record.applied.count() == len(peers) {
				record.allApplied = record.latestApplied
			}
			if intent.Client == peerIndex {
				record.acknowledged = received
			}
			metrics.clients[peerIndex].AppliedOperations++
			metrics.clients[peerIndex].Revision = projection.Acknowledged.Revision
			metrics.clients[peerIndex].MapHash = payload.MapHash // Accept verified this digest against applied state.
			metrics.updateCompletionLocked(index)
			metrics.mu.Unlock()
		case protocol.ServerOperationRejected:
			payload := message.Payload.(*protocol.OperationRejectedPayload)
			index, exists := metrics.indexes[payload.OperationID]
			if !exists || metrics.scenario.Intents[index].Client != peerIndex || index >= metrics.scenario.Config.ConflictPairs*2 || payload.Code != "precondition_failed" {
				return fmt.Errorf("client %d received an unexpected rejection (%s)", peerIndex, payload.Code)
			}
			hash, err := projection.Acknowledged.Hash()
			if err != nil || payload.Revision != projection.Acknowledged.Revision || payload.MapHash != hash {
				return fmt.Errorf("rejection authority does not match client state")
			}
			metrics.mu.Lock()
			record := &metrics.records[index]
			if record.start.IsZero() || record.accepted || !record.rejected.IsZero() {
				metrics.mu.Unlock()
				return fmt.Errorf("duplicate or inconsistent rejection")
			}
			record.rejected = received
			metrics.updateCompletionLocked(index)
			metrics.mu.Unlock()
		case protocol.ServerPresenceUpdate:
			payload := message.Payload.(*protocol.ServerPresenceUpdatePayload)
			recordPresenceDelivery(peers, metrics, peerIndex, payload.ActorID, payload.Sequence)
		case protocol.ServerPresenceSnapshot:
			for _, presence := range message.Payload.(*protocol.PresenceSnapshotPayload).Participants {
				recordPresenceDelivery(peers, metrics, peerIndex, presence.ActorID, presence.Sequence)
			}
		case protocol.ServerPong:
		case protocol.ServerSessionNotice:
			return fmt.Errorf("client %d received session notice %s", peerIndex, message.Payload.(*protocol.SessionNoticePayload).Code)
		default:
			return fmt.Errorf("client %d received unexpected %s after join", peerIndex, message.Envelope.Type)
		}
		select {
		case notify <- struct{}{}:
		default:
		}
	}
}

func recordPresenceDelivery(peers []*concurrentPeer, metrics *concurrentMeasurements, receiver int, actor model.ActorID, sequence uint64) {
	if sequence == 0 || sequence > uint64(metrics.scenario.Config.PresencePerClient) {
		return
	}
	for index, peer := range peers {
		if peer.actor == actor {
			metrics.mu.Lock()
			metrics.presenceSeen[index][sequence-1].add(receiver)
			metrics.mu.Unlock()
			return
		}
	}
}
