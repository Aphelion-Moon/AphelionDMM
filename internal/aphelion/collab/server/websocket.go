package server

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/coder/websocket"

	"sdmm/internal/aphelion/collab/bulktransport"
	"sdmm/internal/aphelion/collab/engine"
	"sdmm/internal/aphelion/collab/model"
	"sdmm/internal/aphelion/collab/protocol"
	collabstore "sdmm/internal/aphelion/collab/store"
	collabtelemetry "sdmm/internal/aphelion/collab/telemetry"
	"sdmm/internal/aphelion/collab/transaction"
)

const webSocketIOTimeout = 5 * time.Second

func (service *Service) handleWebSocket(writer http.ResponseWriter, request *http.Request) {
	if !service.originAllowed(request.Header.Get("Origin")) {
		writeError(writer, http.StatusForbidden, "origin_rejected", "WebSocket origin is not allowed")
		return
	}
	if !headerContains(request.Header.Values("Sec-WebSocket-Protocol"), WebSocketSubprotocol) && !headerContains(request.Header.Values("Sec-WebSocket-Protocol"), protocol.BulkSubprotocol) {
		writeError(writer, http.StatusBadRequest, "protocol_required", "WebSocket protocol version is required")
		return
	}
	if !service.joinLimiter.Allow(remoteIP(request), service.config.Now()) {
		writeError(writer, http.StatusTooManyRequests, "join_rate_limited", "collaboration join rate exceeded")
		return
	}
	service.mutex.RLock()
	tokenValue := bearerToken(request)
	auth, authenticated := service.tokens[tokenValue]
	service.mutex.RUnlock()
	if (!authenticated || auth.launch || auth.webSocketRedeemed || !service.config.Now().Before(auth.expiresAt)) && service.config.HostedAuth != nil {
		hostedSession, err := service.config.HostedAuth.Authorize(request.Context(), tokenValue)
		if err == nil {
			principal, principalErr := principalFromHosted(hostedSession)
			if principalErr == nil {
				auth = tokenRecord{hosted: true, hostedCredential: tokenValue, principal: principal, expiresAt: hostedSession.ExpiresAt}
				authenticated = true
			}
		}
	}
	if !authenticated || auth.launch || auth.webSocketRedeemed || !service.config.Now().Before(auth.expiresAt) {
		writeError(writer, http.StatusUnauthorized, "unauthorized", "session token is invalid")
		return
	}
	if !service.acquireConnection() {
		writeError(writer, http.StatusTooManyRequests, "connection_limit", "collaboration connection limit reached")
		return
	}
	defer service.releaseConnection()

	connection, err := websocket.Accept(writer, request, &websocket.AcceptOptions{
		Subprotocols:    []string{protocol.BulkSubprotocol, WebSocketSubprotocol},
		CompressionMode: websocket.CompressionDisabled,
		// Origin was matched exactly against the configured allowlist before the upgrade.
		InsecureSkipVerify: true,
	})
	if err != nil {
		return
	}
	if service.telemetry != nil {
		service.telemetry.ConnectionChanged(request.Context(), 1)
		defer service.telemetry.ConnectionChanged(context.Background(), -1)
	}
	connection.SetReadLimit(service.limits.MaxWebSocketMessageBytes)
	defer func() { _ = connection.CloseNow() }()
	// Accept hijacks the HTTP connection, so the service context owns the WebSocket lifetime.
	if err := service.serveWebSocket(service.context, connection, tokenValue, auth); err != nil && service.config.OnWebSocketError != nil {
		service.config.OnWebSocketError(err)
	}
}

func (service *Service) acquireConnection() bool {
	service.mutex.Lock()
	defer service.mutex.Unlock()
	if service.activeConnections >= service.limits.MaxConnections {
		return false
	}
	service.activeConnections++
	return true
}

func (service *Service) releaseConnection() {
	service.mutex.Lock()
	service.activeConnections--
	service.mutex.Unlock()
}

func (service *Service) serveWebSocket(parent context.Context, connection *websocket.Conn, tokenValue string, auth tokenRecord) error {
	parent, cancelConnection := context.WithCancel(parent)
	defer cancelConnection()
	readContext, cancelRead := context.WithTimeout(parent, webSocketIOTimeout)
	_, data, err := connection.Read(readContext)
	cancelRead()
	if err != nil {
		return fmt.Errorf("read join: %w", err)
	}
	decoded, err := protocol.DecodeClient(data)
	if err != nil || decoded.Envelope.Type != protocol.ClientJoin || (!auth.hosted && decoded.Envelope.SessionID != auth.sessionID) {
		_ = connection.Close(websocket.StatusPolicyViolation, "valid join required")
		return fmt.Errorf("validate join envelope: %w", err)
	}
	join := decoded.Payload.(*protocol.JoinPayload)
	if join.JoinToken != tokenValue {
		_ = connection.Close(websocket.StatusPolicyViolation, "join token mismatch")
		return fmt.Errorf("validate join token: mismatch")
	}
	if auth.hosted {
		auth, err = service.reauthorizeHosted(parent, auth.hostedCredential, decoded.Envelope.SessionID)
		if err != nil {
			_ = connection.Close(websocket.StatusPolicyViolation, "session authorization unavailable")
			return fmt.Errorf("authorize hosted session join: %w", err)
		}
	}

	service.mutex.RLock()
	session, exists := service.sessions[auth.sessionID]
	service.mutex.RUnlock()
	if !exists {
		_ = connection.Close(websocket.StatusPolicyViolation, "session unavailable")
		return fmt.Errorf("load joined session: unavailable")
	}
	if session.owner.bulkEdits {
		if connection.Subprotocol() != protocol.BulkSubprotocol {
			_ = connection.Close(websocket.StatusPolicyViolation, "bulk-edit-v2 required")
			return fmt.Errorf("session requires bulk-edit-v2")
		}
		parent = context.WithValue(parent, bulkContextKey{}, bulkConnection{codec: service.bulk, canUpload: auth.principal.CanEdit()})
	}
	durable, cancelDurable, err := session.owner.subscribeSharedDurable(service.limits.DurableQueueDepth)
	if err != nil {
		return fmt.Errorf("subscribe durable operations: %w", err)
	}
	defer cancelDurable()
	presenceSnapshot, presenceUpdates, cancelPresence, err := service.hub.SubscribePresence(auth.sessionID, service.limits.PresenceQueueDepth)
	if err != nil {
		return fmt.Errorf("subscribe presence: %w", err)
	}
	defer cancelPresence()
	defer service.hub.DisconnectPresence(auth.sessionID, auth.principal.ActorID())
	snapshot, err := session.owner.Snapshot(parent)
	if err != nil {
		return fmt.Errorf("load joined snapshot: %w", err)
	}
	if err := service.validateJoinCompatibility(snapshot); err != nil {
		_ = connection.Close(websocket.StatusPolicyViolation, "unsupported collaboration version")
		return fmt.Errorf("negotiate joined version: %w", err)
	}
	mapHash, err := snapshot.Hash()
	if err != nil {
		return fmt.Errorf("hash joined snapshot: %w", err)
	}
	resumptionToken, resumptionTokenExpiresAt := auth.hostedCredential, auth.expiresAt
	if !auth.hosted {
		resumptionToken, resumptionTokenExpiresAt, err = service.rotateResumptionToken(tokenValue, auth.sessionID, auth.principal)
		if err != nil {
			_ = connection.Close(websocket.StatusPolicyViolation, "session credential unavailable")
			return fmt.Errorf("rotate resumption credential: %w", err)
		}
	}
	if err := writeServerEnvelope(parent, connection, protocol.ServerEnvelope{
		ProtocolVersion: model.ProtocolVersion,
		MessageID:       "joined-" + decoded.Envelope.MessageID,
		SessionID:       auth.sessionID,
		Type:            protocol.ServerJoined,
	}, protocol.JoinedPayload{BulkEdits: session.owner.bulkEdits, DocumentID: snapshot.DocumentID, ActorID: auth.principal.ActorID(), Role: string(auth.principal.Role()), Revision: snapshot.Revision, MapHash: mapHash, PresenceIntervalMS: uint32(service.config.PresenceInterval / time.Millisecond), ResumptionToken: resumptionToken, ResumptionTokenExpiresAt: resumptionTokenExpiresAt}); err != nil {
		return fmt.Errorf("write joined message: %w", err)
	}
	if pages, ok := service.store.(collabstore.ReplayPageStore); ok {
		replayContext := parent
		finishReplay := func(error) {}
		if service.telemetry != nil {
			count := 0
			if snapshot.Revision >= join.AcknowledgedRevision {
				count = int(snapshot.Revision - join.AcknowledgedRevision)
			}
			replayContext, finishReplay = service.telemetry.Replay(parent, count)
		}
		err := service.writePagedReplay(replayContext, connection, pages, auth.sessionID, snapshot, join.AcknowledgedRevision, mapHash, decoded.Envelope.MessageID)
		finishReplay(err)
		if errors.Is(err, errReplayNeedsSnapshot) {
			return nil
		}
		if err != nil {
			return err
		}
	} else {
		loadContext := parent
		finishLoad := func(error) {}
		if service.telemetry != nil {
			loadContext, finishLoad = service.telemetry.Store(parent, collabtelemetry.StoreLoad)
		}
		var retainedSnapshot model.Snapshot
		var replay []model.AcceptedOperation
		var replayHashes map[model.Revision]string
		batchedReplay, hasBatchedReplay := service.store.(collabstore.ReplayStore)
		if hasBatchedReplay {
			retainedSnapshot, replay, replayHashes, err = batchedReplay.LoadReplay(loadContext, snapshot.DocumentID)
		} else {
			retainedSnapshot, replay, err = service.store.Load(loadContext, snapshot.DocumentID)
		}
		finishLoad(err)
		if err != nil {
			return fmt.Errorf("load reconnect replay: %w", err)
		}
		if join.AcknowledgedRevision < retainedSnapshot.Revision {
			if err := writeServerEnvelope(parent, connection, protocol.ServerEnvelope{ProtocolVersion: model.ProtocolVersion, MessageID: "snapshot-required-" + decoded.Envelope.MessageID, SessionID: auth.sessionID, Type: protocol.ServerSessionNotice}, protocol.SessionNoticePayload{Code: protocol.NoticeSnapshotRequired, Message: "authoritative snapshot is required before replay"}); err != nil {
				return fmt.Errorf("write snapshot-required notice: %w", err)
			}
			_ = connection.Close(websocket.StatusServiceRestart, "snapshot required")
			return nil
		}
		replayCount := 0
		for _, accepted := range replay {
			if accepted.Revision > join.AcknowledgedRevision && accepted.Revision <= snapshot.Revision {
				replayCount++
			}
		}
		replayContext := parent
		finishReplay := func(error) {}
		if service.telemetry != nil {
			replayContext, finishReplay = service.telemetry.Replay(parent, replayCount)
		}
		for _, accepted := range replay {
			if accepted.Revision <= join.AcknowledgedRevision || accepted.Revision > snapshot.Revision {
				continue
			}
			acceptedHash, found := replayHashes[accepted.Revision]
			var hashErr error
			if !hasBatchedReplay {
				acceptedHash, found, hashErr = service.store.RevisionHash(replayContext, snapshot.DocumentID, accepted.Revision)
			}
			if hashErr != nil {
				finishReplay(hashErr)
				return fmt.Errorf("load replay hash at revision %d: %w", accepted.Revision, hashErr)
			}
			if !found {
				err := fmt.Errorf("replay hash at revision %d is not retained", accepted.Revision)
				finishReplay(err)
				return err
			}
			if err := writeServerEnvelope(replayContext, connection, protocol.ServerEnvelope{ProtocolVersion: model.ProtocolVersion, MessageID: "replay-" + string(accepted.OperationID), SessionID: auth.sessionID, Type: protocol.ServerOperationAccepted}, protocol.OperationAcceptedPayload{Operation: accepted, MapHash: acceptedHash}); err != nil {
				finishReplay(err)
				return fmt.Errorf("write replay operation: %w", err)
			}
		}
		if err := writeServerEnvelope(replayContext, connection, protocol.ServerEnvelope{ProtocolVersion: model.ProtocolVersion, MessageID: "replay-complete-" + decoded.Envelope.MessageID, SessionID: auth.sessionID, Type: protocol.ServerReplayComplete}, protocol.ReplayCompletePayload{Revision: snapshot.Revision, MapHash: mapHash}); err != nil {
			finishReplay(err)
			return fmt.Errorf("write replay completion: %w", err)
		}
		finishReplay(nil)
	}
	participants := make([]protocol.ParticipantPresence, 0, len(presenceSnapshot))
	for _, presence := range presenceSnapshot {
		participants = append(participants, protocol.ParticipantPresence{ActorID: presence.ActorID, DisplayName: presence.DisplayName, Sequence: presence.Sequence, Cursor: presence.Cursor, Selection: presence.Selection, Status: presence.Status})
	}
	if err := writeServerEnvelope(parent, connection, protocol.ServerEnvelope{ProtocolVersion: model.ProtocolVersion, MessageID: "presence-snapshot-" + decoded.Envelope.MessageID, SessionID: auth.sessionID, Type: protocol.ServerPresenceSnapshot}, protocol.PresenceSnapshotPayload{Participants: participants}); err != nil {
		return fmt.Errorf("write presence snapshot: %w", err)
	}

	incoming := make(chan incomingMessage)
	readErrors := make(chan error, 1)
	go func() {
		readClientMessages(parent, connection, auth.sessionID, incoming, readErrors)
		// A handler may be validating an edit while the reader observes EOF.
		// Cancel it immediately rather than waiting for the handler's next select.
		// Append reconciliation still uses its separate post-commit context.
		cancelConnection()
	}()
	var reauthorization <-chan time.Time
	var reauthorizationTicker *time.Ticker
	if auth.hosted {
		reauthorizationTicker = time.NewTicker(service.config.HostedReauthorizationInterval)
		defer reauthorizationTicker.Stop()
		reauthorization = reauthorizationTicker.C
	}
	sentRevision := snapshot.Revision
	writeAccepted := func(event *durableEvent) error {
		accepted := event.accepted
		if accepted.Revision <= sentRevision {
			return nil
		}
		if accepted.Revision != sentRevision+1 {
			return fmt.Errorf("durable revision gap: got %d after %d", accepted.Revision, sentRevision)
		}
		acceptedHash, found, hashErr := service.store.RevisionHash(parent, accepted.DocumentID, accepted.Revision)
		if hashErr != nil {
			return fmt.Errorf("load durable hash: %w", hashErr)
		}
		if !found {
			return fmt.Errorf("durable hash at revision %d is not retained", accepted.Revision)
		}
		if err := writeServerEnvelope(parent, connection, protocol.ServerEnvelope{ProtocolVersion: model.ProtocolVersion, MessageID: "accepted-" + string(accepted.OperationID), SessionID: auth.sessionID, Type: protocol.ServerOperationAccepted}, protocol.OperationAcceptedPayload{Operation: accepted, MapHash: acceptedHash}); err != nil {
			return err
		}
		sentRevision = accepted.Revision
		return nil
	}
	flushThrough := func(revision model.Revision) error {
		for sentRevision < revision {
			select {
			case accepted, open := <-durable:
				if !open {
					_ = connection.Close(CloseSlowConsumer, "durable consumer fell behind")
					return fmt.Errorf("durable subscriber fell behind")
				}
				if err := writeAccepted(accepted); err != nil {
					return err
				}
			case <-parent.Done():
				return parent.Err()
			}
		}
		return nil
	}
	for {
		select {
		case <-parent.Done():
			return nil
		case err := <-readErrors:
			if websocket.CloseStatus(err) == websocket.StatusNormalClosure || errors.Is(err, context.Canceled) {
				return nil
			}
			return fmt.Errorf("read client message: %w", err)
		case <-reauthorization:
			auth, err = service.reauthorizeHosted(parent, auth.hostedCredential, auth.sessionID)
			if err != nil {
				_ = connection.Close(websocket.StatusPolicyViolation, "session authorization revoked")
				return fmt.Errorf("reauthorize hosted connection: %w", err)
			}
		case accepted, open := <-durable:
			if !open {
				_ = connection.Close(CloseSlowConsumer, "durable consumer fell behind")
				return fmt.Errorf("durable subscriber fell behind")
			}
			if err := writeAccepted(accepted); err != nil {
				return err
			}
		case presence, open := <-presenceUpdates:
			if !open {
				return nil
			}
			payload := protocol.ServerPresenceUpdatePayload{ActorID: presence.ActorID, DisplayName: presence.DisplayName, Sequence: presence.Sequence, Cursor: presence.Cursor, Selection: presence.Selection, Status: presence.Status}
			if err := writeServerEnvelope(parent, connection, protocol.ServerEnvelope{ProtocolVersion: model.ProtocolVersion, MessageID: fmt.Sprintf("presence-%s-%d", presence.ActorID, presence.Sequence), SessionID: auth.sessionID, Type: protocol.ServerPresenceUpdate}, payload); err != nil {
				return fmt.Errorf("write presence update: %w", err)
			}
		case message := <-incoming:
			if auth.hosted {
				auth, err = service.reauthorizeHosted(parent, auth.hostedCredential, auth.sessionID)
				if err != nil {
					message.release()
					_ = connection.Close(websocket.StatusPolicyViolation, "session authorization revoked")
					return fmt.Errorf("reauthorize hosted message: %w", err)
				}
			}
			err := service.handleClientMessage(parent, connection, session, auth, message.decoded, flushThrough)
			message.release()
			if err != nil {
				return err
			}
		}
	}
}

func (service *Service) validateJoinCompatibility(snapshot model.Snapshot) error {
	_, err := service.compatibility.Negotiate(snapshot.ProtocolVersion, snapshot.SchemaVersion)
	return err
}

type incomingMessage struct {
	decoded protocol.DecodedClient
	release func()
}

func readClientMessages(ctx context.Context, connection *websocket.Conn, sessionID string, messages chan<- incomingMessage, readErrors chan<- error) {
	for {
		readContext, cancel := context.WithTimeout(ctx, 2*webSocketIOTimeout)
		kind, data, err := connection.Read(readContext)
		cancel()
		if err != nil {
			readErrors <- err
			return
		}
		var message protocol.DecodedClient
		release := func() {}
		if kind == websocket.MessageBinary {
			codec := bulkCodec(ctx)
			if codec == nil || !bulkUploadAllowed(ctx) {
				err = fmt.Errorf("unnegotiated bulk submission")
			} else {
				h, op, lease, readErr := codec.ReadAdmitted(ctx, connection, data, 2*webSocketIOTimeout)
				if lease != nil {
					release = lease
				}
				err = readErr
				if err == nil {
					message, err = bulktransport.Submission(h, op)
				}
			}
		} else {
			message, err = protocol.DecodeClientForSession(data, bulkCodec(ctx) != nil)
		}
		if err != nil || message.Envelope.SessionID != sessionID {
			release()
			if err == nil {
				err = fmt.Errorf("message session id does not match authenticated session")
			}
			var admission *transaction.AdmissionError
			if errors.As(err, &admission) {
				_ = connection.Close(websocket.StatusTryAgainLater, admission.Error())
			} else {
				_ = connection.Close(websocket.StatusPolicyViolation, "invalid client message")
			}
			readErrors <- err
			return
		}
		select {
		case messages <- incomingMessage{decoded: message, release: release}:
		case <-ctx.Done():
			release()
			return
		}
	}
}

func (service *Service) handleClientMessage(ctx context.Context, connection *websocket.Conn, session sessionRecord, auth tokenRecord, message protocol.DecodedClient, flushThrough func(model.Revision) error) error {
	switch message.Envelope.Type {
	case protocol.ClientPing:
		ping := message.Payload.(*protocol.PingPayload)
		return writeServerEnvelope(ctx, connection, protocol.ServerEnvelope{ProtocolVersion: model.ProtocolVersion, MessageID: "pong-" + message.Envelope.MessageID, SessionID: auth.sessionID, Type: protocol.ServerPong}, protocol.PongPayload{Nonce: ping.Nonce})
	case protocol.ClientPresenceUpdate:
		if !service.presenceLimiter.Allow(string(auth.principal.ActorID()), service.config.Now()) {
			_ = connection.Close(CloseRateLimited, "presence rate exceeded")
			return fmt.Errorf("presence rate exceeded for actor %q", auth.principal.ActorID())
		}
		presence := message.Payload.(*protocol.PresenceUpdatePayload)
		if err := service.hub.UpdatePresence(auth.sessionID, auth.principal, PresenceUpdate{Sequence: presence.Sequence, Cursor: presence.Cursor, Selection: presence.Selection, Status: presence.Status}); err != nil {
			return fmt.Errorf("update presence: %w", err)
		}
		return nil
	case protocol.ClientProfileUpdate:
		profile := message.Payload.(*protocol.ProfileUpdatePayload)
		if auth.hosted && service.config.HostedRegistry != nil {
			if err := service.config.HostedRegistry.UpdateHostedMemberDisplayName(ctx, auth.sessionID, auth.principal.ActorID(), profile.DisplayName); err != nil {
				return fmt.Errorf("persist hosted display name: %w", err)
			}
		}
		if err := service.hub.UpdateDisplayName(auth.sessionID, auth.principal, profile.DisplayName); err != nil {
			return fmt.Errorf("update display name: %w", err)
		}
		return nil
	case protocol.ClientOperationSubmit:
		submission := message.Payload.(*protocol.OperationSubmitPayload)
		if !session.owner.bulkEdits && len(submission.Operation.Changes) > service.limits.MaxOperationChanges {
			current, snapshotErr := session.owner.Snapshot(ctx)
			if snapshotErr != nil {
				return fmt.Errorf("load limit rejection snapshot: %w", snapshotErr)
			}
			if err := flushThrough(current.Revision); err != nil {
				return err
			}
			currentHash, _ := current.Hash()
			return writeServerEnvelope(ctx, connection, protocol.ServerEnvelope{ProtocolVersion: model.ProtocolVersion, MessageID: "rejected-" + message.Envelope.MessageID, SessionID: auth.sessionID, Type: protocol.ServerOperationRejected}, protocol.OperationRejectedPayload{OperationID: submission.Operation.OperationID, Code: "limit_exceeded", Message: "operation was rejected", Revision: current.Revision, MapHash: currentHash})
		}
		if !service.durableLimiter.Allow(string(auth.principal.ActorID()), service.config.Now()) {
			_ = connection.Close(CloseRateLimited, "durable operation rate exceeded")
			return fmt.Errorf("durable operation rate exceeded for actor %q", auth.principal.ActorID())
		}
		operationContext := ctx
		finishOperation := func(error) {}
		if service.telemetry != nil {
			operationContext, finishOperation = service.telemetry.Operation(ctx, collabtelemetry.OperationSubmit)
		}
		accepted, duplicate, err := service.hub.SubmitWithStatus(operationContext, auth.sessionID, auth.principal, submission.Operation)
		finishOperation(err)
		if err != nil {
			current, snapshotErr := session.owner.Snapshot(ctx)
			if snapshotErr != nil {
				return fmt.Errorf("load rejected snapshot: %w", snapshotErr)
			}
			if err := flushThrough(current.Revision); err != nil {
				return err
			}
			currentHash, _ := current.Hash()
			return writeServerEnvelope(ctx, connection, protocol.ServerEnvelope{ProtocolVersion: model.ProtocolVersion, MessageID: "rejected-" + message.Envelope.MessageID, SessionID: auth.sessionID, Type: protocol.ServerOperationRejected}, protocol.OperationRejectedPayload{OperationID: submission.Operation.OperationID, Code: rejectionCode(err, "operation_rejected"), Message: "operation was rejected", Revision: current.Revision, MapHash: currentHash, AuthoritativeValues: authoritativeValues(current, submission.Operation.Changes)})
		}
		if duplicate {
			if err := flushThrough(accepted.Revision); err != nil {
				return err
			}
			acceptedHash, found, hashErr := service.store.RevisionHash(ctx, accepted.DocumentID, accepted.Revision)
			if hashErr != nil {
				return fmt.Errorf("load duplicate hash: %w", hashErr)
			}
			if !found {
				return fmt.Errorf("duplicate hash at revision %d is not retained", accepted.Revision)
			}
			return writeServerEnvelope(ctx, connection, protocol.ServerEnvelope{ProtocolVersion: model.ProtocolVersion, MessageID: "duplicate-" + message.Envelope.MessageID, SessionID: auth.sessionID, Type: protocol.ServerOperationAccepted}, protocol.OperationAcceptedPayload{Operation: accepted, MapHash: acceptedHash})
		}
		return nil
	case protocol.ClientAcknowledgedRevision:
		return nil
	case protocol.ClientInverseRequest:
		if !service.durableLimiter.Allow(string(auth.principal.ActorID()), service.config.Now()) {
			_ = connection.Close(CloseRateLimited, "durable operation rate exceeded")
			return fmt.Errorf("durable operation rate exceeded for actor %q", auth.principal.ActorID())
		}
		inverse := message.Payload.(*protocol.InverseRequestPayload)
		operationContext := ctx
		finishOperation := func(error) {}
		if service.telemetry != nil {
			operationContext, finishOperation = service.telemetry.Operation(ctx, collabtelemetry.OperationInverse)
		}
		_, err := service.hub.Inverse(operationContext, auth.sessionID, auth.principal, inverse.OperationID)
		finishOperation(err)
		if err != nil {
			var admission *transaction.AdmissionError
			if errors.As(err, &admission) {
				// Do not materialize the rejected inverse's target or a full
				// conflict snapshot after working-memory admission has failed.
				_ = connection.Close(websocket.StatusTryAgainLater, admission.Error())
				return err
			}
			current, snapshotErr := session.owner.Snapshot(ctx)
			if snapshotErr != nil {
				return fmt.Errorf("load inverse rejection snapshot: %w", snapshotErr)
			}
			if err := flushThrough(current.Revision); err != nil {
				return err
			}
			currentHash, _ := current.Hash()
			var changes []model.TileChange
			if target, found, lookupErr := service.store.LookupOperation(ctx, current.DocumentID, inverse.OperationID); lookupErr == nil && found {
				changes = target.Changes
			}
			return writeServerEnvelope(ctx, connection, protocol.ServerEnvelope{ProtocolVersion: model.ProtocolVersion, MessageID: "inverse-rejected-" + message.Envelope.MessageID, SessionID: auth.sessionID, Type: protocol.ServerOperationRejected}, protocol.OperationRejectedPayload{OperationID: inverse.OperationID, Code: rejectionCode(err, "inverse_rejected"), Message: "inverse was rejected", Revision: current.Revision, MapHash: currentHash, AuthoritativeValues: authoritativeValues(current, changes)})
		}
		return nil
	default:
		return fmt.Errorf("unsupported client message %q", message.Envelope.Type)
	}
}

func rejectionCode(err error, fallback string) string {
	if errors.Is(err, errDeliveryLimit) {
		return "limit_exceeded"
	}
	if code := engine.CodeOf(err); code != "" {
		return string(code)
	}
	return fallback
}

func authoritativeValues(snapshot model.Snapshot, changes []model.TileChange) []model.Tile {
	states := make(map[model.Coord]model.TileState, len(snapshot.Tiles))
	for _, tile := range snapshot.Tiles {
		states[tile.Coord] = tile.State
	}
	seen := make(map[model.Coord]struct{}, len(changes))
	values := make([]model.Tile, 0, len(changes))
	for _, change := range changes {
		if _, exists := seen[change.Coord]; exists {
			continue
		}
		seen[change.Coord] = struct{}{}
		values = append(values, model.Tile{Coord: change.Coord, State: model.CloneTileState(states[change.Coord])})
	}
	return values
}

func (service *Service) originAllowed(origin string) bool {
	if origin == "" {
		return false
	}
	for _, allowed := range service.config.AllowedOrigins {
		if origin == allowed {
			return true
		}
	}
	return false
}

func headerContains(values []string, target string) bool {
	for _, value := range values {
		for _, item := range strings.Split(value, ",") {
			if strings.TrimSpace(item) == target {
				return true
			}
		}
	}
	return false
}

func writeServerEnvelope(ctx context.Context, connection *websocket.Conn, envelope protocol.ServerEnvelope, payload any) error {
	// Preserve a small JSON fast path; large transactions use streamed frames.
	if codec := bulkCodec(ctx); codec != nil && envelope.Type == protocol.ServerOperationAccepted {
		accepted, ok := payload.(protocol.OperationAcceptedPayload)
		if !ok {
			return fmt.Errorf("invalid bulk acceptance payload")
		}
		if len(envelope.MessageID) > protocol.MaxIdentifierBytes {
			envelope.MessageID = fmt.Sprintf("response-%x", sha256.Sum256([]byte(envelope.MessageID)))
		}
		if len(accepted.Operation.Changes) > 64 {
			return codec.Write(ctx, connection, bulktransport.AcceptanceHeader(envelope, accepted), accepted.Operation.Changes, webSocketIOTimeout)
		}
		data, err := marshalServerEnvelope(envelope, accepted)
		if err != nil {
			return err
		}
		if len(data) > protocol.MaxMessageBytes {
			return codec.Write(ctx, connection, bulktransport.AcceptanceHeader(envelope, accepted), accepted.Operation.Changes, webSocketIOTimeout)
		}
	}
	if rejection, ok := payload.(protocol.OperationRejectedPayload); ok && len(rejection.AuthoritativeValues) > protocol.MaxOperationChanges {
		rejection.AuthoritativeValues = nil
		payload = rejection
	}
	data, err := marshalServerEnvelope(envelope, payload)
	if err != nil {
		return err
	}
	if len(data) > protocol.MaxMessageBytes {
		// Conflict details are optional. Keep the operation ID and authoritative
		// revision/hash intact so clients can resolve their pending draft, even
		// when the current tiles are much larger than the rejected request.
		if rejection, ok := payload.(protocol.OperationRejectedPayload); ok && envelope.Type == protocol.ServerOperationRejected {
			rejection.AuthoritativeValues = nil
			data, err = marshalServerEnvelope(envelope, rejection)
			if err != nil {
				return err
			}
		}
		if len(data) > protocol.MaxMessageBytes {
			return fmt.Errorf("server message is %d bytes, maximum is %d", len(data), protocol.MaxMessageBytes)
		}
	}
	writeContext, cancel := context.WithTimeout(ctx, webSocketIOTimeout)
	defer cancel()
	return connection.Write(writeContext, websocket.MessageText, data)
}

func marshalServerEnvelope(envelope protocol.ServerEnvelope, payload any) ([]byte, error) {
	// Reply prefixes can push a valid client ID past the wire limit. Keep the
	// normal IDs unchanged and use a stable bounded identity for long replies.
	if len(envelope.MessageID) > protocol.MaxIdentifierBytes {
		envelope.MessageID = fmt.Sprintf("response-%x", sha256.Sum256([]byte(envelope.MessageID)))
	}
	payloadJSON, err := json.Marshal(payload)
	if err != nil {
		return nil, err
	}
	envelope.Payload = payloadJSON
	return json.Marshal(envelope)
}
