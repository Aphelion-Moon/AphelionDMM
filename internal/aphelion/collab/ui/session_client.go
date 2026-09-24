package ui

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/coder/websocket"

	collabclient "sdmm/internal/aphelion/collab/client"
	"sdmm/internal/aphelion/collab/executor"
	"sdmm/internal/aphelion/collab/model"
	"sdmm/internal/aphelion/collab/protocol"
)

const maxSessionResponseBytes = 256 << 20

var errSnapshotFallbackInstalled = errors.New("authoritative snapshot installed; reconnect again for replay")

type SessionClientConfig struct {
	HTTPTimeout  time.Duration
	Transport    collabclient.TransportConfig
	Reconnect    collabclient.ReconnectPolicy
	Now          func() time.Time
	Schedule     func(time.Duration, func()) func()
	NewTransport func() SessionTransport
}

type SessionClient struct {
	config SessionClientConfig
	http   *http.Client

	mutex                   sync.Mutex
	joining                 bool
	transport               sessionTransport
	network                 *collabclient.NetworkExecutor
	machine                 *collabclient.StateMachine
	sessionID               string
	role                    string
	revision                model.Revision
	participants            map[model.ActorID]ObservedPresence
	presenceInterval        time.Duration
	presenceSequence        uint64
	profileSequence         uint64
	nextPresenceAt          time.Time
	pendingPresence         *protocol.PresenceUpdatePayload
	cancelPresence          func()
	presenceGeneration      uint64
	resumptionToken         string
	resumptionExpiresAt     time.Time
	administrationToken     string
	administrationExpiresAt time.Time
	hostedCredential        string
	hostedCredentialExpires time.Time
	hostedDisplayName       string
	hostedBaseURL           string
	hostedSession           bool
	baseURL                 string
	origin                  string
	documentID              model.DocumentID
	actorID                 model.ActorID
	cancelReconnect         context.CancelFunc
	reconnectContext        context.Context
	reconnecting            bool
	newTransport            func() SessionTransport
	lastErr                 error
}

// SessionTransport is a collaboration transport whose terminal result can be observed by the session lifecycle.
type SessionTransport interface {
	collabclient.Transport
	Wait(context.Context) error
}

type sessionTransport = SessionTransport

func NewSessionClient(config SessionClientConfig) *SessionClient {
	if config.HTTPTimeout <= 0 {
		config.HTTPTimeout = 10 * time.Second
	}
	if config.Now == nil {
		config.Now = time.Now
	}
	if config.Schedule == nil {
		config.Schedule = func(delay time.Duration, job func()) func() {
			timer := time.AfterFunc(delay, job)
			return func() { timer.Stop() }
		}
	}
	if config.NewTransport == nil {
		config.NewTransport = func() SessionTransport {
			return collabclient.NewWebSocketTransport(config.Transport)
		}
	}
	return &SessionClient{config: config, machine: collabclient.NewStateMachine(), participants: make(map[model.ActorID]ObservedPresence), newTransport: config.NewTransport, http: &http.Client{Timeout: config.HTTPTimeout, CheckRedirect: func(*http.Request, []*http.Request) error {
		return http.ErrUseLastResponse
	}}}
}

func (client *SessionClient) Create(ctx context.Context, baseURL, launchToken string, snapshot model.Snapshot) (Invitation, error) {
	return client.CreateNamed(ctx, baseURL, launchToken, snapshot, "Owner")
}

func (client *SessionClient) CreateNamed(ctx context.Context, baseURL, launchToken string, snapshot model.Snapshot, displayName string) (Invitation, error) {
	if launchToken == "" {
		return Invitation{}, fmt.Errorf("collaboration launch token is empty")
	}
	displayName = strings.TrimSpace(displayName)
	if displayName == "" || len(displayName) > protocol.MaxDisplayNameBytes {
		return Invitation{}, fmt.Errorf("collaboration owner display name is invalid")
	}
	body, err := json.Marshal(map[string]any{"snapshot": snapshot, "display_name": displayName, "bulk_edits": true})
	if err != nil {
		return Invitation{}, err
	}
	request, err := client.request(ctx, http.MethodPost, baseURL+"/v1/sessions", launchToken, bytes.NewReader(body))
	if err != nil {
		return Invitation{}, err
	}
	request.Header.Set("Content-Type", "application/json")
	response, err := client.http.Do(request)
	if err != nil {
		return Invitation{}, fmt.Errorf("create collaboration session: %w", err)
	}
	defer func() { _ = response.Body.Close() }()
	if response.StatusCode != http.StatusCreated {
		if response.StatusCode == http.StatusRequestEntityTooLarge {
			return Invitation{}, fmt.Errorf("create collaboration session returned HTTP %d: snapshot request is %d bytes", response.StatusCode, len(body))
		}
		return Invitation{}, fmt.Errorf("create collaboration session returned HTTP %d", response.StatusCode)
	}
	var created struct {
		SessionID           string           `json:"session_id"`
		DocumentID          model.DocumentID `json:"document_id"`
		Revision            model.Revision   `json:"revision"`
		MapHash             string           `json:"map_hash"`
		OwnerToken          string           `json:"owner_token"`
		OwnerTokenExpiresAt time.Time        `json:"owner_token_expires_at"`
	}
	if err := decodeLimited(response.Body, &created); err != nil {
		return Invitation{}, fmt.Errorf("decode collaboration session: %w", err)
	}
	if created.SessionID == "" || created.OwnerToken == "" || created.DocumentID != snapshot.DocumentID {
		return Invitation{}, fmt.Errorf("collaboration session response is incompatible with the requested document")
	}
	return Invitation{BaseURL: strings.TrimRight(baseURL, "/"), Origin: strings.TrimRight(baseURL, "/"), SessionID: created.SessionID, Token: created.OwnerToken, TokenExpiresAt: created.OwnerTokenExpiresAt}, nil
}

func (client *SessionClient) Join(ctx context.Context, invitation Invitation) error {
	if err := invitation.validate(); err != nil {
		return err
	}
	client.mutex.Lock()
	if client.joining || client.transport != nil {
		client.mutex.Unlock()
		return ErrSessionActive
	}
	client.joining = true
	client.machine = collabclient.NewStateMachine()
	_ = client.machine.Apply(collabclient.EventConnect)
	client.sessionID = invitation.SessionID
	client.baseURL = invitation.BaseURL
	client.origin = invitation.Origin
	client.role = ""
	client.revision = 0
	client.participants = make(map[model.ActorID]ObservedPresence)
	client.stopPresencePublicationLocked()
	client.presenceInterval = 0
	client.presenceSequence = 0
	client.nextPresenceAt = time.Time{}
	client.resumptionToken = ""
	client.resumptionExpiresAt = time.Time{}
	client.administrationToken = ""
	client.administrationExpiresAt = time.Time{}
	client.lastErr = nil
	client.reconnecting = false
	if client.cancelReconnect != nil {
		client.cancelReconnect()
	}
	reconnectContext, cancelReconnect := context.WithCancel(context.Background())
	client.reconnectContext = reconnectContext
	client.cancelReconnect = cancelReconnect
	machine := client.machine
	client.mutex.Unlock()
	joined := false
	defer func() {
		if !joined {
			client.mutex.Lock()
			if client.machine == machine {
				client.joining = false
				if client.cancelReconnect != nil {
					client.cancelReconnect()
					client.cancelReconnect = nil
				}
				client.reconnectContext = nil
				client.resumptionToken = ""
				client.resumptionExpiresAt = time.Time{}
			}
			client.mutex.Unlock()
		}
	}()

	snapshot, err := client.fetchSnapshot(ctx, invitation)
	if err != nil {
		client.recordConnectionFailure(machine, err)
		return err
	}
	administrationToken := invitation.Token
	joinToken := invitation.Token
	var transport sessionTransport
	var joinedNetwork *collabclient.NetworkExecutor
	var synchronizedRevision model.Revision
	var joinedTransportEnded <-chan error
	var cancelJoinedTransportWait context.CancelFunc
	maxSnapshotFallbacks := client.config.Reconnect.MaxAttempts
	if maxSnapshotFallbacks <= 0 {
		maxSnapshotFallbacks = 8
	}
	snapshotFallbacks := 0
	refreshSnapshot := func() error {
		client.mutex.Lock()
		retryToken := client.resumptionToken
		retryExpiry := client.resumptionExpiresAt
		client.mutex.Unlock()
		if retryToken == "" || (!retryExpiry.IsZero() && !client.config.Now().Before(retryExpiry)) {
			return collabclient.ErrAuthenticationDenied
		}
		retryInvitation := Invitation{BaseURL: invitation.BaseURL, Origin: invitation.Origin, SessionID: invitation.SessionID, Token: retryToken}
		freshSnapshot, snapshotErr := client.fetchSnapshot(ctx, retryInvitation)
		if snapshotErr != nil {
			return snapshotErr
		}
		snapshot = freshSnapshot
		joinToken = retryToken
		return nil
	}

joinAttempts:
	for {
		attemptTransport := client.newTransport()
		attemptSnapshot := snapshot
		var routeMutex sync.Mutex
		var network *collabclient.NetworkExecutor
		ready := make(chan model.Revision, 1)
		errorsFound := make(chan error, 1)
		snapshotRequired := make(chan struct{}, 1)
		receive := func(envelope protocol.ServerEnvelope) {
			decoded, decodeErr := decodeServerEnvelope(envelope)
			if decodeErr != nil {
				nonBlockingError(errorsFound, decodeErr)
				return
			}
			routeMutex.Lock()
			defer routeMutex.Unlock()
			switch decoded.Envelope.Type {
			case protocol.ServerJoined:
				payload := decoded.Payload.(*protocol.JoinedPayload)
				if payload.DocumentID != attemptSnapshot.DocumentID {
					nonBlockingError(errorsFound, fmt.Errorf("joined document does not match fetched snapshot"))
					return
				}
				network, decodeErr = collabclient.NewNetworkExecutor(attemptTransport, attemptSnapshot, payload.ActorID, invitation.SessionID)
				if decodeErr != nil {
					nonBlockingError(errorsFound, decodeErr)
					return
				}
				client.recordJoined(machine, payload)
			case protocol.ServerOperationAccepted, protocol.ServerOperationRejected:
				if network == nil {
					nonBlockingError(errorsFound, fmt.Errorf("received operation before joined message"))
					return
				}
				if receiveErr := network.Receive(envelope); receiveErr != nil {
					nonBlockingError(errorsFound, receiveErr)
					_ = attemptTransport.Close(websocket.StatusInternalError, "collaboration integrity fault")
					return
				}
				client.recordOperation(machine, network, decoded.Envelope.Type)
			case protocol.ServerReplayComplete:
				if network == nil {
					nonBlockingError(errorsFound, fmt.Errorf("received replay completion before joined message"))
					return
				}
				payload := decoded.Payload.(*protocol.ReplayCompletePayload)
				current, snapshotErr := network.Snapshot(context.Background())
				currentHash, hashErr := current.Hash()
				if snapshotErr != nil || hashErr != nil || current.Revision != payload.Revision || currentHash != payload.MapHash {
					nonBlockingError(errorsFound, fmt.Errorf("replay completion does not match client revision"))
					return
				}
				select {
				case ready <- payload.Revision:
				default:
				}
			case protocol.ServerPresenceSnapshot:
				client.recordPresenceSnapshot(decoded.Payload.(*protocol.PresenceSnapshotPayload))
			case protocol.ServerPresenceUpdate:
				client.recordPresenceUpdate(decoded.Payload.(*protocol.ServerPresenceUpdatePayload))
			case protocol.ServerSessionNotice:
				payload := decoded.Payload.(*protocol.SessionNoticePayload)
				if payload.Code == protocol.NoticeSnapshotRequired {
					select {
					case snapshotRequired <- struct{}{}:
					default:
					}
				}
			}
		}
		if err := attemptTransport.Connect(ctx, protocol.JoinRequest{BaseURL: invitation.BaseURL, Origin: invitation.Origin, Token: joinToken, SessionID: invitation.SessionID, AcknowledgedRevision: attemptSnapshot.Revision}, receive); err != nil {
			client.recordConnectionFailure(machine, err)
			return err
		}
		joinToken = ""
		invitation.Token = ""
		waitContext, cancelWait := context.WithCancel(context.Background())
		transportEnded := make(chan error, 1)
		go func() { transportEnded <- attemptTransport.Wait(waitContext) }()
		fallbackRequired := false
		var joinErr error
		select {
		case synchronizedRevision = <-ready:
		case joinErr = <-errorsFound:
		case <-snapshotRequired:
			fallbackRequired = true
		case transportErr := <-transportEnded:
			synchronizedRevision, fallbackRequired, joinErr = initialJoinOutcomeOnTransportEnd(ready, errorsFound, snapshotRequired, transportErr, transportEnded)
		case <-ctx.Done():
			cancelWait()
			_ = attemptTransport.Close(websocket.StatusGoingAway, "join canceled")
			client.recordConnectionFailure(machine, ctx.Err())
			return ctx.Err()
		}
		if fallbackRequired {
			cancelWait()
			_ = attemptTransport.Close(websocket.StatusGoingAway, "snapshot refresh required")
			if snapshotFallbacks >= maxSnapshotFallbacks {
				joinErr = fmt.Errorf("initial join exhausted %d snapshot refresh attempts", maxSnapshotFallbacks)
				client.recordConnectionFailure(machine, joinErr)
				return joinErr
			}
			snapshotFallbacks++
			if err := refreshSnapshot(); err != nil {
				client.recordConnectionFailure(machine, err)
				return err
			}
			continue
		}
		if joinErr != nil {
			cancelWait()
			_ = attemptTransport.Close(websocket.StatusPolicyViolation, "join failed")
			client.recordConnectionFailure(machine, joinErr)
			return joinErr
		}
		routeMutex.Lock()
		joinedNetwork = network
		routeMutex.Unlock()
		if joinedNetwork == nil {
			cancelWait()
			_ = attemptTransport.Close(websocket.StatusInternalError, "join incomplete")
			joinErr := fmt.Errorf("collaboration join completed without an executor")
			client.recordConnectionFailure(machine, joinErr)
			return joinErr
		}
		transport = attemptTransport
		joinedTransportEnded = transportEnded
		cancelJoinedTransportWait = cancelWait
		break joinAttempts
	}
	client.mutex.Lock()
	client.transport = transport
	client.network = joinedNetwork
	client.hostedSession = invitation.Hosted
	client.joining = false
	if client.role == "owner" {
		client.administrationToken = administrationToken
		client.administrationExpiresAt = invitation.TokenExpiresAt
	}
	client.mutex.Unlock()
	client.recordSynchronized(machine, synchronizedRevision)
	joined = true
	go func() {
		transportErr := <-joinedTransportEnded
		cancelJoinedTransportWait()
		client.monitorTransportResult(machine, transport, joinedNetwork, transportErr)
	}()
	return nil
}

func initialJoinOutcomeOnTransportEnd(ready <-chan model.Revision, errorsFound <-chan error, snapshotRequired <-chan struct{}, transportErr error, transportEnded chan error) (model.Revision, bool, error) {
	select {
	case <-snapshotRequired:
		return 0, true, nil
	default:
	}
	select {
	case joinErr := <-errorsFound:
		if joinErr != nil {
			return 0, false, joinErr
		}
	default:
	}
	select {
	case revision := <-ready:
		// The select above already removed this result from the buffered channel.
		// Replay can complete just as the socket closes, so Join may still succeed;
		// put the result back for the established transport monitor to process.
		transportEnded <- transportErr
		return revision, false, nil
	default:
	}
	if transportErr == nil {
		transportErr = collabclient.ErrTransportNotConnected
	}
	return 0, false, transportErr
}

func (client *SessionClient) Leave(context.Context) error {
	client.mutex.Lock()
	transport := client.transport
	network := client.network
	client.transport = nil
	client.network = nil
	client.joining = false
	machine := client.machine
	client.participants = make(map[model.ActorID]ObservedPresence)
	client.stopPresencePublicationLocked()
	client.presenceInterval = 0
	client.presenceSequence = 0
	client.nextPresenceAt = time.Time{}
	client.resumptionToken = ""
	client.resumptionExpiresAt = time.Time{}
	client.administrationToken = ""
	client.administrationExpiresAt = time.Time{}
	client.baseURL = ""
	client.origin = ""
	client.documentID = ""
	client.actorID = ""
	client.hostedSession = false
	client.reconnecting = false
	if client.cancelReconnect != nil {
		client.cancelReconnect()
		client.cancelReconnect = nil
	}
	client.reconnectContext = nil
	client.mutex.Unlock()
	if machine != nil && machine.State() != collabclient.StateClosed {
		_ = machine.Apply(collabclient.EventClose)
	}
	if network != nil {
		network.Terminate(collabclient.ErrExecutorTerminated)
	}
	if transport == nil {
		return nil
	}
	err := transport.Close(websocket.StatusNormalClosure, "left collaboration session")
	if err == nil || errors.Is(err, net.ErrClosed) || websocket.CloseStatus(err) == websocket.StatusNormalClosure {
		return nil
	}
	return err
}

func (client *SessionClient) HasUnacknowledgedOperations() bool {
	client.mutex.Lock()
	network := client.network
	client.mutex.Unlock()
	return network != nil && network.HasUnacknowledgedOperations()
}

func (client *SessionClient) NetworkExecutor() *collabclient.NetworkExecutor {
	client.mutex.Lock()
	defer client.mutex.Unlock()
	return client.network
}

func (client *SessionClient) CollaborationExecutor() executor.Executor {
	client.mutex.Lock()
	defer client.mutex.Unlock()
	return client.network
}

// CreateInvitation mints a short-lived single-use credential for an active owner session.
func (client *SessionClient) CreateInvitation(ctx context.Context, role InvitationRole, displayName string) (Invitation, error) {
	if role != InvitationRoleEditor && role != InvitationRoleViewer {
		return Invitation{}, fmt.Errorf("collaboration invitation role is invalid")
	}
	if displayName == "" || len(displayName) > 128 {
		return Invitation{}, fmt.Errorf("collaboration invitation display name is invalid")
	}
	client.mutex.Lock()
	hostedSession := client.hostedSession
	baseURL := client.baseURL
	origin := client.origin
	sessionID := client.sessionID
	token := client.administrationToken
	tokenExpiresAt := client.administrationExpiresAt
	machine := client.machine
	active := client.transport != nil && client.role == "owner" && (tokenExpiresAt.IsZero() || client.config.Now().Before(tokenExpiresAt))
	client.mutex.Unlock()
	if hostedSession {
		return client.createHostedInvitation(ctx, role, machine, sessionID)
	}
	if !active || baseURL == "" || origin == "" || sessionID == "" || token == "" {
		return Invitation{}, fmt.Errorf("collaboration invitation is unavailable")
	}
	body, err := json.Marshal(map[string]string{"role": string(role), "display_name": displayName})
	if err != nil {
		return Invitation{}, err
	}
	request, err := client.request(ctx, http.MethodPost, baseURL+"/v1/sessions/"+url.PathEscape(sessionID)+"/join-tokens", token, bytes.NewReader(body))
	if err != nil {
		return Invitation{}, err
	}
	request.Header.Set("Content-Type", "application/json")
	response, err := client.http.Do(request)
	if err != nil {
		return Invitation{}, fmt.Errorf("create collaboration invitation: %w", err)
	}
	defer func() { _ = response.Body.Close() }()
	if response.StatusCode != http.StatusCreated {
		return Invitation{}, fmt.Errorf("create collaboration invitation returned HTTP %d", response.StatusCode)
	}
	var created struct {
		Token     string        `json:"token"`
		ActorID   model.ActorID `json:"actor_id"`
		Role      string        `json:"role"`
		ExpiresAt time.Time     `json:"expires_at"`
	}
	if err := decodeLimited(response.Body, &created); err != nil {
		return Invitation{}, fmt.Errorf("decode collaboration invitation: %w", err)
	}
	if created.Token == "" || created.ActorID == "" || created.Role != string(role) || !client.config.Now().Before(created.ExpiresAt) {
		return Invitation{}, fmt.Errorf("collaboration invitation response is incompatible with the request")
	}
	client.mutex.Lock()
	stillCurrent := client.machine == machine && client.transport != nil && client.sessionID == sessionID && client.administrationToken == token
	client.mutex.Unlock()
	if !stillCurrent {
		return Invitation{}, ErrSessionChanged
	}
	invitation := Invitation{BaseURL: baseURL, Origin: origin, SessionID: sessionID, Token: created.Token, TokenExpiresAt: created.ExpiresAt}
	if err := invitation.validate(); err != nil {
		return Invitation{}, err
	}
	return invitation, nil
}

func (client *SessionClient) RefreshConflict(ctx context.Context, operationID model.OperationID) (model.Snapshot, error) {
	network, _, err := client.conflictExecutor()
	if err != nil {
		return model.Snapshot{}, err
	}
	return network.RefreshConflict(ctx, operationID)
}

func (client *SessionClient) DiscardConflict(ctx context.Context, operationID model.OperationID) (model.Snapshot, error) {
	network, machine, err := client.conflictExecutor()
	if err != nil {
		return model.Snapshot{}, err
	}
	snapshot, err := network.DiscardConflict(ctx, operationID)
	if err != nil {
		return model.Snapshot{}, err
	}
	client.recordConflictResolution(machine, network, snapshot)
	return snapshot, nil
}

func (client *SessionClient) RebuildConflict(ctx context.Context, operationID model.OperationID, complete func(model.AcceptedOperation, error)) error {
	if complete == nil {
		return fmt.Errorf("conflict rebuild completion callback is nil")
	}
	network, machine, err := client.conflictExecutor()
	if err != nil {
		return err
	}
	operation, err := network.BuildConflictRebuild(ctx, operationID)
	if err != nil {
		return err
	}
	return network.ExecuteAsync(ctx, operation, func(accepted model.AcceptedOperation, executeErr error) {
		if executeErr == nil {
			network.DismissConflict(operationID)
			if snapshot, snapshotErr := network.Snapshot(context.Background()); snapshotErr != nil {
				executeErr = snapshotErr
			} else {
				client.recordConflictResolution(machine, network, snapshot)
			}
		}
		complete(accepted, executeErr)
	})
}

func (client *SessionClient) conflictExecutor() (*collabclient.NetworkExecutor, *collabclient.StateMachine, error) {
	client.mutex.Lock()
	defer client.mutex.Unlock()
	if client.network == nil || client.machine == nil {
		return nil, nil, fmt.Errorf("collaboration session is not connected")
	}
	return client.network, client.machine, nil
}

func (client *SessionClient) recordConflictResolution(machine *collabclient.StateMachine, network *collabclient.NetworkExecutor, snapshot model.Snapshot) {
	client.mutex.Lock()
	if client.machine != machine || client.network != network {
		client.mutex.Unlock()
		return
	}
	client.revision = snapshot.Revision
	client.mutex.Unlock()
	if len(network.Conflicts()) == 0 && machine.State() == collabclient.StateConflict {
		_ = machine.Apply(collabclient.EventResolved)
	}
}

func (client *SessionClient) Status() SessionStatus {
	client.mutex.Lock()
	machine := client.machine
	status := SessionStatus{SessionID: client.sessionID, Role: client.role, Revision: client.revision, Err: client.lastErr}
	status.InviteReady = client.transport != nil && client.role == "owner" && client.administrationToken != "" && (client.administrationExpiresAt.IsZero() || client.config.Now().Before(client.administrationExpiresAt))
	if client.administrationToken != "" {
		status.SensitiveValues = append(status.SensitiveValues, client.administrationToken)
	}
	if client.resumptionToken != "" {
		status.SensitiveValues = append(status.SensitiveValues, client.resumptionToken)
		status.ReconnectReady = !client.reconnecting && client.config.Now().Before(client.resumptionExpiresAt)
	}
	status.Participants = make([]protocol.ParticipantPresence, 0, len(client.participants))
	for _, observed := range client.participants {
		status.Participants = append(status.Participants, observed.Presence)
	}
	network := client.network
	client.mutex.Unlock()
	if machine == nil {
		status.State = collabclient.StateDisconnected
	} else {
		status.State = machine.State()
	}
	if network != nil {
		status.Conflicts = network.Conflicts()
	}
	return status
}

func (client *SessionClient) ObservedPresence() []ObservedPresence {
	client.mutex.Lock()
	defer client.mutex.Unlock()
	result := make([]ObservedPresence, 0, len(client.participants))
	for _, observed := range client.participants {
		copy := observed
		copy.Presence = cloneParticipantPresence(observed.Presence)
		result = append(result, copy)
	}
	return result
}

func (client *SessionClient) PublishPresence(ctx context.Context, cursor *model.Coord, selection *protocol.PresenceSelection, status string) error {
	if status == "" || len(status) > protocol.MaxIdentifierBytes {
		return fmt.Errorf("collaboration presence status is invalid")
	}
	if cursor != nil && (cursor.X < 1 || cursor.Y < 1 || cursor.Z < 1) {
		return fmt.Errorf("collaboration presence cursor coordinates must be positive")
	}
	client.mutex.Lock()
	defer client.mutex.Unlock()
	if client.transport == nil || client.sessionID == "" || client.presenceInterval <= 0 {
		return collabclient.ErrTransportNotConnected
	}
	now := client.config.Now()
	payload := clonePresencePayload(protocol.PresenceUpdatePayload{Cursor: cursor, Selection: selection, Status: status})
	if now.Before(client.nextPresenceAt) {
		client.pendingPresence = &payload
		if client.cancelPresence == nil {
			generation := client.presenceGeneration
			client.cancelPresence = client.config.Schedule(client.nextPresenceAt.Sub(now), func() {
				client.flushPendingPresence(generation)
			})
		}
		return nil
	}
	client.clearPendingPresenceLocked()
	return client.sendPresenceLocked(ctx, payload, now)
}

func (client *SessionClient) UpdateDisplayName(ctx context.Context, displayName string) error {
	displayName = strings.TrimSpace(displayName)
	if displayName == "" || len(displayName) > protocol.MaxDisplayNameBytes {
		return fmt.Errorf("collaboration display name is invalid")
	}
	client.mutex.Lock()
	defer client.mutex.Unlock()
	if client.transport == nil || client.sessionID == "" {
		return collabclient.ErrTransportNotConnected
	}
	sequence := client.profileSequence + 1
	payload, err := json.Marshal(protocol.ProfileUpdatePayload{DisplayName: displayName})
	if err != nil {
		return err
	}
	envelope := protocol.ClientEnvelope{ProtocolVersion: model.ProtocolVersion, MessageID: fmt.Sprintf("profile-%d", sequence), SessionID: client.sessionID, Type: protocol.ClientProfileUpdate, Payload: payload}
	if err := client.transport.Send(ctx, envelope); err != nil {
		return err
	}
	client.profileSequence = sequence
	return nil
}

func (client *SessionClient) flushPendingPresence(generation uint64) {
	client.mutex.Lock()
	defer client.mutex.Unlock()
	if generation != client.presenceGeneration {
		return
	}
	client.cancelPresence = nil
	if client.pendingPresence == nil || client.transport == nil || client.presenceInterval <= 0 {
		client.pendingPresence = nil
		return
	}
	now := client.config.Now()
	if now.Before(client.nextPresenceAt) {
		client.cancelPresence = client.config.Schedule(client.nextPresenceAt.Sub(now), func() {
			client.flushPendingPresence(generation)
		})
		return
	}
	payload := clonePresencePayload(*client.pendingPresence)
	client.pendingPresence = nil
	if err := client.sendPresenceLocked(context.Background(), payload, now); err != nil {
		client.lastErr = err
	}
}

func (client *SessionClient) sendPresenceLocked(ctx context.Context, payload protocol.PresenceUpdatePayload, now time.Time) error {
	sequence := client.presenceSequence + 1
	payload.Sequence = sequence
	encoded, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	envelope := protocol.ClientEnvelope{ProtocolVersion: model.ProtocolVersion, MessageID: fmt.Sprintf("presence-%d", sequence), SessionID: client.sessionID, Type: protocol.ClientPresenceUpdate, Payload: encoded}
	if err := client.transport.Send(ctx, envelope); err != nil {
		return err
	}
	client.presenceSequence = sequence
	client.nextPresenceAt = now.Add(client.presenceInterval)
	return nil
}

func (client *SessionClient) clearPendingPresenceLocked() {
	if client.cancelPresence != nil {
		client.cancelPresence()
		client.cancelPresence = nil
	}
	client.pendingPresence = nil
}

func (client *SessionClient) stopPresencePublicationLocked() {
	client.clearPendingPresenceLocked()
	client.presenceGeneration++
}

func clonePresencePayload(payload protocol.PresenceUpdatePayload) protocol.PresenceUpdatePayload {
	if payload.Cursor != nil {
		cursor := *payload.Cursor
		payload.Cursor = &cursor
	}
	if payload.Selection != nil {
		selection := *payload.Selection
		payload.Selection = &selection
	}
	return payload
}

func (client *SessionClient) recordJoined(machine *collabclient.StateMachine, payload *protocol.JoinedPayload) {
	if machine.State() == collabclient.StateConnecting || machine.State() == collabclient.StateReconnecting {
		_ = machine.Apply(collabclient.EventConnected)
	}
	client.mutex.Lock()
	if client.machine == machine {
		client.role = payload.Role
		client.revision = payload.Revision
		client.presenceInterval = time.Duration(payload.PresenceIntervalMS) * time.Millisecond
		client.resumptionToken = payload.ResumptionToken
		client.resumptionExpiresAt = payload.ResumptionTokenExpiresAt
		client.documentID = payload.DocumentID
		client.actorID = payload.ActorID
	}
	client.mutex.Unlock()
}

func (client *SessionClient) recordSynchronized(machine *collabclient.StateMachine, revision model.Revision) {
	if machine.State() == collabclient.StateConnecting {
		_ = machine.Apply(collabclient.EventConnected)
	}
	if machine.State() == collabclient.StateSynchronizing {
		_ = machine.Apply(collabclient.EventSynchronized)
	}
	client.mutex.Lock()
	if client.machine == machine {
		client.revision = revision
	}
	client.mutex.Unlock()
}

func (client *SessionClient) recordOperation(machine *collabclient.StateMachine, network *collabclient.NetworkExecutor, messageType protocol.ServerType) {
	snapshot, err := network.Snapshot(context.Background())
	client.mutex.Lock()
	if client.machine == machine {
		if err != nil {
			client.lastErr = err
		} else {
			client.revision = snapshot.Revision
		}
	}
	client.mutex.Unlock()
	if messageType == protocol.ServerOperationRejected && (machine.State() == collabclient.StateSynchronizing || machine.State() == collabclient.StateCaughtUp) {
		_ = machine.Apply(collabclient.EventConflict)
	}
}

func (client *SessionClient) recordPresenceSnapshot(payload *protocol.PresenceSnapshotPayload) {
	client.mutex.Lock()
	defer client.mutex.Unlock()
	client.participants = make(map[model.ActorID]ObservedPresence, len(payload.Participants))
	for _, participant := range payload.Participants {
		client.participants[participant.ActorID] = ObservedPresence{Presence: cloneParticipantPresence(participant), ObservedAt: client.config.Now()}
	}
}

func (client *SessionClient) recordPresenceUpdate(payload *protocol.ServerPresenceUpdatePayload) {
	client.mutex.Lock()
	defer client.mutex.Unlock()
	participant := protocol.ParticipantPresence{ActorID: payload.ActorID, DisplayName: payload.DisplayName, Sequence: payload.Sequence, Cursor: payload.Cursor, Selection: payload.Selection, Status: payload.Status}
	client.participants[payload.ActorID] = ObservedPresence{Presence: cloneParticipantPresence(participant), ObservedAt: client.config.Now()}
}

func (client *SessionClient) recordConnectionFailure(machine *collabclient.StateMachine, err error) {
	if machine.State() == collabclient.StateClosed {
		return
	}
	switch machine.State() {
	case collabclient.StateConnecting, collabclient.StateSynchronizing, collabclient.StateCaughtUp, collabclient.StateReadOnly, collabclient.StateConflict:
		_ = machine.Apply(collabclient.EventConnectionLost)
	}
	client.mutex.Lock()
	if client.machine == machine {
		client.lastErr = err
		client.participants = make(map[model.ActorID]ObservedPresence)
		client.stopPresencePublicationLocked()
		client.presenceInterval = 0
	}
	client.mutex.Unlock()
}

func (client *SessionClient) monitorTransport(machine *collabclient.StateMachine, transport sessionTransport, network *collabclient.NetworkExecutor) {
	err := transport.Wait(context.Background())
	client.monitorTransportResult(machine, transport, network, err)
}

func (client *SessionClient) monitorTransportResult(machine *collabclient.StateMachine, transport sessionTransport, network *collabclient.NetworkExecutor, err error) {
	client.mutex.Lock()
	current := client.machine == machine && client.transport == transport && client.network == network
	client.mutex.Unlock()
	if !current || machine.State() == collabclient.StateClosed {
		return
	}
	network.Suspend(err)
	client.recordConnectionFailure(machine, err)
	_ = client.startReconnect(machine, network)
}

func (client *SessionClient) RetryReconnect() error {
	client.mutex.Lock()
	machine, network := client.machine, client.network
	if machine == nil || machine.State() != collabclient.StateReconnecting {
		client.mutex.Unlock()
		return fmt.Errorf("collaboration session is not awaiting reconnect")
	}
	if client.reconnecting {
		client.mutex.Unlock()
		return ErrReconnectInProgress
	}
	if client.resumptionToken == "" || !client.config.Now().Before(client.resumptionExpiresAt) {
		client.resumptionToken = ""
		client.resumptionExpiresAt = time.Time{}
		client.mutex.Unlock()
		return collabclient.ErrAuthenticationDenied
	}
	if network == nil || client.reconnectContext == nil {
		client.mutex.Unlock()
		return fmt.Errorf("collaboration reconnect is unavailable")
	}
	client.mutex.Unlock()
	return client.startReconnect(machine, network)
}

func (client *SessionClient) startReconnect(machine *collabclient.StateMachine, network *collabclient.NetworkExecutor) error {
	client.mutex.Lock()
	if client.machine != machine || client.network != network || client.reconnectContext == nil || machine.State() != collabclient.StateReconnecting {
		client.mutex.Unlock()
		return fmt.Errorf("collaboration reconnect is unavailable")
	}
	if client.reconnecting {
		client.mutex.Unlock()
		return ErrReconnectInProgress
	}
	client.reconnecting = true
	ctx := client.reconnectContext
	client.mutex.Unlock()
	go client.runReconnect(ctx, machine, network)
	return nil
}

func (client *SessionClient) runReconnect(ctx context.Context, machine *collabclient.StateMachine, network *collabclient.NetworkExecutor) {
	err := client.config.Reconnect.Reconnect(ctx, client.Status().Revision, func(attemptContext context.Context, _ model.Revision) error {
		current, snapshotErr := network.Snapshot(attemptContext)
		if snapshotErr != nil {
			return snapshotErr
		}
		return client.reconnectAttempt(attemptContext, machine, network, current.Revision)
	})
	client.mutex.Lock()
	if client.machine == machine {
		client.reconnecting = false
		if err != nil && (errors.Is(err, collabclient.ErrAuthenticationDenied) || errors.Is(err, collabclient.ErrIncompatibleProtocol)) {
			client.resumptionToken = ""
			client.resumptionExpiresAt = time.Time{}
		}
		if err != nil {
			client.lastErr = err
		}
	}
	client.mutex.Unlock()
}

func (client *SessionClient) reconnectAttempt(ctx context.Context, machine *collabclient.StateMachine, network *collabclient.NetworkExecutor, acknowledged model.Revision) error {
	client.mutex.Lock()
	if client.machine != machine || client.network != network || machine.State() == collabclient.StateClosed {
		client.mutex.Unlock()
		return context.Canceled
	}
	token := client.resumptionToken
	expiresAt := client.resumptionExpiresAt
	baseURL, origin, sessionID := client.baseURL, client.origin, client.sessionID
	documentID, actorID := client.documentID, client.actorID
	client.mutex.Unlock()
	if token == "" || !client.config.Now().Before(expiresAt) {
		return collabclient.PermanentReconnectError(collabclient.ErrAuthenticationDenied)
	}
	transport := client.newTransport()
	if err := network.Resume(transport); err != nil {
		return err
	}
	ready := make(chan model.Revision, 1)
	errorsFound := make(chan error, 1)
	receive := func(envelope protocol.ServerEnvelope) {
		decoded, err := decodeServerEnvelope(envelope)
		if err != nil {
			nonBlockingError(errorsFound, collabclient.PermanentReconnectError(collabclient.ErrIncompatibleProtocol))
			return
		}
		switch decoded.Envelope.Type {
		case protocol.ServerJoined:
			payload := decoded.Payload.(*protocol.JoinedPayload)
			if payload.DocumentID != documentID || payload.ActorID != actorID {
				nonBlockingError(errorsFound, collabclient.PermanentReconnectError(collabclient.ErrAuthenticationDenied))
				return
			}
			client.recordJoined(machine, payload)
		case protocol.ServerOperationAccepted, protocol.ServerOperationRejected:
			if receiveErr := network.Receive(envelope); receiveErr != nil {
				nonBlockingError(errorsFound, receiveErr)
				_ = transport.Close(websocket.StatusInternalError, "collaboration integrity fault")
				return
			}
			client.recordOperation(machine, network, decoded.Envelope.Type)
		case protocol.ServerReplayComplete:
			payload := decoded.Payload.(*protocol.ReplayCompletePayload)
			current, snapshotErr := network.Snapshot(context.Background())
			currentHash, hashErr := current.Hash()
			if snapshotErr != nil || hashErr != nil || current.Revision != payload.Revision || currentHash != payload.MapHash {
				nonBlockingError(errorsFound, fmt.Errorf("replay completion does not match client revision"))
				return
			}
			select {
			case ready <- payload.Revision:
			default:
			}
		case protocol.ServerPresenceSnapshot:
			client.recordPresenceSnapshot(decoded.Payload.(*protocol.PresenceSnapshotPayload))
		case protocol.ServerPresenceUpdate:
			client.recordPresenceUpdate(decoded.Payload.(*protocol.ServerPresenceUpdatePayload))
		case protocol.ServerSessionNotice:
			payload := decoded.Payload.(*protocol.SessionNoticePayload)
			if payload.Code != protocol.NoticeSnapshotRequired {
				return
			}
			if snapshotErr := client.installReconnectSnapshot(ctx, network); snapshotErr != nil {
				if errors.Is(snapshotErr, collabclient.ErrAuthenticationDenied) {
					snapshotErr = collabclient.PermanentReconnectError(snapshotErr)
				}
				nonBlockingError(errorsFound, snapshotErr)
				return
			}
			nonBlockingError(errorsFound, errSnapshotFallbackInstalled)
		}
	}
	if err := transport.Connect(ctx, protocol.JoinRequest{BaseURL: baseURL, Origin: origin, Token: token, SessionID: sessionID, AcknowledgedRevision: acknowledged}, receive); err != nil {
		network.Suspend(err)
		client.recordConnectionFailure(machine, err)
		return err
	}
	waitContext, cancelWait := context.WithCancel(ctx)
	defer cancelWait()
	transportEnded := make(chan error, 1)
	go func() { transportEnded <- transport.Wait(waitContext) }()
	select {
	case synchronizedRevision := <-ready:
		client.mutex.Lock()
		if client.machine != machine || client.network != network || machine.State() == collabclient.StateClosed {
			client.mutex.Unlock()
			_ = transport.Close(websocket.StatusGoingAway, "session changed")
			network.Suspend(context.Canceled)
			return context.Canceled
		}
		client.transport = transport
		client.lastErr = nil
		client.mutex.Unlock()
		client.recordSynchronized(machine, synchronizedRevision)
		go client.monitorTransport(machine, transport, network)
		return nil
	case reconnectErr := <-errorsFound:
		_ = transport.Close(websocket.StatusPolicyViolation, "reconnect failed")
		network.Suspend(reconnectErr)
		client.recordConnectionFailure(machine, reconnectErr)
		return reconnectErr
	case reconnectErr := <-transportEnded:
		if reconnectErr == nil {
			reconnectErr = collabclient.ErrTransportNotConnected
		}
		network.Suspend(reconnectErr)
		client.recordConnectionFailure(machine, reconnectErr)
		return reconnectErr
	case <-ctx.Done():
		_ = transport.Close(websocket.StatusGoingAway, "reconnect canceled")
		network.Suspend(ctx.Err())
		client.recordConnectionFailure(machine, ctx.Err())
		return ctx.Err()
	}
}

func (client *SessionClient) installReconnectSnapshot(ctx context.Context, network *collabclient.NetworkExecutor) error {
	client.mutex.Lock()
	invitation := Invitation{BaseURL: client.baseURL, Origin: client.origin, SessionID: client.sessionID, Token: client.resumptionToken}
	client.mutex.Unlock()
	snapshot, err := client.fetchSnapshot(ctx, invitation)
	invitation.Token = ""
	if err != nil {
		return err
	}
	if err := network.ReplaceAcknowledgedSnapshot(ctx, snapshot); err != nil {
		return err
	}
	client.mutex.Lock()
	if client.network == network {
		client.revision = snapshot.Revision
	}
	client.mutex.Unlock()
	return nil
}

func cloneParticipantPresence(participant protocol.ParticipantPresence) protocol.ParticipantPresence {
	if participant.Cursor != nil {
		cursor := *participant.Cursor
		participant.Cursor = &cursor
	}
	if participant.Selection != nil {
		selection := *participant.Selection
		participant.Selection = &selection
	}
	return participant
}

func (client *SessionClient) fetchSnapshot(ctx context.Context, invitation Invitation) (model.Snapshot, error) {
	path := strings.TrimRight(invitation.BaseURL, "/") + "/v1/sessions/" + url.PathEscape(invitation.SessionID) + "/snapshot"
	request, err := client.request(ctx, http.MethodGet, path, invitation.Token, nil)
	if err != nil {
		return model.Snapshot{}, err
	}
	response, err := client.http.Do(request)
	if err != nil {
		return model.Snapshot{}, fmt.Errorf("fetch collaboration snapshot: %w", err)
	}
	defer func() { _ = response.Body.Close() }()
	if response.StatusCode != http.StatusOK {
		if response.StatusCode == http.StatusUnauthorized || response.StatusCode == http.StatusForbidden {
			return model.Snapshot{}, fmt.Errorf("%w: snapshot returned HTTP %d", collabclient.ErrAuthenticationDenied, response.StatusCode)
		}
		return model.Snapshot{}, fmt.Errorf("fetch collaboration snapshot returned HTTP %d", response.StatusCode)
	}
	var snapshot model.Snapshot
	if err := decodeLimited(response.Body, &snapshot); err != nil {
		return model.Snapshot{}, fmt.Errorf("decode collaboration snapshot: %w", err)
	}
	if _, err := snapshot.Hash(); err != nil {
		return model.Snapshot{}, err
	}
	return snapshot, nil
}

func (client *SessionClient) request(ctx context.Context, method, rawURL, token string, body io.Reader) (*http.Request, error) {
	parsed, err := url.Parse(rawURL)
	if err != nil || parsed.User != nil || parsed.RawQuery != "" || parsed.Fragment != "" || (parsed.Scheme != "http" && parsed.Scheme != "https") {
		return nil, fmt.Errorf("collaboration URL is invalid")
	}
	if parsed.Scheme == "http" {
		ip := net.ParseIP(parsed.Hostname())
		if ip == nil || !ip.IsLoopback() {
			return nil, fmt.Errorf("cleartext collaboration URL is restricted to loopback IP addresses")
		}
	}
	request, err := http.NewRequestWithContext(ctx, method, parsed.String(), body)
	if err != nil {
		return nil, err
	}
	request.Header.Set("Authorization", "Bearer "+token)
	return request, nil
}

func decodeLimited(reader io.Reader, value any) error {
	decoder := json.NewDecoder(io.LimitReader(reader, maxSessionResponseBytes+1))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(value); err != nil {
		return err
	}
	if err := decoder.Decode(&struct{}{}); err != io.EOF {
		return fmt.Errorf("response contains trailing JSON")
	}
	return nil
}

func decodeServerEnvelope(envelope protocol.ServerEnvelope) (protocol.DecodedServer, error) {
	return protocol.DecodeServerEnvelope(envelope)
}

func nonBlockingError(destination chan<- error, err error) {
	select {
	case destination <- err:
	default:
	}
}
