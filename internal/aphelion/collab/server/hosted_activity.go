package server

import (
	"context"
	"errors"
	"sdmm/internal/aphelion/collab/model"
	collabstore "sdmm/internal/aphelion/collab/store"
	"sync"
	"time"
)

const (
	hostedInitialJoinGrace = 5 * time.Minute
	hostedReconnectGrace   = time.Minute
)

type hostedConnection struct {
	sessionID string
	actorID   model.ActorID
	expiresAt time.Time
}

// Each socket owns exactly one registration, independent of cursor presence.
func (service *Service) trackHostedConnection(sessionID string, actorID model.ActorID, expiresAt time.Time) func() {
	service.mutex.Lock()
	if session := service.sessions[sessionID]; session.closing {
		service.mutex.Unlock()
		return nil
	}
	service.nextHostedConnection++
	id := service.nextHostedConnection
	service.hostedConnections[id] = hostedConnection{sessionID, actorID, expiresAt}
	service.mutex.Unlock()
	var once sync.Once
	return func() {
		once.Do(func() {
			service.mutex.Lock()
			defer service.mutex.Unlock()
			delete(service.hostedConnections, id)
			occupied := false
			actorConnected := false
			for _, connection := range service.hostedConnections {
				if connection.sessionID == sessionID {
					occupied = true
					actorConnected = actorConnected || connection.actorID == actorID
				}
			}
			if !occupied {
				session := service.sessions[sessionID]
				if session.hosted {
					session.emptyDeadline = service.config.Now().Add(hostedReconnectGrace)
					service.sessions[sessionID] = session
				}
			}
			// Serialize last-socket cleanup with registration of a newer socket.
			if !actorConnected {
				service.hub.DisconnectPresence(sessionID, actorID)
			}
		})
	}
}

func (service *Service) hostedActivity() map[string]int {
	service.mutex.RLock()
	defer service.mutex.RUnlock()
	actors := make(map[string]map[model.ActorID]bool)
	now := service.config.Now()
	for _, connection := range service.hostedConnections {
		if !now.Before(connection.expiresAt) {
			continue
		}
		if actors[connection.sessionID] == nil {
			actors[connection.sessionID] = make(map[model.ActorID]bool)
		}
		actors[connection.sessionID][connection.actorID] = true
	}
	counts := make(map[string]int, len(actors))
	for session, members := range actors {
		counts[session] = len(members)
	}
	return counts
}

func (service *Service) hostedParticipantCount(id string) int { return service.hostedActivity()[id] }

func (service *Service) hostedDocumentAvailable(id string) bool {
	service.mutex.RLock()
	session, ok := service.sessions[id]
	service.mutex.RUnlock()
	if !ok || session.owner == nil || session.closing {
		return false
	}
	select {
	case <-session.owner.done:
		return false
	default:
		return true
	}
}

func (service *Service) expireHostedSessions(now time.Time) error {
	registry, ok := service.config.HostedRegistry.(collabstore.HostedSessionLifecycleStore)
	if !ok {
		return nil
	}
	service.mutex.Lock()
	occupied := make(map[string]bool)
	for _, connection := range service.hostedConnections {
		occupied[connection.sessionID] = true
	}
	ending := make(map[string]sessionRecord)
	for id, session := range service.sessions {
		if !session.hosted || occupied[id] || session.snapshotTransfers > 0 || now.Before(session.emptyDeadline) {
			continue
		}
		// Fence new sockets before releasing the lock for storage and shutdown.
		session.closing = true
		service.sessions[id] = session
		ending[id] = session
	}
	service.mutex.Unlock()
	var failures error
	for id, session := range ending {
		ctx, cancel := context.WithTimeout(service.context, 10*time.Second)
		err := registry.EndHostedSession(ctx, id)
		if err == nil {
			err = session.owner.Close(ctx)
		}
		cancel()
		if err != nil {
			failures = errors.Join(failures, err)
			continue
		}
		service.mutex.Lock()
		service.hub.endSession(id)
		delete(service.sessions, id)
		service.mutex.Unlock()
	}
	return failures
}

// A member downloading the initial snapshot is joining, but is not yet a
// participant. Fence expiry until the transfer and its WebSocket handoff finish.
func (service *Service) holdHostedSnapshot(id string) (func(), bool) {
	service.mutex.Lock()
	defer service.mutex.Unlock()
	session, exists := service.sessions[id]
	if !exists || session.closing {
		return nil, false
	}
	if !session.hosted {
		return func() {}, true
	}
	session.snapshotTransfers++
	service.sessions[id] = session
	return func() {
		service.mutex.Lock()
		defer service.mutex.Unlock()
		session := service.sessions[id]
		session.snapshotTransfers--
		deadline := service.config.Now().Add(hostedReconnectGrace)
		if session.emptyDeadline.Before(deadline) {
			session.emptyDeadline = deadline
		}
		service.sessions[id] = session
	}, true
}
