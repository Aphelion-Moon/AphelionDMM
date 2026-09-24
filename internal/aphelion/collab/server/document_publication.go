package server

import (
	"sdmm/internal/aphelion/collab/model"
	"sync"
)

func (owner *DocumentOwner) subscribeDurable(buffer int) (<-chan model.AcceptedOperation, func(), error) {
	owner.durableMutex.Lock()
	defer owner.durableMutex.Unlock()
	if owner.durableClosed {
		return nil, nil, ErrDocumentClosed
	}
	if owner.durableSubscribers == nil {
		owner.durableSubscribers = make(map[uint64]chan model.AcceptedOperation)
	}
	owner.nextSubscriberID++
	id := owner.nextSubscriberID
	updates := make(chan model.AcceptedOperation, buffer)
	owner.durableSubscribers[id] = updates
	var once sync.Once
	cancel := func() {
		once.Do(func() {
			owner.durableMutex.Lock()
			defer owner.durableMutex.Unlock()
			if subscriber, exists := owner.durableSubscribers[id]; exists {
				delete(owner.durableSubscribers, id)
				close(subscriber)
			}
		})
	}
	return updates, cancel, nil
}

// Only the authoritative mutation loop publishes. Slow consumers lose their
// subscription instead of delaying the next durable operation.
func (owner *DocumentOwner) publishDurable(accepted model.AcceptedOperation) {
	owner.durableMutex.Lock()
	defer owner.durableMutex.Unlock()
	owner.publishSharedDurable(accepted)
	for id, subscriber := range owner.durableSubscribers {
		select {
		case subscriber <- model.CloneAcceptedOperation(accepted):
		default:
			delete(owner.durableSubscribers, id)
			close(subscriber)
		}
	}
}

func (owner *DocumentOwner) closeDurable() {
	owner.durableMutex.Lock()
	defer owner.durableMutex.Unlock()
	owner.durableClosed = true
	for id, subscriber := range owner.sharedSubscribers {
		close(subscriber)
		delete(owner.sharedSubscribers, id)
	}
	for id, subscriber := range owner.durableSubscribers {
		close(subscriber)
		delete(owner.durableSubscribers, id)
	}
}
