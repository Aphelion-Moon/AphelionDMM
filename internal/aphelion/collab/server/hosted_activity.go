package server

import (
	"sdmm/internal/aphelion/collab/model"
	"sync"
	"time"
)

type hostedConnection struct {
	sessionID string
	actorID   model.ActorID
	expiresAt time.Time
}

// Each socket owns exactly one registration, independent of cursor presence.
func (service *Service) trackHostedConnection(sessionID string, actorID model.ActorID, expiresAt time.Time) func() {
	service.mutex.Lock()
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
			for _, connection := range service.hostedConnections {
				if connection.sessionID == sessionID && connection.actorID == actorID {
					return
				}
			}
			// Serialize last-socket cleanup with registration of a newer socket.
			service.hub.DisconnectPresence(sessionID, actorID)
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
	if !ok || session.owner == nil {
		return false
	}
	select {
	case <-session.owner.done:
		return false
	default:
		return true
	}
}
