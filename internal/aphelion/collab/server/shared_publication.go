package server

import (
	"fmt"
	"sdmm/internal/aphelion/collab/model"
	"sync"
)

// durableEvent is private to the server's read-only network delivery path. Its
// payload is detached once at publication and never mutated. Public hub callers
// still receive independent mutable copies through SubscribeDurable.
type durableEvent struct{ accepted model.AcceptedOperation }

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
func (owner *DocumentOwner) publishSharedDurable(accepted model.AcceptedOperation) {
	if len(owner.sharedSubscribers) == 0 {
		return
	}
	event := &durableEvent{accepted: model.CloneAcceptedOperation(accepted)}
	for id, subscriber := range owner.sharedSubscribers {
		select {
		case subscriber <- event:
		default:
			delete(owner.sharedSubscribers, id)
			close(subscriber)
		}
	}
}
