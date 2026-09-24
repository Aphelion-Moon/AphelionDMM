package server

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	"sdmm/internal/aphelion/collab/model"
	collabtelemetry "sdmm/internal/aphelion/collab/telemetry"
)

type Role string

const (
	RoleViewer Role = "viewer"
	RoleEditor Role = "editor"
	RoleOwner  Role = "owner"
)

var (
	ErrSessionNotFound = errors.New("session not found")
	ErrSessionConflict = errors.New("session already exists")
	ErrNotJoined       = errors.New("principal has not joined the session")
	ErrUnauthorized    = errors.New("principal is not authorized")
)

type Principal struct {
	principalID string
	actorID     model.ActorID
	displayName string
	role        Role
}

func NewPrincipal(principalID string, actorID model.ActorID, displayName string, role Role) (Principal, error) {
	if principalID == "" || len(principalID) > 128 {
		return Principal{}, fmt.Errorf("principal id length is %d, want 1..128", len(principalID))
	}
	if err := actorID.Validate(); err != nil {
		return Principal{}, err
	}
	if displayName == "" || len(displayName) > 128 {
		return Principal{}, fmt.Errorf("display name length is %d, want 1..128", len(displayName))
	}
	if role != RoleViewer && role != RoleEditor && role != RoleOwner {
		return Principal{}, fmt.Errorf("unsupported role %q", role)
	}
	return Principal{principalID: principalID, actorID: actorID, displayName: displayName, role: role}, nil
}

func (principal Principal) ID() string             { return principal.principalID }
func (principal Principal) ActorID() model.ActorID { return principal.actorID }
func (principal Principal) DisplayName() string    { return principal.displayName }
func (principal Principal) Role() Role             { return principal.role }
func (principal Principal) CanEdit() bool {
	return principal.role == RoleEditor || principal.role == RoleOwner
}
func (principal Principal) CanAdminister() bool { return principal.role == RoleOwner }

type hubSession struct {
	owner    *DocumentOwner
	presence *PresenceManager
	members  map[model.ActorID]Principal
}

type Hub struct {
	mutex           sync.RWMutex
	presenceTimeout time.Duration
	sessions        map[string]*hubSession
	telemetry       *collabtelemetry.Telemetry
}

func NewHub(presenceTimeout time.Duration) *Hub {
	return NewHubWithTelemetry(presenceTimeout, nil)
}

func NewHubWithTelemetry(presenceTimeout time.Duration, observability *collabtelemetry.Telemetry) *Hub {
	return &Hub{presenceTimeout: presenceTimeout, sessions: make(map[string]*hubSession), telemetry: observability}
}

func (hub *Hub) Create(sessionID string, owner *DocumentOwner, creator Principal) error {
	if sessionID == "" || len(sessionID) > 128 {
		return fmt.Errorf("session id length is %d, want 1..128", len(sessionID))
	}
	if owner == nil {
		return fmt.Errorf("document owner is nil")
	}
	if !creator.CanAdminister() {
		return ErrUnauthorized
	}
	hub.mutex.Lock()
	defer hub.mutex.Unlock()
	if _, exists := hub.sessions[sessionID]; exists {
		return ErrSessionConflict
	}
	hub.sessions[sessionID] = &hubSession{
		owner:    owner,
		presence: NewPresenceManagerWithTelemetry(hub.presenceTimeout, hub.telemetry),
		members:  map[model.ActorID]Principal{creator.ActorID(): creator},
	}
	return nil
}

func (hub *Hub) Join(sessionID string, principal Principal) error {
	hub.mutex.Lock()
	defer hub.mutex.Unlock()
	session, exists := hub.sessions[sessionID]
	if !exists {
		return ErrSessionNotFound
	}
	if existing, exists := session.members[principal.ActorID()]; exists && existing.ID() != principal.ID() {
		return ErrUnauthorized
	}
	session.members[principal.ActorID()] = principal
	return nil
}

func (hub *Hub) Leave(sessionID string, actorID model.ActorID) error {
	hub.mutex.Lock()
	defer hub.mutex.Unlock()
	session, exists := hub.sessions[sessionID]
	if !exists {
		return ErrSessionNotFound
	}
	if _, exists := session.members[actorID]; !exists {
		return ErrNotJoined
	}
	delete(session.members, actorID)
	session.presence.Remove(actorID)
	return nil
}

func (hub *Hub) UpdateDisplayName(sessionID string, principal Principal, displayName string) error {
	displayName = strings.TrimSpace(displayName)
	hub.mutex.Lock()
	session, exists := hub.sessions[sessionID]
	if !exists {
		hub.mutex.Unlock()
		return ErrSessionNotFound
	}
	member, joined := session.members[principal.ActorID()]
	if !joined || member.ID() != principal.ID() {
		hub.mutex.Unlock()
		return ErrNotJoined
	}
	renamed, err := NewPrincipal(member.ID(), member.ActorID(), displayName, member.Role())
	if err != nil {
		hub.mutex.Unlock()
		return err
	}
	session.members[member.ActorID()] = renamed
	presence := session.presence
	hub.mutex.Unlock()
	presence.Rename(renamed)
	return nil
}

func (hub *Hub) Submit(ctx context.Context, sessionID string, principal Principal, operation model.Operation) (model.AcceptedOperation, error) {
	accepted, _, err := hub.SubmitWithStatus(ctx, sessionID, principal, operation)
	return accepted, err
}

func (hub *Hub) SubmitWithStatus(ctx context.Context, sessionID string, principal Principal, operation model.Operation) (model.AcceptedOperation, bool, error) {
	hub.mutex.RLock()
	session, exists := hub.sessions[sessionID]
	if !exists {
		hub.mutex.RUnlock()
		return model.AcceptedOperation{}, false, ErrSessionNotFound
	}
	member, joined := session.members[principal.ActorID()]
	if !joined || member.ID() != principal.ID() {
		hub.mutex.RUnlock()
		return model.AcceptedOperation{}, false, ErrNotJoined
	}
	if !member.CanEdit() {
		hub.mutex.RUnlock()
		return model.AcceptedOperation{}, false, ErrUnauthorized
	}
	owner := session.owner
	hub.mutex.RUnlock()

	operation.ActorID = member.ActorID()
	accepted, duplicate, err := owner.SubmitWithStatus(ctx, operation)
	if err != nil {
		return model.AcceptedOperation{}, false, err
	}
	return accepted, duplicate, nil
}

func (hub *Hub) Inverse(ctx context.Context, sessionID string, principal Principal, targetID model.OperationID) (model.AcceptedOperation, error) {
	hub.mutex.RLock()
	session, exists := hub.sessions[sessionID]
	if !exists {
		hub.mutex.RUnlock()
		return model.AcceptedOperation{}, ErrSessionNotFound
	}
	member, joined := session.members[principal.ActorID()]
	if !joined || member.ID() != principal.ID() {
		hub.mutex.RUnlock()
		return model.AcceptedOperation{}, ErrNotJoined
	}
	if !member.CanEdit() {
		hub.mutex.RUnlock()
		return model.AcceptedOperation{}, ErrUnauthorized
	}
	owner := session.owner
	hub.mutex.RUnlock()
	inverseID, err := model.NewOperationID()
	if err != nil {
		return model.AcceptedOperation{}, err
	}
	var admit func(int64) (func(), error)
	if codec := bulkCodec(ctx); codec != nil {
		admit = codec.ReserveWorking
	}
	operation, release, err := owner.buildInverseAdmitted(ctx, member.ActorID(), targetID, inverseID, admit)
	if release != nil {
		defer release()
	}
	if err != nil {
		return model.AcceptedOperation{}, err
	}
	accepted, _, err := owner.SubmitWithStatus(ctx, operation)
	if err != nil {
		return model.AcceptedOperation{}, err
	}
	return accepted, nil
}

func (hub *Hub) UpdatePresence(sessionID string, principal Principal, update PresenceUpdate) error {
	hub.mutex.RLock()
	session, exists := hub.sessions[sessionID]
	if !exists {
		hub.mutex.RUnlock()
		return ErrSessionNotFound
	}
	member, joined := session.members[principal.ActorID()]
	presence := session.presence
	hub.mutex.RUnlock()
	if !joined || member.ID() != principal.ID() {
		return ErrNotJoined
	}
	return presence.Update(member, update)
}

func (hub *Hub) DisconnectPresence(sessionID string, actorID model.ActorID) {
	hub.mutex.RLock()
	session, exists := hub.sessions[sessionID]
	hub.mutex.RUnlock()
	if exists {
		session.presence.Remove(actorID)
	}
}

func (hub *Hub) ExpirePresence(now time.Time) int {
	hub.mutex.RLock()
	presenceManagers := make([]*PresenceManager, 0, len(hub.sessions))
	for _, session := range hub.sessions {
		presenceManagers = append(presenceManagers, session.presence)
	}
	hub.mutex.RUnlock()
	removed := 0
	for _, presence := range presenceManagers {
		removed += presence.Expire(now)
	}
	return removed
}

func (hub *Hub) SubscribePresence(sessionID string, buffer int) ([]Presence, <-chan Presence, func(), error) {
	hub.mutex.RLock()
	session, exists := hub.sessions[sessionID]
	hub.mutex.RUnlock()
	if !exists {
		return nil, nil, nil, ErrSessionNotFound
	}
	snapshot, updates, cancel := session.presence.Subscribe(buffer)
	return snapshot, updates, cancel, nil
}

func (hub *Hub) SubscribeDurable(sessionID string, buffer int) (<-chan model.AcceptedOperation, func(), error) {
	if buffer < 1 {
		return nil, nil, fmt.Errorf("durable subscriber buffer must be positive")
	}
	hub.mutex.RLock()
	session, exists := hub.sessions[sessionID]
	hub.mutex.RUnlock()
	if !exists {
		return nil, nil, ErrSessionNotFound
	}
	return session.owner.subscribeDurable(buffer)
}
