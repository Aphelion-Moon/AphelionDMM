package server

import (
	"fmt"
	"sync"

	"sdmm/internal/aphelion/collab/model"
	"sdmm/internal/aphelion/collab/protocol"
)

// durableEvent is private to the server's read-only network delivery path. Its
// payload is detached once at publication and never mutated. Public hub callers
// still receive independent mutable copies through SubscribeDurable.
type durableEvent struct {
	accepted model.AcceptedOperation
	// mapHash is the authoritative hash after accepted, known when it was
	// published; empty means connections must read it from the store.
	mapHash string

	// Every connection of the session sends the same acceptance message, so
	// its JSON encoding is built once and shared.
	encodeOnce sync.Once
	sessionID  string
	encoded    []byte
	encodeErr  error
}

// sharedJSON returns the acceptance message for sessionID, encoded once per
// event. ok is false when sessionID differs from the first caller's.
func (event *durableEvent) sharedJSON(sessionID string, envelope protocol.ServerEnvelope, payload protocol.OperationAcceptedPayload) ([]byte, bool, error) {
	event.encodeOnce.Do(func() {
		event.sessionID = sessionID
		event.encoded, event.encodeErr = marshalServerEnvelope(envelope, payload)
	})
	if event.sessionID != sessionID {
		return nil, false, nil
	}
	return event.encoded, true, event.encodeErr
}

func (owner *DocumentOwner) subscribeSharedDurable(buffer int) (<-chan *durableEvent, func(), error) {
	if buffer < 1 {
		return nil, nil, fmt.Errorf("durable subscriber buffer must be positive")
	}
	owner.durableMutex.Lock()
	defer owner.durableMutex.Unlock()
	if owner.durableClosed {
		return nil, nil, ErrDocumentClosed
	}
	if owner.sharedSubscribers == nil {
		owner.sharedSubscribers = make(map[uint64]chan *durableEvent)
	}
	owner.nextSubscriberID++
	id := owner.nextSubscriberID
	updates := make(chan *durableEvent, buffer)
	owner.sharedSubscribers[id] = updates
	var once sync.Once
	cancel := func() {
		once.Do(func() {
			owner.durableMutex.Lock()
			defer owner.durableMutex.Unlock()
			if subscriber, exists := owner.sharedSubscribers[id]; exists {
				delete(owner.sharedSubscribers, id)
				close(subscriber)
			}
		})
	}
	return updates, cancel, nil
}

// Called under durableMutex by the sole authoritative mutation loop.
func (owner *DocumentOwner) publishSharedDurable(accepted model.AcceptedOperation, mapHash string) {
	if len(owner.sharedSubscribers) == 0 {
		return
	}
	event := &durableEvent{accepted: model.CloneAcceptedOperation(accepted), mapHash: mapHash}
	for id, subscriber := range owner.sharedSubscribers {
		select {
		case subscriber <- event:
		default:
			delete(owner.sharedSubscribers, id)
			close(subscriber)
		}
	}
}
