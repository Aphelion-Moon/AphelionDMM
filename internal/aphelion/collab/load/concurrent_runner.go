package load

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/coder/websocket"
	"sdmm/internal/aphelion/collab/model"
	"sdmm/internal/aphelion/collab/protocol"
)

// RunConcurrent uses fixed absolute offer times and independent client writers.
// A slow send accumulates visible schedule lag; acknowledgements never release
// the next intent. Setup and final HTTP verification are outside latency samples.
// Failure returns partial counts with GatePassed false, never a capacity pass.
func RunConcurrent(ctx context.Context, config RunConfig, scenario ConcurrentScenario) (result ConcurrentResult, runErr error) {
	defer func() {
		if runErr != nil {
			result.GatePassed = false
			if result.Failure == "" {
				result.Failure = runErr.Error()
			}
			result.TimedOut = errors.Is(ctx.Err(), context.DeadlineExceeded)
		}
	}()
	empty := ConcurrentResult{Mode: "concurrent-independent-v1", Seed: scenario.Config.Seed, PlannedOperations: len(scenario.Intents)}
	if _, ok := ctx.Deadline(); !ok {
		return empty, fmt.Errorf("concurrent load requires an overall context deadline")
	}
	if err := validateRunConfig(config); err != nil {
		return empty, err
	}
	if err := scenario.Verify(); err != nil {
		return empty, err
	}
	client := &http.Client{Timeout: 10 * time.Second}
	tokens, err := concurrentTokens(ctx, client, config, len(scenario.Actors))
	if err != nil {
		return empty, err
	}
	peers := make([]*concurrentPeer, 0, len(tokens))
	defer func() {
		for _, peer := range peers {
			_ = peer.connection.CloseNow()
		}
	}()
	for _, token := range tokens {
		peer, err := connectConcurrentPeer(ctx, config, token, scenario.Initial)
		if err != nil {
			return empty, err
		}
		for _, existing := range peers {
			if existing.actor == peer.actor {
				_ = peer.connection.CloseNow()
				return empty, fmt.Errorf("load clients must have distinct authenticated actors")
			}
		}
		peers = append(peers, peer)
	}
	result, err = runConcurrentPeers(ctx, config, scenario, peers)
	if err != nil {
		return result, err
	}
	snapshot, err := fetchSnapshot(ctx, client, config, tokens[0])
	if err == nil {
		result.ServerRevision = snapshot.Revision
		result.ServerMapHash, err = snapshot.Hash()
	}
	if err != nil {
		result.Failure = fmt.Sprintf("verify final server snapshot: %v", err)
		return result, err
	}
	correct := result.SentOperations == len(scenario.Intents) && result.AcceptedOperations == scenario.ExpectedAccepted && result.RejectedOperations == scenario.ExpectedRejected &&
		result.AppliedDeliveries == scenario.ExpectedAccepted*len(peers) && result.UnresolvedOperations == 0 && result.ServerRevision == model.Revision(scenario.ExpectedAccepted) && result.ServerMapHash == scenario.ExpectedMapHash
	for _, peer := range result.Clients {
		correct = correct && peer.Revision == result.ServerRevision && peer.MapHash == result.ServerMapHash && peer.AppliedOperations == scenario.ExpectedAccepted
	}
	if !correct {
		result.Failure = "operation counts or independently applied client/server state differ from the manifest"
		return result, errors.New(result.Failure)
	}
	result.GatePassed = scenario.Config.MaximumP95AcknowledgementMilliseconds == 0 || result.ScheduleToAcknowledgement.P95Millis <= float64(scenario.Config.MaximumP95AcknowledgementMilliseconds)
	if !result.GatePassed {
		result.Failure = "schedule-to-acknowledgement p95 exceeded configured limit"
	}
	return result, nil
}

func concurrentTokens(ctx context.Context, client *http.Client, config RunConfig, count int) ([]string, error) {
	tokens := make([]string, count)
	if len(config.EditorTokens) != 0 && len(config.EditorTokens) != count {
		return nil, fmt.Errorf("hosted credential count must match client count")
	}
	for i := range tokens {
		var err error
		if len(config.EditorTokens) != 0 {
			tokens[i] = config.EditorTokens[i]
			if tokens[i] == "" {
				return nil, fmt.Errorf("hosted editor credential is empty")
			}
			err = provisionHostedEditor(ctx, client, config, tokens[i])
		} else {
			tokens[i], err = mintEditorToken(ctx, client, config, i)
		}
		if err != nil {
			return nil, err
		}
	}
	return tokens, nil
}

func connectConcurrentPeer(ctx context.Context, config RunConfig, token string, initial model.Snapshot) (*concurrentPeer, error) {
	url := "ws" + strings.TrimPrefix(strings.TrimRight(config.Endpoint, "/"), "http") + "/v1/collaboration"
	connection, _, err := websocket.Dial(ctx, url, &websocket.DialOptions{HTTPHeader: http.Header{"Authorization": []string{"Bearer " + token}, "Origin": []string{config.Origin}}, Subprotocols: []string{protocol.BulkSubprotocol, "apheliondmm.collaboration.v1"}})
	if err != nil {
		return nil, err
	}
	connection.SetReadLimit(protocol.MaxMessageBytes)
	peer := &concurrentPeer{connection: connection}
	failed := true
	defer func() {
		if failed {
			_ = connection.CloseNow()
		}
	}()
	if err := writeClientEnvelope(ctx, connection, protocol.ClientEnvelope{ProtocolVersion: model.ProtocolVersion, MessageID: "join", SessionID: config.SessionID, Type: protocol.ClientJoin}, protocol.JoinPayload{JoinToken: token}); err != nil {
		return nil, err
	}
	initialHash, err := initial.Hash()
	if err != nil {
		return nil, err
	}
	joined, replayed, presence := false, false, false
	for !joined || !replayed || !presence {
		kind, data, err := connection.Read(ctx)
		if err != nil {
			return nil, err
		}
		message, err := decodeLoadEnvelope(ctx, connection, kind, data)
		if err != nil {
			return nil, err
		}
		if message.Envelope.SessionID != config.SessionID {
			return nil, fmt.Errorf("joined message belongs to another load session")
		}
		switch message.Envelope.Type {
		case protocol.ServerJoined:
			payload := message.Payload.(*protocol.JoinedPayload)
			if payload.DocumentID != initial.DocumentID || payload.Revision != initial.Revision || payload.MapHash != initialHash || payload.Role != "editor" {
				return nil, fmt.Errorf("joined authority differs from the controlled workload")
			}
			peer.actor, peer.presenceInterval = payload.ActorID, time.Duration(payload.PresenceIntervalMS)*time.Millisecond
			joined = true
		case protocol.ServerReplayComplete:
			payload := message.Payload.(*protocol.ReplayCompletePayload)
			if payload.Revision != initial.Revision || payload.MapHash != initialHash {
				return nil, fmt.Errorf("replay baseline differs from the controlled workload")
			}
			replayed = true
		case protocol.ServerPresenceSnapshot:
			presence = true
		case protocol.ServerPresenceUpdate: // Other load participants may finish joining first.
		default:
			return nil, fmt.Errorf("unexpected %s while joining load workload", message.Envelope.Type)
		}
	}
	failed = false
	return peer, nil
}

func runConcurrentPeers(parent context.Context, config RunConfig, scenario ConcurrentScenario, peers []*concurrentPeer) (ConcurrentResult, error) {
	ctx, cancel := context.WithCancel(parent)
	defer cancel()
	metrics := newConcurrentMeasurements(scenario, time.Now().Add(10*time.Millisecond))
	metrics.sessionID = config.SessionID
	notify := make(chan struct{}, 1)
	var readers, writers sync.WaitGroup
	fail := func(err error) {
		metrics.mu.Lock()
		if metrics.err == nil {
			metrics.err = err
		}
		metrics.mu.Unlock()
		cancel()
	}
	for index := range peers {
		readers.Add(1)
		go func() {
			defer readers.Done()
			if err := readConcurrentPeer(ctx, index, peers, metrics, notify); err != nil && ctx.Err() == nil {
				fail(err)
			}
		}()
		writers.Add(2)
		go func() {
			defer writers.Done()
			if err := writeConcurrentOperations(ctx, config, peers[index], index, metrics); err != nil && ctx.Err() == nil {
				fail(err)
			}
		}()
		go func() {
			defer writers.Done()
			if err := writeConcurrentPresence(ctx, config, peers[index], index, metrics); err != nil && ctx.Err() == nil {
				fail(err)
			}
		}()
	}
	writersDone := make(chan struct{})
	go func() { writers.Wait(); close(writersDone) }()
	var runErr error
	writing := true
	for writing || !metrics.complete() {
		select {
		case <-writersDone:
			writing = false
			writersDone = nil
		case <-notify:
		case <-ctx.Done():
			runErr = ctx.Err()
		}
		if runErr != nil {
			break
		}
	}
	if runErr == nil && scenario.Config.PresencePerClient > 0 {
		// Presence is lossy. Count unique deliveries at this bounded observation
		// cutoff instead of waiting forever or treating missing presence as edits.
		interval := time.Duration(0)
		for _, peer := range peers {
			interval = max(interval, peer.presenceInterval)
		}
		runErr = waitContext(ctx, interval+25*time.Millisecond)
	}
	cancel()
	for _, peer := range peers {
		_ = peer.connection.CloseNow()
	}
	writers.Wait()
	readers.Wait()
	metrics.mu.Lock()
	if metrics.err != nil {
		runErr = metrics.err
	}
	metrics.mu.Unlock()
	result := metrics.result(time.Now())
	if runErr != nil {
		result.Failure = runErr.Error()
		result.TimedOut = errors.Is(parent.Err(), context.DeadlineExceeded)
	}
	return result, runErr
}

func writeConcurrentOperations(ctx context.Context, config RunConfig, peer *concurrentPeer, client int, metrics *concurrentMeasurements) error {
	for index, intent := range metrics.scenario.Intents {
		if intent.Client != client {
			continue
		}
		if err := waitUntil(ctx, scheduledAt(metrics.epoch, index, metrics.scenario.Config.TargetOperationsPerSecond)); err != nil {
			return err
		}
		peer.writeMu.Lock()
		metrics.mu.Lock()
		metrics.records[index].start = time.Now()
		metrics.mu.Unlock()
		err := writeClientEnvelope(ctx, peer.connection, protocol.ClientEnvelope{ProtocolVersion: model.ProtocolVersion, MessageID: "operation-" + string(intent.Operation.OperationID), SessionID: config.SessionID, Type: protocol.ClientOperationSubmit}, protocol.OperationSubmitPayload{Operation: intent.Operation})
		if err == nil {
			metrics.mu.Lock()
			metrics.records[index].sent = time.Now()
			metrics.updateCompletionLocked(index)
			metrics.mu.Unlock()
		}
		peer.writeMu.Unlock()
		if err != nil {
			return err
		}
	}
	return nil
}

func writeConcurrentPresence(ctx context.Context, config RunConfig, peer *concurrentPeer, client int, metrics *concurrentMeasurements) error {
	count, rate := metrics.scenario.Config.PresencePerClient, metrics.scenario.Config.TargetPresencePerSecondPerEditor
	nextAllowed := metrics.epoch
	for next := 0; next < count; {
		due := scheduledAt(metrics.epoch, next, rate)
		if due.Before(nextAllowed) {
			due = nextAllowed
		}
		if err := waitUntil(ctx, due); err != nil {
			return err
		}
		peer.writeMu.Lock()
		latest := max(next, scheduledCount(metrics.epoch, time.Now(), rate, count)-1)
		metrics.mu.Lock()
		metrics.presenceCoalesced += latest - next
		metrics.mu.Unlock()
		sequence := latest + 1
		cursor := model.Coord{X: (client+sequence)%metrics.scenario.Config.MaxX + 1, Y: (client*2+sequence)%metrics.scenario.Config.MaxY + 1, Z: 1}
		err := writeClientEnvelope(ctx, peer.connection, protocol.ClientEnvelope{ProtocolVersion: model.ProtocolVersion, MessageID: fmt.Sprintf("presence-%d", sequence), SessionID: config.SessionID, Type: protocol.ClientPresenceUpdate}, protocol.PresenceUpdatePayload{Sequence: uint64(sequence), Cursor: &cursor, Status: "active"})
		if err == nil {
			metrics.mu.Lock()
			metrics.presenceSent[client][latest] = true
			metrics.mu.Unlock()
		}
		nextAllowed = time.Now().Add(peer.presenceInterval)
		peer.writeMu.Unlock()
		if err != nil {
			return err
		}
		next = sequence
	}
	return nil
}

func waitUntil(ctx context.Context, due time.Time) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if duration := time.Until(due); duration > 0 {
		return waitContext(ctx, duration)
	}
	return nil
}
